package mesh

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nats-io/nats.go"
)

// fakeDriver records Apply/Close calls so handler behavior can be asserted
// without a TUN device or a real WireGuard interface.
type fakeDriver struct {
	applied  []*MeshConfig
	closed   int
	applyErr error
}

func (f *fakeDriver) Apply(_ context.Context, cfg *MeshConfig) error {
	f.applied = append(f.applied, cfg)
	return f.applyErr
}
func (f *fakeDriver) Close() error { f.closed++; return nil }
func (f *fakeDriver) Name() string { return "fake" }

func TestNewHandlerDefaultDriver(t *testing.T) {
	h := NewHandler("agent-1", nil, nil, nil)
	if got := h.driver.Name(); got == "" {
		t.Fatal("NewHandler with nil driver did not install a default driver")
	}
}

func TestHandleAppliesValidConfig(t *testing.T) {
	fake := &fakeDriver{}
	h := NewHandler("agent-1", nil, fake, nil)

	cfg := validConfig()
	raw, _ := json.Marshal(cfg)
	h.handle(context.Background(), &nats.Msg{Data: raw})

	if len(fake.applied) != 1 {
		t.Fatalf("driver.Apply called %d times, want 1", len(fake.applied))
	}
	if fake.applied[0].AgentID != "agent-1" {
		t.Fatalf("driver applied wrong agent: %+v", fake.applied[0])
	}
}

func TestHandleRejectsBadPayload(t *testing.T) {
	fake := &fakeDriver{}
	h := NewHandler("agent-1", nil, fake, nil)
	h.handle(context.Background(), &nats.Msg{Data: []byte(`not json`)})
	if len(fake.applied) != 0 {
		t.Fatalf("driver.Apply called on invalid JSON: %d calls", len(fake.applied))
	}
}

func TestHandleRejectsAgentMismatch(t *testing.T) {
	fake := &fakeDriver{}
	h := NewHandler("agent-1", nil, fake, nil)

	cfg := validConfig()
	cfg.AgentID = "agent-2" // payload claims a different subject owner
	raw, _ := json.Marshal(cfg)
	h.handle(context.Background(), &nats.Msg{Data: raw})

	if len(fake.applied) != 0 {
		t.Fatalf("driver.Apply called on cross-agent config: %d calls", len(fake.applied))
	}
}

func TestHandleInvalidConfigNoApply(t *testing.T) {
	fake := &fakeDriver{}
	h := NewHandler("agent-1", nil, fake, nil)

	cfg := validConfig()
	cfg.PrivateKey = "bad-base64!!"
	raw, _ := json.Marshal(cfg)
	h.handle(context.Background(), &nats.Msg{Data: raw})

	if len(fake.applied) != 0 {
		t.Fatalf("driver.Apply called on invalid config: %d calls", len(fake.applied))
	}
}

func TestHandleDriverErrorNoPanic(t *testing.T) {
	fake := &fakeDriver{applyErr: errors.New("tun unavailable")}
	h := NewHandler("agent-1", nil, fake, nil)

	cfg := validConfig()
	raw, _ := json.Marshal(cfg)
	h.handle(context.Background(), &nats.Msg{Data: raw})

	if len(fake.applied) != 1 {
		t.Fatalf("driver.Apply not reached: %d calls", len(fake.applied))
	}
}

func TestCloseCallsDriver(t *testing.T) {
	fake := &fakeDriver{}
	h := NewHandler("agent-1", nil, fake, nil)
	h.Close()
	h.Close() // idempotent
	if fake.closed != 1 {
		t.Fatalf("driver.Close called %d times, want 1 (idempotent)", fake.closed)
	}
}
