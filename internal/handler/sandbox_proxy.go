package handler

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"latere.ai/x/pkg/authkit/jwt"
	"latere.ai/x/pkg/authkit/oidc"
	"latere.ai/x/pkg/otel"

	"latere.ai/x/wallfacer/internal/auth"
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
	// AuthURL is the issuer wallfacer asks for its own service token, the
	// credential it presents at the URL above. ClientID and ClientSecret
	// are the confidential client registered for that: the token is minted
	// with the client_credentials grant, scope github:mint-token, and
	// re-minted before it expires (rule R5: a service acting as itself
	// holds a service token, never a long-lived static one).
	AuthURL      string
	ClientID     string
	ClientSecret string
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
		AuthURL:                  os.Getenv("SANDBOX_PROXY_AUTH_URL"),
		ClientID:                 os.Getenv("SANDBOX_PROXY_CLIENT_ID"),
		ClientSecret:             os.Getenv("SANDBOX_PROXY_CLIENT_SECRET"),
		AnthropicKey:             os.Getenv("ANTHROPIC_API_KEY"),
		OpenAIKey:                os.Getenv("OPENAI_API_KEY"),
	}
	cfg.Enabled = cfg.AuthInstallationTokenURL != "" &&
		cfg.AuthURL != "" && cfg.ClientID != "" && cfg.ClientSecret != "" &&
		(cfg.AnthropicKey != "" || cfg.OpenAIKey != "")
	return cfg
}

// SandboxProxy is constructed once by the CLI boot path and holds the
// HTTP client plus config. The three trust-plane routes are methods
// on this struct.
type SandboxProxy struct {
	Cfg    SandboxProxyConfig
	Client *http.Client
	// Tokens hands out wallfacer's own service token for the issuer's
	// installation-token endpoint, minted through client_credentials and
	// reused until it nears expiry. Tests substitute a fixed source.
	Tokens TokenSource
	// Validator validates the inbound sandbox JWT. The JWT is issued
	// by auth with aud=wallfacer-sandbox-proxy; we additionally
	// require one of scp=llm:proxy / scp=github:token per route. Nil
	// means no validator is configured: an enabled proxy then rejects
	// every request (fail closed).
	Validator *jwt.Validator
}

// TokenSource is what hands the proxy its own credential per call.
type TokenSource interface {
	Token(ctx context.Context) (string, error)
}

// NewSandboxProxy constructs a trust-plane proxy from config and a
// JWT validator. A nil validator does not skip JWT checks: an enabled
// proxy without a validator rejects every request (fail closed).
// Local runs without credentials keep cfg.Enabled false and 503
// before any JWT check.
func NewSandboxProxy(cfg SandboxProxyConfig, v *jwt.Validator) *SandboxProxy {
	var tokens TokenSource
	if cfg.ClientID != "" && cfg.ClientSecret != "" && cfg.AuthURL != "" {
		tokens = oidc.NewServiceTokenSource(cfg.AuthURL, cfg.ClientID, cfg.ClientSecret, "", []string{"github:mint-token"})
	}
	return &SandboxProxy{
		Tokens:    tokens,
		Cfg:       cfg,
		Client:    &http.Client{Timeout: 5 * time.Minute, Transport: otel.Transport(nil)},
		Validator: v,
	}
}

// llmEndpoint is one upstream inference endpoint the trust plane
// forwards: the method and the exact path under the provider root.
type llmEndpoint struct {
	method string
	path   string
}

// The proxy substitutes the org's provider key, so the reachable
// surface is pinned to inference. Everything else on the provider API
// (files, fine-tuning, batches, admin, billing) is refused: a sandbox
// must not be able to upload data, spend on training, or read account
// state with a key it never holds.
var (
	anthropicEndpoints = []llmEndpoint{
		{http.MethodPost, "/v1/messages"},
		{http.MethodPost, "/v1/messages/count_tokens"},
	}
	openaiEndpoints = []llmEndpoint{
		{http.MethodPost, "/v1/chat/completions"},
		{http.MethodPost, "/v1/responses"},
		{http.MethodPost, "/v1/embeddings"},
		{http.MethodGet, "/v1/models"},
	}
)

