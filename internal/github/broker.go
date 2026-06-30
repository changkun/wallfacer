package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// HTTPBroker is the live [Broker]: it obtains the principal's GitHub token from
// the latere.ai auth service's self endpoint (GET /me/integrations/github/token)
// using the signed-in user's bearer token. This is the local/single-user path
// (the user's own token authorizes; no service scope needed). The principal
// argument is implicit in the bearer token, so it is not sent.
type HTTPBroker struct {
	// AuthBaseURL is the auth service root (e.g. https://auth.latere.ai).
	AuthBaseURL string
	// TokenSource returns the current user bearer token to authenticate to auth.
	TokenSource func(context.Context) (string, error)
	// HTTP is the client used; nil means a 15s-timeout default.
	HTTP *http.Client
}

func (b *HTTPBroker) httpClient() *http.Client {
	if b.HTTP != nil {
		return b.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Token fetches and maps the brokered GitHub credential. A missing/expired user
// session or an unconnected principal both surface as [ErrNotConnected] so the
// UI prompts to connect.
func (b *HTTPBroker) Token(ctx context.Context, _ Principal) (*Token, error) {
	if b.AuthBaseURL == "" || b.TokenSource == nil {
		return nil, ErrNotConnected
	}
	bearer, err := b.TokenSource(ctx)
	if err != nil || bearer == "" {
		return nil, ErrNotConnected
	}
	url := strings.TrimRight(b.AuthBaseURL, "/") + "/me/integrations/github/token"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("github: build broker request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	req.Header.Set("Accept", "application/json")

	resp, err := b.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: broker request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	switch resp.StatusCode {
	case http.StatusOK:
		// fall through to decode
	case http.StatusNotFound, http.StatusUnauthorized, http.StatusServiceUnavailable:
		// 404: principal not connected. 401: user session invalid -> reconnect.
		// 503: brokering not configured on auth. All -> prompt to connect.
		return nil, ErrNotConnected
	default:
		return nil, fmt.Errorf("github: broker status %d: %s", resp.StatusCode, body)
	}

	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresAt   string `json:"expires_at"`
		Login       string `json:"login"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("github: decode broker token: %w", err)
	}
	if out.AccessToken == "" {
		return nil, ErrNotConnected
	}
	tok := &Token{AccessToken: out.AccessToken, Login: out.Login}
	if out.ExpiresAt != "" {
		if exp, perr := time.Parse(time.RFC3339, out.ExpiresAt); perr == nil {
			tok.Expiry = exp
		}
	}
	return tok, nil
}

// ensure HTTPBroker satisfies Broker.
var _ Broker = (*HTTPBroker)(nil)
