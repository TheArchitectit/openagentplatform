package main

import (
	"crypto/tls"
	"crypto/x509"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/relay"
)

// Flags is the relay binary's configuration surface. Every value is supplied via
// a command-line flag or its ALLOWED_ENV_ prefix environment variable (see
// ParseFlags). The relay requires TLS for its WSS listener (spec R.1), so
// CertFile and KeyFile are mandatory.
type Flags struct {
	WSSListenAddr   string        // e.g. ":7000"
	CertFile        string        // TLS cert for WSS
	KeyFile         string        // TLS key for WSS
	AdminAddr       string        // admin listener bind address (loopback by default)
	TrustCAPath     string        // platform CA cert PEM for admin mTLS (RELAY-04)
	TrustConfigPath string        // issued-identity trust config (consumed in RELAY-02)
	MaxConnections  int           // per-tenant connection cap (0 = unlimited)
	IdleTimeout     time.Duration // idle reaping window
	StoreDSN        string        // optional Postgres DSN for durable state (§8.7); empty = in-memory
}

// relayConfig builds the internal relay.RelayConfig from the parsed flags. It is
// additive-only: no existing RelayConfig field is removed or repurposed. The
// TLSConfig is loaded here (fail-closed) so the listener always has a valid
// certificate before ServeWS binds.
//
// Since I.3 (RELAY-02), the WSS TLS config also requires mTLS client
// certificates verified against the platform CA (--trust-ca). This is the same
// CA pool used by the admin listener; a single --trust-ca flag covers both.
func (f *Flags) relayConfig() (relay.RelayConfig, error) {
	if f.CertFile == "" || f.KeyFile == "" {
		return relay.RelayConfig{}, fmt.Errorf("relay: -cert and -key are required for WSS")
	}
	cert, err := tls.LoadX509KeyPair(f.CertFile, f.KeyFile)
	if err != nil {
		return relay.RelayConfig{}, fmt.Errorf("relay: load TLS key pair: %w", err)
	}
	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	// I.3: require mTLS on the WSS data plane (RELAY-02 §1.1). The same --trust-ca
	// flag supplies the CA pool for both WSS and admin listeners.
	if f.TrustCAPath != "" {
		pool, err := loadCAPool(f.TrustCAPath)
		if err != nil {
			return relay.RelayConfig{}, fmt.Errorf("relay: load WSS trust CA: %w", err)
		}
		tlsCfg.ClientCAs = pool
		tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
	}

	return relay.RelayConfig{
		ListenAddr:     f.WSSListenAddr,
		TLSConfig:      tlsCfg,
		MaxConnections: f.MaxConnections,
		IdleTimeout:    f.IdleTimeout,
	}, nil
}

// loadCAPool reads a PEM-encoded CA certificate file and returns an x509 pool.
// Shared by the WSS and admin TLS configs.
func loadCAPool(path string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read CA file: %w", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("CA file contains no valid certificates")
	}
	return pool, nil
}

// adminTLSConfig builds the admin listener's TLS config: the same server
// certificate as WSS, plus mandatory client-certificate verification against
// the platform CA. Fail-closed: a missing or unreadable CA file is an error.
func (f *Flags) adminTLSConfig() (*tls.Config, error) {
	cert, err := tls.LoadX509KeyPair(f.CertFile, f.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("relay: load TLS key pair: %w", err)
	}

	if f.TrustCAPath == "" {
		return nil, fmt.Errorf("relay: -trust-ca is required for the admin listener")
	}
	pool, err := loadCAPool(f.TrustCAPath)
	if err != nil {
		return nil, fmt.Errorf("relay: admin listener trust CA: %w", err)
	}

	return relay.AdminTLSConfig(cert, pool), nil
}

// ParseFlags populates a Flags from command-line flags (and returns the flagset
// so tests can inject custom args). Defaults: listen on :7000, 1000 per-tenant
// connections, 30-minute idle timeout. Cert/key/trust-config are empty by
// default and MUST be supplied for a real run (relayConfig fail-closed).
func ParseFlags(args []string) (*Flags, error) {
	fs := flag.NewFlagSet("relay", flag.ContinueOnError)
	f := &Flags{}
	fs.StringVar(&f.WSSListenAddr, "listen", ":7000", "WSS listen address")
	fs.StringVar(&f.CertFile, "cert", "", "TLS certificate file for WSS (required)")
	fs.StringVar(&f.KeyFile, "key", "", "TLS key file for WSS (required)")
	fs.StringVar(&f.AdminAddr, "admin-addr", "127.0.0.1:9090", "admin listener bind address")
	fs.StringVar(&f.TrustCAPath, "trust-ca", "", "platform CA cert PEM for WSS + admin mTLS (required)")
	fs.StringVar(&f.TrustConfigPath, "trust-config", "", "issued-identity trust config path (RELAY-02)")
	fs.IntVar(&f.MaxConnections, "max-connections", 1000, "per-tenant max concurrent connections (0 = unlimited)")
	fs.DurationVar(&f.IdleTimeout, "idle-timeout", 30*time.Minute, "idle connection reaping window")
	fs.StringVar(&f.StoreDSN, "store-dsn", "", "Postgres DSN for durable connection/metric state (a2a-relay §8); empty = in-memory")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return f, nil
}