// allowedLLMEndpoint reports whether method+tail names an allowlisted
// endpoint. Exact match only: no prefixes, no trailing slash, so
// /v1/messages/../files cannot be smuggled past the list (ServeMux has
// already cleaned the path by the time it reaches the handler).
func allowedLLMEndpoint(list []llmEndpoint, method, tail string) bool {
	return slices.Contains(list, llmEndpoint{method: method, path: tail})
}

// LLMAnthropic handles /internal/sandbox-proxy/llm/anthropic/... for
// the endpoints in anthropicEndpoints.
func (p *SandboxProxy) LLMAnthropic(w http.ResponseWriter, r *http.Request) {
	p.forwardLLM(w, r, "https://api.anthropic.com", p.Cfg.AnthropicKey,
		"/internal/sandbox-proxy/llm/anthropic", "llm:proxy", anthropicEndpoints,
		func(req *http.Request) {
			// Anthropic uses x-api-key, not Authorization.
			req.Header.Set("x-api-key", p.Cfg.AnthropicKey)
			req.Header.Set("anthropic-version", r.Header.Get("anthropic-version"))
			req.Header.Del("Authorization")
		})
}

// LLMOpenAI handles /internal/sandbox-proxy/llm/openai/... for the
// endpoints in openaiEndpoints.
func (p *SandboxProxy) LLMOpenAI(w http.ResponseWriter, r *http.Request) {
	p.forwardLLM(w, r, "https://api.openai.com", p.Cfg.OpenAIKey,
		"/internal/sandbox-proxy/llm/openai", "llm:proxy", openaiEndpoints,
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

	// Find the caller's installation that covers this repo. v1 always asks
	// auth to resolve by principal+repo so we don't need to maintain our own
	// installation table.
	userSub := callerSub(claims)
	if userSub == "" {
		http.Error(w, "token lacks sub", http.StatusForbidden)
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
	if p.Tokens == nil {
		http.Error(w, "sandbox proxy has no service credential", http.StatusServiceUnavailable)
		return
	}
	serviceToken, err := p.Tokens.Token(r.Context())
	if err != nil {
		http.Error(w, "service token: "+err.Error(), http.StatusBadGateway)
		return
	}
	req.Header.Set("Authorization", "Bearer "+serviceToken)

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
	allowed []llmEndpoint,
	mutateReq func(*http.Request),
) {
	if !p.Cfg.Enabled {
		http.Error(w, "sandbox proxy disabled", http.StatusServiceUnavailable)
		return
	}
	if _, ok := p.requireClaims(w, r, scope); !ok {
		return
	}
	tail := strings.TrimPrefix(r.URL.Path, trim)
	if !allowedLLMEndpoint(allowed, r.Method, tail) {
		http.Error(w, "endpoint not proxied", http.StatusNotFound)
		return
	}
	if key == "" {
		http.Error(w, "provider key not configured", http.StatusServiceUnavailable)
		return
	}
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
// A nil validator fails closed: the proxy cannot establish who is
// calling, so it rejects instead of accepting the request as
// anonymous-but-authorized.
func (p *SandboxProxy) requireClaims(w http.ResponseWriter, r *http.Request, scope string) (*jwt.Claims, bool) {
	if p.Validator == nil {
		http.Error(w, "sandbox proxy JWT validator not configured", http.StatusServiceUnavailable)
		return nil, false
	}
	tok, ok := auth.BearerToken(r.Header.Get("Authorization"))
	if !ok {
		http.Error(w, "missing bearer", http.StatusUnauthorized)
		return nil, false
	}
	claims, err := p.Validator.Validate(tok)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnauthorized)
		return nil, false
	}
	if !slices.Contains(claims.Aud, "wallfacer-sandbox-proxy") {
		http.Error(w, "aud mismatch", http.StatusForbidden)
		return nil, false
	}
	if !slices.Contains(claims.Scopes, scope) {
		http.Error(w, fmt.Sprintf("missing scope %s", scope), http.StatusForbidden)
		return nil, false
	}
	return claims, true
}

// callerSub is the user a proxied call acts for. It used to resolve an RFC
// 8693 delegator first, so a delegated agent's call attributed to its grantor;
// the identity service removed agent delegation and no longer mints a token
// carrying a delegator, so the principal IS the user.
func callerSub(c *jwt.Claims) string {
	if c == nil {
		return ""
	}
	return c.Sub
}
