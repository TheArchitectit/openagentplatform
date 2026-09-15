package executor

import (
	"strings"
	"testing"
)

// TestCappedBufferKeepsMostRecentBytes verifies the overflow path: the
// buffer never exceeds MaxOutputBytes and always retains the most recent
// writes. The previous implementation could grow past the cap and corrupt
// its contents via overlapping self-copies.
func TestCappedBufferKeepsMostRecentBytes(t *testing.T) {
	b := &cappedBuffer{}
	var total int
	chunk := strings.Repeat("a", 1024)
	for i := 0; i < MaxOutputBytes/512; i++ { // ~2x the cap in total
		if _, err := b.Write([]byte(chunk)); err != nil {
			t.Fatalf("write: %v", err)
		}
		total += len(chunk)
	}
	if got := len(b.buf); got != MaxOutputBytes {
		t.Fatalf("buffer length = %d, want exactly %d", got, MaxOutputBytes)
	}
	out := b.String()
	if len(out) != MaxOutputBytes {
		t.Fatalf("String length = %d, want %d", len(out), MaxOutputBytes)
	}
	if !strings.HasSuffix(out, chunk) {
		t.Error("most recent write not retained at the tail")
	}
	// The content must be contiguous 'a's — the old overlapping-copy path
	// produced corrupted mixtures.
	if strings.Count(out, "a") != MaxOutputBytes {
		t.Error("buffer contents corrupted")
	}
}

// TestCappedBufferExactFit verifies the boundary between grow and cap.
func TestCappedBufferExactFit(t *testing.T) {
	b := &cappedBuffer{}
	if _, err := b.Write(make([]byte, MaxOutputBytes)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if len(b.buf) != MaxOutputBytes {
		t.Fatalf("len = %d", len(b.buf))
	}
	// One more byte over the cap.
	over := strings.Repeat("z", 1024)
	if _, err := b.Write([]byte(over)); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := b.String(); got[MaxOutputBytes-1024:] != over {
		t.Error("overflow write not retained")
	}
}

// TestCmdExecutorRegistered verifies the "cmd" runtime (previously
// normalised but never registered) resolves to an executor.
func TestCmdExecutorRegistered(t *testing.T) {
	reg := Default()
	if e := reg.Get(RuntimeCmd); e == nil {
		t.Fatal("no executor registered for the cmd runtime")
	}
}
