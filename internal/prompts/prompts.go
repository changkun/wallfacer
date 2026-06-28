// Package prompts provides template-based rendering for all agent prompt
// strings used throughout wallfacer. Templates live alongside this file as
// *.tmpl files and are embedded into the binary at compile time via go:embed.
//
// The package exposes a Manager type that supports optional per-user overrides:
// if ~/.wallfacer/prompts/<name>.tmpl exists it is used in place of the
// embedded default. Package-level functions delegate to Default (no overrides)
// for backward compatibility.
package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"text/template"

	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/pkg/atomicfile"
)

//go:embed *.tmpl
var fs embed.FS // embedded template filesystem, compiled into the binary

// embeddedTmpl holds the parsed set of all *.tmpl files embedded in the binary.
// It is populated once during init and shared (read-only) by all Manager instances.
var embeddedTmpl *template.Template

// Default is set in init() after embeddedTmpl is populated.
var Default *Manager

func init() {
	var err error
	embeddedTmpl, err = template.New("").
		Funcs(templateFuncMap()).
		ParseFS(fs, "*.tmpl")
	if err != nil {
		panic(fmt.Sprintf("prompts: parse templates: %v", err))
	}
	Default = &Manager{embedded: embeddedTmpl}
}

// templateFuncMap returns the shared FuncMap used by all prompt templates.
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"mul": func(a, b float64) float64 { return a * b },
		"sub": func(a, b float64) float64 { return a - b },
	}
}

// embeddedToAPI maps embedded template file names to user-facing API names.
var embeddedToAPI = map[string]string{
	"oversight.tmpl":            "oversight",
	"title.tmpl":                "title",
	"commit.tmpl":               "commit_message",
	"conflict.tmpl":             "conflict_resolution",
	"test.tmpl":                 "test_verification",
	"spec.tmpl":                 "spec",
	"spec_system_empty.tmpl":    "spec_system_empty",
	"spec_system_nonempty.tmpl": "spec_system_nonempty",
	"task_prompt_refine.tmpl":   "task_prompt_refine",
}

// apiToEmbedded maps user-facing API names to embedded template file names.
var apiToEmbedded = map[string]string{
	"oversight":            "oversight.tmpl",
	"title":                "title.tmpl",
	"commit_message":       "commit.tmpl",
	"conflict_resolution":  "conflict.tmpl",
	"test_verification":    "test.tmpl",
	"spec":                 "spec.tmpl",
	"spec_system_empty":    "spec_system_empty.tmpl",
	"spec_system_nonempty": "spec_system_nonempty.tmpl",
	"task_prompt_refine":   "task_prompt_refine.tmpl",
}

// knownNames is the ordered list of all user-facing template API names.
var knownNames = []string{
	"task_prompt_refine",
	"oversight",
	"title",
	"commit_message",
	"conflict_resolution",
	"test_verification",
	"spec",
	"spec_system_empty",
	"spec_system_nonempty",
}

// Manager manages the built-in prompt templates (see knownNames for the
// authoritative list) with optional per-user overrides stored in userDir.
//
// On each render call the Manager checks userDir/<apiName>.tmpl; if the file
// is readable and parses without error it is used in place of the embedded
// default. Errors (missing file, parse failure, execute failure) silently fall
// back to the embedded template so that a bad override never breaks production.
type Manager struct {
	embedded *template.Template
	userDir  string // ~/.wallfacer/prompts; empty = no overrides
}

// NewManager creates a Manager with the given user override directory.
// If userDir is empty, overrides are disabled and embedded templates are
// always used. The directory need not exist at construction time.
func NewManager(userDir string) *Manager {
	return &Manager{
		embedded: embeddedTmpl,
		userDir:  userDir,
	}
}

// KnownNames returns all known template API names in a fixed order.
func (m *Manager) KnownNames() []string {
	return slices.Clone(knownNames)
}

// PromptsDir returns the user override directory for this Manager, or an
// empty string if no directory was configured.
func (m *Manager) PromptsDir() string {
	return m.userDir
}

// render executes the named embedded template (e.g. "commit.tmpl"),
// checking the user override directory first.
func (m *Manager) render(embeddedName string, data any) string {
	if m.userDir != "" {
		if apiName, ok := embeddedToAPI[embeddedName]; ok {
			overridePath := filepath.Join(m.userDir, apiName+".tmpl")
			if content, err := os.ReadFile(overridePath); err == nil {
				out, execErr := executeOverride(embeddedName, content, data)
				if execErr != nil {
					logger.Prompts.Warn("prompt override execution failed, using default",
						"name", apiName,
						"error", execErr,
					)
				} else {
					return out
				}
			}
		}
	}
	var buf bytes.Buffer
	if err := m.embedded.ExecuteTemplate(&buf, embeddedName, data); err != nil {
		panic(fmt.Sprintf("prompts: render %s: %v", embeddedName, err))
	}
	return buf.String()
}

