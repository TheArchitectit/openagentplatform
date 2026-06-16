// Package checkers implements the various check types the agent can execute.
package checkers

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// Result is the outcome of a check.
type Result struct {
	OK        bool        `json:"ok"`
	Status    string      `json:"status,omitempty"`
	Message   string      `json:"message,omitempty"`
	Value     interface{} `json:"value,omitempty"`
	Duration  int64       `json:"duration_ms"`
	Timestamp int64       `json:"timestamp"`
	Error     string      `json:"error,omitempty"`
}

// CheckRequest is the parameters supplied to a checker.
type CheckRequest struct {
	Type     string                 `json:"type"`
	Target   string                 `json:"target,omitempty"`
	Timeout  int                    `json:"timeout_sec,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
	Script   string                 `json:"script,omitempty"`
	Command  string                 `json:"command,omitempty"`
	Args     []string               `json:"args,omitempty"`
	Expected string                 `json:"expected,omitempty"`
}

// Checker is the interface every check type implements.
type Checker interface {
	Name() string
	Run(ctx context.Context, req *CheckRequest) *Result
}

var (
	regMu      sync.RWMutex
	registry   = map[string]Checker{}
	defaultReg = []Checker{
		&PingChecker{},
		&HTTPChecker{},
		&TCPChecker{},
		&DNSChecker{},
		&CPUChecker{},
		&MemoryChecker{},
		&DiskChecker{},
		&ServiceChecker{},
	}
)

func init() {
	for _, c := range defaultReg {
		registry[strings.ToLower(c.Name())] = c
	}
}

// Register adds a checker under the given type name.
func Register(name string, c Checker) {
	regMu.Lock()
	defer regMu.Unlock()
	registry[strings.ToLower(name)] = c
}

// Get returns a checker for the given type, or an error.
func Get(checkType string) (Checker, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	c, ok := registry[strings.ToLower(checkType)]
	if !ok {
		return nil, fmt.Errorf("unknown check type: %s", checkType)
	}
	return c, nil
}

// Types returns the list of registered check type names.
func Types() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(registry))
	for k := range registry {
		out = append(out, k)
	}
	return out
}

// Run dispatches a check to the appropriate checker. The supplied timeout is
// applied on top of the checker's own internal logic.
func Run(ctx context.Context, req *CheckRequest) *Result {
	start := time.Now()
	if req == nil {
		return &Result{OK: false, Error: "nil request", Duration: time.Since(start).Milliseconds(), Timestamp: time.Now().Unix()}
	}
	checker, err := Get(req.Type)
	if err != nil {
		return &Result{OK: false, Error: err.Error(), Duration: time.Since(start).Milliseconds(), Timestamp: time.Now().Unix()}
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(req.Timeout)*time.Second)
		defer cancel()
	}
	res := checker.Run(ctx, req)
	if res == nil {
		res = &Result{OK: false, Error: "checker returned nil result"}
	}
	if res.Timestamp == 0 {
		res.Timestamp = time.Now().Unix()
	}
	if res.Duration == 0 {
		res.Duration = time.Since(start).Milliseconds()
	}
	return res
}
