package cli

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"latere.ai/x/pkg/bearer"

	"latere.ai/x/wallfacer/internal/auth"
)

// serverAPIKeyFile is the file under the config dir that holds the server
// API key a local instance generates when WALLFACER_SERVER_API_KEY is unset.
// Same-machine clients (`wallfacer status`, the e2e scripts) read it from
// there; the browser gets it injected into index.html on a loopback request.
const serverAPIKeyFile = "server-api-key"

// loadOrCreateServerAPIKey returns the persisted local-mode server API key,
// generating a 32-byte random one on first use. Stored outside the .env file
// on purpose: the .env is handed to agent processes, and the key gating the
// API that supervises those agents must not travel with them.
func loadOrCreateServerAPIKey(configDir string) (string, error) {
	return loadOrCreateSecret(filepath.Join(configDir, serverAPIKeyFile), "server api key")
}

// readServerAPIKey returns the key a same-machine client presents to the
// local server: WALLFACER_SERVER_API_KEY from the environment when set, else
// the generated key file, else "" (server started with no key, e.g. cloud).
func readServerAPIKey(configDir string) string {
	if k := strings.TrimSpace(os.Getenv("WALLFACER_SERVER_API_KEY")); k != "" {
		return k
	}
	b, err := os.ReadFile(filepath.Join(configDir, serverAPIKeyFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// loadOrCreateSecret returns the hex-encoded 32-byte secret stored at path,
// generating and persisting one (mode 0600) when the file is absent or too
// short. label names the secret in errors.
func loadOrCreateSecret(path, label string) (string, error) {
	if b, err := os.ReadFile(path); err == nil {
		if k := strings.TrimSpace(string(b)); len(k) >= 32 {
			return k, nil
		}
	}
	raw := make([]byte, 32)
	if _, err := cryptorand.Read(raw); err != nil {
		return "", err
	}
	key := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
		return "", fmt.Errorf("persist %s: %w", label, err)
	}
	return key, nil
}

// indexKeyAllowed reports whether the index.html served for r may carry the
// server API key. GET / is deliberately public so the SPA shell loads, which
// makes the injected key the one secret an anonymous caller could read; it is
// released only to a caller that already holds it (Authorization: Bearer or
// ?token=, the same forms BearerAuthMiddleware accepts) or that connects from
// this machine. RemoteAddr is the TCP peer, never a forwarded header, so a
// reverse proxy in front of the server does not make every client loopback.
func indexKeyAllowed(r *http.Request, key string) bool {
	if key == "" {
		return false
	}
	if tok, ok := auth.BearerToken(r.Header.Get("Authorization")); ok && bearer.Equal(tok, key) {
		return true
	}
	if tok := r.URL.Query().Get("token"); tok != "" && bearer.Equal(tok, key) {
		return true
	}
	return isLoopbackRemote(r.RemoteAddr)
}

// isLoopbackRemote reports whether a host:port peer address is a loopback IP.
func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
