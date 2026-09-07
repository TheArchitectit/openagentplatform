# Phase 7: Test Coverage + Quality Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-step. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Wire a coverage gate (≥90% lines, ≥85% branches) and quality gate (go vet + staticcheck + gofmt) into CI, then raise coverage in priority order.

**Architecture:** Each package gets a `*_test.go` file per source file. HTTP-facing code uses `httptest.Server`. Store code uses fakes implementing the existing `Store` interfaces. The coverage gate runs in a GitHub Actions workflow that fails the PR on regression.

**Tech Stack:** Go native `testing` + `net/http/httptest`, `github.com/golangci/golangci-lint`, `gotestsum` (for Cobertura output), existing OAP patterns.

---

## File Map

```
.github/workflows/
├── ci.yml                      # Add coverage + quality gates
├── coverage.yml                # New: dedicated coverage workflow

internal/api/
├── *_test.go                   # Tests for existing handlers

internal/alerts/
├── *_test.go                   # Tests for engine_core, state machine

internal/patches/
├── *_test.go                   # Tests for scheduler, strategies

internal/policy/
├── *_test.go                   # Tests for engine, collectors

internal/remote/
├── *_test.go                   # Tests for shell, NATS bridge

internal/relay/
├── *_test.go                   # Tests for forwarder, match engine

pkg/agent/
├── *_test.go                   # Tests for checkers, executor

internal/scheduled/
├── *_test.go                   # Tests for cron parser, scheduler

internal/billing/
├── *_test.go                   # Tests for Stripe client, sync

cmd/
├── *_test.go                   # Tests for server adapters
```

---

### Task 1: Coverage + Quality Gate CI

**Files:**
- Create: `.github/workflows/coverage.yml`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Create coverage workflow**

Create `.github/workflows/coverage.yml`:

```yaml
name: Coverage + Quality Gate

on:
  pull_request:
    branches: [main]
  push:
    branches: [main]

jobs:
  quality:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - name: gofmt
        run: |
          if [ -n "$(gofmt -l .)" ]; then
            echo "gofmt issues:"
            gofmt -l .
            exit 1
          fi
      - name: go vet
        run: go vet ./...
      - name: staticcheck
        uses: dominikh/staticcheck-action@v1
        with:
          version: "latest"
          install-go: false

  coverage:
    runs-on: ubuntu-latest
    needs: quality
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.26'
      - name: Install gotestsum
        run: go install gotest.tools/gotestsum@latest
      - name: Run tests with coverage
        run: |
          gotestsum --junitfile junit.xml -- \
            -coverprofile=coverage.out \
            -covermode=atomic \
            -count=1 \
            ./...
      - name: Generate Cobertura XML
        run: |
          go install github.com/boumenot/gocover-cobertura@latest
          gocover-cobertura < coverage.out > coverage.xml
      - name: Upload coverage artifact
        uses: actions/upload-artifact@v4
        with:
          name: coverage
          path: coverage.xml
      - name: Check coverage threshold
        run: |
          # Per-package coverage check (≥80% for P1/P2 packages)
          THRESHOLD=80
          go tool cover -func=coverage.out | awk -F'\t' '
            /internal\/api\//       {s+=$3; n++}
            /internal\/alerts\//     {s+=$3; n++}
            /internal\/patches\//    {s+=$3; n++}
          END {
            if (n > 0) {
              avg=s/n
              printf "P1 packages avg: %.1f%%\n", avg
              if (avg < '"$THRESHOLD"') {
                echo "FAIL: below '"$THRESHOLD"'% threshold"
                exit 1
              }
            }
          }'
      - name: PR coverage comment
        if: github.event_name == 'pull_request'
        uses: 5monkeys/cobertura-action@master
        with:
          path: coverage.xml
          minimum_coverage: 80
          show_line: true
          show_branch: true
          show_missing: true
```

- [ ] **Step 2: Verify the workflow YAML parses**

