package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"latere.ai/x/pkg/authkit/cli"
)

// coordinationModeCases are the two modes the coordination dial side differs
// between: a local single-user instance and a cloud-mode deployment.
var coordinationModeCases = []struct {
	name  string
	env   string
	cloud bool
}{
	{"local instance", "# empty\n", false},
	{"cloud mode", "WALLFACER_CLOUD=true\n", true},
}

// runCoordinationModeServer boots the run tree with env as its env file and
// returns it with a session cookie the instance minted and the host's shared
// token store. The local instance's connector is kept from dialing the hosted
// coordinator, and the issuer is a local server that refuses every call, so a
// token mint never leaves the machine. The session cookie does not depend on
// the issuer: its key comes from the cookie key under the config directory.
func runCoordinationModeServer(t *testing.T, env string) (*ServerComponents, []*http.Cookie, cli.TokenStore) {
	t.Helper()
	isolateKeyGateEnv(t)
	t.Setenv("WALLFACER_COORDINATION", "0")
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "issuer unavailable in tests", http.StatusServiceUnavailable)
	}))
	t.Cleanup(issuer.Close)
	sc, configDir := runServerOn(t, 0, "AUTH_URL="+issuer.URL+"\n"+env)

	path, err := cli.DefaultFileTokenStorePath()
	if err != nil {
		t.Fatalf("token store path: %v", err)
	}
	store, err := cli.NewFileTokenStore(path)
	if err != nil {
		t.Fatalf("open token store: %v", err)
	}
	return sc, sessionCookies(t, 0, configDir), store
}

// storedAccessToken returns the access token the shared token store holds, or
// "" when it holds none.
func storedAccessToken(t *testing.T, store cli.TokenStore) string {
	t.Helper()
	tok, err := store.Load()
	if err != nil {
		t.Fatalf("load token store: %v", err)
	}
	if tok == nil {
		return ""
	}
	return tok.AccessToken
}

// TestInitServer_CoordinationDialSideRunsOnlyOutsideCloudMode drives the
// assembled server stack in both modes with a request that carries a session
// cookie the instance minted. On a local instance the session token bridge
// copies the browser's token into the host's shared token store, which the
// coordination connector and the latere command read, and the coordination
// toggle is wired. On a cloud-mode deployment, which serves many principals,
// no browser's session reaches that store and the connector does not run, so
// GET /api/coordination/status reports coordination unavailable.
func TestInitServer_CoordinationDialSideRunsOnlyOutsideCloudMode(t *testing.T) {
	for _, tc := range coordinationModeCases {
		t.Run(tc.name, func(t *testing.T) {
			sc, cookies, store := runCoordinationModeServer(t, tc.env)

			if w := serve(sc, keyGateRequest(t, "/api/config", keyGateLoopbackPeer, "", cookies)); w.Code != http.StatusOK {
				t.Fatalf("GET /api/config with a session: status = %d, want 200; body %s", w.Code, w.Body.String())
			}
			if stored := storedAccessToken(t, store) != ""; stored == tc.cloud {
				t.Errorf("shared token store holds the browser session's token = %v, want %v", stored, !tc.cloud)
			}

			w := serve(sc, keyGateRequest(t, "/api/coordination/status", keyGateLoopbackPeer, "", cookies))
			if w.Code != http.StatusOK {
				t.Fatalf("GET /api/coordination/status: status = %d, want 200; body %s", w.Code, w.Body.String())
			}
			var status struct {
				Available bool `json:"available"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
				t.Fatalf("decode coordination status: %v; body %s", err, w.Body.String())
			}
			if status.Available == tc.cloud {
				t.Errorf("coordination status available = %v, want %v; body %s", status.Available, !tc.cloud, w.Body.String())
			}
		})
	}
}

// TestInitServer_SignOutClearsSharedTokenStoreOnlyOutsideCloudMode covers the
// sign-out side of the same split. On a local instance signing out of the board
// clears the host's shared token store, so the connector stops; on a cloud-mode
// deployment a browser's sign-out leaves that store as it was.
func TestInitServer_SignOutClearsSharedTokenStoreOnlyOutsideCloudMode(t *testing.T) {
	for _, tc := range coordinationModeCases {
		t.Run(tc.name, func(t *testing.T) {
			sc, cookies, store := runCoordinationModeServer(t, tc.env)
			const hostToken = "host-sign-in"
			if err := store.Save(&oauth2.Token{AccessToken: hostToken, Expiry: time.Now().Add(time.Hour)}); err != nil {
				t.Fatalf("seed token store: %v", err)
			}

			w := serve(sc, keyGateRequest(t, "/logout", keyGateLoopbackPeer, "", cookies))
			if w.Code >= http.StatusBadRequest {
				t.Fatalf("GET /logout: status = %d, want a success or a redirect; body %s", w.Code, w.Body.String())
			}

			got := storedAccessToken(t, store)
			if tc.cloud && got != hostToken {
				t.Errorf("after a browser's sign-out the shared token store holds %q, want the host's %q", got, hostToken)
			}
			if !tc.cloud && got != "" {
				t.Errorf("after sign-out the shared token store holds %q, want it cleared", got)
			}
		})
	}
}
