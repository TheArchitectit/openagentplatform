package main

import (
	"crypto/tls"
	"flag"
	"fmt"
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
	TrustConfigPath string        // issued-identity trust config (consumed in RELAY-02)
	MaxConnections  int           // per-tenant connection cap (0 = unlimited)
	IdleTimeout     time.Duration // idle reaping window
}

// relayConfig builds the internal relay.RelayConfig from the parsed flags. It is
// additive-only: no existing RelayConfig field is removed or repurposed. The
// TLSConfig is loaded here (fail-closed) so the listener always has a valid
// certificate before ServeWS binds.
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
	return relay.RelayConfig{
		ListenAddr:     f.WSSListenAddr,
		TLSConfig:      tlsCfg,
		MaxConnections: f.MaxConnections,
		IdleTimeout:    f.IdleTimeout,
	}, nil
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
	fs.StringVar(&f.TrustConfigPath, "trust-config", "", "issued-identity trust config path (RELAY-02)")
	fs.IntVar(&f.MaxConnections, "max-connections", 1000, "per-tenant max concurrent connections (0 = unlimited)")
	fs.DurationVar(&f.IdleTimeout, "idle-timeout", 30*time.Minute, "idle connection reaping window")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	return f, nil
}