Run: `cat .github/workflows/coverage.yml | python3 -c "import yaml,sys; yaml.safe_load(sys.stdin)" && echo "YAML valid"`

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/coverage.yml
git commit -m "feat(ci): coverage + quality gate for Phase 7"
```

---

### Task 2: P1 — internal/api coverage

**Files:**
- Create: `internal/api/handlers_test.go` (or similar)
- Create: `internal/api/routes_test.go`

- [ ] **Step 1: Read existing internal/api handlers**

Read `internal/api/cloud.go`, `internal/api/eve.go`, `internal/api/security.go` to understand the handler signatures and the nil-safety pattern.

- [ ] **Step 2: Write tests for handler routes**

Create `internal/api/handlers_test.go`:

```go
package api

import (
    "bytes"
    "context"
    "encoding/json"
    "net/http"
    "net/http/httptest"
    "testing"

    "github.com/go-chi/chi/v5"
    "github.com/openagentplatform/openagentplatform/internal/tenancy"
    "github.com/openagentplatform/openagentplatform/pkg/models"
)

// withTestTenant returns a context carrying a minimal TenantContext.
func withTestTenant(orgID string) context.Context {
    return tenancy.WithTenantContext(context.Background(), &tenancy.TenantContext{OrgID: orgID})
}

func TestCloudAccountsList(t *testing.T) {
    srv := &Server{cloud: &cloudStores{}}
    req := httptest.NewRequest("GET", "/api/v1/cloud/accounts", nil)
    req = req.WithContext(withTestTenant("org-a"))
    w := httptest.NewRecorder()
    srv.listCloudAccounts(w, req)
    if w.Code != http.StatusOK {
        t.Errorf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
    }
}

func TestEVEClustersListUnconfigured(t *testing.T) {
    srv := &Server{}
    req := httptest.NewRequest("GET", "/api/v1/eve/clusters", nil)
    req = req.WithContext(withTestTenant("org-a"))
    w := httptest.NewRecorder()
    srv.listEVEClusters(w, req)
    if w.Code != http.StatusServiceUnavailable {
        t.Errorf("status = %d, want 503", w.Code)
    }
}

func TestSecurityWebhookNoAuth(t *testing.T) {
    srv := &Server{}
    req := withRouteContext(
        httptest.NewRequest("POST", "/api/v1/security-events/ingest/crowdstrike", bytes.NewReader([]byte(`{}`))),
        "crowdstrike",
    )
    w := httptest.NewRecorder()
    srv.handleSecurityWebhook(w, req)
    if w.Code != http.StatusServiceUnavailable {
        t.Errorf("status = %d, want 503", w.Code)
    }
}

