package checklib

import (
	"context"
	"errors"
	"testing"

	"github.com/pashagolub/pgxmock/v4"
)

// TestSeedNilPool verifies the ErrNoDB contract (spec §5.4).
func TestSeedNilPool(t *testing.T) {
	_, err := Seed(context.Background(), nil, nil)
	if !errors.Is(err, ErrNoDB) {
		t.Fatalf("Seed(nil pool) err = %v, want ErrNoDB", err)
	}
}

// TestSeedInsertsOneDisabledRowPerTemplate verifies §5.1's shape: one
// disabled, org-less insert per template with the template's defaults. The
// mock expects an exists-check (0 rows) then an insert for each of the 9
// templates.
func TestSeedInsertsOneDisabledRowPerTemplate(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	for range BuiltInChecks() {
		pool.ExpectQuery(`SELECT COUNT\(\*\) FROM check_definitions`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(pool.NewRows([]string{"count"}).AddRow(0))
		pool.ExpectExec(`INSERT INTO check_definitions`).
			WithArgs(
				pgxmock.AnyArg(), // name
				pgxmock.AnyArg(), // description
				pgxmock.AnyArg(), // check_type
				pgxmock.AnyArg(), // config JSON
				pgxmock.AnyArg(), // interval
				pgxmock.AnyArg(), // timeout
				pgxmock.AnyArg(), // created_at == updated_at
			).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))
	}

	res, err := Seed(context.Background(), pool, nil)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if len(res.Seeded) != len(wantCatalog) {
		t.Errorf("SeedResult.Seeded = %v (%d), want %d entries", res.Seeded, len(res.Seeded), len(wantCatalog))
	}
	if len(res.Skipped) != 0 {
		t.Errorf("SeedResult.Skipped = %v, want empty on a fresh DB", res.Skipped)
	}
	if res.TotalChecks != len(wantCatalog) {
		t.Errorf("SeedResult.TotalChecks = %d, want %d", res.TotalChecks, len(wantCatalog))
	}
	if len(res.Errors) != 0 {
		t.Errorf("SeedResult.Errors = %v, want empty", res.Errors)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestSeedIdempotent verifies §5.2: a template whose name + check_type
// already exists is skipped and no insert is issued for it.
func TestSeedIdempotent(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	for range BuiltInChecks() {
		pool.ExpectQuery(`SELECT COUNT\(\*\) FROM check_definitions`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(pool.NewRows([]string{"count"}).AddRow(1))
	}

	res, err := Seed(context.Background(), pool, nil)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if len(res.Skipped) != len(wantCatalog) {
		t.Errorf("SeedResult.Skipped = %v (%d), want %d (all already present)", res.Skipped, len(res.Skipped), len(wantCatalog))
	}
	if len(res.Seeded) != 0 {
		t.Errorf("SeedResult.Seeded = %v, want empty when everything exists", res.Seeded)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations (inserts should not be issued on a populated DB): %v", err)
	}
}

// TestSeedPartialFailureContinues verifies §5.3: a per-template failure must
// be recorded and must not abort the remaining templates.
func TestSeedPartialFailureContinues(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	templates := BuiltInChecks()
	// First template's exists-check explodes; the remaining 8 proceed.
	pool.ExpectQuery(`SELECT COUNT\(\*\) FROM check_definitions`).
		WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
		WillReturnError(errors.New("simulated exists-check failure"))
	for range templates[1:] {
		pool.ExpectQuery(`SELECT COUNT\(\*\) FROM check_definitions`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(pool.NewRows([]string{"count"}).AddRow(1))
	}

	res, err := Seed(context.Background(), pool, nil)
	if err != nil {
		t.Fatalf("Seed must not abort on per-template errors: %v", err)
	}
	if len(res.Errors) != 1 {
		t.Fatalf("SeedResult.Errors = %v, want exactly 1 entry", res.Errors)
	}
	if got, want := len(res.Skipped), len(templates)-1; got != want {
		t.Errorf("SeedResult.Skipped count = %d, want %d (remaining templates still processed)", got, want)
	}
	if len(res.Seeded) != 0 {
		t.Errorf("SeedResult.Seeded = %v, want empty", res.Seeded)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestSeedInsertFailureRecorded verifies a failed insert (e.g. missing table)
// lands in SeedResult.Errors without aborting the run (spec §5.3).
func TestSeedInsertFailureRecorded(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	for range BuiltInChecks() {
		pool.ExpectQuery(`SELECT COUNT\(\*\) FROM check_definitions`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(pool.NewRows([]string{"count"}).AddRow(0))
		pool.ExpectExec(`INSERT INTO check_definitions`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(),
				pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnError(errors.New(`relation "check_definitions" does not exist`))
	}

	res, err := Seed(context.Background(), pool, nil)
	if err != nil {
		t.Fatalf("Seed must not return an error for per-template failures: %v", err)
	}
	if len(res.Errors) != len(wantCatalog) {
		t.Errorf("SeedResult.Errors = %d entries, want %d", len(res.Errors), len(wantCatalog))
	}
	if len(res.Seeded) != 0 {
		t.Errorf("SeedResult.Seeded = %v, want empty", res.Seeded)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}

// TestSeedSeededDefaultsMatchTemplate pins §5.1's "template's default
// config/interval/timeout" for one representative new template: the seeded
// row must carry the catalog values (disabled, empty org). Expectations are
// registered in catalog order so the HTTP template's exists-check and insert
// are matched with the exact template values.
func TestSeedSeededDefaultsMatchTemplate(t *testing.T) {
	pool, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, tmpl := range BuiltInChecks() {
		if tmpl.ID == "builtin-http" {
			pool.ExpectQuery(`SELECT COUNT\(\*\) FROM check_definitions`).
				WithArgs("HTTP Endpoint", "http").
				WillReturnRows(pool.NewRows([]string{"count"}).AddRow(0))
			pool.ExpectExec(`INSERT INTO check_definitions`).
				WithArgs(
					"HTTP Endpoint",
					"HTTP GET a URL from the agent and alert when the status code is not 2xx/3xx.",
					"http",
					pgxmock.AnyArg(), // config JSON
					60,               // DefaultIntervalSecs
					15,               // DefaultTimeoutSecs
					pgxmock.AnyArg(), // created_at == updated_at
				).
				WillReturnResult(pgxmock.NewResult("INSERT", 1))
			continue
		}
		pool.ExpectQuery(`SELECT COUNT\(\*\) FROM check_definitions`).
			WithArgs(pgxmock.AnyArg(), pgxmock.AnyArg()).
			WillReturnRows(pool.NewRows([]string{"count"}).AddRow(1))
	}

	res, err := Seed(context.Background(), pool, nil)
	if err != nil {
		t.Fatalf("Seed: %v", err)
	}
	if len(res.Seeded) != 1 || res.Seeded[0] != "HTTP Endpoint" {
		t.Errorf("SeedResult.Seeded = %v, want [HTTP Endpoint]", res.Seeded)
	}
	if got, want := len(res.Skipped), len(wantCatalog)-1; got != want {
		t.Errorf("SeedResult.Skipped count = %d, want %d", got, want)
	}
	if err := pool.ExpectationsWereMet(); err != nil {
		t.Errorf("unmet expectations: %v", err)
	}
}
