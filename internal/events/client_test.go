package events

import "testing"

// --- Client.Close idempotency ---

func TestClient_CloseNil(t *testing.T) {
	// Should not panic.
	var c *Client
	c.Close()
}

func TestClient_CloseIdempotent(t *testing.T) {
	// We can't easily create a real NATS client in tests without a server,
	// but we can test that Close is safe to call on a zero-value Client
	// (nil conn).
	c := &Client{log: nil}
	// Close should handle nil conn gracefully.
	// Note: Close calls c.conn.Drain() when conn != nil, so with conn == nil
	// it should return early after draining subs (which is nil).
	c.Close()
	c.Close() // second call should not panic
}

func TestClient_IsConnected_NilClient(t *testing.T) {
	var c *Client
	if c.IsConnected() {
		t.Error("nil client should report not connected")
	}
}

func TestClient_IsConnected_NilConn(t *testing.T) {
	c := &Client{}
	if c.IsConnected() {
		t.Error("client with nil conn should report not connected")
	}
}

// --- NewHeaderCarrier tests ---

func TestNewHeaderCarrier(t *testing.T) {
	carrier := NewHeaderCarrier(nil)
	carrier.Set("key1", "val1")
	if got := carrier.Get("key1"); got != "val1" {
		t.Errorf("Get = %q, want %q", got, "val1")
	}
	keys := carrier.Keys()
	if len(keys) != 1 || keys[0] != "key1" {
		t.Errorf("Keys = %v, want [key1]", keys)
	}
}

func TestNewHeaderCarrier_NilHeader(t *testing.T) {
	carrier := NewHeaderCarrier(nil)
	// Should still work: creates internal header.
	carrier.Set("foo", "bar")
	if got := carrier.Get("foo"); got != "bar" {
		t.Errorf("Get = %q, want %q", got, "bar")
	}
}
