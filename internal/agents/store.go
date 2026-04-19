package agents

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
	if dir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read agents dir %s: %w", dir, err)
	}
	var roles []Role
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
		var a diskAgent
		if err := yaml.Unmarshal(body, &a); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if a.Slug == "" {
			return nil, fmt.Errorf("parse %s: slug is required", path)
		}
		if !IsValidSlug(a.Slug) {
			return nil, fmt.Errorf("parse %s: slug %q is not kebab-case (2-40 chars, lowercase, digits, hyphens)", path, a.Slug)
		}
		if a.Title == "" {
			return nil, fmt.Errorf("parse %s: title is required", path)
		}
		//nolint:staticcheck // S1016: the two types share fields by design but are distinct — diskAgent is the wire format, Role is the runtime descriptor; keep them explicitly decoupled so a future field split does not silently coerce.
		roles = append(roles, Role{
			Slug:               a.Slug,
			Title:              a.Title,
			Description:        a.Description,
			PromptTemplateName: a.PromptTemplateName,
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
	if !IsValidSlug(role.Slug) {
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
		Capabilities:       role.Capabilities,
		Multiturn:          role.Multiturn,
		Harness:            role.Harness,
	})
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	path := filepath.Join(dir, role.Slug+".yaml")
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

// DeleteUserAgent removes dir/<slug>.yaml. Returns nil when the
// file is already absent so callers can treat delete as idempotent.
func DeleteUserAgent(dir, slug string) error {
	if !IsValidSlug(slug) {
		return fmt.Errorf("invalid slug %q", slug)
	}
	path := filepath.Join(dir, slug+".yaml")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
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
	seen := make(map[string]bool, len(BuiltinAgents))
	all := make([]Role, 0, len(BuiltinAgents)+len(user))
	for _, b := range BuiltinAgents {
		seen[b.Slug] = true
		all = append(all, b)
	}
	for _, u := range user {
		if seen[u.Slug] {
			return nil, fmt.Errorf("user agent %q shadows a built-in slug; rename the file", u.Slug)
		}
		seen[u.Slug] = true
		all = append(all, u)
	}
	return NewRegistry(all...), nil
}

// IsValidSlug reports whether s is a valid agent slug: 2–40
// characters of lowercase letters, digits, and hyphens; neither
// leading nor trailing hyphen. Matches the validation enforced by
// the /api/agents POST handler.
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

// IsBuiltin reports whether slug names a built-in agent. Used by
// PUT/DELETE handlers to reject mutations targeting shipped roles.
func IsBuiltin(slug string) bool {
	for i := range BuiltinAgents {
		if BuiltinAgents[i].Slug == slug {
			return true
		}
	}
	return false
}
