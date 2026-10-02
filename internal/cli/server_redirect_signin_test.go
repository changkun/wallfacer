package cli

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/authkit/oidc"
	"latere.ai/x/pkg/httpjson"
)

// redirectSignInUnavailable is the wire code /login and the org switch answer
// with when the redirect sign-in cannot complete on the instance. It is
// spelled out here because clients match on the string: renaming the code in
// the handler has to fail a test.
const redirectSignInUnavailable = "redirect_sign_in_unavailable"

// occupyPort binds an OS-assigned port for the life of the test and returns
// it, so a server asked for that port has to fall back to another one.
func occupyPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() {
		if err := ln.Close(); err != nil {
			t.Errorf("close occupying listener: %v", err)
		}
	})
	return ln.Addr().(*net.TCPAddr).Port
}

// runServerOn boots the run tree asked to listen on port, with env as the
// content of its env file, and returns it with its config directory.
func runServerOn(t *testing.T, port int, env string) (*ServerComponents, string) {
	t.Helper()
	// Lookup prefers the shell over the env file, so a redirect URL exported
	// in the developer's shell would decide these tests.
	t.Setenv("AUTH_REDIRECT_URL", "")
	configDir := t.TempDir()
	envFile := filepath.Join(configDir, ".env")
	if err := os.WriteFile(envFile, []byte(env), 0600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	sc := initServer(configDir, ServerConfig{
		LogFormat: "text",
		Addr:      fmt.Sprintf(":%d", port),
		DataDir:   filepath.Join(configDir, "data"),
		EnvFile:   envFile,
	}, testFS(t), testFS(t))
	t.Cleanup(sc.Shutdown)
	return sc, configDir
}

// runServerOnFreePort boots the run tree on a port that was free a moment
// before, so the server binds the port it asked for and its default redirect
// URL names it. It skips the test when another process took the port in
// between.
func runServerOnFreePort(t *testing.T) (sc *ServerComponents, configDir string, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port = ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	sc, configDir = runServerOn(t, port, "# empty\n")
	if sc.ActualPort != port {
		t.Skipf("port %d was taken between release and bind; server fell back to %d", port, sc.ActualPort)
	}
	return sc, configDir, port
}

// sessionCookies mints the session cookie a signed-in browser holds for the
// instance under configDir. The cookie key is the one initServer persisted
// there, so the server's cookie middleware accepts it.
func sessionCookies(t *testing.T, port int, configDir string) []*http.Cookie {
	t.Helper()
	cfg, err := resolveAuthConfig(oidc.Config{}, fmt.Sprintf(":%d", port), configDir)
	if err != nil {
		t.Fatalf("resolve auth config: %v", err)
	}
	client := oidc.New(cfg)
	if client == nil {
		t.Fatal("oidc.New returned nil for the default public config")
	}
	rec := httptest.NewRecorder()
	if err := client.SetSession(rec, &oidc.Session{
		AccessToken: "access-token",
		Expiry:      time.Now().Add(time.Hour),
		User:        oidc.User{Identity: authkit.Identity{Sub: "u-1"}},
	}); err != nil {
		t.Fatalf("set session: %v", err)
	}
	return rec.Result().Cookies()
}

// serve runs one request through the server's whole middleware stack.
func serve(sc *ServerComponents, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	sc.Srv.Handler.ServeHTTP(w, req)
	return w
}

// wantRedirectSignInUnavailable asserts w is the error envelope for the
// redirect sign-in being off, and that the browser was sent nowhere.
func wantRedirectSignInUnavailable(t *testing.T, name string, w *httptest.ResponseRecorder) {
	t.Helper()
	if loc := w.Header().Get("Location"); loc != "" {
		t.Errorf("%s: redirected to %q, want no redirect", name, loc)
	}
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("%s: status = %d, want 503; body %s", name, w.Code, w.Body.String())
	}
	var env httpjson.ErrorEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("%s: decode error envelope: %v; body %s", name, err, w.Body.String())
	}
	if env.Error.Code != redirectSignInUnavailable {
		t.Errorf("%s: code = %q, want %q", name, env.Error.Code, redirectSignInUnavailable)
	}
	if env.Error.Message == "" {
		t.Errorf("%s: empty message", name)
	}
}

// configFlag reads auth_redirect_enabled from GET /api/config.
func configFlag(t *testing.T, sc *ServerComponents, key string) bool {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/config", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := serve(sc, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/config: status = %d; body %s", w.Code, w.Body.String())
	}
	var cfg map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode /api/config: %v", err)
	}
	enabled, ok := cfg["auth_redirect_enabled"].(bool)
	if !ok {
		t.Fatalf("auth_redirect_enabled = %v, want a boolean", cfg["auth_redirect_enabled"])
	}
	return enabled
}

