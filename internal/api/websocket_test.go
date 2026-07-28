package api

import (
	"sync"
	"testing"
	"time"
)

// TestWsSendSafeAfterClose verifies that broadcasting to a client that has
// been closed (as readPump's defer does) does not panic on a send to a closed
// channel. Previously the wsClient.mu/closed fields existed but were never
// read or written, so Broadcast's `case c.send <- frame:` could execute
// against a channel that readPump's defer had just closed.
//
// Run with -race to also catch the data race on the closed flag.
func TestWsSendSafeAfterClose(t *testing.T) {
	hub := newWsHub(newDiscardLogger())
	c := &wsClient{
		send:   make(chan []byte, 1),
		subs:   map[wsChannel]struct{}{"alerts": {}},
		mu:     sync.Mutex{},
		closed: false,
		userID: "user-race",
		log:    newDiscardLogger(),
		hub:    hub,
	}
	hub.add(c)

	frame := []byte(`{"type":"event"}`)

	// Simulate readPump's disconnect cleanup: set closed, then close channel.
	closeClient := func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		hub.remove(c)
		close(c.send)
	}

	// Broadcast many frames concurrently with the close. Without sendSafe's
	// closed-check this would panic (send on closed channel) roughly half the
	// time; with -race it would also flag the unsynchronised access.
	var wg sync.WaitGroup
	const broadcasters = 8
	for i := 0; i < broadcasters; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Use sendSafe directly (what Broadcast now calls).
				c.sendSafe(frame, hub.log)
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		closeClient()
	}()

	// If sendSafe ever sends on the closed channel, the goroutine panics and
	// the test process crashes (recovered by the test runner as a failure).
	wg.Wait()
}

// TestWsSendSafeDropsOnFullBuffer verifies a full send buffer does not block
// the broadcaster (the `default:` branch) and is simply dropped.
func TestWsSendSafeDropsOnFullBuffer(t *testing.T) {
	c := &wsClient{
		send:   make(chan []byte, 1),
		mu:     sync.Mutex{},
		closed: false,
		log:    newDiscardLogger(),
	}
	c.send <- []byte("first") // fill the buffer

	// This must not block.
	done := make(chan struct{})
	go func() {
		c.sendSafe([]byte("second"), nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("sendSafe blocked on a full buffer")
	}
}
