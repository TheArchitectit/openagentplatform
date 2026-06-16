package checkers

import (
	"context"
	"net"
	"time"
)

// TCPChecker verifies a TCP endpoint accepts connections.
type TCPChecker struct{}

func (t *TCPChecker) Name() string { return "tcp" }

func (t *TCPChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	if req.Target == "" {
		return &Result{OK: false, Error: "tcp check requires target"}
	}
	timeout := 5 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	d := net.Dialer{Timeout: timeout}
	start := time.Now()
	conn, err := d.DialContext(ctx, "tcp", req.Target)
	if err != nil {
		return &Result{OK: false, Error: err.Error(), Duration: time.Since(start).Milliseconds()}
	}
	_ = conn.Close()
	return &Result{OK: true, Status: "open", Duration: time.Since(start).Milliseconds()}
}
