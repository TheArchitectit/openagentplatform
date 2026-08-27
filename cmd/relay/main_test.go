package main

import (
	"testing"
)

// TestRelayBinary_Smoke_StartStop is a build-time smoke test. It verifies the
// relay binary compiles and that ParseFlags + relayConfig produce a valid
// configuration. A full start/stop lifecycle test against a live WSS listener
// is covered by internal/relay/ws_test.go.
func TestRelayBinary_Smoke_StartStop(t *testing.T) {
	t.Parallel()

	t.Run("empty flags reject missing cert/key", func(t *testing.T) {
		f, err := ParseFlags([]string{})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if _, err := f.relayConfig(); err == nil {
			t.Fatal("expected error for missing cert/key")
		}
	})

	t.Run("valid cert produces config", func(t *testing.T) {
		certPath, keyPath := generateTestCert(t)
		f, err := ParseFlags([]string{"-cert", certPath, "-key", keyPath, "-listen", ":0"})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		cfg, err := f.relayConfig()
		if err != nil {
			t.Fatalf("relayConfig: %v", err)
		}
		if cfg.ListenAddr != ":0" {
			t.Errorf("listen = %q, want :0", cfg.ListenAddr)
		}
		if cfg.TLSConfig == nil {
			t.Fatal("TLSConfig should not be nil")
		}
	})
}
