package agents

import (
	"fmt"
	"os"
	"path/filepath"

	"latere.ai/x/wallfacer/internal/pkg/atomicfile"
	"latere.ai/x/wallfacer/internal/pkg/registry"
	"latere.ai/x/wallfacer/internal/pkg/slugutil"
	"latere.ai/x/wallfacer/internal/pkg/yamldir"

	"gopkg.in/yaml.v3"
)

// diskAgent is the on-disk YAML shape for a user-authored agent.
// Mirrors the fields on Role; the loader converts it to Role at
// read time so call sites work against the canonical descriptor.
type diskAgent struct {
	Slug               string   `yaml:"slug"`
	Title              string   `yaml:"title"`
	Description        string   `yaml:"description,omitempty"`
	PromptTemplateName string   `yaml:"prompt_template_name,omitempty"`
	PromptTmpl         string   `yaml:"prompt_tmpl,omitempty"`
	Capabilities       []string `yaml:"capabilities,omitempty"`
	Multiturn          bool     `yaml:"multiturn,omitempty"`
	Harness            string   `yaml:"harness,omitempty"`
}

// LoadUserAgents reads every *.yaml / *.yml file under dir and
// returns the parsed Role slice in filesystem order. A missing
// directory is not an error (no user agents yet is a valid
// state); a malformed file is fatal because silent skip masks
// typos.
func LoadUserAgents(dir string) ([]Role, error) {
	files, err := yamldir.ReadAll("agents", dir)
	if err != nil {
		return nil, err
	}
	var roles []Role
	for _, f := range files {
		var a diskAgent
		if err := yaml.Unmarshal(f.Body, &a); err != nil {
			return nil, fmt.Errorf("parse %s: %w", f.Path, err)
		}
		if a.Slug == "" {
			return nil, fmt.Errorf("parse %s: slug is required", f.Path)
		}
		if !slugutil.IsValid(a.Slug) {
			return nil, fmt.Errorf("parse %s: slug %q is not kebab-case (2-40 chars, lowercase, digits, hyphens)", f.Path, a.Slug)
		}
		if a.Title == "" {
			return nil, fmt.Errorf("parse %s: title is required", f.Path)
		}
		//nolint:staticcheck // S1016: the two types share fields by design but are distinct — diskAgent is the wire format, Role is the runtime descriptor; keep them explicitly decoupled so a future field split does not silently coerce.
		roles = append(roles, Role{
			Slug:               a.Slug,
			Title:              a.Title,
			Description:        a.Description,
			PromptTemplateName: a.PromptTemplateName,
			PromptTmpl:         a.PromptTmpl,
			Capabilities:       a.Capabilities,
			Multiturn:          a.Multiturn,
			Harness:            a.Harness,
		})
	}
	return roles, nil
}

// WriteUserAgent persists a single user-authored agent to
// dir/<slug>.yaml using an atomic temp-file + rename. The caller
// must have already validated the slug does not collide with a
// built-in.
func WriteUserAgent(dir string, role Role) error {
	if !slugutil.IsValid(role.Slug) {
		return fmt.Errorf("invalid slug %q", role.Slug)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	//nolint:staticcheck // S1016: see LoadUserAgents — diskAgent stays explicitly distinct from Role.
	body, err := yaml.Marshal(diskAgent{
		Slug:               role.Slug,
		Title:              role.Title,
		Description:        role.Description,
		PromptTemplateName: role.PromptTemplateName,
		PromptTmpl:         role.PromptTmpl,
		Capabilities:       role.Capabilities,
		Multiturn:          role.Multiturn,
		Harness:            role.Harness,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	path := filepath.Join(dir, role.Slug+".yaml")
	if err := atomicfile.Write(path, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// DeleteUserAgent removes dir/<slug>.yaml. Returns nil when the
// file is already absent so callers can treat delete as idempotent.
func DeleteUserAgent(dir, slug string) error {
	return yamldir.Remove(dir, slug)
}

// NewMergedRegistry returns a Registry combining the built-in
// catalog with user-authored roles loaded from dir. Built-in
// slugs are protected: a user file whose slug collides with a
// built-in yields an error rather than silently overriding the
// built-in, so users cannot brick the core flow pipeline by
// dropping a malformed file into the directory.
func NewMergedRegistry(dir string) (*Registry, error) {
	user, err := LoadUserAgents(dir)
	if err != nil {
		return nil, err
	}
	all, err := registry.MergeUnique("agent", BuiltinAgents, user, func(r Role) string { return r.Slug }, nil)
	if err != nil {
		return nil, err
	}
	return NewRegistry(all...), nil
}

// IsBuiltin reports whether slug names a built-in agent. Used by
// PUT/DELETE handlers to reject mutations targeting shipped roles.
func IsBuiltin(slug string) bool {
	return registry.ContainsSlug(BuiltinAgents, slug, func(r Role) string { return r.Slug })
}
