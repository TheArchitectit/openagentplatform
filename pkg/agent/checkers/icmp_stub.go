//go:build !unix

package checkers

import (
	"context"
	"time"
)

// PingICMP is a stub on non-Unix platforms. It always fails gracefully so
// the registry lookup still works.
func PingICMP(_ context.Context, _ string, _ time.Duration) (*Result, error) {
	return &Result{OK: false, Error: "icmp ping not supported on this platform"}, nil
}
