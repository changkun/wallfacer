package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"changkun.de/x/wallfacer/internal/auth"
)

// ok200 is a trivial terminal handler that records invocation and
// returns 200; used to check pass-through vs. short-circuit behavior.
func ok200() (http.Handler, *bool) {
	reached := false
	h := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		reached = true
		w.WriteHeader(http.StatusOK)
	})
	return h, &reached
}

// --- RequireSuperadmin -----------------------------------------------------

func TestRequireSuperadmin_SuperadminClaim_Passes(t *testing.T) {
	inner, reached := ok200()
	h := auth.RequireSuperadmin(inner)

	r := httptest.NewRequest(http.MethodPost, "/api/admin/rebuild-index", nil)
	r = r.WithContext(auth.WithClaims(r.Context(), &auth.Claims{Sub: "root", IsSuperadmin: true}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !*reached {
		t.Fatal("inner handler not invoked for superadmin")
	}
}

func TestRequireSuperadmin_RegularUser_Forbidden(t *testing.T) {
	inner, reached := ok200()
	h := auth.RequireSuperadmin(inner)

	r := httptest.NewRequest(http.MethodPost, "/api/admin/rebuild-index", nil)
	r = r.WithContext(auth.WithClaims(r.Context(), &auth.Claims{Sub: "alice", IsSuperadmin: false}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if *reached {
		t.Error("inner handler reached despite non-superadmin")
	}
}

func TestRequireSuperadmin_NoClaims_Unauthorized(t *testing.T) {
	inner, reached := ok200()
	h := auth.RequireSuperadmin(inner)

	r := httptest.NewRequest(http.MethodPost, "/api/admin/rebuild-index", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if *reached {
		t.Error("inner handler reached without any claims")
	}
}

// --- RequireScope ----------------------------------------------------------

func TestRequireScope_WithScope_Passes(t *testing.T) {
	inner, reached := ok200()
	h := auth.RequireScope("admin:tasks")(inner)

	r := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	r = r.WithContext(auth.WithClaims(r.Context(), &auth.Claims{
		Sub:    "alice",
		Scopes: []string{"read:projects", "admin:tasks"},
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !*reached {
		t.Fatal("inner handler not invoked despite matching scope")
	}
}

func TestRequireScope_WithoutScope_Forbidden(t *testing.T) {
	inner, reached := ok200()
	h := auth.RequireScope("admin:tasks")(inner)

	r := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	r = r.WithContext(auth.WithClaims(r.Context(), &auth.Claims{
		Sub:    "alice",
		Scopes: []string{"read:projects"},
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
	if *reached {
		t.Error("inner handler reached without required scope")
	}
}

func TestRequireScope_NoClaims_Unauthorized(t *testing.T) {
	inner, reached := ok200()
	h := auth.RequireScope("admin:tasks")(inner)

	r := httptest.NewRequest(http.MethodGet, "/api/anything", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", w.Code)
	}
	if *reached {
		t.Error("inner handler reached without any claims")
	}
}
