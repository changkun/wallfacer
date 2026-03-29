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
	"math"
	"os"
	"path/filepath"
	"slices"
	"text/template"

	"changkun.de/x/wallfacer/internal/logger"
	"changkun.de/x/wallfacer/internal/pkg/atomicfile"
)

//go:embed *.tmpl
var fs embed.FS

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
		// exploitCount returns how many of `total` ideas should be exploitation-style,
		// clamped to [0, total]. Used by the ideation template to split ideas.
		"exploitCount": func(ratio float64, total int) int {
			n := int(math.Round(ratio * float64(total)))
			if n > total {
				n = total
			}
			return n
		},
		// exploreCount is the complement of exploitCount: total minus the exploit share.
		"exploreCount": func(ratio float64, total int) int {
			n := int(math.Round(ratio * float64(total)))
			if n > total {
				n = total
			}
			return total - n
		},
	}
}

// embeddedToAPI maps embedded template file names to user-facing API names.
var embeddedToAPI = map[string]string{
	"ideation.tmpl":     "ideation",
	"refinement.tmpl":   "refinement",
	"oversight.tmpl":    "oversight",
	"title.tmpl":        "title",
	"commit.tmpl":       "commit_message",
	"conflict.tmpl":     "conflict_resolution",
	"test.tmpl":         "test_verification",
	"instructions.tmpl": "instructions",
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
	"instructions":        "instructions.tmpl",
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
	"instructions",
}

// Manager manages the eight built-in prompt templates with optional
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
	case "refinement":
		return RefinementData{
			CreatedAt: "2024-01-01 00:00:00",
			Today:     "2024-01-01",
			Status:    "backlog",
			Prompt:    "example prompt",
		}, true
	case "ideation":
		return IdeationData{}, true
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
	case "instructions":
		return InstructionsData{
			Workspaces:          []InstructionsWorkspace{{Name: "example-repo"}},
			RepoInstructionRefs: []InstructionsRepoRef{{Workspace: "example-repo", Filename: "AGENTS.md"}},
		}, true
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

// WorkspaceSignal represents a single scored hotspot file from workspace analysis.
// It carries enough context for the advisor to understand why the file was surfaced
// and which workspace it belongs to when multiple workspaces are active.
type WorkspaceSignal struct {
	// DisplayPath is the path shown to the advisor. It is workspace-relative when
	// only one workspace is active, and prefixed with the workspace basename
	// (e.g. "wallfacer/internal/runner/ideate.go") when multiple workspaces exist.
	DisplayPath string

	// Score is the raw signal strength: commit count for churn signals, marker
	// occurrence count for TODO signals.
	Score int

	// Reason is a human-readable description of why this file was selected,
	// e.g. "11 commits" or "3 TODO markers".
	Reason string

	// Workspace is the basename of the workspace directory this path belongs to.
	// Empty when only one workspace is active.
	Workspace string

	// Boosted is true when the path received a score multiplier for matching a
	// preferred source directory pattern (internal/, ui/js/ non-vendor, ui/partials/,
	// or a test file suffix). Boosted paths rank ahead of equivalent-count vendor paths.
	Boosted bool
}

// IdeationData holds template variables for the ideation prompt.
type IdeationData struct {
	ExistingTasks  []IdeationTask
	Categories     []string
	FailureSignals []string // tasks that failed or had failing tests

	// ChurnHotspots contains recently-modified files scored and filtered by the
	// signal pipeline. Vendor/generated/artifact paths are excluded; files in
	// actionable source directories are boosted.
	ChurnHotspots []WorkspaceSignal

	// TodoHotspots contains files with high TODO/FIXME/XXX marker density,
	// scored and filtered. Prompt templates and vendor paths are excluded.
	TodoHotspots []WorkspaceSignal

	// FilteredChurnCount is the number of churn paths excluded by ignore rules
	// (vendor, generated, minified). Used to inform the advisor that filtering occurred.
	FilteredChurnCount int

	// FilteredTodoCount is the number of TODO paths excluded by ignore rules
	// (vendor, generated, prompt templates). Used to inform the advisor of filtering.
	FilteredTodoCount int

	RejectedTitles []string // previously proposed but rejected idea titles (within TTL)

	// ExploitRatio is the fraction (0.0–1.0) of ideas that should be
	// exploitation-style (improve existing code) vs exploration-style (new
	// features / new directions). Default 0.8 means 80% exploitation.
	ExploitRatio float64
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

// InstructionsWorkspace represents a single workspace entry for the instructions template.
type InstructionsWorkspace struct {
	Name string // basename of the workspace directory
}

// InstructionsRepoRef represents a per-repo instructions file reference.
type InstructionsRepoRef struct {
	Workspace string // basename of the workspace directory
	Filename  string // "AGENTS.md" or "CLAUDE.md"
}

// InstructionsData holds template variables for the workspace instructions prompt.
type InstructionsData struct {
	Workspaces          []InstructionsWorkspace
	RepoInstructionRefs []InstructionsRepoRef
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

// Instructions renders the workspace instructions (AGENTS.md) content.
func (m *Manager) Instructions(d InstructionsData) string { return m.render("instructions.tmpl", d) }

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

// Instructions renders the workspace instructions (AGENTS.md) content.
func Instructions(d InstructionsData) string { return Default.Instructions(d) }
