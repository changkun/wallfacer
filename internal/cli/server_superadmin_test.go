package cli

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"changkun.de/x/wallfacer/internal/auth"
	"changkun.de/x/wallfacer/internal/handler"
	"changkun.de/x/wallfacer/internal/metrics"
	"changkun.de/x/wallfacer/internal/runner"
	"changkun.de/x/wallfacer/internal/store"
)

// fakeAuthProvider satisfies the handler.AuthProvider interface well
// enough to flip h.HasAuth() to true. The individual method bodies are
// never called by the routes exercised here; they exist only to
// satisfy the interface.
type fakeAuthProvider struct{}

func (fakeAuthProvider) HandleLogin(http.ResponseWriter, *http.Request)    {}
func (fakeAuthProvider) HandleCallback(http.ResponseWriter, *http.Request) {}
func (fakeAuthProvider) HandleLogout(http.ResponseWriter, *http.Request)   {}
func (fakeAuthProvider) UserFromRequest(http.ResponseWriter, *http.Request) *auth.User {
	return nil
}
func (fakeAuthProvider) AuthURL() string { return "https://auth.latere.ai" }

// newSuperadminMuxHandler builds the Handler + BuildMux combo used
// by the three tests below. apiKey is always empty (no static-key
// gate); cloud toggles whether SetAuth is called (enabling cloud mode
// + superadmin enforcement on /api/admin/*).
func newSuperadminMuxHandler(t *testing.T, cloud bool) http.Handler {
	t.Helper()
	workdir := t.TempDir()
	s, err := store.NewFileStore(workdir + "/data")
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	t.Cleanup(s.Close)
	r := runner.NewRunner(s, runner.RunnerConfig{
		Command:      "true",
		EnvFile:      workdir + "/.env",
		WorktreesDir: workdir + "/worktrees",
		Workspaces:   []string{workdir},
	})
	h := handler.NewHandler(s, r, workdir, []string{workdir}, nil)
	if cloud {
		h.SetAuth(fakeAuthProvider{})
	}
	reg := metrics.NewRegistry()
	return BuildMux(h, reg, IndexViewData{}, testFS(t), testFS(t), nil, false)
}

// TestAdminRebuildIndex_CloudSuperadmin200 mirrors the spec: cloud
// mode + superadmin claim in context reaches the handler, which
// returns a non-error status.
func TestAdminRebuildIndex_CloudSuperadmin200(t *testing.T) {
	mux := newSuperadminMuxHandler(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/rebuild-index", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{Sub: "root", IsSuperadmin: true}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}

// TestAdminRebuildIndex_CloudRegular403 covers the denied-principal
// case: valid claims, but IsSuperadmin is false. The RequireSuperadmin
// wrapper short-circuits before the handler runs.
func TestAdminRebuildIndex_CloudRegular403(t *testing.T) {
	mux := newSuperadminMuxHandler(t, true)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/rebuild-index", nil)
	req = req.WithContext(auth.WithClaims(req.Context(), &auth.Claims{Sub: "alice", IsSuperadmin: false}))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", w.Code)
	}
}

// TestAdminRebuildIndex_LocalMode200 confirms local mode is untouched:
// no claims, no wrapper, handler still reachable. This is today's
// behavior and the spec requires it.
func TestAdminRebuildIndex_LocalMode200(t *testing.T) {
	mux := newSuperadminMuxHandler(t, false)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/rebuild-index", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", w.Code, w.Body.String())
	}
}
