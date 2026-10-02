package cli

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestBuildMux_FeedbackNeedsAPrincipalOnlyInCloudMode verifies that on a local
// instance, which wires a sign-in provider on every run, a request with no
// browser principal reaches the feedback handler, while a cloud-mode
// deployment refuses it with 401 sign_in_required.
func TestBuildMux_FeedbackNeedsAPrincipalOnlyInCloudMode(t *testing.T) {
	path := "/api/tasks/" + uuid.NewString() + "/feedback"
	body := `{"message":"tighten the tests"}`

	local := newSuperadminMuxHandler(t, false)
	w := httptest.NewRecorder()
	local.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if w.Code == http.StatusUnauthorized || strings.Contains(w.Body.String(), "sign_in_required") {
		t.Fatalf("local: status = %d body %q, want the feedback handler's answer, not the sign-in gate", w.Code, w.Body.String())
	}

	cloud := newSuperadminMuxHandler(t, true)
	w = httptest.NewRecorder()
	cloud.ServeHTTP(w, httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)))
	if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "sign_in_required") {
		t.Fatalf("cloud: status = %d body %q, want 401 sign_in_required", w.Code, w.Body.String())
	}
}

// TestBuildMux_SpecCommentsNeedAPrincipalInEveryMode verifies that the
// spec-comment routes refuse a request with no browser principal on a local
// instance as on a cloud-mode deployment: they serve the coordination relay
// from the connector's token, which a signed-out browser must not reach.
func TestBuildMux_SpecCommentsNeedAPrincipalInEveryMode(t *testing.T) {
	for _, cloud := range []bool{false, true} {
		mux := newSuperadminMuxHandler(t, cloud)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/spec-comments", nil))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("cloud=%v: status = %d body %q, want 401", cloud, w.Code, w.Body.String())
		}
	}
}