// executeOverride parses and executes override content, returning the result
// or an error if parsing or execution fails.
func executeOverride(name string, content []byte, data any) (string, error) {
	t, err := template.New(name).Funcs(templateFuncMap()).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parse override %s: %w", name, err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute override %s: %w", name, err)
	}
	return buf.String(), nil
}

// Content returns the effective template content for the given API name:
// the user override file content if it exists and is readable, otherwise the
// embedded default. hasOverride is true when a user override file was found
// (regardless of whether it could be read).
func (m *Manager) Content(apiName string) (content string, hasOverride bool, err error) {
	embeddedName, ok := apiToEmbedded[apiName]
	if !ok {
		return "", false, fmt.Errorf("unknown template name %q", apiName)
	}
	if m.userDir != "" {
		overridePath := filepath.Join(m.userDir, apiName+".tmpl")
		raw, readErr := os.ReadFile(overridePath)
		if readErr == nil {
			return string(raw), true, nil
		}
		if !os.IsNotExist(readErr) {
			return "", true, readErr // exists but unreadable
		}
	}
	// Fall back to embedded.
	raw, err := fs.ReadFile(embeddedName)
	if err != nil {
		return "", false, fmt.Errorf("read embedded template %q: %w", embeddedName, err)
	}
	return string(raw), false, nil
}

// WriteOverride validates and writes a user override template for the given
// API name. Returns an error if the template does not parse correctly.
func (m *Manager) WriteOverride(apiName, content string) error {
	if _, ok := apiToEmbedded[apiName]; !ok {
		return fmt.Errorf("unknown template name %q", apiName)
	}
	if err := ValidateTemplate(content); err != nil {
		return err
	}
	if m.userDir == "" {
		return fmt.Errorf("no user prompts directory configured")
	}
	if err := os.MkdirAll(m.userDir, 0755); err != nil {
		return fmt.Errorf("create prompts dir: %w", err)
	}
	return atomicfile.Write(filepath.Join(m.userDir, apiName+".tmpl"), []byte(content), 0644)
}

// DeleteOverride removes the user override for the given API name.
// Returns an os.ErrNotExist-wrapped error if no override file was present.
func (m *Manager) DeleteOverride(apiName string) error {
	if _, ok := apiToEmbedded[apiName]; !ok {
		return fmt.Errorf("unknown template name %q", apiName)
	}
	if m.userDir == "" {
		return fmt.Errorf("no user prompts directory configured")
	}
	return os.Remove(filepath.Join(m.userDir, apiName+".tmpl"))
}

// ValidateTemplate parses content as a Go template (with the shared FuncMap)
// and returns an error if it is syntactically invalid.
func ValidateTemplate(content string) error {
	_, err := template.New("validate").Funcs(templateFuncMap()).Parse(content)
	return err
}

// mockContextFor returns a fully initialized zero-valued context struct for
// the given template API name. It is used by Validate to perform a dry-run
// execution and catch field-access errors at write time.
func mockContextFor(apiName string) (interface{}, bool) {
	switch apiName {
	case "task_prompt_refine":
		return RefinementData{
			CreatedAt: "2024-01-01 00:00:00",
			Today:     "2024-01-01",
			Status:    "backlog",
			Prompt:    "example prompt",
		}, true
	case "commit_message":
		return CommitData{
			Prompt:   "example prompt",
			DiffStat: "1 file changed",
		}, true
	case "conflict_resolution":
		return ConflictData{
			ContainerPath: "/workspace/example",
			DefaultBranch: "main",
		}, true
	case "test_verification":
		return TestData{OriginalPrompt: "example prompt"}, true
	case "oversight":
		return struct{ ActivityLog string }{ActivityLog: "example activity log"}, true
	case "title":
		return struct{ Prompt string }{Prompt: "example task"}, true
	case "spec":
		return nil, true
	case "spec_system_empty", "spec_system_nonempty":
		return nil, true
	default:
		return nil, false
	}
}

// Validate parses content as a Go template and performs a dry-run execution
// against a representative mock context for the given API name. This catches
// both syntax errors and field-access errors (e.g. referencing a field that
// does not exist on the context struct) at write time.
func (m *Manager) Validate(apiName, content string) error {
	if _, ok := apiToEmbedded[apiName]; !ok {
		return fmt.Errorf("unknown template name %q", apiName)
	}
	tmpl, err := template.New(apiName).Funcs(templateFuncMap()).Parse(content)
	if err != nil {
		return fmt.Errorf("template parse error: %w", err)
	}
	ctx, ok := mockContextFor(apiName)
	if !ok {
		return fmt.Errorf("unknown template name %q", apiName)
	}
	if err := tmpl.Execute(io.Discard, ctx); err != nil {
		return fmt.Errorf("template execution error: %w", err)
	}
	return nil
}

