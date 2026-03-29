package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// FlowState represents the state of an OAuth flow.
type FlowState string

// OAuth flow states.
const (
	FlowPending FlowState = "pending"
	FlowSuccess FlowState = "success"
	FlowError   FlowState = "error"
)

// FlowStatus is the externally visible status of an OAuth flow.
type FlowStatus struct {
	State FlowState `json:"state"`
	Error string    `json:"error,omitempty"`
}

// Flow holds the in-progress state of a single OAuth authorization attempt.
type Flow struct {
	mu       sync.Mutex
	provider Provider
	verifier string
	state    string // random state param for CSRF protection
	callback *CallbackServer
	cancel   context.CancelFunc
	status   FlowStatus
}

// TokenWriter is a callback invoked when a token is successfully obtained.
// It should persist the token (e.g., write to .env via envconfig.Update).
type TokenWriter func(envKey, token string) error

// Manager coordinates OAuth flows. At most one flow per provider at a time.
type Manager struct {
	mu          sync.Mutex
	flows       map[string]*Flow
	TokenWriter TokenWriter
	HTTPClient  *http.Client // for token exchange; nil uses http.DefaultClient
}

// NewManager creates a new OAuth flow manager.
func NewManager() *Manager {
	return &Manager{
		flows: make(map[string]*Flow),
	}
}

// Start begins an OAuth flow for the given provider. If a flow is already
// in progress for this provider, it is cancelled first. Returns the
// authorization URL the user should open in their browser.
func (m *Manager) Start(ctx context.Context, provider Provider) (string, error) {
	m.Cancel(provider.Name)

	verifier, err := GenerateCodeVerifier()
	if err != nil {
		return "", fmt.Errorf("generate verifier: %w", err)
	}
	challenge := S256Challenge(verifier)

	stateParam, err := GenerateState()
	if err != nil {
		return "", fmt.Errorf("generate state: %w", err)
	}

	flowCtx, flowCancel := context.WithTimeout(ctx, 5*time.Minute)

	cb, err := NewCallbackServer(flowCtx)
	if err != nil {
		flowCancel()
		return "", fmt.Errorf("start callback server: %w", err)
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/callback", cb.Port())

	// Build authorization URL.
	params := url.Values{
		"client_id":             {provider.ClientID},
		"redirect_uri":          {redirectURI},
		"response_type":         {"code"},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
		"state":                 {stateParam},
	}
	if len(provider.Scopes) > 0 {
		params.Set("scope", strings.Join(provider.Scopes, " "))
	}
	authorizeURL := provider.AuthorizeURL + "?" + params.Encode()

	flow := &Flow{
		provider: provider,
		verifier: verifier,
		state:    stateParam,
		callback: cb,
		cancel:   flowCancel,
		status:   FlowStatus{State: FlowPending},
	}

	m.mu.Lock()
	m.flows[provider.Name] = flow
	m.mu.Unlock()

	// Background goroutine: wait for callback, exchange token, update status.
	go m.runFlow(flow, redirectURI)

	return authorizeURL, nil
}

// Status returns the current status of the flow for a provider.
// If no flow exists, returns FlowError with a "no flow" message.
func (m *Manager) Status(providerName string) FlowStatus {
	m.mu.Lock()
	flow, ok := m.flows[providerName]
	m.mu.Unlock()
	if !ok {
		return FlowStatus{State: FlowError, Error: "no active flow"}
	}
	flow.mu.Lock()
	defer flow.mu.Unlock()
	return flow.status
}

// Cancel aborts the flow for a provider, if one is in progress.
func (m *Manager) Cancel(providerName string) {
	m.mu.Lock()
	flow, ok := m.flows[providerName]
	if ok {
		delete(m.flows, providerName)
	}
	m.mu.Unlock()
	if ok {
		flow.cancel()
		flow.callback.Close()
	}
}

func (m *Manager) runFlow(flow *Flow, redirectURI string) {
	result, err := flow.callback.Wait()
	if err != nil {
		flow.mu.Lock()
		flow.status = FlowStatus{State: FlowError, Error: "callback timeout or cancelled"}
		flow.mu.Unlock()
		return
	}

	if result.Error != "" {
		flow.mu.Lock()
		flow.status = FlowStatus{State: FlowError, Error: result.Error + ": " + result.ErrorDescription}
		flow.mu.Unlock()
		return
	}

	if result.State != flow.state {
		flow.mu.Lock()
		flow.status = FlowStatus{State: FlowError, Error: "state mismatch"}
		flow.mu.Unlock()
		return
	}

	client := m.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	token, err := exchangeToken(client, flow.provider, result.Code, flow.verifier, redirectURI)
	if err != nil {
		flow.mu.Lock()
		flow.status = FlowStatus{State: FlowError, Error: "token exchange failed: " + err.Error()}
		flow.mu.Unlock()
		return
	}

	// Write token via callback.
	if m.TokenWriter != nil {
		if err := m.TokenWriter(flow.provider.TokenEnvKey, token); err != nil {
			flow.mu.Lock()
			flow.status = FlowStatus{State: FlowError, Error: "failed to save token: " + err.Error()}
			flow.mu.Unlock()
			return
		}
	}

	flow.mu.Lock()
	flow.status = FlowStatus{State: FlowSuccess}
	flow.mu.Unlock()
}

// exchangeToken sends the authorization code to the token endpoint and
// returns the access token (or api_key) from the response.
func exchangeToken(client *http.Client, provider Provider, code, verifier, redirectURI string) (string, error) {
	data := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {redirectURI},
		"code_verifier": {verifier},
		"client_id":     {provider.ClientID},
	}

	resp, err := client.PostForm(provider.TokenURL, data)
	if err != nil {
		return "", fmt.Errorf("POST %s: %w", provider.TokenURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, body)
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("parse token response: %w", err)
	}

	// Try access_token first, then api_key (provider-dependent).
	for _, key := range []string{"access_token", "api_key"} {
		if v, ok := result[key].(string); ok && v != "" {
			return v, nil
		}
	}

	return "", fmt.Errorf("no access_token or api_key in response: %s", body)
}
