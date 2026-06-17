package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/openagentplatform/openagentplatform/internal/auth"
)

// WebSocket upgrader. CheckOrigin is permissive in dev; production
// deployments should restrict this to the configured public URL.
var wsUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		// The session cookie is HTTP-only and SameSite=Lax, so browsers
		// will only send it on same-site or top-level navigations. The
		// auth check below is the real gate; allow the upgrade to
		// proceed so we can reject with 401 if auth fails.
		return true
	},
}

// wsChannel is a named topic clients can subscribe to.
type wsChannel string

const (
	wsChannelAgents  wsChannel = "agents"
	wsChannelChecks  wsChannel = "checks"
	wsChannelAlerts  wsChannel = "alerts"
	wsChannelPatches wsChannel = "patches"
	wsChannelScripts wsChannel = "scripts"
)

// wsMessage is the wire envelope used in both directions.
type wsMessage struct {
	Type    string          `json:"type"`
	Channel wsChannel       `json:"channel,omitempty"`
	Event   string          `json:"event,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Message string          `json:"message,omitempty"`
}

// wsClient represents a single connected browser/agent connection.
type wsClient struct {
	conn     *websocket.Conn
	send     chan []byte
	subs     map[wsChannel]struct{}
	mu       sync.Mutex
	closed   bool
	userID   string
	log      *slog.Logger
	hub      *wsHub
}

// wsHub is the per-process subscription manager. It is shared by all
// wsClient instances and by the API/heartbeat paths that publish events.
type wsHub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
	byChan  map[wsChannel]map[*wsClient]struct{}
	log     *slog.Logger
}

func newWsHub(log *slog.Logger) *wsHub {
	return &wsHub{
		clients: make(map[*wsClient]struct{}),
		byChan:  make(map[wsChannel]map[*wsClient]struct{}),
		log:     log,
	}
}

func (h *wsHub) add(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
	for ch := range c.subs {
		set, ok := h.byChan[ch]
		if !ok {
			set = make(map[*wsClient]struct{})
			h.byChan[ch] = set
		}
		set[c] = struct{}{}
	}
}

func (h *wsHub) remove(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
	for ch := range c.subs {
		if set, ok := h.byChan[ch]; ok {
			delete(set, c)
			if len(set) == 0 {
				delete(h.byChan, ch)
			}
		}
	}
}

func (h *wsHub) subscribe(c *wsClient, ch wsChannel) {
	h.mu.Lock()
	defer h.mu.Unlock()
	c.subs[ch] = struct{}{}
	set, ok := h.byChan[ch]
	if !ok {
		set = make(map[*wsClient]struct{})
		h.byChan[ch] = set
	}
	set[c] = struct{}{}
}

func (h *wsHub) unsubscribe(c *wsClient, ch wsChannel) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(c.subs, ch)
	if set, ok := h.byChan[ch]; ok {
		delete(set, c)
		if len(set) == 0 {
			delete(h.byChan, ch)
		}
	}
}

// Broadcast sends a message to every client subscribed to channel ch.
// Non-blocking: if a client's send buffer is full, the client is
// dropped (its read loop will tear the connection down).
func (h *wsHub) Broadcast(ch wsChannel, event string, data any) {
	payload, err := json.Marshal(data)
	if err != nil {
		h.log.Warn("ws broadcast: marshal failed", "err", err, "channel", ch)
		return
	}
	env := wsMessage{
		Type:    "event",
		Channel: ch,
		Event:   event,
		Data:    payload,
	}
	frame, err := json.Marshal(env)
	if err != nil {
		h.log.Warn("ws broadcast: envelope marshal failed", "err", err)
		return
	}
	h.mu.RLock()
	subs := h.byChan[ch]
	clients := make([]*wsClient, 0, len(subs))
	for c := range subs {
		clients = append(clients, c)
	}
	h.mu.RUnlock()
	for _, c := range clients {
		select {
		case c.send <- frame:
		default:
			h.log.Warn("ws client send buffer full, dropping", "user", c.userID)
		}
	}
}

// ClientCount returns the number of currently connected clients.
func (h *wsHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// clientCount is a package-local alias used by the diagnostics
// handler. Keeping it unexported makes it clear the method is only
// meant for internal observability endpoints.
func (h *wsHub) clientCount() int { return h.ClientCount() }

// subscriptionCount returns the total number of active channel
// subscriptions across all clients. It is a useful indicator of how
// busy the event bus is.
func (h *wsHub) subscriptionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	total := 0
	for _, set := range h.byChan {
		total += len(set)
	}
	return total
}

// Hub returns the server's WebSocket hub, creating it on first use.
// It is safe to call from any goroutine.
func (s *Server) Hub() *wsHub {
	s.wsOnce.Do(func() {
		s.wsHub = newWsHub(s.log)
	})
	return s.wsHub
}

// handleWebSocket upgrades the HTTP connection to a WebSocket,
// authenticates the client via cookie or ?token=, and runs the
// read/write pumps.
func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Auth: cookie or query-param token.
	var claims *auth.SessionClaims
	if sm := s.sessionMinter; sm != nil {
		tok := bearerOrCookie(r, sessionCookieName)
		if tok == "" {
			tok = r.URL.Query().Get("token")
		}
		if tok == "" {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		c, err := sm.Parse(tok)
		if err != nil {
			http.Error(w, `{"error":"invalid_token"}`, http.StatusUnauthorized)
			return
		}
		claims = c
	} else {
		http.Error(w, `{"error":"session_not_configured"}`, http.StatusServiceUnavailable)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.log.Warn("ws upgrade failed", "err", err)
		return
	}

	hub := s.Hub()
	c := &wsClient{
		conn:   conn,
		send:   make(chan []byte, 64),
		subs:   make(map[wsChannel]struct{}),
		userID: claims.Subject,
		log:    s.log,
		hub:    hub,
	}

	hub.add(c)
	s.log.Info("ws client connected", "user", claims.Subject, "clients", hub.ClientCount())

	// Send hello so the client knows the protocol is alive.
	hello, _ := json.Marshal(wsMessage{
		Type: "hello",
		Data: json.RawMessage(`{"protocol":"oap-ws-1"}`),
	})
	select {
	case c.send <- hello:
	default:
	}

	// Start pumps. writePump exits when the send channel closes;
	// readPump exits when the client disconnects or sends a bad frame.
	go c.writePump()
	c.readPump()
}

// readPump consumes frames from the client. The only frame types
// accepted are subscribe/unsubscribe/ping. Anything else is ignored.
// On disconnect the client is removed from the hub.
func (c *wsClient) readPump() {
	defer func() {
		c.hub.remove(c)
		_ = c.conn.Close()
		close(c.send)
		c.log.Info("ws client disconnected", "user", c.userID, "clients", c.hub.ClientCount())
	}()

	c.conn.SetReadLimit(64 * 1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	for {
		_, raw, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err,
				websocket.CloseGoingAway,
				websocket.CloseNormalClosure,
				websocket.CloseAbnormalClosure) {
				c.log.Debug("ws read error", "err", err, "user", c.userID)
			}
			return
		}
		var msg wsMessage
		if err := json.Unmarshal(raw, &msg); err != nil {
			c.log.Debug("ws invalid json", "err", err)
			continue
		}
		switch msg.Type {
		case "ping":
			pong, _ := json.Marshal(wsMessage{Type: "pong"})
			select {
			case c.send <- pong:
			default:
			}
		case "subscribe":
			if !validChannel(msg.Channel) {
				continue
			}
			c.hub.subscribe(c, msg.Channel)
			ack, _ := json.Marshal(wsMessage{Type: "subscribed", Channel: msg.Channel})
			select {
			case c.send <- ack:
			default:
			}
		case "unsubscribe":
			if !validChannel(msg.Channel) {
				continue
			}
			c.hub.unsubscribe(c, msg.Channel)
			ack, _ := json.Marshal(wsMessage{Type: "unsubscribed", Channel: msg.Channel})
			select {
			case c.send <- ack:
			default:
			}
		}
	}
}

// writePump ships messages from the send channel onto the wire and
// emits application-level pings every 30s.
func (c *wsClient) writePump() {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		_ = c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func validChannel(ch wsChannel) bool {
	switch ch {
	case wsChannelAgents, wsChannelChecks, wsChannelAlerts,
		wsChannelPatches, wsChannelScripts:
		return true
	}
	return false
}

// PublishHeartbeat broadcasts a heartbeat event to all clients on
// the "agents" channel. It is exported so that the heartbeat event
// handler (or any other code path that mutates agent state) can
// trigger a real-time update.
func (s *Server) PublishHeartbeat(ctx context.Context, hb any) {
	if s.wsHub == nil {
		return
	}
	s.wsHub.Broadcast(wsChannelAgents, "heartbeat", hb)
}

// PublishCheckResult broadcasts a check-result event to all clients
// on the "checks" channel.
func (s *Server) PublishCheckResult(ctx context.Context, cr any) {
	if s.wsHub == nil {
		return
	}
	s.wsHub.Broadcast(wsChannelChecks, "result", cr)
}

// PublishAlert broadcasts an alert event to all clients on the
// "alerts" channel.
func (s *Server) PublishAlert(ctx context.Context, a any) {
	if s.wsHub == nil {
		return
	}
	s.wsHub.Broadcast(wsChannelAlerts, "alert", a)
}

// PublishPatchEvent broadcasts a patch lifecycle event to all clients
// on the "patches" channel (approved, deployed, rolled back, etc.).
func (s *Server) PublishPatchEvent(ctx context.Context, event string, data any) {
	if s.wsHub == nil {
		return
	}
	s.wsHub.Broadcast(wsChannelPatches, event, data)
}

// PublishScriptEvent broadcasts a script execution event to all clients
// on the "scripts" channel (started, completed, failed, etc.).
func (s *Server) PublishScriptEvent(ctx context.Context, event string, data any) {
	if s.wsHub == nil {
		return
	}
	s.wsHub.Broadcast(wsChannelScripts, event, data)
}
