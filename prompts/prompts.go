// Package prompts provides template-based rendering for all agent prompt
// strings used throughout wallfacer. Templates live alongside this file as
// *.tmpl files and are embedded into the binary at compile time via go:embed.
package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"text/template"
)

//go:embed *.tmpl
var fs embed.FS

var tmpl *template.Template

func init() {
	var err error
	tmpl, err = template.New("").
		Funcs(template.FuncMap{
			"add": func(a, b int) int { return a + b },
		}).
		ParseFS(fs, "*.tmpl")
	if err != nil {
		panic(fmt.Sprintf("prompts: parse templates: %v", err))
	}
}

func render(name string, data any) string {
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, name, data); err != nil {
		panic(fmt.Sprintf("prompts: render %s: %v", name, err))
	}
	return buf.String()
}

// RefinementData holds template variables for the refinement prompt.
type RefinementData struct {
	CreatedAt        string
	Today            string
	AgeDays          int
	Status           string
	Prompt           string
	UserInstructions string // optional; rendered only when non-empty
}

// Refinement renders the spec-writing agent prompt.
func Refinement(d RefinementData) string {
	return render("refinement.tmpl", d)
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

// Ideation renders the brainstorm agent prompt.
func Ideation(d IdeationData) string {
	return render("ideation.tmpl", d)
}

// Oversight renders the oversight summarization prompt for the given
// pre-formatted activity log text.
func Oversight(activityLog string) string {
	return render("oversight.tmpl", struct{ ActivityLog string }{activityLog})
}

// Title renders the title-generation prompt for the given task prompt.
func Title(taskPrompt string) string {
	return render("title.tmpl", struct{ Prompt string }{taskPrompt})
}

// CommitData holds template variables for the commit message prompt.
type CommitData struct {
	Prompt    string
	DiffStat  string
	RecentLog string // optional; rendered only when non-empty
}

// CommitMessage renders the commit message generation prompt.
func CommitMessage(d CommitData) string {
	return render("commit.tmpl", d)
}

// ConflictData holds template variables for the conflict resolution prompt.
type ConflictData struct {
	ContainerPath string
	DefaultBranch string
}

// ConflictResolution renders the rebase conflict resolution prompt.
func ConflictResolution(d ConflictData) string {
	return render("conflict.tmpl", d)
}

// TestData holds template variables for the test verification prompt.
type TestData struct {
	OriginalPrompt string
	Criteria       string // optional
	ImplResult     string // optional
	Diff           string // optional
}

// TestVerification renders the test verification agent prompt.
func TestVerification(d TestData) string {
	return render("test.tmpl", d)
}
