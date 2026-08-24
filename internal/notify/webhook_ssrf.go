package notify

// SSRF guardrails for the webhook channel. Webhook targets must never be
// internal addresses, loopback, link-local, cloud instance-metadata
// endpoints, or hosts that resolve to a blocked IP.

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// validateWebhookURL blocks outbound requests to internal addresses to
// prevent Server-Side Request Forgery. It rejects loopback, link-local,
// private (RFC1918), and unicast-mesh (RFC4193) hosts, as well as the
// cloud-instance metadata endpoints (169.254.169.254) and hostnames that
// already resolve to a blocked IP. Hosts that don't resolve yet are allowed
// through validation; the dial-time check in webhookHTTPClient re-verifies
// the resolved IP to defeat DNS rebinding.
func validateWebhookURL(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("webhook: invalid url: %w", err)
	}
	host := u.Hostname()
	if host == "" {
		return errors.New("webhook: url has no host")
	}
	// Reject well-known internal hostnames outright.
	if isBlockedHostname(host) {
		return fmt.Errorf("webhook: host %q is blocked (internal/loopback address)", host)
	}
	// If the host is already a literal IP, validate it directly.
	if ip := net.ParseIP(host); ip != nil {
		if isBlockedIP(ip) {
			return fmt.Errorf("webhook: host %q is blocked (internal/loopback address)", host)
		}
		return nil
	}
	// Resolve the hostname and reject if any A/AAAA record is internal.
	ips, err := net.LookupIP(host)
	if err != nil {
		// Unresolvable now — allow; the dial check catches rebinding later.
		return nil
	}
	for _, ip := range ips {
		if isBlockedIP(ip) {
			return fmt.Errorf("webhook: host %q resolves to blocked address %s", host, ip)
		}
	}
	return nil
}

// isBlockedHostname matches hostnames that should never be a webhook target.
func isBlockedHostname(host string) bool {
	switch strings.ToLower(strings.TrimSuffix(host, ".")) {
	case "localhost", "localhost.localdomain", "ip6-localhost", "ip6-loopback",
		"metadata", "metadata.google.internal":
		return true
	}
	return false
}

// isBlockedIP reports whether an IP is internal/metadata and must not be a
// webhook destination.
func isBlockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
		return true
	}
	// AWS / GCP / Azure / OpenStack instance-metadata endpoint.
	if ip.Equal(net.IPv4(169, 254, 169, 254)) {
		return true
	}
	// IPv6 unicast mesh/local (fc00::/7, fe80::/10) are already covered by
	// IsPrivate/IsLinkLocalUnicast, but keep an explicit guard for clarity.
	return false
}

// webhookDialContext wraps net.Dialer.DialContext and rejects any resolved
// address that is internal/metadata. This is the authoritative SSRF guard:
// even if a hostname passed validation because it was unresolvable, the
// dialer re-checks the IP the resolver actually returns at connect time.
func webhookDialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(host)
	if ip == nil {
		ips, lerr := net.DefaultResolver.LookupIPAddr(ctx, host)
		if lerr != nil {
			return nil, lerr
		}
		if len(ips) == 0 {
			return nil, fmt.Errorf("webhook: no addresses for %s", host)
		}
		// Check ALL resolved IPs — not just the first — to prevent
		// multi-IP bypass where the first is public but others are private.
		for _, resolved := range ips {
			if isBlockedIP(resolved.IP) {
				return nil, fmt.Errorf("webhook: dial to blocked address %s refused (resolved from %s)", resolved.IP, host)
			}
		}
		ip = ips[0].IP
	}
	if isBlockedIP(ip) {
		return nil, fmt.Errorf("webhook: dial to blocked address %s refused", ip)
	}
	d := net.Dialer{Timeout: 10 * time.Second}
	return d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
}

// webhookHTTPClient returns an *http.Client whose transport re-checks the
// resolved destination IP on every dial, defeating DNS-rebinding SSRF.
func webhookHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: webhookDialContext,
		},
	}
}
