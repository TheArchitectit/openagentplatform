// Package api - hitl_events.go implements the hitl-approval spec R6.5
// real-time channel: every approval lifecycle action the engine appends to
// its audit log is fanned out to live SSE subscribers, so the approval
// queue updates without polling. The stream is fed through the manager's
// additive audit sink (same seam R4.4 uses), which keeps the engine
// transport-agnostic.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"

	"github.com/openagentplatform/openagentplatform/a2a/hitl"
)

// ApprovalEvent is the JSON payload of one SSE message on the approval
// stream. It mirrors a hitl.AuditEntry; clients refetch the approval (or
// list) on receipt and treat the event as a change signal.
type ApprovalEvent struct {
	ApprovalID string            `json:"approval_id"`
	Action     string            `json:"action"` // created, approved, rejected, escalated, expired, notified, ...
	Actor      string            `json:"actor,omitempty"`
	Reason     string            `json:"reason,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Timestamp  string            `json:"timestamp"`
}

// streamSubscriber is one connected SSE client. The buffered channel plus
// non-blocking send means a slow consumer is dropped rather than stalling
// the audit sink goroutine.
type streamSubscriber struct {
	ch chan ApprovalEvent
}

// ApprovalEventStream fans lifecycle events to connected SSE clients.
// Safe for concurrent use.
type ApprovalEventStream struct {
	mu   sync.RWMutex
	subs map[*streamSubscriber]struct{}
}

// NewApprovalEventStream creates an empty stream.
func NewApprovalEventStream() *ApprovalEventStream {
	return &ApprovalEventStream{subs: map[*streamSubscriber]struct{}{}}
}

// WireHITLStream attaches the stream to an approval manager as an audit
// sink (additive — the R4 audit writer keeps its own sink). Returns the
// stream for route registration.
func WireHITLStream(m *hitl.ApprovalManager, stream *ApprovalEventStream) {
	if m == nil || stream == nil {
		return
	}
	m.AddAuditSink(func(e hitl.AuditEntry) {
		stream.publish(ApprovalEvent{
			ApprovalID: e.ApprovalID,
			Action:     e.Action,
			Actor:      e.Actor,
			Reason:     e.Reason,
			Metadata:   e.Metadata,
			Timestamp:  e.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00"),
		})
	})
}

func (s *ApprovalEventStream) publish(ev ApprovalEvent) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for sub := range s.subs {
		select {
		case sub.ch <- ev:
		default:
			// Buffer full: drop the event for this subscriber rather
			// than block the sink.
		}
	}
}

// Subscribe registers a subscriber and returns its channel plus a cancel
// func that must be called when the client disconnects.
func (s *ApprovalEventStream) Subscribe() (<-chan ApprovalEvent, func()) {
	sub := &streamSubscriber{ch: make(chan ApprovalEvent, 32)}
	s.mu.Lock()
	s.subs[sub] = struct{}{}
	s.mu.Unlock()
	return sub.ch, func() {
		s.mu.Lock()
		delete(s.subs, sub)
		s.mu.Unlock()
		close(sub.ch)
	}
}

// SubscriberCount returns the number of live subscribers (for tests and
// observability).
func (s *ApprovalEventStream) SubscriberCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.subs)
}

// handleApprovalEvents serves GET /a2a/approvals/events as an SSE stream
// (spec R6.5). One "event: approval" message per lifecycle action, plus a
// periodic "event: ping" keep-alive comment. Requires the stream wiring;
// without it the endpoint answers 503 like the rest of the HITL surface.
func (s *Server) handleApprovalEvents(w http.ResponseWriter, r *http.Request) {
	if s.hitlStream == nil {
		hitlNotConfigured(w)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeRESTJSON(w, http.StatusNotImplemented, map[string]string{"error": "streaming_unsupported"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events, cancel := s.hitlStream.Subscribe()
	defer cancel()

	// Initial hello so the browser's EventSource fires onopen promptly.
	_, _ = fmt.Fprint(w, "event: hello\ndata: {}\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-events:
			if !ok {
				return
			}
			payload, err := json.Marshal(ev)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "event: approval\ndata: %s\n\n", payload); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
