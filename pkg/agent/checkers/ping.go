package checkers

import (
	"context"
	"time"
)

// PingChecker performs an ICMP ping.
type PingChecker struct{}

func (p *PingChecker) Name() string { return "ping" }

func (p *PingChecker) Run(ctx context.Context, req *CheckRequest) *Result {
	if req.Target == "" {
		return &Result{OK: false, Error: "ping requires target"}
	}
	timeout := 5 * time.Second
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}
	res, err := PingICMP(ctx, req.Target, timeout)
	if err != nil {
		return &Result{OK: false, Error: err.Error()}
	}
	return res
}
