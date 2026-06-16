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

// CheckerMetadata describes a registered checker.
type CheckerMetadata struct {
	Name              string   `json:"name"`
	Version           string   `json:"version"`
	Description       string   `json:"description"`
	SupportedPlatforms []string `json:"supported_platforms"`
}

// MetaChecker is an optional extension: checkers that implement it supply
// additional metadata used for --list-checkers and platform filtering.
type MetaChecker interface {
	Checker
	Metadata() CheckerMetadata
}

var (
	regMu      sync.RWMutex
	registry   = map[string]Checker{}
	metadata   = map[string]CheckerMetadata{}
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
	// Default timeout applied when neither req.Timeout nor wrap-timeout is set.
	defaultWrapTimeout = 30 * time.Second
)

func init() {
	for _, c := range defaultReg {
		var meta CheckerMetadata
		if m, ok := c.(MetaChecker); ok {
			meta = m.Metadata()
		} else {
			meta = autoMetadata(c)
		}
		registerInternal(c.Name(), c, meta)
	}
}

// autoMetadata derives a CheckerMetadata for checkers that do not implement
// MetaChecker. The list of supported platforms defaults to "any".
func autoMetadata(c Checker) CheckerMetadata {
	name := c.Name()
	platforms := []string{"any"}
	return CheckerMetadata{
		Name:               name,
		Version:            "1.0.0",
		Description:        fmt.Sprintf("%s checker", name),
		SupportedPlatforms: platforms,
	}
}

func registerInternal(name string, c Checker, meta CheckerMetadata) {
	regMu.Lock()
	defer regMu.Unlock()
	key := strings.ToLower(name)
	registry[key] = c
	metadata[key] = meta
}

// Register adds a checker under the given type name. If the checker
// implements MetaChecker, the supplied metadata is used; otherwise a
// default is generated.
func Register(name string, c Checker) {
	var meta CheckerMetadata
	if m, ok := c.(MetaChecker); ok {
		meta = m.Metadata()
	} else {
		meta = autoMetadata(c)
	}
	registerInternal(name, c, meta)
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

// GetMetadata returns the metadata for a registered checker.
func GetMetadata(checkType string) (CheckerMetadata, error) {
	regMu.RLock()
	defer regMu.RUnlock()
	m, ok := metadata[strings.ToLower(checkType)]
	if !ok {
		return CheckerMetadata{}, fmt.Errorf("unknown check type: %s", checkType)
	}
	return m, nil
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

// AllMetadata returns metadata for every registered checker, sorted by name.
func AllMetadata() []CheckerMetadata {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]CheckerMetadata, 0, len(metadata))
	for _, m := range metadata {
		out = append(out, m)
	}
	return out
}

// SetDefaultWrapTimeout overrides the timeout applied by Run when neither
// the request nor the caller supplies one.
func SetDefaultWrapTimeout(d time.Duration) {
	if d <= 0 {
		return
	}
	defaultWrapTimeout = d
}

// Run dispatches a check to the appropriate checker. The supplied timeout is
// applied on top of the checker's own internal logic; if the checker exceeds
// it the returned Result is marked failed with a timeout error.
func Run(ctx context.Context, req *CheckRequest) *Result {
	start := time.Now()
	if req == nil {
		return &Result{OK: false, Error: "nil request", Duration: time.Since(start).Milliseconds(), Timestamp: time.Now().Unix()}
	}
	checker, err := Get(req.Type)
	if err != nil {
		return &Result{OK: false, Error: err.Error(), Duration: time.Since(start).Milliseconds(), Timestamp: time.Now().Unix()}
	}

	timeout := defaultWrapTimeout
	if req.Timeout > 0 {
		timeout = time.Duration(req.Timeout) * time.Second
	}

	res := runWithTimeout(ctx, checker, req, timeout)
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

// runWithTimeout enforces a hard ceiling on the checker, even if the
// checker ignores its own ctx. A separate timer goroutine kills the
// bookkeeping (we can't actually kill a misbehaving checker goroutine,
// but we return a timeout result the instant the deadline expires).
func runWithTimeout(parent context.Context, checker Checker, req *CheckRequest, timeout time.Duration) *Result {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()

	type res struct {
		r *Result
	}
	ch := make(chan res, 1)
	go func() {
		ch <- res{r: checker.Run(ctx, req)}
	}()

	start := time.Now()
	select {
	case r := <-ch:
		return r.r
	case <-ctx.Done():
		if r := <-ch; r.r != nil {
			// Return whatever the checker produced, but mark it failed.
			if r.r.OK {
				r.r.OK = false
			}
			if r.r.Error == "" {
				r.r.Error = "check timed out"
			}
			r.r.Duration = time.Since(start).Milliseconds()
			return r.r
		}
		return &Result{
			OK:       false,
			Error:    "check timed out",
			Duration: time.Since(start).Milliseconds(),
		}
	}
}
