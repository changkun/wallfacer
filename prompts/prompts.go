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
	"os"
	"path/filepath"
	"text/template"
)

//go:embed *.tmpl
var fs embed.FS

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
	}
}

// embeddedToAPI maps embedded template file names to user-facing API names.
var embeddedToAPI = map[string]string{
	"ideation.tmpl":   "ideation",
	"refinement.tmpl": "refinement",
	"oversight.tmpl":  "oversight",
	"title.tmpl":      "title",
	"commit.tmpl":     "commit_message",
	"conflict.tmpl":   "conflict_resolution",
	"test.tmpl":       "test_verification",
}

// apiToEmbedded maps user-facing API names to embedded template file names.
var apiToEmbedded = map[string]string{
	"ideation":            "ideation.tmpl",
	"refinement":          "refinement.tmpl",
	"oversight":           "oversight.tmpl",
	"title":               "title.tmpl",
	"commit_message":      "commit.tmpl",
	"conflict_resolution": "conflict.tmpl",
	"test_verification":   "test.tmpl",
}

// knownNames is the ordered list of all user-facing template API names.
var knownNames = []string{
	"ideation",
	"refinement",
	"oversight",
	"title",
	"commit_message",
	"conflict_resolution",
	"test_verification",
}

// Manager manages the seven built-in prompt templates with optional
// per-user overrides stored in userDir.
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
	result := make([]string, len(knownNames))
	copy(result, knownNames)
	return result
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
				if out, execErr := executeOverride(embeddedName, content, data); execErr == nil {
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
	path := filepath.Join(m.userDir, apiName+".tmpl")
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
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

// --- Data structs (unchanged public API) ---

// RefinementData holds template variables for the refinement prompt.
type RefinementData struct {
	CreatedAt        string
	Today            string
	AgeDays          int
	Status           string
	Prompt           string
	UserInstructions string // optional; rendered only when non-empty
}

// IdeationTask represents a single existing task shown to the brainstorm agent
// for deduplication context. Title and Prompt should already be pre-processed
// (truncated, default title applied) by the caller.
type IdeationTask struct {
	Title  string
	Status string
	Prompt string
}

// IdeationData holds template variables for the ideation prompt.
type IdeationData struct {
	ExistingTasks  []IdeationTask
	Categories     []string
	FailureSignals []string // tasks that failed or had failing tests
	ChurnSignals   []string // recently-modified hot files
	TodoSignals    []string // files with high TODO/FIXME density
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

// TestData holds template variables for the test verification prompt.
type TestData struct {
	OriginalPrompt string
	Criteria       string // optional
	ImplResult     string // optional
	Diff           string // optional
}

// --- Manager methods ---

// Refinement renders the spec-writing agent prompt.
func (m *Manager) Refinement(d RefinementData) string { return m.render("refinement.tmpl", d) }

// Ideation renders the brainstorm agent prompt.
func (m *Manager) Ideation(d IdeationData) string { return m.render("ideation.tmpl", d) }

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

// ConflictResolution renders the rebase conflict resolution prompt.
func (m *Manager) ConflictResolution(d ConflictData) string { return m.render("conflict.tmpl", d) }

// TestVerification renders the test verification agent prompt.
func (m *Manager) TestVerification(d TestData) string { return m.render("test.tmpl", d) }

// --- Package-level functions (delegate to Default for backward compatibility) ---

// Refinement renders the spec-writing agent prompt.
func Refinement(d RefinementData) string { return Default.Refinement(d) }

// Ideation renders the brainstorm agent prompt.
func Ideation(d IdeationData) string { return Default.Ideation(d) }

// Oversight renders the oversight summarization prompt for the given
// pre-formatted activity log text.
func Oversight(activityLog string) string { return Default.Oversight(activityLog) }

// Title renders the title-generation prompt for the given task prompt.
func Title(taskPrompt string) string { return Default.Title(taskPrompt) }

// CommitMessage renders the commit message generation prompt.
func CommitMessage(d CommitData) string { return Default.CommitMessage(d) }

// ConflictResolution renders the rebase conflict resolution prompt.
func ConflictResolution(d ConflictData) string { return Default.ConflictResolution(d) }

// TestVerification renders the test verification agent prompt.
func TestVerification(d TestData) string { return Default.TestVerification(d) }
