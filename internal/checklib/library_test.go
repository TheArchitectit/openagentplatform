package checklib

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/openagentplatform/openagentplatform/internal/auth"
	"github.com/openagentplatform/openagentplatform/pkg/agent/checkers"
	"github.com/pashagolub/pgxmock/v4"
)

// wantCatalog is the canonical 9-type set from rmm-core §3.2 / check-library
// §2.1. Both the contents and the order are pinned by the spec.
var wantCatalog = []struct {
	id        string
	name      string
	checkType string
	category  string
	interval  int
	timeout   int
}{
	{"builtin-ping", "Ping", "ping", "network", 60, 10},
	{"builtin-cpu", "CPU Usage", "cpu", "system", 60, 15},
	{"builtin-memory", "Memory Usage", "memory", "system", 60, 10},
	{"builtin-disk", "Disk Usage", "disk", "system", 300, 10},
	{"builtin-service", "Service Status", "service", "system", 60, 10},
	{"builtin-http", "HTTP Endpoint", "http", "network", 60, 15},
	{"builtin-tcp", "TCP Port", "tcp", "network", 60, 10},
	{"builtin-dns", "DNS Resolution", "dns", "network", 60, 10},
	{"builtin-script", "Script", "script", "system", 300, 60},
}

// TestBuiltInChecksCatalog verifies the catalog covers the full 9-type set
// (rmm-core §3.2) with the exact IDs, names, types, categories, and default
// scheduling from check-library §2.1, in the §2.1 order.
func TestBuiltInChecksCatalog(t *testing.T) {
	got := BuiltInChecks()
	if len(got) != len(wantCatalog) {
		t.Fatalf("catalog has %d templates, want %d: %v", len(got), len(wantCatalog), templateIDs(got))
	}
	for i, w := range wantCatalog {
		g := got[i]
		if g.ID != w.id {
			t.Errorf("template[%d].ID = %q, want %q", i, g.ID, w.id)
		}
		if g.Name != w.name {
			t.Errorf("template[%d].Name = %q, want %q", i, g.Name, w.name)
		}
		if g.CheckType != w.checkType {
			t.Errorf("template[%d].CheckType = %q, want %q", i, g.CheckType, w.checkType)
		}
		if g.Category != w.category {
			t.Errorf("template[%d].Category = %q, want %q", i, g.Category, w.category)
		}
		if g.DefaultIntervalSecs != w.interval {
			t.Errorf("%s: DefaultIntervalSecs = %d, want %d", g.ID, g.DefaultIntervalSecs, w.interval)
		}
		if g.DefaultTimeoutSecs != w.timeout {
			t.Errorf("%s: DefaultTimeoutSecs = %d, want %d", g.ID, g.DefaultTimeoutSecs, w.timeout)
		}
		if g.Description == "" {
			t.Errorf("%s has an empty Description", g.ID)
		}
		if len(g.DefaultConfig) == 0 {
			t.Errorf("%s has an empty DefaultConfig", g.ID)
		}
		if g.ConfigSchema == nil {
			t.Errorf("%s has no ConfigSchema", g.ID)
		}
	}
}

// TestBuiltInCheckTypesExecutable pins spec §1.4: every template's CheckType
// must be registered in the agent-side default registry so an instantiated
// check is executable by an agent.
func TestBuiltInCheckTypesExecutable(t *testing.T) {
	registered := map[string]bool{}
	for _, name := range checkers.Types() {
		registered[name] = true
	}
	for _, tmpl := range BuiltInChecks() {
		if !registered[tmpl.CheckType] {
			t.Errorf("template %s: check type %q is not registered in the agent-side default registry",
				tmpl.ID, tmpl.CheckType)
			continue
		}
		c, err := checkers.Get(tmpl.CheckType)
		if err != nil {
			t.Errorf("template %s: checkers.Get(%q): %v", tmpl.ID, tmpl.CheckType, err)
			continue
		}
		if c.Name() != tmpl.CheckType {
			t.Errorf("template %s: registry name %q != template CheckType %q",
				tmpl.ID, c.Name(), tmpl.CheckType)
		}
	}
}

