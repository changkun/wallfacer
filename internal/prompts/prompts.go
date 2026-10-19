// Package prompts renders the agent prompt strings used throughout wallfacer.
// Templates live alongside this file as *.tmpl files and are embedded into the
// binary at compile time via go:embed.
package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed *.tmpl
var fs embed.FS // embedded template filesystem, compiled into the binary

// embeddedTmpl holds the parsed set of all *.tmpl files embedded in the binary.
// It is populated once during init and read-only afterwards.
var embeddedTmpl *template.Template

func init() {
	var err error
	embeddedTmpl, err = template.New("").
		Funcs(templateFuncMap()).
		ParseFS(fs, "*.tmpl")
	if err != nil {
		panic(fmt.Sprintf("prompts: parse templates: %v", err))
	}
}

// templateFuncMap returns the FuncMap the prompt templates use.
func templateFuncMap() template.FuncMap {
	return template.FuncMap{
		"add": func(a, b int) int { return a + b },
		"mul": func(a, b float64) float64 { return a * b },
		"sub": func(a, b float64) float64 { return a - b },
	}
}

// render executes the named embedded template (e.g. "commit.tmpl"). The
// templates and their data types ship together in the binary, so a template
// that fails to execute is a defect in the build and panics rather than
// handing an agent a partial prompt.
func render(name string, data any) string {
	var buf bytes.Buffer
	if err := embeddedTmpl.ExecuteTemplate(&buf, name, data); err != nil {
		panic(fmt.Sprintf("prompts: render %s: %v", name, err))
	}
	return buf.String()
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
	WorktreePath  string // host path of the task's worktree, the agent's working directory
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

// --- Renderers ---

// TaskPromptRefine renders the task-mode spec-mode agent system prompt.
// Uses the same RefinementData fields as the task's pinned metadata.
func TaskPromptRefine(d RefinementData) string {
	return render("task_prompt_refine.tmpl", d)
}

// Oversight renders the oversight summarization prompt for the given
// pre-formatted activity log text.
func Oversight(activityLog string) string {
	return render("oversight.tmpl", struct{ ActivityLog string }{activityLog})
}

// CommitMessage renders the commit message generation prompt.
func CommitMessage(d CommitData) string { return render("commit.tmpl", d) }

// DriftAssessment renders the task-done drift-assessment prompt.
func DriftAssessment(d DriftData) string { return render("drift.tmpl", d) }

// ConflictResolution renders the rebase conflict resolution prompt.
func ConflictResolution(d ConflictData) string { return render("conflict.tmpl", d) }

// SpecSystemEmpty renders the spec-mode prompt prefix used
// when the workspace spec tree is empty (no non-archived parseable
// specs). Encourages the agent to emit a `/spec-new` directive for
// substantive spec work.
func SpecSystemEmpty() string {
	return render("spec_system_empty.tmpl", nil)
}

// SpecSystemNonempty renders the spec-mode prompt prefix used
// when at least one non-archived spec exists. Steers the agent toward
// editing existing specs rather than creating new ones.
func SpecSystemNonempty() string {
	return render("spec_system_nonempty.tmpl", nil)
}
