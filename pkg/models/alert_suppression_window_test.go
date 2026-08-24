package models

import (
	"testing"
	"time"
)

func TestAlertSuppressionWindowIsActiveAt(t *testing.T) {
	start := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)

	// Non-recurring window active between start (inclusive) and end (exclusive).
	w := &AlertSuppressionWindow{Start: start, End: end, Enabled: true}
	if !w.IsActiveAt(time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC)) {
		t.Fatal("expected active inside window")
	}
	if w.IsActiveAt(time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("expected inactive at end boundary (exclusive)")
	}
	if w.IsActiveAt(time.Date(2026, 8, 24, 9, 59, 59, 0, time.UTC)) {
		t.Fatal("expected inactive before start")
	}

	// Disabled window never active.
	disabled := &AlertSuppressionWindow{Start: start, End: end, Enabled: false}
	if disabled.IsActiveAt(time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC)) {
		t.Fatal("expected inactive when disabled")
	}

	// Recurring window: weekday + time-of-day from Start/End.
	// 2026-08-24 is a Monday.
	monday := time.Date(2026, 8, 24, 11, 0, 0, 0, time.UTC)
	tuesday := time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC)
	recur := &AlertSuppressionWindow{
		Start:     time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC),
		Recurring: true,
		Weekdays:  []time.Weekday{time.Monday},
		Enabled:   true,
	}
	if !recur.IsActiveAt(monday) {
		t.Fatal("expected active on matching weekday at matching time")
	}
	if recur.IsActiveAt(tuesday) {
		t.Fatal("expected inactive on non-matching weekday")
	}

	// Recurring window with no weekday restriction is active every day.
	allDays := &AlertSuppressionWindow{
		Start:     time.Date(2026, 8, 25, 11, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 8, 25, 12, 0, 0, 0, time.UTC),
		Recurring: true,
		Enabled:   true,
	}
	if !allDays.IsActiveAt(time.Date(2026, 8, 25, 11, 30, 0, 0, time.UTC)) {
		t.Fatal("expected active on any weekday when Weekdays is empty")
	}
	if allDays.IsActiveAt(time.Date(2026, 8, 25, 12, 30, 0, 0, time.UTC)) {
		t.Fatal("expected inactive outside time window")
	}

	// Nil window is safe.
	var nilWindow *AlertSuppressionWindow
	if nilWindow.IsActiveAt(time.Now()) {
		t.Fatal("expected inactive for nil window")
	}
}

// TestAlertSuppressionWindowIsActiveAtTimezone verifies that a recurring
// window evaluates weekday + time-of-day in its own IANA timezone, not UTC.
func TestAlertSuppressionWindowIsActiveAtTimezone(t *testing.T) {
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Skip("timezone database unavailable")
	}

	// Window: 09:00-10:00 in Los Angeles. The stored Start/End carry the
	// LA location, and Timezone names the same zone.
	start := time.Date(2026, 8, 24, 9, 0, 0, 0, loc)
	end := time.Date(2026, 8, 24, 10, 0, 0, 0, loc)
	w := &AlertSuppressionWindow{
		Start:     start,
		End:       end,
		Recurring: true,
		Weekdays:  []time.Weekday{time.Monday},
		Timezone:  "America/Los_Angeles",
		Enabled:   true,
	}

	// 2026-08-24 09:30 LA == 16:30 UTC. Active.
	activeInstant := time.Date(2026, 8, 24, 16, 30, 0, 0, time.UTC)
	if !w.IsActiveAt(activeInstant) {
		t.Fatal("expected active at 09:30 LA (16:30 UTC)")
	}

	// 2026-08-24 11:00 LA == 18:00 UTC. Outside the window (after 10:00).
	inactiveInstant := time.Date(2026, 8, 24, 18, 0, 0, 0, time.UTC)
	if w.IsActiveAt(inactiveInstant) {
		t.Fatal("expected inactive at 11:00 LA (18:00 UTC)")
	}

	// A window WITHOUT a timezone uses UTC. Same stored times (09:00-10:00
	// UTC) evaluated at 16:30 UTC must be inactive.
	wUTC := &AlertSuppressionWindow{
		Start:     time.Date(2026, 8, 24, 9, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC),
		Recurring: true,
		Weekdays:  []time.Weekday{time.Monday},
		Enabled:   true,
	}
	if wUTC.IsActiveAt(activeInstant) {
		t.Fatal("expected UTC window inactive at 16:30 UTC (outside 09-10)")
	}
}

// TestAlertSuppressionWindowIsActiveAtRecurringOvernight verifies a recurring
// window whose end time-of-day is earlier than its start (e.g. 22:00->06:00)
// is active across midnight.
func TestAlertSuppressionWindowIsActiveAtRecurringOvernight(t *testing.T) {
	w := &AlertSuppressionWindow{
		Start:     time.Date(2026, 8, 24, 22, 0, 0, 0, time.UTC),
		End:       time.Date(2026, 8, 24, 6, 0, 0, 0, time.UTC),
		Recurring: true,
		Weekdays:  []time.Weekday{time.Monday},
		Enabled:   true,
	}
	// Tuesday 02:00 should be inside a Mon-only overnight window? No — the
	// weekday is Monday and 02:00 Tuesday is not Monday. Use Monday 02:00.
	mon2am := time.Date(2026, 8, 24, 2, 0, 0, 0, time.UTC) // Monday
	if !w.IsActiveAt(mon2am) {
		t.Fatal("expected active at 02:00 Monday inside overnight window")
	}
	// Monday 23:00 is also inside.
	if !w.IsActiveAt(time.Date(2026, 8, 24, 23, 0, 0, 0, time.UTC)) {
		t.Fatal("expected active at 23:00 Monday inside overnight window")
	}
	// Tuesday 02:00 is outside (weekday mismatch).
	if w.IsActiveAt(time.Date(2026, 8, 25, 2, 0, 0, 0, time.UTC)) {
		t.Fatal("expected inactive at 02:00 Tuesday (not a Monday window)")
	}
	// Monday 12:00 (midday) is outside the overnight window.
	if w.IsActiveAt(time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)) {
		t.Fatal("expected inactive at 12:00 Monday (outside overnight window)")
	}
}

// TestAlertSuppressionWindowNonRecurringBoundaries verifies the [start,end)
// half-open semantics for non-recurring windows.
func TestAlertSuppressionWindowNonRecurringBoundaries(t *testing.T) {
	start := time.Date(2026, 8, 24, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 8, 24, 12, 0, 0, 0, time.UTC)
	w := &AlertSuppressionWindow{Start: start, End: end, Enabled: true}

	if !w.IsActiveAt(start) {
		t.Fatal("expected active exactly at start (inclusive)")
	}
	if w.IsActiveAt(end) {
		t.Fatal("expected inactive exactly at end (exclusive)")
	}
	if w.IsActiveAt(start.Add(-time.Second)) {
		t.Fatal("expected inactive just before start")
	}
	if w.IsActiveAt(end.Add(time.Second)) {
		t.Fatal("expected inactive just after end")
	}
}
