package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestConfig_ParseAndValidate verifies that ParseFlags accepts valid flag
// combinations and relayConfig validates TLS cert/key presence.
func TestConfig_ParseAndValidate(t *testing.T) {
	t.Parallel()

	t.Run("valid flags parse", func(t *testing.T) {
		f, err := ParseFlags([]string{"-listen", ":8000", "-max-connections", "500", "-idle-timeout", "15m"})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if f.WSSListenAddr != ":8000" {
			t.Errorf("listen = %q, want :8000", f.WSSListenAddr)
		}
		if f.MaxConnections != 500 {
			t.Errorf("max-connections = %d, want 500", f.MaxConnections)
		}
		if f.IdleTimeout != 15*time.Minute {
			t.Errorf("idle-timeout = %v, want 15m", f.IdleTimeout)
		}
	})

	t.Run("defaults", func(t *testing.T) {
		f, err := ParseFlags([]string{})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if f.WSSListenAddr != ":7000" {
			t.Errorf("default listen = %q, want :7000", f.WSSListenAddr)
		}
		if f.AdminAddr != "127.0.0.1:9090" {
			t.Errorf("default admin-addr = %q, want 127.0.0.1:9090", f.AdminAddr)
		}
		if f.MaxConnections != 1000 {
			t.Errorf("default max-connections = %d, want 1000", f.MaxConnections)
		}
		if f.IdleTimeout != 30*time.Minute {
			t.Errorf("default idle-timeout = %v, want 30m", f.IdleTimeout)
		}
	})

	t.Run("missing cert and key errors", func(t *testing.T) {
		f, err := ParseFlags([]string{})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		_, err = f.relayConfig()
		if err == nil {
			t.Fatal("relayConfig should error when -cert/-key are empty")
		}
	})

	t.Run("invalid cert/key file errors", func(t *testing.T) {
		f, err := ParseFlags([]string{"-cert", "/nonexistent/cert.pem", "-key", "/nonexistent/key.pem"})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		_, err = f.relayConfig()
		if err == nil {
			t.Fatal("relayConfig should error when cert/key files do not exist")
		}
	})

	t.Run("valid cert/key loads", func(t *testing.T) {
		certPath, keyPath := generateTestCert(t)
		f, err := ParseFlags([]string{"-cert", certPath, "-key", keyPath})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		cfg, err := f.relayConfig()
		if err != nil {
			t.Fatalf("relayConfig: %v", err)
		}
		if cfg.TLSConfig == nil {
			t.Fatal("TLSConfig should not be nil")
		}
		if len(cfg.TLSConfig.Certificates) != 1 {
			t.Fatalf("certificates = %d, want 1", len(cfg.TLSConfig.Certificates))
		}
	})

	t.Run("admin TLS requires trust CA", func(t *testing.T) {
		certPath, keyPath := generateTestCert(t)
		f, err := ParseFlags([]string{"-cert", certPath, "-key", keyPath})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if _, err := f.adminTLSConfig(); err == nil {
			t.Fatal("adminTLSConfig should error when -trust-ca is empty")
		}
	})

	t.Run("admin TLS loads CA (fail-closed mTLS)", func(t *testing.T) {
		certPath, keyPath := generateTestCert(t)
		// A self-signed cert is its own CA; reuse it as the trust anchor.
		f, err := ParseFlags([]string{"-cert", certPath, "-key", keyPath, "-trust-ca", certPath})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		cfg, err := f.adminTLSConfig()
		if err != nil {
			t.Fatalf("adminTLSConfig: %v", err)
		}
		if cfg.ClientAuth != tls.RequireAndVerifyClientCert {
			t.Errorf("ClientAuth = %v, want RequireAndVerifyClientCert", cfg.ClientAuth)
		}
		if cfg.ClientCAs == nil {
			t.Error("ClientCAs should not be nil")
		}
	})

	// §8.7: RELAY_STORE_DSN env equivalent, plus the README-documented env
	// fallbacks. Explicit flags win over env. Own function: t.Setenv is
	// incompatible with the parent's t.Parallel.
}

func TestConfig_EnvFallbacks(t *testing.T) {
	t.Run("env fallbacks and flag precedence", func(t *testing.T) {
		t.Setenv("RELAY_LISTEN_ADDR", ":7100")
		t.Setenv("RELAY_MAX_CONNECTIONS", "250")
		t.Setenv("RELAY_IDLE_TIMEOUT", "5m")
		t.Setenv("RELAY_STORE_DSN", "postgres://env-dsn")

		f, err := ParseFlags([]string{})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if f.WSSListenAddr != ":7100" {
			t.Errorf("env listen = %q, want :7100", f.WSSListenAddr)
		}
		if f.MaxConnections != 250 {
			t.Errorf("env max-connections = %d, want 250", f.MaxConnections)
		}
		if f.IdleTimeout != 5*time.Minute {
			t.Errorf("env idle-timeout = %v, want 5m", f.IdleTimeout)
		}
		if f.StoreDSN != "postgres://env-dsn" {
			t.Errorf("env store-dsn = %q, want postgres://env-dsn", f.StoreDSN)
		}

		f, err = ParseFlags([]string{"-listen", ":7000", "-max-connections", "42", "-idle-timeout", "1m", "-store-dsn", "postgres://flag-dsn"})
		if err != nil {
			t.Fatalf("ParseFlags with flags: %v", err)
		}
		if f.WSSListenAddr != ":7000" {
			t.Errorf("flag should win over env: listen = %q", f.WSSListenAddr)
		}
		if f.MaxConnections != 42 {
			t.Errorf("flag should win over env: max-connections = %d, want 42", f.MaxConnections)
		}
		if f.IdleTimeout != time.Minute {
			t.Errorf("flag should win over env: idle-timeout = %v, want 1m", f.IdleTimeout)
		}
		if f.StoreDSN != "postgres://flag-dsn" {
			t.Errorf("flag should win over env: store-dsn = %q, want postgres://flag-dsn", f.StoreDSN)
		}
	})

	t.Run("invalid env values fall back to defaults", func(t *testing.T) {
		t.Setenv("RELAY_MAX_CONNECTIONS", "not-a-number")
		t.Setenv("RELAY_IDLE_TIMEOUT", "garbage")
		f, err := ParseFlags([]string{})
		if err != nil {
			t.Fatalf("ParseFlags: %v", err)
		}
		if f.MaxConnections != 1000 {
			t.Errorf("invalid env max-connections = %d, want default 1000", f.MaxConnections)
		}
		if f.IdleTimeout != 30*time.Minute {
			t.Errorf("invalid env idle-timeout = %v, want default 30m", f.IdleTimeout)
		}
	})
}

// generateTestCert writes a self-signed ECDSA P-256 cert + key to temp files
// and returns their paths. Certs are test-only, never committed.
func generateTestCert(t *testing.T) (certPath, keyPath string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "test-relay"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	dir := t.TempDir()
	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	}), 0644); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{
		Type:  "EC PRIVATE KEY",
		Bytes: keyDER,
	}), 0600); err != nil {
		t.Fatalf("write key: %v", err)
	}
	return certPath, keyPath
}
