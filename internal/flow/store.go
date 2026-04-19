package flow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"

	"changkun.de/x/wallfacer/internal/store"
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
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read flows dir %s: %w", dir, err)
	}
	var flows []Flow
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, name)
		body, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", path, err)
		}
		var f diskFlow
		if err := yaml.Unmarshal(body, &f); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if f.Slug == "" {
			return nil, fmt.Errorf("parse %s: slug is required", path)
		}
		if !IsValidSlug(f.Slug) {
			return nil, fmt.Errorf("parse %s: slug %q is not kebab-case (2-40 chars)", path, f.Slug)
		}
		if f.Name == "" {
			return nil, fmt.Errorf("parse %s: name is required", path)
		}
		if len(f.Steps) == 0 {
			return nil, fmt.Errorf("parse %s: at least one step is required", path)
		}
		steps := make([]Step, 0, len(f.Steps))
		for i, s := range f.Steps {
			if s.AgentSlug == "" {
				return nil, fmt.Errorf("parse %s: step %d missing agent_slug", path, i)
			}
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
	if !IsValidSlug(f.Slug) {
		return fmt.Errorf("invalid slug %q", f.Slug)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	steps := make([]diskStep, len(f.Steps))
	for i, s := range f.Steps {
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
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}

// DeleteUserFlow removes dir/<slug>.yaml. Idempotent: missing file
// yields nil.
func DeleteUserFlow(dir, slug string) error {
	if !IsValidSlug(slug) {
		return fmt.Errorf("invalid slug %q", slug)
	}
	path := filepath.Join(dir, slug+".yaml")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// NewMergedRegistry combines the built-in catalog with user-authored
// flows loaded from dir. Built-in slugs win on collision: a user
// file shadowing a built-in is rejected with an error so the core
// flows (implement / brainstorm / refine-only / test-only) cannot
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

// IsValidSlug enforces the same slug format flows and agents share:
// 2–40 chars, lowercase letters / digits / hyphens, no leading or
// trailing hyphen.
func IsValidSlug(s string) bool {
	if len(s) < 2 || len(s) > 40 {
		return false
	}
	for i, c := range s {
		switch {
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		case c == '-':
			if i == 0 || i == len(s)-1 {
				return false
			}
		default:
			return false
		}
	}
	return true
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
