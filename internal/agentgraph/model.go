package agentgraph

import (
	"context"

	"latere.ai/x/topos"
	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/flow"
)

// ModelMode selects how an agent-graph run reaches a model. It is the
// wallfacer-side, topos-free mirror of topos.ModelKind: the host configures a
// ModelConfig in these terms and the seam maps it onto topos.ModelOptions, so no
// wallfacer package outside this seam names a topos model type.
type ModelMode string

const (
	// ModelModeFake is the deterministic, network-free model. It is the zero
	// value, so an unconfigured ModelConfig falls back to fake.
	ModelModeFake ModelMode = ""
	// ModelModeLux reaches a provider through Lux (the model gateway): BaseURL
	// points at a Lux endpoint and APIKey is a Lux virtual key.
	ModelModeLux ModelMode = "lux"
	// ModelModeDirect talks to a provider endpoint directly (BYO key).
	ModelModeDirect ModelMode = "direct"
)

// ModelConfig is wallfacer's topos-free description of the model an agentic run
// should use. The runner derives it from wallfacer's existing credential
// settings (the .env file: ANTHROPIC_API_KEY / ANTHROPIC_BASE_URL /
// CLAUDE_DEFAULT_MODEL) and hands it to the seam; only the seam turns it into a
// topos.ModelOptions. The zero value selects the fake model.
//
// Only a static APIKey (the x-api-key credential, e.g. a Lux "lux_*" virtual
// key) is wired for M4. Bearer-style credentials (ANTHROPIC_AUTH_TOKEN gateway
// tokens, CLAUDE_CODE_OAUTH_TOKEN) need a per-call BearerSource and are deferred.
type ModelConfig struct {
	Mode     ModelMode
	Provider string // "anthropic" (anthropic-wire); empty defaults to anthropic in topos
	Model    string // model id, e.g. "claude-sonnet-4-6"
	BaseURL  string // e.g. "https://lux.latere.ai/anthropic"
	APIKey   string // x-api-key credential (Lux virtual key or a direct provider key)
}

// hasCredential reports whether the config carries a usable static credential.
// Without one a Lux/Direct run cannot authenticate, so the mapping degrades to
// the fake model rather than building a guaranteed-401 real adapter.
func (c ModelConfig) hasCredential() bool { return c.APIKey != "" }

// modelOptions maps a topos-free ModelConfig onto topos.ModelOptions. It is the
// single place that names topos.ModelOptions. The fallback to ModelFake is
// centralized here: an unconfigured config (Mode fake / zero value) and a
// real-mode config missing a credential both produce the fake model, so callers
// never need to pre-check before running.
func modelOptions(c ModelConfig) topos.ModelOptions {
	if !c.hasCredential() {
		return topos.ModelOptions{Kind: topos.ModelFake}
	}
	switch c.Mode {
	case ModelModeLux:
		return topos.ModelOptions{
			Kind:     topos.ModelLux,
			Provider: c.Provider,
			Model:    c.Model,
			BaseURL:  c.BaseURL,
			APIKey:   c.APIKey,
		}
	case ModelModeDirect:
		return topos.ModelOptions{
			Kind:     topos.ModelDirect,
			Provider: c.Provider,
			Model:    c.Model,
			BaseURL:  c.BaseURL,
			APIKey:   c.APIKey,
		}
	default: // ModelModeFake or any unknown mode
		return topos.ModelOptions{Kind: topos.ModelFake}
	}
}

// ModelOptions builds the topos model options a ModelConfig selects, exposed so
// the mapping can be asserted without running a model. It returns a
// topos.ModelOptions value: a caller can read its fields (Kind, BaseURL, APIKey)
// structurally without importing topos, keeping this seam the only place that
// names the topos type.
func ModelOptions(c ModelConfig) topos.ModelOptions { return modelOptions(c) }

// runOptions builds the topos.Options for an agentic run from the session id,
// the model config, and the flow. It is the single place that names
// topos.Options: the model selection comes from the config and the recursion
// bound (MaxHandoffDepth) rides on the flow, so a zero flow depth passes 0 and
// the topos runner applies its own default (3).
func runOptions(sessionID string, c ModelConfig, f flow.Flow) topos.Options {
	return topos.Options{
		SessionID:       sessionID,
		Model:           modelOptions(c),
		MaxHandoffDepth: f.MaxHandoffDepth,
	}
}

// RunOptions builds the topos.Options a run will use, exposed so the mapping
// (notably MaxHandoffDepth threading) can be asserted without running a model.
// A caller can read the returned value's fields structurally without importing
// topos, keeping this seam the only place that names the topos type.
func RunOptions(sessionID string, c ModelConfig, f flow.Flow) topos.Options {
	return runOptions(sessionID, c, f)
}

// RunFlowWithModel runs a flow through the agent-graph runtime using the model
// the config selects, returning a topos-free Result. When the config carries no
// credential it transparently uses the deterministic fake model, so tests and
// no-credential dev keep working. sessionID seeds the run id so lineage node ids
// (<session>/<agent>) are stable.
//
// OQ-1 (sandbox) is resolved minimally for M4: Options.Sandbox is left nil, so a
// run uses the topos local sandbox. Sharing wallfacer's executor.Backend through
// a topos.Sandbox adapter is future work.
func RunFlowWithModel(ctx context.Context, sessionID string, c ModelConfig, f flow.Flow, reg *agents.Registry, prompt string) (Result, error) {
	res, err := RunFlow(ctx, runOptions(sessionID, c, f), f, reg, prompt)
	if err != nil {
		return Result{}, err
	}
	return toResult(res), nil
}
