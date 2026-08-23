// Package monitoring provides in-memory health, alert, and compliance aggregates for monitoring APIs.
package monitoring

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

const (
	HealthHealthy   = "healthy"
	HealthDegraded  = "degraded"
	HealthUnhealthy = "unhealthy"
	HealthUnknown   = "unknown"
)

// HealthCheck identifies a component check used by HealthChecker.
type HealthCheck interface {
	CheckHealth(context.Context) ComponentHealth
}

// HealthCheckFunc adapts a function to HealthCheck.
type HealthCheckFunc func(context.Context) ComponentHealth

func (f HealthCheckFunc) CheckHealth(ctx context.Context) ComponentHealth { return f(ctx) }

// ComponentHealth is the latest health state of an agent, backend, or service.
type ComponentHealth struct {
	Name      string         `json:"name"`
	Kind      string         `json:"kind"`
	Status    string         `json:"status"`
	Message   string         `json:"message,omitempty"`
	CheckedAt time.Time      `json:"checked_at"`
	Details   map[string]any `json:"details,omitempty"`
}

// HealthReport is an aggregate snapshot. The worst component state determines Status.
type HealthReport struct {
	Status     string            `json:"status"`
	CheckedAt  time.Time         `json:"checked_at"`
	Components []ComponentHealth `json:"components"`
	Counts     map[string]int    `json:"counts"`
}

// HealthChecker aggregates independently supplied component checks.
type HealthChecker struct {
	mu     sync.RWMutex
	checks map[string]HealthCheck
	now    func() time.Time
}

func NewHealthChecker() *HealthChecker {
	return &HealthChecker{checks: make(map[string]HealthCheck), now: time.Now}
}

func (h *HealthChecker) Register(name string, check HealthCheck) error {
	if name == "" {
		return errors.New("monitoring: health check name required")
	}
	if check == nil {
		return errors.New("monitoring: nil health check")
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.checks == nil {
		h.checks = make(map[string]HealthCheck)
	}
	h.checks[name] = check
	return nil
}

func (h *HealthChecker) Check(ctx context.Context) HealthReport {
	now := time.Now
	if h.now != nil {
		now = h.now
	}
	h.mu.RLock()
	report := HealthReport{Status: HealthHealthy, CheckedAt: now(), Components: make([]ComponentHealth, 0, len(h.checks)), Counts: make(map[string]int)}
	names := make([]string, 0, len(h.checks))
	for name := range h.checks {
		names = append(names, name)
	}
	h.mu.RUnlock()
	sort.Strings(names)
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, name := range names {
		result := h.checks[name].CheckHealth(ctx)
		if result.Name == "" {
			result.Name = name
		}
		if result.CheckedAt.IsZero() {
			result.CheckedAt = report.CheckedAt
		}
		result.Status = normalizeHealthStatus(result.Status)
		report.Components = append(report.Components, result)
		report.Counts[result.Status]++
		if healthRank(result.Status) > healthRank(report.Status) {
			report.Status = result.Status
		}
	}
	if len(report.Components) == 0 {
		report.Status = HealthUnknown
	}
	return report
}

func normalizeHealthStatus(status string) string {
	switch status {
	case HealthHealthy, HealthDegraded, HealthUnhealthy, HealthUnknown:
		return status
	default:
		return HealthUnknown
	}
}

func healthRank(status string) int {
	switch status {
	case HealthUnhealthy:
		return 3
	case HealthDegraded:
		return 2
	case HealthUnknown:
		return 1
	default:
		return 0
	}
}