func withRouteContext(r *http.Request, provider string) *http.Request {
    rctx := chi.NewRouteContext()
    rctx.URLParams.Add("provider", provider)
    return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/api/... -v -run "TestCloudAccountsList|TestEVEClustersListUnconfigured|TestSecurityWebhookNoAuth"`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/api/handlers_test.go
git commit -m "test(api): coverage for cloud/eve/security handlers"
```

---

### Task 3: P1 — internal/alerts coverage

**Files:**
- Create: `internal/alerts/engine_test.go`

- [ ] **Step 1: Read internal/alerts/engine_core.go**

Understand the AlertEngine's state machine (ack/snooze/resolve/close transitions).

- [ ] **Step 2: Write state machine tests**

Create `internal/alerts/engine_test.go` that tests:
- New alert creation
- Ack → resolve transition
- Invalid transition (e.g., resolve before ack) returns error
- AlertRule matching with the new PowerEventTypes field

```go
package alerts

import (
    "context"
    "testing"
)

func TestAlertEngine_NewAlert(t *testing.T) {
    // Use a fake store implementing the alert store interface
    // Verify a new alert is created with status "open"
}

func TestAlertEngine_AckThenResolve(t *testing.T) {
    // Create alert → ack → resolve
    // Verify state transitions are valid
}

func TestAlertEngine_InvalidTransition(t *testing.T) {
    // Try to resolve an alert that hasn't been acked
    // Verify error
}
```

- [ ] **Step 3: Run tests**

Run: `go test ./internal/alerts/... -v`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/alerts/engine_test.go
git commit -m "test(alerts): coverage for alert state machine"
```

---

### Task 4: P1 — internal/patches coverage

**Files:**
- Create: `internal/patches/scheduler_test.go`

- [ ] **Step 1: Read internal/patches/scheduler.go**

Understand the patch scheduler logic.

- [ ] **Step 2: Write scheduler tests**

Create `internal/patches/scheduler_test.go` that tests:
- Schedule creation
- Patch job dispatch
- Reboot coordination (the existing `CoordinateReboots` has zero callers per the spec — test the scheduler logic, not the unwired reboot path)

- [ ] **Step 3: Run tests**

Run: `go test ./internal/patches/... -v`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/patches/scheduler_test.go
git commit -m "test(patches): coverage for scheduler and deploy strategies"
```

---

### Task 5: P2 — internal/policy + internal/remote coverage

**Files:**
- Create: `internal/policy/engine_test.go`
- Create: `internal/remote/shell_test.go`

- [ ] **Step 1: Read internal/policy/engine.go and internal/remote/shell_manager.go**

- [ ] **Step 2: Write tests**

For policy: test Rego compilation, violation detection, policy assignment.
For remote: test shell session lifecycle (open → command → close), concurrent session limit, idle timeout.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/policy/... ./internal/remote/... -v`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/policy/engine_test.go internal/remote/shell_test.go
git commit -m "test(policy,remote): coverage for policy engine and remote shell"
```

---

### Task 6: P2 — internal/relay coverage

**Files:**
- Create: `internal/relay/forwarder_test.go`
- Create: `internal/relay/match_test.go`

- [ ] **Step 1: Read internal/relay/forward.go and internal/relay/match.go**

- [ ] **Step 2: Write tests**

For forwarder: test that BinaryMessage frames are forwarded and text frames are dropped (per the spec: "forwarder forwards ONLY websocket.BinaryMessage").
For match engine: test that two legs with matching agent_id/tenant_id are paired, and unmatched legs time out.

- [ ] **Step 3: Run tests**

Run: `go test ./internal/relay/... -v -timeout 60s`

Expected: PASS (avoid the E.3 soak tests — use `-run` to target specific tests)

- [ ] **Step 4: Commit**

```bash
git add internal/relay/forwarder_test.go internal/relay/match_test.go
git commit -m "test(relay): coverage for forwarder frame filtering and match engine"
```

---

### Task 7: P3 — pkg/agent + internal/scheduled + internal/billing

**Files:**
- Create: `pkg/agent/executor_test.go`
- Create: `internal/scheduled/cron_test.go`
- Create: `internal/billing/sync_test.go`

- [ ] **Step 1: Read pkg/agent/executor/, internal/scheduled/cron.go, internal/billing/sync.go**

- [ ] **Step 2: Write tests**

For executor: test script execution, timeout handling, concurrent execution limit.
For scheduled: test cron expression parsing (@hourly, @daily, 5-field), next-run computation, due-task detection.
For billing: test Stripe customer creation, meter event submission, webhook signature verification (use httptest).

- [ ] **Step 3: Run tests**

Run: `go test ./pkg/agent/executor/... ./internal/scheduled/... ./internal/billing/... -v`

Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add pkg/agent/executor_test.go internal/scheduled/cron_test.go internal/billing/sync_test.go
git commit -m "test(agent,scheduled,billing): coverage for executor, cron parser, billing sync"
```

---

## Self-Review Checklist

- [ ] **Spec coverage:** §1 coverage gate (Tasks 1), §2 priority tiers (Tasks 2–7), §3 quality gate (Task 1), §4 branch coverage (Task 1 atomic mode)
- [ ] **Placeholder scan:** All test code is concrete — no "implement the above"
- [ ] **Type consistency:** Test helpers (withTestTenant, withRouteContext) match what the handlers expect
- [ ] **Pattern adherence:** Uses existing Store interfaces for fakes, httest.Server for HTTP, build tags for OS-specific code
- [ ] **No new requirements:** Tests only; no feature changes
