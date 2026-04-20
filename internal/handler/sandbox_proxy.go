package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/auth"
)

// SandboxProxyConfig is everything the three trust-plane endpoints
// need from the environment. Populated by the CLI boot path from
// SANDBOX_PROXY_* env vars; left zero in local mode (handlers return
// 503 when disabled).
type SandboxProxyConfig struct {
	// Enabled flips the routes on. When false, handlers 503 with a
	// configuration-error message.
	Enabled bool
	// AuthInstallationTokenURL is auth's
	// /internal/github/installation-token endpoint. Wallfacer calls
	// this to mint a per-repo GitHub App installation token for the
	// sidecar's git-credential request.
	AuthInstallationTokenURL string
	// AuthServiceToken is wallfacer's long-lived JWT (scope
	// github:mint-token) used as the Bearer when calling the URL
	// above.
	AuthServiceToken string
	// AnthropicKey / OpenAIKey are the upstream API keys the trust
	// plane substitutes for the sandbox's inbound placeholder
	// Authorization. v1 shares a single org-level key per provider;
	// per-user keys can layer on once the store carries them.
	AnthropicKey string
	OpenAIKey    string
}

// LoadSandboxProxyConfig reads SANDBOX_PROXY_* env vars. Absent vars
// leave the field zero; Enabled is true only when every required
// field is set.
func LoadSandboxProxyConfig() SandboxProxyConfig {
	cfg := SandboxProxyConfig{
		AuthInstallationTokenURL: os.Getenv("SANDBOX_PROXY_AUTH_INSTALLATION_URL"),
		AuthServiceToken:         os.Getenv("SANDBOX_PROXY_AUTH_SERVICE_TOKEN"),
		AnthropicKey:             os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIKey:                os.Getenv("OPENAI_API_KEY"),
	}
	cfg.Enabled = cfg.AuthInstallationTokenURL != "" &&
		cfg.AuthServiceToken != "" &&
		(cfg.AnthropicKey != "" || cfg.OpenAIKey != "")
	return cfg
}

// SandboxProxy is constructed once by the CLI boot path and holds the
// HTTP client plus config. The three trust-plane routes are methods
// on this struct.
type SandboxProxy struct {
	Cfg    SandboxProxyConfig
	Client *http.Client
	// validator validates the inbound sandbox JWT. Nil disables
	// validation (local / test). The JWT is issued by auth with
	// aud=wallfacer-sandbox-proxy; we additionally require one of
	// scp=llm:proxy / scp=github:token per route.
	Validator *auth.Validator
}

// NewSandboxProxy constructs a trust-plane proxy from config and an
// optional JWT validator. When the validator is nil (local mode) the
// routes skip JWT checks; callers still rely on cfg.Enabled to 503
// requests until credentials are configured.
func NewSandboxProxy(cfg SandboxProxyConfig, v *auth.Validator) *SandboxProxy {
	return &SandboxProxy{
		Cfg:       cfg,
		Client:    &http.Client{Timeout: 5 * time.Minute},
		Validator: v,
	}
}

// LLMAnthropic handles POST /internal/sandbox-proxy/llm/anthropic/...
func (p *SandboxProxy) LLMAnthropic(w http.ResponseWriter, r *http.Request) {
	p.forwardLLM(w, r, "https://api.anthropic.com", p.Cfg.AnthropicKey,
		"/internal/sandbox-proxy/llm/anthropic", "llm:proxy",
		func(req *http.Request) {
			// Anthropic uses x-api-key, not Authorization.
			req.Header.Set("x-api-key", p.Cfg.AnthropicKey)
			req.Header.Set("anthropic-version", r.Header.Get("anthropic-version"))
			req.Header.Del("Authorization")
		})
}

// LLMOpenAI handles POST /internal/sandbox-proxy/llm/openai/...
func (p *SandboxProxy) LLMOpenAI(w http.ResponseWriter, r *http.Request) {
	p.forwardLLM(w, r, "https://api.openai.com", p.Cfg.OpenAIKey,
		"/internal/sandbox-proxy/llm/openai", "llm:proxy",
		func(req *http.Request) {
			req.Header.Set("Authorization", "Bearer "+p.Cfg.OpenAIKey)
		})
}

