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
