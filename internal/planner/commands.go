package planner

import (
	"bytes"
	"embed"
	"sort"
	"strings"
	"text/template"
)

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
		{"break-down", "Decompose the focused spec into sub-specs or tasks", "/break-down", "breakdown.tmpl"},
		{"create", "Create a new spec file with proper frontmatter", "/create <title>", "create.tmpl"},
		{"status", "Update the focused spec's status", "/status <state>", "status.tmpl"},
		{"validate", "Check the focused spec against document model rules", "/validate", "validate.tmpl"},
		{"impact", "Analyze what code and specs would be affected", "/impact", "impact.tmpl"},
		{"dispatch", "Prepare the focused spec for dispatch to the task board", "/dispatch", "dispatch.tmpl"},
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

	t, err := template.New(name).Parse(string(content))
	if err != nil {
		return input, false
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return input, false
	}

	return buf.String(), true
}
