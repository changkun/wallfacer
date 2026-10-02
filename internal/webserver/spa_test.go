package webserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"latere.ai/x/pkg/httpjson"
)

// coordinatorMux registers the route shapes `wallfacer web` mounts (a POST
// prefix, GET API routes, a GET probe, the asset prefixes) and then the SPA
// fallback, as runWeb does. ServeMux panics on conflicting patterns, so
// building it proves the catch-alls coexist with those routes.
func coordinatorMux(t *testing.T) *http.ServeMux {
	t.Helper()
	frontend := fstest.MapFS{
		"frontend/dist/index.html":    {Data: []byte("<!doctype html><div id=\"app\"></div>")},
		"frontend/dist/assets/app.js": {Data: []byte("console.log('ok')")},
	}
	ok := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/coordination/ws", ok)
	mux.HandleFunc("POST /v1/telemetry/", ok)
	mux.HandleFunc("GET /livez", ok)
	mux.HandleFunc("GET /login", ok)
	mux.HandleFunc("GET /api/me", ok)
	if !MountSPA(mux, frontend) {
		t.Fatal("MountSPA returned false for frontend/dist fixture")
	}
	SPAFallback(mux, frontend)
	return mux
}

// TestSPAFallback_UnknownAPIPathAnswers404JSON verifies that on the
// coordinator site an /api path no route serves answers 404 in the error
// envelope for every method, not the SPA page with 200.
func TestSPAFallback_UnknownAPIPathAnswers404JSON(t *testing.T) {
	mux := coordinatorMux(t)
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/does-not-exist"},
		{http.MethodPost, "/api/does-not-exist"},
		{http.MethodGet, "/api/me/extra"},
		{http.MethodGet, "/api/"},
		{http.MethodGet, "/api"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			rr := httptest.NewRecorder()
			mux.ServeHTTP(rr, httptest.NewRequest(tc.method, tc.path, nil))
			if rr.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404; body %q", rr.Code, rr.Body.String())
			}
			var env httpjson.ErrorEnvelope
			if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
				t.Fatalf("decode envelope: %v (%q)", err, rr.Body.String())
			}
			if env.Error.Code != "not_found" || env.Error.Message == "" {
				t.Fatalf("envelope = %+v, want code not_found with a message", env.Error)
			}
		})
	}
}

// TestSPAFallback_KnownAPIPathWrongMethodAnswers405 verifies that a path a
// route serves under GET answers another method 405 in the error envelope,
// with an Allow header naming GET.
func TestSPAFallback_KnownAPIPathWrongMethodAnswers405(t *testing.T) {
	mux := coordinatorMux(t)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/me", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405; body %q", rr.Code, rr.Body.String())
	}
	if allow := rr.Header().Get("Allow"); !strings.Contains(allow, http.MethodGet) || strings.Contains(allow, http.MethodPost) {
		t.Errorf("Allow = %q, want GET and not POST", allow)
	}
	var env httpjson.ErrorEnvelope
	if err := json.Unmarshal(rr.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode envelope: %v (%q)", err, rr.Body.String())
	}
	if env.Error.Code != "method_not_allowed" {
		t.Fatalf("envelope = %+v, want code method_not_allowed", env.Error)
	}
}

// TestSPAFallback_DeepLinksAndRoutes verifies that the coordinator site still
// serves the SPA page for a client route under GET and HEAD, answers another
// method on it 405, and leaves the registered routes their requests.
func TestSPAFallback_DeepLinksAndRoutes(t *testing.T) {
	mux := coordinatorMux(t)
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(method, "/board/some/route", nil))
		if rr.Code != http.StatusOK || !strings.Contains(rr.Header().Get("Content-Type"), "text/html") {
			t.Errorf("%s deep link: status %d type %q, want 200 text/html", method, rr.Code, rr.Header().Get("Content-Type"))
		}
	}
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/board", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST deep link: status %d, want 405", rr.Code)
	}
	for _, req := range []*http.Request{
		httptest.NewRequest(http.MethodPost, "/v1/telemetry/v1/traces", nil),
		httptest.NewRequest(http.MethodGet, "/api/me", nil),
		httptest.NewRequest(http.MethodGet, "/livez", nil),
	} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK || rr.Body.Len() != 0 {
			t.Errorf("%s %s: status %d body %q, want the route's empty 200", req.Method, req.URL.Path, rr.Code, rr.Body.String())
		}
	}
}

func TestMountSPAUsesFrontendDistFromEmbeddedRoot(t *testing.T) {
	frontend := fstest.MapFS{
		"frontend/dist/index.html":    {Data: []byte("<!doctype html><div id=\"app\"></div>")},
		"frontend/dist/assets/app.js": {Data: []byte("console.log('ok')")},
	}
	mux := http.NewServeMux()

	if !MountSPA(mux, frontend) {
		t.Fatal("MountSPA returned false for frontend/dist fixture")
	}
	SPAFallback(mux, frontend)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/docs/usage", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("fallback status = %d, want %d", rr.Code, http.StatusOK)
	}
	if !strings.Contains(rr.Body.String(), `id="app"`) {
		t.Fatalf("fallback body = %q, want embedded index.html", rr.Body.String())
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("asset status = %d, want %d", rr.Code, http.StatusOK)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != "console.log('ok')" {
		t.Fatalf("asset body = %q, want app.js", got)
	}
}
