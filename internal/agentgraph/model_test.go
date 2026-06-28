package agentgraph_test

import (
	"testing"

	"latere.ai/x/wallfacer/internal/agentgraph"
)

// TestModelOptions_Mapping asserts the ModelConfig -> topos.ModelOptions mapping
// without running a model. It reads the returned value's fields structurally, so
// the test never names a topos type (the agentgraph seam stays the only place
// that does). Kind is compared via its string form for the same reason.
func TestModelOptions_Mapping(t *testing.T) {
	const (
		luxURL = "https://lux.latere.ai/anthropic"
		key    = "lux_test_key"
		model  = "claude-sonnet-4-6"
	)

	t.Run("configured lux", func(t *testing.T) {
		opts := agentgraph.ModelOptions(agentgraph.ModelConfig{
			Mode:     agentgraph.ModelModeLux,
			Provider: "anthropic",
			Model:    model,
			BaseURL:  luxURL,
			APIKey:   key,
		})
		if got := string(opts.Kind); got != "lux" {
			t.Errorf("Kind = %q, want lux", got)
		}
		if opts.BaseURL != luxURL {
			t.Errorf("BaseURL = %q, want %q", opts.BaseURL, luxURL)
		}
		if opts.APIKey != key {
			t.Errorf("APIKey = %q, want %q", opts.APIKey, key)
		}
		if opts.Model != model {
			t.Errorf("Model = %q, want %q", opts.Model, model)
		}
		if opts.Provider != "anthropic" {
			t.Errorf("Provider = %q, want anthropic", opts.Provider)
		}
	})

	t.Run("configured direct", func(t *testing.T) {
		opts := agentgraph.ModelOptions(agentgraph.ModelConfig{
			Mode:   agentgraph.ModelModeDirect,
			APIKey: key,
		})
		if got := string(opts.Kind); got != "direct" {
			t.Errorf("Kind = %q, want direct", got)
		}
		if opts.APIKey != key {
			t.Errorf("APIKey = %q, want %q", opts.APIKey, key)
		}
	})

	t.Run("unconfigured falls back to fake", func(t *testing.T) {
		opts := agentgraph.ModelOptions(agentgraph.ModelConfig{})
		if got := string(opts.Kind); got != "fake" {
			t.Errorf("Kind = %q, want fake", got)
		}
		if opts.APIKey != "" || opts.BaseURL != "" {
			t.Errorf("fake options carry no credential, got APIKey=%q BaseURL=%q", opts.APIKey, opts.BaseURL)
		}
	})

	t.Run("real mode without credential falls back to fake", func(t *testing.T) {
		// A Lux mode with no key cannot authenticate; the mapping degrades to
		// fake rather than building a guaranteed-401 real adapter.
		opts := agentgraph.ModelOptions(agentgraph.ModelConfig{
			Mode:    agentgraph.ModelModeLux,
			BaseURL: luxURL,
		})
		if got := string(opts.Kind); got != "fake" {
			t.Errorf("Kind = %q, want fake", got)
		}
	})
}
