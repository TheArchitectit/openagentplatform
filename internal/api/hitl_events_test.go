// Package api - R6.5 approval event stream tests. The SSE handler is
// exercised with a live httptest client; the subscriber plumbing with the
// unit-level stream type.
package api

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/a2a/hitl"
)

func newHITLServerWithStream() (*Server, *hitl.ApprovalManager, *ApprovalEventStream) {
	srv, mgr := newHITLServer()
	stream := NewApprovalEventStream()
	WireHITLStream(mgr, stream)
	srv.hitlStream = stream
	return srv, mgr, stream
}

func TestApprovalStreamPublishOnLifecycle(t *testing.T) {
	srv, mgr, _ := newHITLServerWithStream()

	sse := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.handleApprovalEvents(w, r)
	}))
	defer sse.Close()

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, sse.URL, nil)
	resp, err := sse.Client().Do(req)
	if err != nil {
		t.Fatalf("GET stream: %v", err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q", ct)
	}

	scanner := bufio.NewScanner(resp.Body)
	// hello first.
	if !waitForLine(scanner, "event: hello", t) {
		return
	}

	// Drive one lifecycle action after the client connected.
	if _, err := mgr.CreateRequest("ap-1", "secret_access", "agent-9", "high", "task-1", nil); err != nil {
		t.Fatalf("CreateRequest: %v", err)
	}
	// Events arrive asynchronously through the sink goroutine.
	if !waitForLine(scanner, "event: approval", t) {
		return
	}
	if !scanner.Scan() || !strings.Contains(scanner.Text(), `"approval_id":"ap-1"`) || !strings.Contains(scanner.Text(), `"action":"created"`) {
		t.Fatalf("event data = %q, want ap-1/created", scanner.Text())
	}

	// A decision action fans out too.
	if err := mgr.Approve("ap-1", "admin@corp", "ok"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if !waitForLine(scanner, "event: approval", t) {
		return
	}
	scanner.Scan()
	if !strings.Contains(scanner.Text(), `"action":"approved"`) {
		t.Fatalf("decision event data = %q, want approved", scanner.Text())
	}
}

func waitForLine(scanner *bufio.Scanner, want string, t *testing.T) bool {
	t.Helper()
	deadline := time.After(3 * time.Second)
	done := make(chan string, 1)
	go func() {
		for scanner.Scan() {
			if strings.HasPrefix(scanner.Text(), want) {
				done <- scanner.Text()
				return
			}
		}
		done <- ""
	}()
	select {
	case <-done:
		return true
	case <-deadline:
		t.Fatalf("timed out waiting for %q on SSE stream", want)
		return false
	}
}

func TestApprovalStreamNotConfigured(t *testing.T) {
	srv, _ := newHITLServer()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/approvals/events", nil)

	// handleApprovalEvents is guarded by the same 503 posture as the rest.
	srv.handleApprovalEvents(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "hitl_not_configured") {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestApprovalStreamSubscriberCleanup(t *testing.T) {
	stream := NewApprovalEventStream()
	if stream.SubscriberCount() != 0 {
		t.Fatal("want zero subscribers initially")
	}
	_, cancel := stream.Subscribe()
	if stream.SubscriberCount() != 1 {
		t.Fatalf("count = %d, want 1", stream.SubscriberCount())
	}
	cancel()
	if stream.SubscriberCount() != 0 {
		t.Fatalf("count after cancel = %d, want 0", stream.SubscriberCount())
	}
	// Publishing with zero subscribers must not panic or block.
	stream.publish(ApprovalEvent{ApprovalID: "x", Action: "created"})
}
