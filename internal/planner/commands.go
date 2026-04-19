package planner

import (
	"bytes"
	"embed"
	"sort"
	"strings"
	"text/template"
)

// slugMaxLen caps the length of slugs produced by [Slugify] so the
// resulting path stays comfortable on Windows (MAX_PATH) and in small
// file trees. When trimmed, the cut prefers a word boundary (`-`) over
// a hard truncation so the slug still reads cleanly.
const slugMaxLen = 48

// Slugify turns a free-form title into a URL-safe spec filename stem:
// lowercase, runs of non-alphanumerics collapse to a single `-`, and
// the result is trimmed to [slugMaxLen] chars at the nearest trailing
// word boundary. Returns the empty string when the input has no
// alphanumeric characters — callers should treat that as an error.
func Slugify(title string) string {
	var b strings.Builder
	lastWasDash := true // treats leading separators as dashes so we skip them
	for _, r := range strings.ToLower(title) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastWasDash = false
			continue
		}
		if lastWasDash {
			continue
		}
		b.WriteByte('-')
		lastWasDash = true
	}
	out := strings.TrimRight(b.String(), "-")
	if out == "" {
		return ""
	}
	if len(out) <= slugMaxLen {
		return out
	}
	// Hunt for the last `-` before slugMaxLen so the cut lands at a
	// word boundary; fall back to a hard cut if the first word is
	// longer than the cap.
	cut := strings.LastIndexByte(out[:slugMaxLen], '-')
	if cut <= 0 {
		return out[:slugMaxLen]
	}
	return out[:cut]
}

//go:embed commands_templates/*.tmpl
var commandTemplatesFS embed.FS

// Command describes a slash command available in the planning chat.
type Command struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Usage       string `json:"usage"`
}

// commandDef is the internal definition used to register a command.
type commandDef struct {
	Command
	templateFile string // filename in commands_templates/
}

// CommandRegistry holds the built-in slash commands and their templates.
type CommandRegistry struct {
	commands map[string]commandDef
}

// NewCommandRegistry creates a registry with all built-in slash commands.
func NewCommandRegistry() *CommandRegistry {
	r := &CommandRegistry{
		commands: make(map[string]commandDef),
	}

	defs := []struct {
		name, desc, usage, tmpl string
	}{
		{"summarize", "Produce a structured summary of the focused spec", "/summarize [words]", "summarize.tmpl"},
		{"create", "Create a new spec file with proper frontmatter", "/create <title>", "create.tmpl"},
		{"validate", "Check the focused spec against document model rules", "/validate", "validate.tmpl"},
		{"impact", "Analyze what code and specs would be affected", "/impact", "impact.tmpl"},
		{"status", "Update the focused spec's status", "/status <state>", "status.tmpl"},
		{"break-down", "Decompose the focused spec into sub-specs or tasks", "/break-down [design|tasks]", "breakdown.tmpl"},
		{"review-breakdown", "Validate a task breakdown for correctness", "/review-breakdown", "review-breakdown.tmpl"},
		{"dispatch", "Dispatch the focused spec to the task board", "/dispatch", "dispatch.tmpl"},
		{"review-impl", "Review implementation against the spec's criteria", "/review-impl [commit-range]", "review-impl.tmpl"},
		{"diff", "Compare completed implementation against spec", "/diff [commit-range]", "diff.tmpl"},
		{"wrapup", "Finalize a completed spec with outcome and status", "/wrapup", "wrapup.tmpl"},
	}

	for _, d := range defs {
		r.commands[d.name] = commandDef{
			Command: Command{
				Name:        d.name,
				Description: d.desc,
				Usage:       d.usage,
			},
			templateFile: d.tmpl,
		}
	}
	return r
}

// Commands returns all registered commands sorted by name.
func (r *CommandRegistry) Commands() []Command {
	cmds := make([]Command, 0, len(r.commands))
	for _, def := range r.commands {
		cmds = append(cmds, def.Command)
	}
	sort.Slice(cmds, func(i, j int) bool { return cmds[i].Name < cmds[j].Name })
	return cmds
}

// expandData holds the template variables for slash command expansion.
type expandData struct {
	FocusedSpec string
	Args        string
	WordLimit   string
	Title       string
	State       string
}

// Expand checks if input is a slash command and expands it into a
// structured prompt. Returns the expanded text and true, or the
// original input and false if not a slash command.
func (r *CommandRegistry) Expand(input, focusedSpec string) (string, bool) {
	input = strings.TrimSpace(input)
	if !strings.HasPrefix(input, "/") {
		return input, false
	}

	// Split into command name and args.
	parts := strings.SplitN(input[1:], " ", 2)
	name := parts[0]
	args := ""
	if len(parts) > 1 {
		args = strings.TrimSpace(parts[1])
	}

	def, ok := r.commands[name]
	if !ok {
		return input, false
	}

	data := expandData{
		FocusedSpec: focusedSpec,
		Args:        args,
		WordLimit:   "200", // default
		Title:       args,
		State:       args,
	}

	// Override defaults from args for specific commands.
	if name == "summarize" && args != "" {
		data.WordLimit = args
	}

	content, err := commandTemplatesFS.ReadFile("commands_templates/" + def.templateFile)
	if err != nil {
		return input, false
	}

	t, err := template.New(name).Funcs(template.FuncMap{
		"slugify": Slugify,
	}).Parse(string(content))
	if err != nil {
		return input, false
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return input, false
	}

	return buf.String(), true
}