// --- Data structs ---
// These types define the template context for each prompt. Field names must
// match the {{.FieldName}} references in the corresponding *.tmpl files.

// RefinementData holds template variables for the refinement prompt.
type RefinementData struct {
	CreatedAt        string
	Today            string
	AgeDays          int
	Status           string
	Prompt           string
	UserInstructions string // optional; rendered only when non-empty
}

// CommitData holds template variables for the commit message prompt.
type CommitData struct {
	Prompt    string
	DiffStat  string
	RecentLog string // optional; rendered only when non-empty
}

// ConflictData holds template variables for the conflict resolution prompt.
type ConflictData struct {
	ContainerPath string
	DefaultBranch string
}

// DriftData holds template variables for the drift-assessment prompt: the
// spec body and the task's actual changes. Affects and ChangedFiles are
// newline-joined for rendering.
type DriftData struct {
	SpecBody     string
	Affects      string
	ChangedFiles string
	Diff         string
}

// TestData holds template variables for the test verification prompt.
type TestData struct {
	OriginalPrompt string
	Criteria       string // optional
	ImplResult     string // optional
	Diff           string // optional
}

// --- Manager methods ---

// TaskPromptRefine renders the task-mode spec-mode agent system prompt.
// Uses the same RefinementData fields as the task's pinned metadata.
func (m *Manager) TaskPromptRefine(d RefinementData) string {
	return m.render("task_prompt_refine.tmpl", d)
}

// Oversight renders the oversight summarization prompt for the given
// pre-formatted activity log text.
func (m *Manager) Oversight(activityLog string) string {
	return m.render("oversight.tmpl", struct{ ActivityLog string }{activityLog})
}

// Title renders the title-generation prompt for the given task prompt.
func (m *Manager) Title(taskPrompt string) string {
	return m.render("title.tmpl", struct{ Prompt string }{taskPrompt})
}

// CommitMessage renders the commit message generation prompt.
func (m *Manager) CommitMessage(d CommitData) string { return m.render("commit.tmpl", d) }

// DriftAssessment renders the task-done drift-assessment prompt.
func (m *Manager) DriftAssessment(d DriftData) string { return m.render("drift.tmpl", d) }

// ConflictResolution renders the rebase conflict resolution prompt.
func (m *Manager) ConflictResolution(d ConflictData) string { return m.render("conflict.tmpl", d) }

// TestVerification renders the test verification agent prompt.
func (m *Manager) TestVerification(d TestData) string { return m.render("test.tmpl", d) }

// Spec renders the spec-mode agent system prompt.
func (m *Manager) Spec() string { return m.render("spec.tmpl", nil) }

// SpecSystemEmpty renders the spec-mode prompt prefix used
// when the workspace spec tree is empty (no non-archived parseable
// specs). Encourages the agent to emit a `/spec-new` directive for
// substantive spec work.
func (m *Manager) SpecSystemEmpty() string {
	return m.render("spec_system_empty.tmpl", nil)
}

// SpecSystemNonempty renders the spec-mode prompt prefix used
// when at least one non-archived spec exists. Steers the agent toward
// editing existing specs rather than creating new ones.
func (m *Manager) SpecSystemNonempty() string {
	return m.render("spec_system_nonempty.tmpl", nil)
}

// --- Package-level functions (delegate to Default for backward compatibility) ---

// TaskPromptRefine renders the task-mode spec-mode agent system prompt.
func TaskPromptRefine(d RefinementData) string { return Default.TaskPromptRefine(d) }

// Oversight renders the oversight summarization prompt for the given
// pre-formatted activity log text.
func Oversight(activityLog string) string { return Default.Oversight(activityLog) }

// Title renders the title-generation prompt for the given task prompt.
func Title(taskPrompt string) string { return Default.Title(taskPrompt) }

// CommitMessage renders the commit message generation prompt.
func CommitMessage(d CommitData) string { return Default.CommitMessage(d) }

// DriftAssessment renders the task-done drift-assessment prompt.
func DriftAssessment(d DriftData) string { return Default.DriftAssessment(d) }

// ConflictResolution renders the rebase conflict resolution prompt.
func ConflictResolution(d ConflictData) string { return Default.ConflictResolution(d) }

// TestVerification renders the test verification agent prompt.
func TestVerification(d TestData) string { return Default.TestVerification(d) }

// Spec renders the spec-mode agent system prompt.
func Spec() string { return Default.Spec() }

// SpecSystemEmpty renders the spec-mode prompt prefix used
// when the workspace spec tree is empty.
func SpecSystemEmpty() string { return Default.SpecSystemEmpty() }

// SpecSystemNonempty renders the spec-mode prompt prefix used
// when at least one non-archived spec exists.
func SpecSystemNonempty() string { return Default.SpecSystemNonempty() }
