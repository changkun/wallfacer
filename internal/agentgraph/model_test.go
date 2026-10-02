package agentgraph_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"latere.ai/x/wallfacer/internal/agentgraph"
)

// TestModelOptions_Mapping asserts the ModelConfig -> topos.ModelOptions mapping
// without running a model. It reads the returned value's fields structurally, so
// the test never names a topos type (the agentgraph seam stays the only place
// that does). Kind is compared via its string form for the same reason.
func TestModelOptions_Mapping(t *testing.T) {
	const (
		luxURL = "https://models.example.com/v1/models"
		key    = "lux_test_key"
		model  = "claude-sonnet-4-6"
	)

	t.Run("configured lux", func(t *testing.T) {
		opts, err := agentgraph.ModelOptions(agentgraph.ModelConfig{
			Mode:     agentgraph.ModelModeLux,
			Provider: "anthropic",
			Model:    model,
			BaseURL:  luxURL,
			APIKey:   key,
		})
		if err != nil {
			t.Fatalf("ModelOptions: %v", err)
		}
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
		opts, err := agentgraph.ModelOptions(agentgraph.ModelConfig{
			Mode:   agentgraph.ModelModeDirect,
			APIKey: key,
		})
		if err != nil {
			t.Fatalf("ModelOptions: %v", err)
		}
		if got := string(opts.Kind); got != "direct" {
			t.Errorf("Kind = %q, want direct", got)
		}
		if opts.APIKey != key {
			t.Errorf("APIKey = %q, want %q", opts.APIKey, key)
		}
	})

	t.Run("test model named explicitly", func(t *testing.T) {
		opts, err := agentgraph.ModelOptions(agentgraph.ModelConfig{Mode: agentgraph.ModelModeFake})
		if err != nil {
			t.Fatalf("ModelOptions: %v", err)
		}
		if got := string(opts.Kind); got != "fake" {
			t.Errorf("Kind = %q, want fake", got)
		}
		if opts.APIKey != "" || opts.BaseURL != "" {
			t.Errorf("test-model options carry no credential, got APIKey=%q BaseURL=%q", opts.APIKey, opts.BaseURL)
		}
	})

	t.Run("unconfigured is refused", func(t *testing.T) {
		opts, err := agentgraph.ModelOptions(agentgraph.ModelConfig{})
		if !errors.Is(err, agentgraph.ErrNoModelCredential) {
			t.Fatalf("err = %v, want ErrNoModelCredential", err)
		}
		if opts.Kind != "" {
			t.Errorf("Kind = %q, want no model selected alongside the error", opts.Kind)
		}
	})

	t.Run("real mode without credential is refused", func(t *testing.T) {
		// A Lux mode with no key cannot authenticate, and it must not resolve
		// to any other model either.
		opts, err := agentgraph.ModelOptions(agentgraph.ModelConfig{
			Mode:    agentgraph.ModelModeLux,
			BaseURL: luxURL,
		})
		if !errors.Is(err, agentgraph.ErrNoModelCredential) {
			t.Fatalf("err = %v, want ErrNoModelCredential", err)
		}
		if opts.Kind != "" {
			t.Errorf("Kind = %q, want no model selected alongside the error", opts.Kind)
		}
	})

	t.Run("credential under an unset or unknown mode is refused", func(t *testing.T) {
		for _, mode := range []agentgraph.ModelMode{"", "gateway"} {
			opts, err := agentgraph.ModelOptions(agentgraph.ModelConfig{Mode: mode, APIKey: key})
			if err == nil {
				t.Errorf("mode %q: no error, got Kind %q", mode, opts.Kind)
			}
		}
	})
}

// TestRunWithoutModelCredentialIsRefused asserts that RunAgent starts no run for
// a config that carries no credential: the zero value and a real mode missing
// its key both return ErrNoModelCredential, and nothing is written to the
// worktree.
func TestRunWithoutModelCredentialIsRefused(t *testing.T) {
	configs := map[string]agentgraph.ModelConfig{
		"unconfigured":       {},
		"lux without key":    {Mode: agentgraph.ModelModeLux, BaseURL: "https://models.example.com/v1/models"},
		"direct without key": {Mode: agentgraph.ModelModeDirect},
	}
	for name, cfg := range configs {
		t.Run(name, func(t *testing.T) {
			worktree := t.TempDir()
			if _, err := agentgraph.RunAgent(t.Context(), "run-refused", cfg, "implement", "", "hi > marker.txt", worktree, nil); !errors.Is(err, agentgraph.ErrNoModelCredential) {
				t.Errorf("RunAgent: err = %v, want ErrNoModelCredential", err)
			}
			if _, err := os.Stat(filepath.Join(worktree, "marker.txt")); err == nil {
				t.Error("marker.txt exists in the worktree; a refused run must not run anything")
			}
		})
	}
}
