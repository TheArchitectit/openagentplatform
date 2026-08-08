package api

import (
	"context"
	"errors"
	"fmt"

	"github.com/openagentplatform/openagentplatform/pkg/models"
)

// checkStore is the interface the API uses to read/write check definitions
// and assignments. The default Postgres implementation is pgCheckStore.

type checkStore interface {
	InsertCheck(ctx context.Context, c *models.CheckDefinition) error
	GetCheck(ctx context.Context, orgID, id string) (*models.CheckDefinition, error)
	ListChecks(ctx context.Context, f CheckListFilter) ([]models.CheckDefinition, int, error)
	UpdateCheck(ctx context.Context, orgID, id string, patch CheckPatch) (*models.CheckDefinition, error)
	DeleteCheck(ctx context.Context, orgID, id string) error
	CountAssignments(ctx context.Context, checkID string) (int, error)
	AssignCheck(ctx context.Context, a *models.CheckAssignment) error
	AssignCheckToSite(ctx context.Context, checkID, siteID, assignedBy string) (int, error)
	RemoveAssignment(ctx context.Context, checkID, agentID string) error
	ListAssignments(ctx context.Context, checkID string) ([]models.CheckAssignmentDetail, error)
	GetAssignmentsForAgent(ctx context.Context, agentID string) ([]string, error)
}

// checkStoreFunc returns the active store. Wrapped in a method so tests can
// swap in an in-memory store.
func (s *Server) checkStore() checkStore {
	return &pgCheckStore{pool: s.db}
}

// Valid check types. The config schema is validated against this list.
// validCheckTypes lists the check types the API accepts.
// Note: "custom" is intentionally absent — there is no handler for it
// in pkg/agent/checkers/registry.go. Custom checks should be expressed
// as "script" (with a script body in the config) or as a new dedicated
// checker that is registered in the registry. Re-introduce "custom"
// only once a corresponding checker implementation exists.
var validCheckTypes = map[string]bool{
	"ping":    true,
	"http":    true,
	"tcp":     true,
	"dns":     true,
	"cpu":     true,
	"memory":  true,
	"disk":    true,
	"service": true,
	"script":  true,
}

// validateCheckConfig applies the per-type schema and defaults from the
// task spec. Returns the canonicalised config (with defaults applied) and
// an error explaining the first violation.
func validateCheckConfig(checkType string, raw map[string]any) (map[string]any, error) {
	if !validCheckTypes[checkType] {
		return nil, fmt.Errorf("invalid check_type: %s", checkType)
	}
	if raw == nil {
		raw = map[string]any{}
	}
	switch checkType {
	case "ping":
		host, _ := raw["host"].(string)
		if host == "" {
			return nil, errors.New("ping: host is required")
		}
		count := getIntDefault(raw, "count", 3)
		timeout := getIntDefault(raw, "timeout_ms", 3000)
		return map[string]any{"host": host, "count": count, "timeout_ms": timeout}, nil
	case "http":
		url, _ := raw["url"].(string)
		if url == "" {
			return nil, errors.New("http: url is required")
		}
		method, _ := raw["method"].(string)
		if method == "" {
			method = "GET"
		}
		expStatus := getIntDefault(raw, "expected_status", 200)
		expBody, _ := raw["expected_body"].(string)
		timeout := getIntDefault(raw, "timeout_ms", 5000)
		follow := true
		if v, ok := raw["follow_redirects"]; ok {
			if b, ok := v.(bool); ok {
				follow = b
			}
		}
		out := map[string]any{
			"url": url, "method": method, "expected_status": expStatus,
			"timeout_ms": timeout, "follow_redirects": follow,
		}
		if expBody != "" {
			out["expected_body"] = expBody
		}
		return out, nil
	case "tcp":
		host, _ := raw["host"].(string)
		portF, _ := raw["port"].(float64)
		port := int(portF)
		if host == "" || port == 0 {
			return nil, errors.New("tcp: host and port are required")
		}
		timeout := getIntDefault(raw, "timeout_ms", 5000)
		return map[string]any{"host": host, "port": port, "timeout_ms": timeout}, nil
	case "dns":
		hostname, _ := raw["hostname"].(string)
		if hostname == "" {
			return nil, errors.New("dns: hostname is required")
		}
		out := map[string]any{"hostname": hostname}
		if v, ok := raw["expected_ips"]; ok {
			out["expected_ips"] = v
		}
		if ns, _ := raw["nameserver"].(string); ns != "" {
			out["nameserver"] = ns
		}
		return out, nil
	case "cpu":
		return map[string]any{
			"threshold_percent": getFloatDefault(raw, "threshold_percent", 90),
			"duration_seconds":  getIntDefault(raw, "duration_seconds", 60),
		}, nil
	case "memory":
		return map[string]any{
			"threshold_percent": getFloatDefault(raw, "threshold_percent", 90),
		}, nil
	case "disk":
		path, _ := raw["path"].(string)
		if path == "" {
			path = "/"
		}
		return map[string]any{
			"path":              path,
			"threshold_percent": getFloatDefault(raw, "threshold_percent", 90),
		}, nil
	case "service":
		svc, _ := raw["service_name"].(string)
		if svc == "" {
			return nil, errors.New("service: service_name is required")
		}
		state, _ := raw["expected_state"].(string)
		if state == "" {
			state = "running"
		}
		return map[string]any{"service_name": svc, "expected_state": state}, nil
	case "script":
		rt, _ := raw["runtime"].(string)
		body, _ := raw["script_body"].(string)
		if rt == "" || body == "" {
			return nil, errors.New("script: runtime and script_body are required")
		}
		return map[string]any{
			"runtime":         rt,
			"script_body":     body,
			"timeout_seconds": getIntDefault(raw, "timeout_seconds", 30),
		}, nil
	}
	return nil, fmt.Errorf("unhandled check_type: %s", checkType)
}

func getIntDefault(m map[string]any, key string, def int) int {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return int(n)
		case int:
			return n
		case int64:
			return int(n)
		}
	}
	return def
}

func getFloatDefault(m map[string]any, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return def
}

// handleCreateCheck validates and persists a new check definition.
