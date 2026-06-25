package flow

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"latere.ai/x/wallfacer/internal/pkg/atomicfile"
	"latere.ai/x/wallfacer/internal/pkg/slugutil"
	"latere.ai/x/wallfacer/internal/pkg/yamldir"
	"latere.ai/x/wallfacer/internal/store"
)

// diskFlow is the on-disk YAML shape for a user-authored flow. It
// mirrors Flow with store-friendly types and a steps array that
// carries every field flow.Step exposes.
type diskFlow struct {
	Slug        string     `yaml:"slug"`
	Name        string     `yaml:"name"`
	Description string     `yaml:"description,omitempty"`
	SpawnKind   string     `yaml:"spawn_kind,omitempty"`
	Steps       []diskStep `yaml:"steps"`
}

type diskStep struct {
	AgentSlug         string   `yaml:"agent_slug"`
	Optional          bool     `yaml:"optional,omitempty"`
	InputFrom         string   `yaml:"input_from,omitempty"`
	RunInParallelWith []string `yaml:"run_in_parallel_with,omitempty"`
}

// LoadUserFlows reads every *.yaml / *.yml file under dir and
// returns the parsed Flow slice in filesystem order. Missing
// directory is not an error (no user flows yet is valid);
// malformed files are fatal so typos don't silently vanish.
func LoadUserFlows(dir string) ([]Flow, error) {
	files, err := yamldir.ReadAll("flows", dir)
	if err != nil {
		return nil, err
	}
	var flows []Flow
	for _, file := range files {
		var f diskFlow
		if err := yaml.Unmarshal(file.Body, &f); err != nil {
			return nil, fmt.Errorf("parse %s: %w", file.Path, err)
		}
		path := file.Path
		if f.Slug == "" {
			return nil, fmt.Errorf("parse %s: slug is required", path)
		}
		if !slugutil.IsValid(f.Slug) {
			return nil, fmt.Errorf("parse %s: slug %q is not kebab-case (2-40 chars)", path, f.Slug)
		}
		if f.Name == "" {
			return nil, fmt.Errorf("parse %s: name is required", path)
		}
		if len(f.Steps) == 0 {
			return nil, fmt.Errorf("parse %s: at least one step is required", path)
		}
		steps := make([]Step, 0, len(f.Steps))
		seenSlugs := make(map[string]bool, len(f.Steps))
		for i, s := range f.Steps {
			if s.AgentSlug == "" {
				return nil, fmt.Errorf("parse %s: step %d missing agent_slug", path, i)
			}
			// AgentSlug is the unique key for result wiring and parallel
			// grouping in the engine; duplicates silently clobber each other.
			if seenSlugs[s.AgentSlug] {
				return nil, fmt.Errorf("parse %s: duplicate agent_slug %q at step %d", path, s.AgentSlug, i)
			}
			seenSlugs[s.AgentSlug] = true
			//nolint:staticcheck // S1016: diskStep is the wire format, Step is the runtime shape; keep the literal copy explicit.
			steps = append(steps, Step{
				AgentSlug:         s.AgentSlug,
				Optional:          s.Optional,
				InputFrom:         s.InputFrom,
				RunInParallelWith: s.RunInParallelWith,
			})
		}
		flows = append(flows, Flow{
			Slug:        f.Slug,
			Name:        f.Name,
			Description: f.Description,
			SpawnKind:   store.TaskKind(f.SpawnKind),
			Steps:       steps,
		})
	}
	return flows, nil
}

// WriteUserFlow persists a single user-authored flow to
// dir/<slug>.yaml using an atomic temp-file + rename.
func WriteUserFlow(dir string, f Flow) error {
	if !slugutil.IsValid(f.Slug) {
		return fmt.Errorf("invalid slug %q", f.Slug)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	steps := make([]diskStep, len(f.Steps))
	for i, s := range f.Steps {
		//nolint:staticcheck // S1016: see LoadUserFlows — keep diskStep distinct from Step.
		steps[i] = diskStep{
			AgentSlug:         s.AgentSlug,
			Optional:          s.Optional,
			InputFrom:         s.InputFrom,
			RunInParallelWith: s.RunInParallelWith,
		}
	}
	body, err := yaml.Marshal(diskFlow{
		Slug:        f.Slug,
		Name:        f.Name,
		Description: f.Description,
		SpawnKind:   string(f.SpawnKind),
		Steps:       steps,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	path := filepath.Join(dir, f.Slug+".yaml")
	if err := atomicfile.Write(path, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// DeleteUserFlow removes dir/<slug>.yaml. Idempotent: missing file
// yields nil.
func DeleteUserFlow(dir, slug string) error {
	return yamldir.Remove(dir, slug)
}

// NewMergedRegistry combines the built-in catalog with user-authored
// flows loaded from dir. Built-in slugs win on collision: a user
// file shadowing a built-in is rejected with an error so the core
// flows (implement / brainstorm / test-only) cannot
// be quietly overridden.
func NewMergedRegistry(dir string) (*Registry, error) {
	user, err := LoadUserFlows(dir)
	if err != nil {
		return nil, err
	}
	seen := make(map[string]bool, len(builtins))
	all := make([]Flow, 0, len(builtins)+len(user))
	for _, b := range builtins {
		b.Builtin = true
		seen[b.Slug] = true
		all = append(all, b)
	}
	for _, u := range user {
		if seen[u.Slug] {
			return nil, fmt.Errorf("user flow %q shadows a built-in slug; rename the file", u.Slug)
		}
		seen[u.Slug] = true
		all = append(all, u)
	}
	return NewRegistry(all...), nil
}

// IsBuiltin reports whether slug names a built-in flow.
func IsBuiltin(slug string) bool {
	for i := range builtins {
		if builtins[i].Slug == slug {
			return true
		}
	}
	return false
}
