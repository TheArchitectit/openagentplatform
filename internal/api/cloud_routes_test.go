package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestCloudRoutesRegistered verifies the /api/v1/cloud route group is
// mounted. The handlers return 503 when the cloud stores are not wired,
// which proves the routes ARE registered — just not yet backed.
func TestCloudRoutesRegistered(t *testing.T) {
	srv := &Server{}
	r := chi.NewRouter()
	srv.mountCloudRoutes(r)

	routes := []struct {
		method, path string
	}{
		{"GET", "/cloud/accounts"},
		{"POST", "/cloud/accounts"},
		{"DELETE", "/cloud/accounts/abc"},
		{"GET", "/cloud/resources"},
		{"GET", "/cloud/resources/abc"},
		{"POST", "/cloud/resources/abc/enroll"},
		{"POST", "/cloud/resources/abc/ignore"},
		{"GET", "/cloud/policies"},
		{"POST", "/cloud/policies"},
		{"PUT", "/cloud/policies/abc"},
		{"DELETE", "/cloud/policies/abc"},
		{"GET", "/cloud/costs"},
		{"GET", "/cloud/drift"},
	}
	for _, route := range routes {
		req := httptest.NewRequest(route.method, route.path, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusNotFound {
			t.Errorf("%s %s → 404, want route registered", route.method, route.path)
		}
	}
}
