package cli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

const (
	keyGateLoopbackPeer = "127.0.0.1:52100"
	keyGateRemotePeer   = "192.168.1.20:52100"
)

// isolateKeyGateEnv empties the shell variables that decide which bearer
// tokens the instance accepts and whether it runs in cloud mode, and points
// the home and config directories at a temporary one. Lookup prefers a
// non-empty shell value over the env file, so without the first the
// developer's shell would decide these tests. The server's session token
// bridge writes a cookie's token into the token file under the user config
// directory (on macOS under $HOME, whatever XDG_CONFIG_HOME says), so without
// the second a request carrying a test session would overwrite the
// developer's real sign-in.
func isolateKeyGateEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"AUTH_URL", "AUTH_CLIENT_ID", "AUTH_AUDIENCE", "AUTH_ISSUER", "AUTH_JWKS_URL",
		"WALLFACER_CLOUD", "WALLFACER_SERVER_API_KEY",
	} {
		t.Setenv(k, "")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
}

// keyGateRequest builds a GET for target from peer. bearer, when set, is
// presented as the Authorization bearer; cookies are attached as sent. The
// request context ends after a short deadline, so a streaming handler the
// gate wrongly admits returns instead of holding the test open.
func keyGateRequest(t *testing.T, target, peer, bearer string, cookies []*http.Cookie) *http.Request {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	t.Cleanup(cancel)
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	req.RemoteAddr = peer
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	return req
}

// TestInitServer_LocalKeyGate_IdentityDoesNotReplaceKeyFromAnotherHost drives
// the assembled server stack of a local instance. A valid token for the
// instance's audience (the OAuth client id by default) and a session cookie
// the instance itself minted both carry an identity, and from a peer on
// another host neither one stands in for the server key, on the JSON API and
// on the streaming paths alike. From this machine both keep passing, which
// also proves the token and the cookie are accepted as identities, so the
// 401s are the gate's answer and not a failed validation.
func TestInitServer_LocalKeyGate_IdentityDoesNotReplaceKeyFromAnotherHost(t *testing.T) {
	signer, jwks := mintRSAKeyAndJWKS(t)
	isolateKeyGateEnv(t)
	sc, configDir := runServerOn(t, 0, "AUTH_JWKS_URL="+jwks.URL+"\n")
	key := readServerAPIKey(configDir)
	if key == "" {
		t.Fatal("local instance started without a server key")
	}
	token := signJWTFor(t, signer, "u-other", "wallfacer", time.Now().Add(time.Hour))
	cookies := sessionCookies(t, 0, configDir)

	tests := []struct {
		name    string
		target  string
		peer    string
		bearer  string
		cookies []*http.Cookie
		want    int
	}{
		{"token, another host", "/api/config", keyGateRemotePeer, token, nil, http.StatusUnauthorized},
		{"token, another host, terminal", "/api/terminal/ws", keyGateRemotePeer, token, nil, http.StatusUnauthorized},
		{"token, another host, task stream", "/api/tasks/stream", keyGateRemotePeer, token, nil, http.StatusUnauthorized},
		{"session, another host", "/api/config", keyGateRemotePeer, "", cookies, http.StatusUnauthorized},
		{"session, another host, terminal", "/api/terminal/ws", keyGateRemotePeer, "", cookies, http.StatusUnauthorized},
		{"server key, another host", "/api/config", keyGateRemotePeer, key, nil, http.StatusOK},
		{"server key and session, another host", "/api/config", keyGateRemotePeer, key, cookies, http.StatusOK},
		{"token, this machine", "/api/config", keyGateLoopbackPeer, token, nil, http.StatusOK},
		{"session, this machine", "/api/config", keyGateLoopbackPeer, "", cookies, http.StatusOK},
		{"nothing, this machine", "/api/config", keyGateLoopbackPeer, "", nil, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			w := serve(sc, keyGateRequest(t, tc.target, tc.peer, tc.bearer, tc.cookies))
			if w.Code != tc.want {
				t.Errorf("GET %s with %s: status = %d, want %d; body %s", tc.target, tc.name, w.Code, tc.want, w.Body.String())
			}
		})
	}
}

// TestInitServer_CloudKeyGate_IdentityPassesFromAnyHost covers cloud mode,
// where sign-in is forced and data is scoped per principal: a valid token
// carries a peer on another host past a configured server key, which stays
// the credential for scripts.
func TestInitServer_CloudKeyGate_IdentityPassesFromAnyHost(t *testing.T) {
	signer, jwks := mintRSAKeyAndJWKS(t)
	isolateKeyGateEnv(t)
	const key = "cloud-script-key-0123456789abcdef0123456789abcdef"
	sc, _ := runServerOn(t, 0, "WALLFACER_CLOUD=true\nWALLFACER_SERVER_API_KEY="+key+"\nAUTH_JWKS_URL="+jwks.URL+"\n")
	token := signJWTFor(t, signer, "u-cloud", "wallfacer", time.Now().Add(time.Hour))

	if w := serve(sc, keyGateRequest(t, "/api/config", keyGateRemotePeer, token, nil)); w.Code != http.StatusOK {
		t.Errorf("GET /api/config with a token from another host: status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	if w := serve(sc, keyGateRequest(t, "/api/config", keyGateRemotePeer, key, nil)); w.Code != http.StatusOK {
		t.Errorf("GET /api/config with the server key from another host: status = %d, want 200; body %s", w.Code, w.Body.String())
	}
	if w := serve(sc, keyGateRequest(t, "/api/config", keyGateRemotePeer, "", nil)); w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/config with nothing from another host: status = %d, want 401; body %s", w.Code, w.Body.String())
	}
}
