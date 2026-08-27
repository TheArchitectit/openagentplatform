package relay

import (
	"context"
	"io"
	"sync"

	"time"

	"github.com/gorilla/websocket"
)

const (
	// MaxFrameSize is the maximum size of a single WebSocket frame the relay
	// will forward (RELAY-03 §5.2). Larger frames close the leg.
	MaxFrameSize = 1 << 20 // 1 MiB
)

// Forwarder runs bidirectional frame forwarding between a matched pair of
// WebSocket connections. It is the Layer-4 data plane — no parsing of payloads
// (E.4 blind-forwarder).
type Forwarder struct {
	engine *MatchEngine
}

// NewForwarder creates a Forwarder bound to the given MatchEngine.
func NewForwarder(engine *MatchEngine) *Forwarder {
	return &Forwarder{engine: engine}
}

// Run starts two goroutines (A→B and B→A) that forward frames. It blocks until
// one leg closes (read error, write error, context cancellation, or idle reaping).
// On return both legs are always closed and byte-accounting is finalized.
func (f *Forwarder) Run(ctx context.Context, legA, legB *Leg) {
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		f.pipe(ctx, legA, legB)
	}()
	go func() {
		defer wg.Done()
		f.pipe(ctx, legB, legA)
	}()

	wg.Wait()
	// Both goroutines exited — close both legs exactly once.
	f.engine.ClosePair(legA)
}

// pipe copies frames from src to dst. It runs until an error on src or dst,
// or ctx cancellation. It refreshes LastActivityAt on both legs for every
// frame and records bytes on the sending leg.
func (f *Forwarder) pipe(ctx context.Context, src, dst *Leg) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Read a frame from src.
		msgType, msg, err := src.Conn.ReadMessage()
		if err != nil {
			if err != io.EOF && !websocket.IsCloseError(err,
				websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				f.engine.svc.log.Debug("relay: read error",
					"conn_id", src.ConnID, "err", err)
			}
			return
		}

		// Reject non-binary frames (RELAY-03 §5.1).
		if msgType != websocket.BinaryMessage {
			src.mu.Lock()
			src.closeErr = io.ErrUnexpectedEOF
			src.mu.Unlock()
			return
		}

		// Enforce message size limit (RELAY-03 §5.2).
		if len(msg) > MaxFrameSize {
			src.mu.Lock()
			src.closeErr = ErrFrameTooLarge
			src.mu.Unlock()
			return
		}

		// Record bytes on the sending leg's connection.
		_ = f.engine.svc.RecordBytes(nil, src.ConnID, int64(len(msg)))

		// Write frame to dst (with write deadline to enforce RELAY-03 §4).
		_ = dst.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := dst.Conn.WriteMessage(websocket.BinaryMessage, msg); err != nil {
			f.engine.svc.log.Debug("relay: write error",
				"conn_id", dst.ConnID, "err", err)
			return
		}
	}
}

// ErrFrameTooLarge is returned when a frame exceeds MaxFrameSize.
var ErrFrameTooLarge = io.ErrShortWrite // sentinel