// TestInitServer_PortFallback_RedirectSignInOff covers a second instance on
// one machine: the requested port is held by another listener, the server
// binds a free one, and the default redirect URL still names the requested
// port. Starting the authorization-code flow there would have the issuer
// deliver the code to the other listener, so every entry point that starts it
// answers the error envelope instead, the SPA is told through /api/config,
// and the device-code flow stays wired.
func TestInitServer_PortFallback_RedirectSignInOff(t *testing.T) {
	occupied := occupyPort(t)
	sc, configDir := runServerOn(t, occupied, "# empty\n")
	if sc.ActualPort == occupied {
		t.Fatalf("server bound the occupied port %d, want a fallback port", occupied)
	}
	key := readServerAPIKey(configDir)
	cookies := sessionCookies(t, occupied, configDir)

	// A signed-in browser on this machine: the session cookie carries the
	// request past the server key check.
	signedIn := httptest.NewRequest(http.MethodGet, "/login?org_id=", nil)
	signedIn.RemoteAddr = "127.0.0.1:52100"
	for _, c := range cookies {
		signedIn.AddCookie(c)
	}
	wantRedirectSignInUnavailable(t, "GET /login with a session", serve(sc, signedIn))

	withKey := httptest.NewRequest(http.MethodGet, "/login", nil)
	withKey.Header.Set("Authorization", "Bearer "+key)
	wantRedirectSignInUnavailable(t, "GET /login with the server key", serve(sc, withKey))

	// A signed-out browser on this machine gets the same answer, not the
	// server key check's 401.
	anon := httptest.NewRequest(http.MethodGet, "/login", nil)
	anon.RemoteAddr = "127.0.0.1:52100"
	wantRedirectSignInUnavailable(t, "GET /login, signed out, from this machine", serve(sc, anon))

	// The org switch ends in /login, so it is refused before it gives up the
	// session the browser holds.
	switchOrg := httptest.NewRequest(http.MethodPost, "/api/me/switch-org", strings.NewReader(`{"org_id":""}`))
	switchOrg.Header.Set("Content-Type", "application/json")
	switchOrg.RemoteAddr = "127.0.0.1:52100"
	for _, c := range cookies {
		switchOrg.AddCookie(c)
	}
	w := serve(sc, switchOrg)
	wantRedirectSignInUnavailable(t, "POST /api/me/switch-org", w)
	if got := w.Header().Values("Set-Cookie"); len(got) != 0 {
		t.Errorf("POST /api/me/switch-org: Set-Cookie = %v, want the session left alone", got)
	}

	if configFlag(t, sc, key) {
		t.Error("auth_redirect_enabled = true on a fallback port, want false")
	}

	poll := httptest.NewRequest(http.MethodGet, "/api/auth/device/poll", nil)
	poll.Header.Set("Authorization", "Bearer "+key)
	if w := serve(sc, poll); w.Code != http.StatusOK {
		t.Errorf("GET /api/auth/device/poll: status = %d, want 200 (device sign-in stays available); body %s", w.Code, w.Body.String())
	}
}

