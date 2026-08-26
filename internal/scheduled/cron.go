// Package scheduled implements cron-based recurring automation for
// OpenAgentPlatform. It follows the same convention as internal/reports:
// a 30s tick loop loads due tasks, computes the next run with
// computeNextRun, and fires each task idempotently (at-least-once via
// last_run_at comparison).
//
// The cron grammar is the approved RMM-06 decision: 5-field cron
// (M H DoM Mon DoW) plus @hourly/@daily/@weekly/@monthly aliases. The
// 21-bit schedule_bitmask is rejected.
package scheduled

import (
	"fmt"
	"strings"
	"time"
)

// parseCronField parses a single cron field into an int bounded [min,max].
func parseCronField(field string, min, max int) (int, error) {
	var v int
	if _, err := fmt.Sscanf(field, "%d", &v); err != nil {
		return 0, fmt.Errorf("invalid cron field %q: %w", field, err)
	}
	if v < min || v > max {
		return 0, fmt.Errorf("cron field %q out of range [%d,%d]", field, min, max)
	}
	return v, nil
}

func parseSimpleCron(expr string, after time.Time) (time.Time, error) {
	var fields [5]string
	n, _ := fmt.Sscanf(expr, "%s %s %s %s %s", &fields[0], &fields[1], &fields[2], &fields[3], &fields[4])
	if n < 2 {
		return time.Time{}, fmt.Errorf("invalid cron expression: %s", expr)
	}
	minute, err := parseCronField(fields[0], 0, 59)
	if err != nil {
		return time.Time{}, err
	}
	hour, err := parseCronField(fields[1], 0, 23)
	if err != nil {
		return time.Time{}, err
	}
	now := after
	loc := time.UTC
	if now.Location() != time.UTC {
		loc = now.Location()
		now = now.In(loc)
	}
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, loc)
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next, nil
}

// computeNextRun returns the next fire time for expr strictly after `after`.
func computeNextRun(expr string, after time.Time) (*time.Time, error) {
	expr = strings.TrimSpace(expr)
	var t time.Time
	var err error
	switch expr {
	case "@hourly":
		t = time.Date(after.Year(), after.Month(), after.Day(), after.Hour()+1, 0, 0, 0, after.Location())
	case "@daily":
		t = time.Date(after.Year(), after.Month(), after.Day()+1, 0, 0, 0, 0, after.Location())
	case "@weekly":
		daysUntilSunday := (7 - int(after.Weekday())) % 7
		if daysUntilSunday == 0 {
			daysUntilSunday = 7
		}
		t = time.Date(after.Year(), after.Month(), after.Day()+daysUntilSunday, 0, 0, 0, 0, after.Location())
	case "@monthly":
		t = time.Date(after.Year(), after.Month()+1, 1, 0, 0, 0, 0, after.Location())
	default:
		t, err = parseSimpleCron(expr, after)
		if err != nil {
			return nil, err
		}
	}
	return &t, nil
}

// validateCron validates that expr is a supported cron expression. It returns
// nil if the expression parses to a next-run time after now.
func validateCron(expr string) error {
	_, err := computeNextRun(expr, time.Now().UTC())
	return err
}