// TestTemplateDefaultConfigs verifies each new template ships a usable
// DefaultConfig (spec §2.2) without modification.
func TestTemplateDefaultConfigs(t *testing.T) {
	cases := []struct {
		id   string
		want map[string]any
	}{
		{"builtin-http", map[string]any{"url": "https://example.com", "expected_status": 200}},
		{"builtin-tcp", map[string]any{"host": "localhost", "port": 22}},
		{"builtin-dns", map[string]any{"resolver": "system", "query": "example.com"}},
		{"builtin-script", map[string]any{"path": ""}},
	}
	for _, c := range cases {
		tmpl, err := FindTemplate(c.id)
		if err != nil {
			t.Fatalf("FindTemplate(%s): %v", c.id, err)
		}
		if len(tmpl.DefaultConfig) != len(c.want) {
			t.Errorf("%s DefaultConfig = %v, want keys %v", c.id, tmpl.DefaultConfig, c.want)
			continue
		}
		for k, want := range c.want {
			got, ok := tmpl.DefaultConfig[k]
			if !ok {
				t.Errorf("%s DefaultConfig missing key %q", c.id, k)
				continue
			}
			if got != want {
				t.Errorf("%s DefaultConfig[%q] = %v, want %v", c.id, k, got, want)
			}
		}
	}
}

// TestFindTemplate verifies the lookup contract backing the instantiate
// endpoint's 404 behavior (spec §3.1).
func TestFindTemplate(t *testing.T) {
	tmpl, err := FindTemplate("builtin-ping")
	if err != nil {
		t.Fatalf("FindTemplate(builtin-ping) returned error: %v", err)
	}
	if tmpl.CheckType != "ping" {
		t.Errorf("FindTemplate(builtin-ping).CheckType = %q, want ping", tmpl.CheckType)
	}
	if _, err := FindTemplate("builtin-nope"); err == nil {
		t.Error("FindTemplate(builtin-nope) should return an error")
	} else if err.Error() != "template not found: builtin-nope" {
		t.Errorf("unexpected error text: %q", err.Error())
	}
}

// TestGetTemplateByName verifies case-insensitive name matching (spec §3.2).
func TestGetTemplateByName(t *testing.T) {
	for _, name := range []string{"Ping", "ping", "PING", "pInG"} {
		tmpl, ok := GetTemplateByName(name)
		if !ok {
			t.Fatalf("GetTemplateByName(%q) not found", name)
		}
		if tmpl.ID != "builtin-ping" {
			t.Errorf("GetTemplateByName(%q) returned %q, want builtin-ping", name, tmpl.ID)
		}
	}
	if _, ok := GetTemplateByName("No Such Template"); ok {
		t.Error("GetTemplateByName(unknown) should return false")
	}
}

// TestUniqueTemplateIDsAndTypes guards against accidental duplicate IDs or
// check types when the catalog grows.
func TestUniqueTemplateIDsAndTypes(t *testing.T) {
	seenID := map[string]bool{}
	seenType := map[string]bool{}
	for _, tmpl := range BuiltInChecks() {
		if seenID[tmpl.ID] {
			t.Errorf("duplicate template ID %q", tmpl.ID)
		}
		seenID[tmpl.ID] = true
		if seenType[tmpl.CheckType] {
			t.Errorf("duplicate check type %q", tmpl.CheckType)
		}
		seenType[tmpl.CheckType] = true
	}
}

func templateIDs(tmpls []CheckTemplate) []string {
	out := make([]string, 0, len(tmpls))
	for _, t := range tmpls {
		out = append(out, t.ID)
	}
	return out
}

// newTestLibrary builds a Library over a pgxmock pool plus a chi router with
// both route groups registered, mirroring the production wiring (spec §4.6).
func newTestLibrary(t *testing.T) (*Library, pgxmock.PgxPoolIface, http.Handler) {
	t.Helper()
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	lib := newLibraryFromPool(pool)
	r := chi.NewRouter()
	lib.RegisterRoutes(r)
	return lib, pool, r
}