// GitHubToken handles GET /internal/sandbox-proxy/github-token?repo=owner/name.
// Returns a per-repo installation token minted by auth. The sidecar
// wraps the response in git credential helper format locally.
func (p *SandboxProxy) GitHubToken(w http.ResponseWriter, r *http.Request) {
	if !p.Cfg.Enabled {
		http.Error(w, "sandbox proxy disabled", http.StatusServiceUnavailable)
		return
	}
	claims, ok := p.requireClaims(w, r, "github:token")
	if !ok {
		return
	}
	repo := r.URL.Query().Get("repo")
	if repo == "" || !strings.Contains(repo, "/") {
		http.Error(w, "repo=owner/name required", http.StatusBadRequest)
		return
	}

	// Find the caller's installation that covers this repo. The
	// claims carry the delegating user sub in act.sub (RFC 8693); v1
	// always asks auth to resolve by principal+repo so we don't need
	// to maintain our own installation table.
	userSub := delegatorSub(claims)
	if userSub == "" {
		http.Error(w, "token lacks act.sub", http.StatusForbidden)
		return
	}

	// Auth's endpoint takes installation_id; we don't have it here.
	// Resolve by calling auth with principal+repo. Auth owns the
	// github_app_installations table and picks the right row.
	target, err := url.Parse(p.Cfg.AuthInstallationTokenURL)
	if err != nil {
		http.Error(w, "bad SANDBOX_PROXY_AUTH_INSTALLATION_URL", http.StatusInternalServerError)
		return
	}
	q := target.Query()
	q.Set("principal", userSub)
	q.Set("repo", repo)
	target.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target.String(), nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	req.Header.Set("Authorization", "Bearer "+p.Cfg.AuthServiceToken)

	resp, err := p.Client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<14))
		http.Error(w, string(b), resp.StatusCode)
		return
	}
	// Pass through the JSON body verbatim (creds-proxy knows the shape).
	w.Header().Set("Content-Type", "application/json")
	_, _ = io.Copy(w, resp.Body)
}

// ---- internals ----

func (p *SandboxProxy) forwardLLM(
	w http.ResponseWriter,
	r *http.Request,
	upstream string,
	key string,
	trim string,
	scope string,
	mutateReq func(*http.Request),
) {
	if !p.Cfg.Enabled {
		http.Error(w, "sandbox proxy disabled", http.StatusServiceUnavailable)
		return
	}
	if _, ok := p.requireClaims(w, r, scope); !ok {
		return
	}
	if key == "" {
		http.Error(w, "provider key not configured", http.StatusServiceUnavailable)
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, trim)
	target, err := url.Parse(upstream + tail)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	target.RawQuery = r.URL.RawQuery

	req, err := http.NewRequestWithContext(r.Context(), r.Method, target.String(), r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Copy safe headers from the caller, then let the mutate hook
	// overwrite credentials.
	for k, vs := range r.Header {
		switch strings.ToLower(k) {
		case "host", "content-length",
			"connection", "proxy-connection", "keep-alive",
			"transfer-encoding", "upgrade", "trailer", "te":
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	mutateReq(req)

	resp, err := p.Client.Do(req)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	// Flush chunks for streaming responses.
	if flusher, ok := w.(http.Flusher); ok {
		buf := make([]byte, 4096)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
				flusher.Flush()
			}
			if rerr != nil {
				return
			}
		}
	}
	_, _ = io.Copy(w, resp.Body)
}

// requireClaims validates the inbound JWT and the required scope.
// When validator is nil (local / test) it accepts the request as
// anonymous-but-authorized; handlers still need to check the other
// side's Enabled flag before doing work.
func (p *SandboxProxy) requireClaims(w http.ResponseWriter, r *http.Request, scope string) (*auth.Claims, bool) {
	if p.Validator == nil {
		return &auth.Claims{}, true
	}
	tok, ok := bearerToken(r.Header.Get("Authorization"))
	if !ok {
		http.Error(w, "missing bearer", http.StatusUnauthorized)
		return nil, false
	}
	claims, err := p.Validator.Validate(tok)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return nil, false
	}
	if !hasAud(claims.Aud, "wallfacer-sandbox-proxy") {
		http.Error(w, "aud mismatch", http.StatusForbidden)
		return nil, false
	}
	if !hasScope(claims.Scopes, scope) {
		http.Error(w, fmt.Sprintf("missing scope %s", scope), http.StatusForbidden)
		return nil, false
	}
	return claims, true
}

func bearerToken(h string) (string, bool) {
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(p):])
	return tok, tok != ""
}

func hasScope(scopes []string, want string) bool {
	for _, s := range scopes {
		if s == want {
			return true
		}
	}
	return false
}

func hasAud(aud []string, want string) bool {
	for _, a := range aud {
		if a == want {
			return true
		}
	}
	return false
}

func delegatorSub(c *auth.Claims) string {
	if c == nil {
		return ""
	}
	if c.Act != nil && c.Act.Sub != "" {
		return c.Act.Sub
	}
	// Fallback: when act is absent, the principal IS the user.
	return c.Sub
}

// Compile-time guard so future refactors keep JSON import wired.
var _ = json.Marshal
