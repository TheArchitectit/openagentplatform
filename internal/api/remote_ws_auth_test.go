package api

import (
	"testing"
	"time"

	"github.com/openagentplatform/openagentplatform/internal/auth"
)

// TestVerifyWSUserFailClosed verifies the remote-shell WebSocket auth helper
// no longer synthesises an admin identity when no SessionMinter is configured.
// Previously ANY non-empty ?token= value granted full admin shell access in
// that case (the insecure dev fallback). It must now reject all tokens.
func TestVerifyWSUserFailClosed(t *testing.T) {
	h := NewRemoteHandler(newDiscardLogger())

	t.Run("nil minter rejects any token", func(t *testing.T) {
		// A non-empty token that previously granted admin.
		if c, ok := h.verifyWSUser("any-non-empty-value"); ok || c != nil {
			t.Errorf("expected (nil,false) with nil minter, got (%v,%v)", c, ok)
		}
	})
	t.Run("nil minter rejects empty token", func(t *testing.T) {
		if c, ok := h.verifyWSUser(""); ok || c != nil {
			t.Errorf("expected (nil,false) for empty token, got (%v,%v)", c, ok)
		}
	})

	t.Run("configured minter rejects bad token", func(t *testing.T) {
		sm, err := auth.NewSessionMinter("oap-test", "oap-test", time.Hour, "")
		if err != nil {
			t.Fatalf("NewSessionMinter: %v", err)
		}
		h.SessionMinter = sm
		if c, ok := h.verifyWSUser("not-a-valid-jwt"); ok || c != nil {
			t.Errorf("expected (nil,false) for invalid token, got (%v,%v)", c, ok)
		}
	})

	t.Run("configured minter accepts valid token", func(t *testing.T) {
		sm, err := auth.NewSessionMinter("oap-test", "oap-test", time.Hour, "")
		if err != nil {
			t.Fatalf("NewSessionMinter: %v", err)
		}
		h.SessionMinter = sm
		tok, err := sm.Mint(&auth.Claims{
			Subject: "user-1", Email: "u@example.com", OrgID: "org-1", Groups: []string{"oap-admins"},
		})
		if err != nil {
			t.Fatalf("Mint: %v", err)
		}
		c, ok := h.verifyWSUser(tok)
		if !ok || c == nil {
			t.Fatalf("expected valid token to be accepted, got (%v,%v)", c, ok)
		}
		if c.Role != auth.RoleAdmin {
			t.Errorf("expected admin role from oap-admins group, got %q", c.Role)
		}
	})
}
