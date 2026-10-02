package agentgraph

import (
	"errors"
	"fmt"

	"latere.ai/x/topos"
)

// ErrNoModelCredential reports that a ModelConfig carries no model credential.
// RunAgent returns it before a runner is built, so a run without a credential
// never starts and never touches its working directory.
var ErrNoModelCredential = errors.New("agentgraph: model config carries no credential")

// ModelMode selects how an in-process run reaches a model. It is the
// wallfacer-side, topos-free mirror of topos.ModelKind: the host configures a
// ModelConfig in these terms and the seam maps it onto topos.ModelOptions, so no
// wallfacer package outside this seam names a topos model type. The zero value
// selects nothing: a config that leaves Mode unset is refused.
type ModelMode string

const (
	// ModelModeLux reaches a provider through Lux (the model gateway): BaseURL
	// points at a Lux endpoint and APIKey is a Lux virtual key.
	ModelModeLux ModelMode = "lux"
	// ModelModeDirect talks to a provider endpoint directly (BYO key).
	ModelModeDirect ModelMode = "direct"
	// ModelModeFake selects the runtime's deterministic, network-free test
	// model. It exists for tests and for exercising the seam without a
	// credential, and it is reachable only by naming it: no other mode and no
	// missing credential resolves to it, and the runner's production wiring
	// never sets it.
	ModelModeFake ModelMode = "fake"
)

// ModelConfig is wallfacer's topos-free description of the model an in-process
// run should use. The runner derives it from wallfacer's existing credential
// settings (the .env file: ANTHROPIC_API_KEY / ANTHROPIC_BASE_URL /
// CLAUDE_DEFAULT_MODEL) and hands it to the seam; only the seam turns it into a
// topos.ModelOptions. The zero value is not runnable: it yields
// ErrNoModelCredential.
//
// Only a static APIKey (the x-api-key credential, e.g. a Lux "lux_*" virtual
// key) is wired for M4. Bearer-style credentials (ANTHROPIC_AUTH_TOKEN gateway
// tokens, CLAUDE_CODE_OAUTH_TOKEN) need a per-call BearerSource and are deferred.
type ModelConfig struct {
	Mode     ModelMode
	Provider string // "anthropic" (anthropic-wire); empty defaults to anthropic in topos
	Model    string // model id, e.g. "claude-sonnet-4-6"
	BaseURL  string // gateway root in Lux mode, e.g. "https://api.latere.ai/v1/models"
	APIKey   string // x-api-key credential (Lux virtual key or a direct provider key)
}

// modelOptions maps a topos-free ModelConfig onto topos.ModelOptions. It is the
// single place that names topos.ModelOptions. A config must either name the test
// model explicitly (ModelModeFake) or carry a credential for a real mode: a
// config without a credential returns ErrNoModelCredential, and a credential
// under an unset or unknown mode is an error too, so no input degrades to the
// test model.
func modelOptions(c ModelConfig) (topos.ModelOptions, error) {
	if c.Mode == ModelModeFake {
		return topos.ModelOptions{Kind: topos.ModelFake}, nil
	}
	if c.APIKey == "" {
		return topos.ModelOptions{}, fmt.Errorf("%w (mode %q)", ErrNoModelCredential, c.Mode)
	}
	switch c.Mode {
	case ModelModeLux:
		return topos.ModelOptions{
			Kind:     topos.ModelLux,
			Provider: c.Provider,
			Model:    c.Model,
			BaseURL:  c.BaseURL,
			APIKey:   c.APIKey,
		}, nil
	case ModelModeDirect:
		return topos.ModelOptions{
			Kind:     topos.ModelDirect,
			Provider: c.Provider,
			Model:    c.Model,
			BaseURL:  c.BaseURL,
			APIKey:   c.APIKey,
		}, nil
	default:
		return topos.ModelOptions{}, fmt.Errorf("agentgraph: unknown model mode %q", c.Mode)
	}
}

// runOptions builds the topos.Options for an in-process run from the session
// id and the model config. It is the single place that names topos.Options.
// sessionID seeds the run id so trace node ids (<session>/<agent>) are stable.
// It returns modelOptions' error for a config that selects no model.
func runOptions(sessionID string, c ModelConfig) (topos.Options, error) {
	model, err := modelOptions(c)
	if err != nil {
		return topos.Options{}, err
	}
	return topos.Options{
		SessionID: sessionID,
		Model:     model,
	}, nil
}

// toEvent converts a topos.Event into the topos-free Event. Node is the
// event's topos SessionID, which equals the emitting agent's trace node id.
func toEvent(e topos.Event) Event {
	return Event{
		Name:        e.Name,
		Node:        e.SessionID,
		AgentID:     e.AgentID,
		At:          e.At,
		PayloadJSON: e.PayloadJSON,
	}
}
