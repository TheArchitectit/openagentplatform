package checkers

import (
	"context"
	"net"
	"time"
)

// DNSChecker resolves a hostname and verifies at least one A/AAAA record.
type DNSChecker struct{}

func (d *DNSChecker) Name() string { return "dns" }

// Metadata describes the DNS checker.
func (d *DNSChecker) Metadata() CheckerMetadata {
	return CheckerMetadata{
		Name:        "dns",
		Version:     "1.0.0",
		Description: "Resolves a hostname using the system resolver and verifies at least one A/AAAA record exists.",
		SupportedPlatforms: []string{
			"linux", "darwin", "freebsd", "netbsd", "openbsd", "windows",
		},
	}
}

func (d *DNSChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	if req.Target == "" {
		return &Result{OK: false, Error: "dns check requires target"}
	}
	r := net.Resolver{PreferGo: true}
	start := time.Now()
	addrs, err := r.LookupHost(ctx, req.Target)
	if err != nil {
		return &Result{OK: false, Error: err.Error(), Duration: time.Since(start).Milliseconds()}
	}
	if len(addrs) == 0 {
		return &Result{OK: false, Error: "no addresses returned", Duration: time.Since(start).Milliseconds()}
	}
	return &Result{
		OK:       true,
		Status:   "resolved",
		Value:    addrs,
		Duration: time.Since(start).Milliseconds(),
	}
}
