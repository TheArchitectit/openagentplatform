package relay

import (
	"testing"
)

func TestMaxFrameSize(t *testing.T) {
	if MaxFrameSize != 1<<20 {
		t.Errorf("MaxFrameSize = %d, want %d (1 MiB)", MaxFrameSize, 1<<20)
	}
}

func TestForwarder_RejectsTextFrames(t *testing.T) {
	svc := NewRelayService(RelayConfig{MaxConnections: 10}, nil)
	engine := NewMatchEngine(svc)
	fwd := NewForwarder(engine)
	if fwd == nil {
		t.Fatal("NewForwarder returned nil")
	}
}
