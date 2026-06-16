package checklib

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SeedResult summarises what the seeder did.
type SeedResult struct {
	Seeded      []string `json:"seeded"`
	Skipped     []string `json:"skipped"`
	TotalChecks int      `json:"total_checks"`
	Errors      []string `json:"errors,omitempty"`
}

// Seed inserts one disabled check_definitions row for each built-in
// template that does not already exist (matched by name + check_type).
// The seeded checks are intentionally disabled so they are visible in
// the library but do not run until an operator explicitly enables and
// assigns them. This is idempotent: running it again on a populated
// database is a no-op.
func Seed(ctx context.Context, pool *pgxpool.Pool, log *slog.Logger) (SeedResult, error) {
	res := SeedResult{Seeded: []string{}, Skipped: []string{}, Errors: []string{}}
	if pool == nil {
		return res, ErrNoDB
	}
	if log == nil {
		log = slog.Default()
	}

	templates := BuiltInChecks()
	res.TotalChecks = len(templates)

	for _, t := range templates {
		// Check if a check with this name + check_type already exists.
		const existsQ = `
			SELECT COUNT(*) FROM check_definitions
			WHERE name = $1 AND check_type = $2
		`
		var n int
		if err := pool.QueryRow(ctx, existsQ, t.Name, t.CheckType).Scan(&n); err != nil {
			msg := fmt.Sprintf("exists-check failed for %s: %v", t.Name, err)
			log.Warn("seeder: exists check failed", "name", t.Name, "err", err)
			res.Errors = append(res.Errors, msg)
			continue
		}
		if n > 0 {
			res.Skipped = append(res.Skipped, t.Name)
			continue
		}

		cfgJSON, err := json.Marshal(t.DefaultConfig)
		if err != nil {
			msg := fmt.Sprintf("marshal config failed for %s: %v", t.Name, err)
			res.Errors = append(res.Errors, msg)
			continue
		}
		now := time.Now().UTC()
		const insertQ = `
			INSERT INTO check_definitions (
				id, org_id, name, description, check_type, config,
				interval_seconds, timeout_seconds, enabled, created_at, updated_at
			) VALUES (
				gen_random_uuid(), '', $1, $2, $3, $4,
				$5, $6, false, $7, $7
			)
		`
		_, err = pool.Exec(ctx, insertQ,
			t.Name, t.Description, t.CheckType, cfgJSON,
			t.DefaultIntervalSecs, t.DefaultTimeoutSecs, now,
		)
		if err != nil {
			// It's possible the check_results / check_definitions table doesn't
			// exist yet. Don't treat that as fatal — log and move on.
			msg := fmt.Sprintf("insert failed for %s: %v", t.Name, err)
			log.Warn("seeder: insert failed (table missing?)", "name", t.Name, "err", err)
			res.Errors = append(res.Errors, msg)
			continue
		}
		res.Seeded = append(res.Seeded, t.Name)
		log.Info("seeder: seeded built-in check", "name", t.Name, "check_type", t.CheckType)
	}

	log.Info("seeder complete",
		"seeded", len(res.Seeded),
		"skipped", len(res.Skipped),
		"errors", len(res.Errors),
	)
	return res, nil
}

// ensure pgx import is retained (Scan into int above)
var _ = pgx.ErrNoRows