// TestHandleListLibrary verifies GET /library serves the full catalog with
// the {"templates": [...], "total": N} envelope and keeps working with a nil
// pool (spec §1.3, §4.1).
func TestHandleListLibrary(t *testing.T) {
	lib := NewLibrary(nil)
	r := chi.NewRouter()
	lib.RegisterReadRoutes(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/library", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /library status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	var body struct {
		Templates []CheckTemplate `json:"templates"`
		Total     int             `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	if body.Total != len(wantCatalog) {
		t.Errorf("list total = %d, want %d", body.Total, len(wantCatalog))
	}
	if len(body.Templates) != len(wantCatalog) {
		t.Fatalf("list returned %d templates, want %d", len(body.Templates), len(wantCatalog))
	}
	for i, w := range wantCatalog {
		if body.Templates[i].ID != w.id {
			t.Errorf("list template[%d].ID = %q, want %q", i, body.Templates[i].ID, w.id)
		}
	}
}

// TestInstantiateNilDB verifies the 503 db_unavailable contract (spec §4.5).
func TestInstantiateNilDB(t *testing.T) {
	lib := NewLibrary(nil)
	r := chi.NewRouter()
	lib.RegisterMutatingRoutes(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/library/builtin-ping/create", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("nil pool status = %d, want 503 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"db_unavailable"}` {
		t.Errorf("nil pool body = %q, want db_unavailable", got)
	}
}

// TestInstantiateMissingTemplateID verifies the 400 path (spec §4.5). Chi
// never matches an empty URL param through the normal router, so exercise
// the handler directly with an empty param.
func TestInstantiateMissingTemplateID(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	lib := newLibraryFromPool(pool)

	req := httptest.NewRequest(http.MethodPost, "/library//create", nil)
	rec := httptest.NewRecorder()
	lib.handleInstantiateFromTemplate(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("empty template_id status = %d, want 400 (body=%s)", rec.Code, rec.Body.String())
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"error":"missing_template_id"}` {
		t.Errorf("body = %q, want missing_template_id", got)
	}
}

// TestInstantiateUnknownTemplate verifies the 404 path (spec §4.5).
func TestInstantiateUnknownTemplate(t *testing.T) {
	_, pool, router := newTestLibrary(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/library/builtin-nope/create", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown template status = %d, want 404 (body=%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 404 body: %v", err)
	}
	if body["error"] != "template_not_found" {
		t.Errorf("404 error = %v, want template_not_found", body["error"])
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// returningTimestamps builds the RETURNING created_at, updated_at rows.
func returningTimestamps(pool pgxmock.PgxPoolIface) *pgxmock.Rows {
	now := time.Now().UTC()
	return pool.NewRows([]string{"created_at", "updated_at"}).AddRow(now, now)
}

// TestInstantiateDefaults verifies the optional-body path: an empty request
// falls back to the template's name, description, config, interval, timeout,
// and enabled=true (spec §4.3, §4.4: unauthenticated means empty org_id).
func TestInstantiateDefaults(t *testing.T) {
	_, pool, router := newTestLibrary(t)

	pool.ExpectQuery(`INSERT INTO check_definitions`).
		WithArgs(
			pgxmock.AnyArg(), // id
			"",               // org_id (no identity in context)
			"TCP Port",       // template name
			"Verify a TCP endpoint accepts connections on a host and port.",
			"tcp",
			pgxmock.AnyArg(), // config JSON
			60,               // template interval
			10,               // template timeout
			true,             // enabled defaults true
			pgxmock.AnyArg(), // created_at == updated_at (single $10)
		).
		WillReturnRows(returningTimestamps(pool))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/library/builtin-tcp/create", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("default instantiate status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 201 body: %v", err)
	}
	if body["name"] != "TCP Port" {
		t.Errorf("name = %v, want template name", body["name"])
	}
	if body["check_type"] != "tcp" {
		t.Errorf("check_type = %v, want tcp", body["check_type"])
	}
	if id, _ := body["id"].(string); id == "" {
		t.Error("response must carry a server-generated id")
	}
	if iv, _ := body["interval_seconds"].(float64); iv != 60 {
		t.Errorf("interval_seconds = %v, want 60", body["interval_seconds"])
	}
	if tv, _ := body["timeout_seconds"].(float64); tv != 10 {
		t.Errorf("timeout_seconds = %v, want 10", body["timeout_seconds"])
	}
	if en, _ := body["enabled"].(bool); !en {
		t.Errorf("enabled = %v, want true", body["enabled"])
	}
	cfg, _ := body["config"].(map[string]any)
	if cfg["host"] != "localhost" || cfg["port"] != float64(22) {
		t.Errorf("config = %v, want template defaults", cfg)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestInstantiateOverridesMerge verifies the merge rules of spec §4.3:
// config merges key-by-key (request keys win, unspecified keys keep
// defaults); non-positive interval/timeout fall back to template defaults;
// enabled=false is honored when explicitly present. Authenticated org_id is
// carried through (spec §4.4).
func TestInstantiateOverridesMerge(t *testing.T) {
	_, pool, router := newTestLibrary(t)

	pool.ExpectQuery(`INSERT INTO check_definitions`).
		WithArgs(
			pgxmock.AnyArg(), // id
			"org-1",          // org_id from claims
			"Database port",  // request name
			"db TCP check",   // request description
			"tcp",            // check type
			pgxmock.AnyArg(), // merged config JSON
			90,               // request interval
			10,               // request timeout <= 0 -> template default 10
			false,            // explicit enabled=false
			pgxmock.AnyArg(), // created_at == updated_at (single $10)
		).
		WillReturnRows(returningTimestamps(pool))

	payload := `{"name":"Database port","description":"db TCP check",` +
		`"config":{"host":"db.internal","port":5432},` +
		`"interval_seconds":90,"timeout_seconds":0,"enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/library/builtin-tcp/create", strings.NewReader(payload))
	req = req.WithContext(auth.WithUser(req.Context(), &auth.SessionClaims{
		Email: "ops@example.com",
		Role:  "admin",
		OrgID: "org-1",
	}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("override instantiate status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		ID              string         `json:"id"`
		TemplateID      string         `json:"template_id"`
		Name            string         `json:"name"`
		Description     string         `json:"description"`
		CheckType       string         `json:"check_type"`
		Config          map[string]any `json:"config"`
		IntervalSeconds int            `json:"interval_seconds"`
		TimeoutSeconds  int            `json:"timeout_seconds"`
		Enabled         bool           `json:"enabled"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode merged body: %v", err)
	}
	if resp.Name != "Database port" || resp.Description != "db TCP check" {
		t.Errorf("override name/description not honored: %q / %q", resp.Name, resp.Description)
	}
	if resp.TemplateID != "builtin-tcp" {
		t.Errorf("template_id = %q, want builtin-tcp", resp.TemplateID)
	}
	if resp.IntervalSeconds != 90 {
		t.Errorf("interval_seconds = %d, want 90 (request override)", resp.IntervalSeconds)
	}
	if resp.TimeoutSeconds != 10 {
		t.Errorf("timeout_seconds = %d, want 10 (non-positive falls back to template default)", resp.TimeoutSeconds)
	}
	if resp.Enabled {
		t.Error("enabled = true, want false (explicitly overridden)")
	}
	if resp.Config["host"] != "db.internal" || resp.Config["port"] != float64(5432) {
		t.Errorf("request config keys lost: %v", resp.Config)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestInstantiateConfigMergeKeepsDefaults verifies that overriding a single
// config key keeps the remaining template defaults in place (spec §4.3).
func TestInstantiateConfigMergeKeepsDefaults(t *testing.T) {
	_, pool, router := newTestLibrary(t)

	pool.ExpectQuery(`INSERT INTO check_definitions`).
		WithArgs(
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
		).
		WillReturnRows(returningTimestamps(pool))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/library/builtin-http/create",
		strings.NewReader(`{"config":{"expected_status":204}}`))
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("partial-config instantiate status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	var resp struct {
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode merged config: %v", err)
	}
	// Request key wins; unspecified template defaults are preserved.
	if resp.Config["expected_status"] != float64(204) {
		t.Errorf("expected_status = %v, want 204 (request override)", resp.Config["expected_status"])
	}
	if resp.Config["url"] != "https://example.com" {
		t.Errorf("url = %v, want template default kept", resp.Config["url"])
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestInstantiateInsertFailure verifies the 500 insert_failed path with a
// detail field (spec §4.5).
func TestInstantiateInsertFailure(t *testing.T) {
	_, pool, router := newTestLibrary(t)

	pool.ExpectQuery(`INSERT INTO check_definitions`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(contextDeadlineExceeded())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/library/builtin-ping/create", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("insert failure status = %d, want 500 (body=%s)", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode 500 body: %v", err)
	}
	if body["error"] != "insert_failed" {
		t.Errorf("500 error = %v, want insert_failed", body["error"])
	}
	if detail, _ := body["detail"].(string); detail == "" {
		t.Error("500 response should carry a detail field")
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

func contextDeadlineExceeded() error {
	return errInsertFailed{}
}

type errInsertFailed struct{}

func (errInsertFailed) Error() string { return "insert failed: simulated db error" }

// TestInstantiateNoBodyWithAuthDefaultsOrg verifies §4.4's empty-org fallback
// in the other direction: WithUser with a nil claims pointer must not panic
// and must produce an empty org_id.
func TestInstantiateNilClaims(t *testing.T) {
	_, pool, router := newTestLibrary(t)

	pool.ExpectQuery(`INSERT INTO check_definitions`).
		WithArgs(
			pgxmock.AnyArg(),
			"", // nil claims -> empty org_id
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
			pgxmock.AnyArg(),
		).
		WillReturnRows(returningTimestamps(pool))

	req := httptest.NewRequest(http.MethodPost, "/library/builtin-dns/create", nil)
	req = req.WithContext(auth.WithUser(req.Context(), nil))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("nil claims instantiate status = %d, want 201 (body=%s)", rec.Code, rec.Body.String())
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestRegisterRoutesSeparately verifies RegisterReadRoutes and
// RegisterMutatingRoutes are independently usable and that the mutating route
// is the flat POST /library/{template_id}/create path (spec §4.6).
func TestRegisterRoutesSeparately(t *testing.T) {
	lib := NewLibrary(nil)

	readOnly := chi.NewRouter()
	lib.RegisterReadRoutes(readOnly)
	mutatingOnly := chi.NewRouter()
	lib.RegisterMutatingRoutes(mutatingOnly)

	if code := doRequest(readOnly, http.MethodGet, "/library"); code != http.StatusOK {
		t.Errorf("read router GET /library = %d, want 200", code)
	}
	if code := doRequest(readOnly, http.MethodPost, "/library/builtin-ping/create"); code != http.StatusNotFound {
		t.Errorf("read router POST create = %d, want 404 (not registered)", code)
	}
	if code := doRequest(mutatingOnly, http.MethodPost, "/library/builtin-ping/create"); code != http.StatusServiceUnavailable {
		t.Errorf("mutating router POST create = %d, want 503 (nil db)", code)
	}
	if code := doRequest(mutatingOnly, http.MethodGet, "/library"); code != http.StatusNotFound {
		t.Errorf("mutating router GET /library = %d, want 404 (not registered)", code)
	}
}

func doRequest(r chi.Router, method, path string) int {
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec.Code
}

// TestNewTemplatesSeededShape verifies the seeder contract holds for the
// new templates: Seed() would insert one disabled row per template. See
// seeder_test.go for the full seeder behavior tests.
func TestNewTemplatesSeededShape(t *testing.T) {
	for _, id := range []string{"builtin-http", "builtin-tcp", "builtin-dns", "builtin-script"} {
		tmpl, err := FindTemplate(id)
		if err != nil {
			t.Fatalf("FindTemplate(%s): %v", id, err)
		}
		if _, err := json.Marshal(tmpl.DefaultConfig); err != nil {
			t.Errorf("%s DefaultConfig not marshalable: %v", id, err)
		}
		if _, err := json.Marshal(tmpl.ConfigSchema); err != nil {
			t.Errorf("%s ConfigSchema not marshalable: %v", id, err)
		}
	}
	_ = context.Background
}
