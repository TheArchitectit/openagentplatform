package api

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/openagentplatform/openagentplatform/internal/license"
)

// TestSuppressionWindowRecurringInvalidTimezone verifies that a recurring
// window with an explicit, non-empty but invalid IANA timezone is rejected
// by the API validator rather than being silently coerced to UTC by the
// store. The validator is the contract boundary: an invalid timezone is a
// client error (400), not a server-side default.
func TestSuppressionWindowRecurringInvalidTimezone(t *testing.T) {
	s := newSuppressionTestServer(t, license.TierProfessional)
	router := buildSuppressionRouter(t, s)
	token := mintToken(t, s, []string{"oap-admins"})

	body := map[string]any{
		"name":      "Nightly maintenance",
		"start":     "2026-08-24T22:00:00Z",
		"end":       "2026-08-25T06:00:00Z",
		"recurring": true,
		"weekdays":  []int{1, 2, 3},
		"timezone":  "Not/A/Real/Timezone",
		"enabled":   true,
	}
	rec := doSuppressionReq(t, router, http.MethodPost, "/api/v1/alert-suppression-windows", token, body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid timezone, got %d (body=%s)", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("invalid timezone")) {
		t.Fatalf("expected 'invalid timezone' error, got %q", rec.Body.String())
	}
}

// TestSuppressionWindowRecurringValidTimezone verifies a valid IANA timezone
// is accepted for a recurring window.
func TestSuppressionWindowRecurringValidTimezone(t *testing.T) {
	s := newSuppressionTestServer(t, license.TierProfessional)
	router := buildSuppressionRouter(t, s)
	token := mintToken(t, s, []string{"oap-admins"})

	body := map[string]any{
		"name":      "Nightly maintenance",
		"start":     "2026-08-24T22:00:00Z",
		"end":       "2026-08-25T06:00:00Z",
		"recurring": true,
		"weekdays":  []int{1, 2, 3},
		"timezone":  "America/New_York",
		"enabled":   true,
	}
	rec := doSuppressionReq(t, router, http.MethodPost, "/api/v1/alert-suppression-windows", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for valid timezone, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestSuppressionWindowRecurringEmptyTimezone verifies that an empty timezone
// is allowed (the established contract permits empty to mean UTC via store
// fallback). Only explicit invalid non-empty strings must error.
func TestSuppressionWindowRecurringEmptyTimezone(t *testing.T) {
	s := newSuppressionTestServer(t, license.TierProfessional)
	router := buildSuppressionRouter(t, s)
	token := mintToken(t, s, []string{"oap-admins"})

	body := map[string]any{
		"name":      "Nightly maintenance",
		"start":     "2026-08-24T22:00:00Z",
		"end":       "2026-08-25T06:00:00Z",
		"recurring": true,
		"weekdays":  []int{1, 2, 3},
		"timezone":  "",
		"enabled":   true,
	}
	rec := doSuppressionReq(t, router, http.MethodPost, "/api/v1/alert-suppression-windows", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for empty timezone, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestSuppressionWindowNonRecurringIgnoresTimezone verifies that a
// non-recurring window does not validate the timezone (it compares instants
// directly and ignores it), so an invalid timezone is accepted for
// non-recurring windows.
func TestSuppressionWindowNonRecurringIgnoresTimezone(t *testing.T) {
	s := newSuppressionTestServer(t, license.TierProfessional)
	router := buildSuppressionRouter(t, s)
	token := mintToken(t, s, []string{"oap-admins"})

	body := map[string]any{
		"name":      "One-off maintenance",
		"start":     "2026-08-24T22:00:00Z",
		"end":       "2026-08-25T06:00:00Z",
		"recurring": false,
		"timezone":  "Not/A/Real/Timezone",
		"enabled":   true,
	}
	rec := doSuppressionReq(t, router, http.MethodPost, "/api/v1/alert-suppression-windows", token, body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for non-recurring window (timezone ignored), got %d (body=%s)", rec.Code, rec.Body.String())
	}
}

// TestSuppressionWindowRecurringInvalidTimezoneUpdate verifies the update
// path also rejects an invalid timezone for recurring windows.
func TestSuppressionWindowRecurringInvalidTimezoneUpdate(t *testing.T) {
	s := newSuppressionTestServer(t, license.TierProfessional)
	router := buildSuppressionRouter(t, s)
	token := mintToken(t, s, []string{"oap-admins"})

	// Seed a valid recurring window.
	create := map[string]any{
		"name":      "Nightly maintenance",
		"start":     "2026-08-24T22:00:00Z",
		"end":       "2026-08-25T06:00:00Z",
		"recurring": true,
		"weekdays":  []int{1, 2, 3},
		"timezone":  "America/New_York",
		"enabled":   true,
	}
	rec := doSuppressionReq(t, router, http.MethodPost, "/api/v1/alert-suppression-windows", token, create)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed create expected 201, got %d", rec.Code)
	}

	// Now update with an invalid timezone.
	update := map[string]any{
		"name":      "Nightly maintenance",
		"start":     "2026-08-24T22:00:00Z",
		"end":       "2026-08-25T06:00:00Z",
		"recurring": true,
		"weekdays":  []int{1, 2, 3},
		"timezone":  "Not/A/Real/Timezone",
		"enabled":   true,
	}
	rec = doSuppressionReq(t, router, http.MethodPut, "/api/v1/alert-suppression-windows/seed-id", token, update)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid timezone on update, got %d (body=%s)", rec.Code, rec.Body.String())
	}
}