// TestInitServer_PortFallback_ExplicitRedirectKeepsSignIn covers a redirect
// URL the operator set: it names the address the deployment is reached on,
// which need not be the bound port, so a port fallback leaves the redirect
// sign-in on.
func TestInitServer_PortFallback_ExplicitRedirectKeepsSignIn(t *testing.T) {
	occupied := occupyPort(t)
	sc, configDir := runServerOn(t, occupied, "AUTH_REDIRECT_URL=https://wf.example.test/callback\n")
	if sc.ActualPort == occupied {
		t.Fatalf("server bound the occupied port %d, want a fallback port", occupied)
	}
	key := readServerAPIKey(configDir)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := serve(sc, req)
	if w.Code != http.StatusFound {
		t.Fatalf("GET /login: status = %d, want 302; body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "redirect_uri=https%3A%2F%2Fwf.example.test%2Fcallback") {
		t.Errorf("Location %q does not carry the configured redirect URL", loc)
	}
	if !configFlag(t, sc, key) {
		t.Error("auth_redirect_enabled = false with an explicit redirect URL, want true")
	}
}

// TestInitServer_RequestedPortBound_RedirectSignInOn covers the single
// instance: the requested port binds, the default redirect URL names it, and
// /login sends the browser to the issuer.
func TestInitServer_RequestedPortBound_RedirectSignInOn(t *testing.T) {
	sc, configDir, port := runServerOnFreePort(t)
	key := readServerAPIKey(configDir)

	req := httptest.NewRequest(http.MethodGet, "/login", nil)
	req.Header.Set("Authorization", "Bearer "+key)
	w := serve(sc, req)
	if w.Code != http.StatusFound {
		t.Fatalf("GET /login: status = %d, want 302; body %s", w.Code, w.Body.String())
	}
	want := fmt.Sprintf("redirect_uri=http%%3A%%2F%%2Flocalhost%%3A%d%%2Fcallback", port)
	if loc := w.Header().Get("Location"); !strings.Contains(loc, want) {
		t.Errorf("Location %q missing %q", loc, want)
	}
	if !configFlag(t, sc, key) {
		t.Error("auth_redirect_enabled = false on the requested port, want true")
	}

	// A signed-out browser on this machine: a navigation carries neither the
	// server key nor a session, and still has to reach the issuer and come
	// back through /callback.
	anon := httptest.NewRequest(http.MethodGet, "/login", nil)
	anon.RemoteAddr = "127.0.0.1:52100"
	w = serve(sc, anon)
	if w.Code != http.StatusFound {
		t.Fatalf("GET /login, signed out, from this machine: status = %d, want 302; body %s", w.Code, w.Body.String())
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, want) {
		t.Errorf("signed out: Location %q missing %q", loc, want)
	}
	// With no flow cookie the callback restarts the sign-in; what matters
	// here is that the handler, not the server key check, answers.
	back := httptest.NewRequest(http.MethodGet, "/callback?code=c&state=s", nil)
	back.RemoteAddr = "127.0.0.1:52100"
	w = serve(sc, back)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login" {
		t.Errorf("GET /callback, signed out, from this machine: status = %d Location %q, want 302 to /login; body %s",
			w.Code, w.Header().Get("Location"), w.Body.String())
	}

	// A peer on another host could finish a sign-in of its own here and hold
	// a session that passes the key check, so it does not start one.
	for _, target := range []string{"/login", "/callback?code=c&state=s"} {
		remote := httptest.NewRequest(http.MethodGet, target, nil)
		remote.RemoteAddr = "192.168.1.20:52100"
		if w := serve(sc, remote); w.Code != http.StatusUnauthorized {
			t.Errorf("GET %s, signed out, from another host: status = %d, want 401", target, w.Code)
		}
	}
}

// TestInitServer_OrgSwitch_ClearsSessionAndReachesLogin covers an org switch
// on a local instance from start to the issuer. The instance serves plain
// HTTP, so the session lives under the cookie name without the "__Host-"
// prefix; the switch expires that cookie, and the navigation to /login that
// follows, now without a session, still passes the server key check from this
// machine and carries the target context to the issuer.
func TestInitServer_OrgSwitch_ClearsSessionAndReachesLogin(t *testing.T) {
	sc, configDir, port := runServerOnFreePort(t)
	cookies := sessionCookies(t, port, configDir)
	if len(cookies) != 1 {
		t.Fatalf("session cookies = %v, want one", cookies)
	}
	held := cookies[0].Name

	switchOrg := httptest.NewRequest(http.MethodPost, "/api/me/switch-org", strings.NewReader(`{"org_id":""}`))
	switchOrg.Header.Set("Content-Type", "application/json")
	switchOrg.RemoteAddr = "127.0.0.1:52100"
	switchOrg.AddCookie(cookies[0])
	w := serve(sc, switchOrg)
	if w.Code != http.StatusOK {
		t.Fatalf("POST /api/me/switch-org: status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	var body struct {
		Redirect string `json:"redirect"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode switch answer: %v; body %s", err, w.Body.String())
	}
	if body.Redirect != "/login?org_id=" {
		t.Fatalf("redirect = %q, want /login?org_id=", body.Redirect)
	}
	expired := false
	for _, c := range w.Result().Cookies() {
		if c.Name == held && c.MaxAge < 0 {
			expired = true
		}
	}
	if !expired {
		t.Errorf("POST /api/me/switch-org: Set-Cookie = %v, want %s expired", w.Header().Values("Set-Cookie"), held)
	}

	// The browser follows the redirect with the session gone.
	follow := httptest.NewRequest(http.MethodGet, body.Redirect, nil)
	follow.RemoteAddr = "127.0.0.1:52100"
	w = serve(sc, follow)
	if w.Code != http.StatusFound {
		t.Fatalf("GET %s without a session: status = %d, want 302; body %s", body.Redirect, w.Code, w.Body.String())
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse Location: %v", err)
	}
	if !strings.HasPrefix(loc.String(), "https://auth.latere.ai/authorize") {
		t.Errorf("Location = %q, want the issuer's authorize endpoint", loc)
	}
	// Present and empty: the issuer reads that as the personal context.
	if vs, ok := loc.Query()["org_id"]; !ok || len(vs) != 1 || vs[0] != "" {
		t.Errorf("Location %q: org_id = %v (present %v), want present and empty", loc, vs, ok)
	}
}
