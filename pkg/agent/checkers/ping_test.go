package checkers

import (
	"context"
	"testing"
)

// TestPingCheckerSmoke verifies the PingChecker handles its inputs sanely:
// missing target must produce a Result with OK=false, while a well-formed
// request to a non-existent or unreachable host must not panic and must
// return a Result regardless of the underlying PingICMP implementation.
//
// On non-Unix builds PingICMP is a stub that always reports "not supported",
// so the smoke check tolerates either outcome as long as the checker does
// not crash.
func TestPingCheckerSmoke(t *testing.T) {
	p := &PingChecker{}

	t.Run("empty target returns error result", func(t *testing.T) {
		res := p.Run(context.Background(), &CheckRequest{
			Type:   "ping",
			Target: "",
		})
		if res == nil {
			t.Fatal("Run returned nil result for empty target")
		}
		if res.OK {
			t.Error("expected OK=false for empty target")
		}
		if res.Error == "" {
			t.Error("expected non-empty Error for empty target")
		}
	})

	t.Run("unreachable target returns a result without panic", func(t *testing.T) {
		res := p.Run(context.Background(), &CheckRequest{
			Type:    "ping",
			Target:  "127.0.0.1", // loopback; non-Unix stub will report unsupported
			Timeout: 1,
		})
		if res == nil {
			t.Fatal("Run returned nil result")
		}
		// On Unix builds this should normally succeed for loopback; on
		// non-Unix (e.g. windows test runners) PingICMP is stubbed and
		// will return OK=false with an explanatory error. Either is OK
		// — we only assert the checker did not panic.
		_ = res.OK
		_ = res.Error
	})
}

// TestCheckerMetadata verifies that the PingChecker's Metadata is populated
// with the expected name, version and platform list so the registry and
// dashboard can rely on the fields being present.
func TestCheckerMetadata(t *testing.T) {
	p := &PingChecker{}
	md := p.Metadata()

	if md.Name != "ping" {
		t.Errorf("Metadata.Name: got %q, want %q", md.Name, "ping")
	}
	if md.Version == "" {
		t.Error("Metadata.Version should not be empty")
	}
	if md.Description == "" {
		t.Error("Metadata.Description should not be empty")
	}
	if len(md.SupportedPlatforms) == 0 {
		t.Error("Metadata.SupportedPlatforms should not be empty")
	}

	// Spot-check that common platforms are advertised.
	want := []string{"linux", "darwin", "windows"}
	have := make(map[string]struct{}, len(md.SupportedPlatforms))
	for _, p := range md.SupportedPlatforms {
		have[p] = struct{}{}
	}
	for _, w := range want {
		if _, ok := have[w]; !ok {
			t.Errorf("Metadata.SupportedPlatforms missing %q (have %v)", w, md.SupportedPlatforms)
		}
	}

	// And that Name() round-trips.
	if p.Name() != md.Name {
		t.Errorf("Name() = %q, Metadata.Name = %q", p.Name(), md.Name)
	}
}
