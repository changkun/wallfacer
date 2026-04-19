// Package envconfig provides helpers for reading and updating the wallfacer
// .env file that is passed to task containers via --env-file.
package envconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/atomicfile"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/store"
)

// Config holds the known configuration values from the .env file.
type Config struct {
	OAuthToken           string // CLAUDE_CODE_OAUTH_TOKEN
	APIKey               string // ANTHROPIC_API_KEY
	AuthToken            string // ANTHROPIC_AUTH_TOKEN (gateway proxy token)
	BaseURL              string // ANTHROPIC_BASE_URL
	ServerAPIKey         string // WALLFACER_SERVER_API_KEY
	DefaultModel         string // CLAUDE_DEFAULT_MODEL
	TitleModel           string // CLAUDE_TITLE_MODEL
	MaxParallelTasks     int    // WALLFACER_MAX_PARALLEL (0 means use default)
	MaxTestParallelTasks int    // WALLFACER_MAX_TEST_PARALLEL (0 means use default)
	OversightInterval    int    // WALLFACER_OVERSIGHT_INTERVAL in minutes (0 = disabled)
	ArchivedTasksPerPage int    // WALLFACER_ARCHIVED_TASKS_PER_PAGE (0 means use default)
	AutoPushEnabled      bool   // WALLFACER_AUTO_PUSH ("true"/"false")
	AutoPushThreshold    int    // WALLFACER_AUTO_PUSH_THRESHOLD (0 means use default of 1)
	PlanningWindowDays   int    // WALLFACER_PLANNING_WINDOW_DAYS — default planning cost window (days); 0 = all time

	// OpenAI Codex sandbox fields.
	OpenAIAPIKey      string // OPENAI_API_KEY
	OpenAIBaseURL     string // OPENAI_BASE_URL
	CodexDefaultModel string // CODEX_DEFAULT_MODEL
	CodexTitleModel   string // CODEX_TITLE_MODEL

	DefaultSandbox        sandbox.Type // WALLFACER_DEFAULT_SANDBOX
	ImplementationSandbox sandbox.Type // WALLFACER_SANDBOX_IMPLEMENTATION
	TestingSandbox        sandbox.Type // WALLFACER_SANDBOX_TESTING
	RefinementSandbox     sandbox.Type // WALLFACER_SANDBOX_REFINEMENT
	TitleSandbox          sandbox.Type // WALLFACER_SANDBOX_TITLE
	OversightSandbox      sandbox.Type // WALLFACER_SANDBOX_OVERSIGHT
	CommitMessageSandbox  sandbox.Type // WALLFACER_SANDBOX_COMMIT_MESSAGE
	IdeaAgentSandbox      sandbox.Type // WALLFACER_SANDBOX_IDEA_AGENT
	SandboxFast           bool         // WALLFACER_SANDBOX_FAST ("true"/"false"), defaults to true when unset

	// SandboxBackend is populated by the `wallfacer run --backend` flag (not
	// read from the .env file). "" and "local" mean the container backend;
	// "host" selects the host-process backend. Kept on Config as plumbing so
	// the runner's New() reads it from one place regardless of source.
	SandboxBackend   string
	HostClaudeBinary string // WALLFACER_HOST_CLAUDE_BINARY — explicit path to host claude CLI (optional)
	HostCodexBinary  string // WALLFACER_HOST_CODEX_BINARY  — explicit path to host codex CLI  (optional)
	ContainerNetwork string // WALLFACER_CONTAINER_NETWORK
	ContainerCPUs    string // WALLFACER_CONTAINER_CPUS   e.g. "2.0" (empty = no limit)
	ContainerMemory  string // WALLFACER_CONTAINER_MEMORY e.g. "4g"  (empty = no limit)
	TaskWorkers      bool   // WALLFACER_TASK_WORKERS ("true"/"false"), defaults to true when unset
	DependencyCaches bool   // WALLFACER_DEPENDENCY_CACHES ("true"/"false"), defaults to false
	TerminalEnabled  bool   // WALLFACER_TERMINAL_ENABLED ("true"/"false"), defaults to true when unset

	Workspaces []string // WALLFACER_WORKSPACES (path-list separated absolute paths)

	// Cloud gates every cloud-only UI surface and HTTP route (latere.ai
	// sign-in badge today; tenant-filesystem, billing, remote-control
	// later). Sourced from WALLFACER_CLOUD; parsed here so the CLI entry
	// point reads a single config surface.
	Cloud bool // WALLFACER_CLOUD ("true"/"1"/"yes", case-insensitive)
}

// knownKeys is the ordered list of keys managed by this package.
// This order determines where newly-appended keys appear in the file.
// Note: ANTHROPIC_AUTH_TOKEN is intentionally omitted — it is read-only
// (parsed but never written by Update) because it is managed externally.
var knownKeys = []string{
	"CLAUDE_CODE_OAUTH_TOKEN",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_BASE_URL",
	"WALLFACER_SERVER_API_KEY",
	"OPENAI_API_KEY",
	"OPENAI_BASE_URL",
	"CLAUDE_DEFAULT_MODEL",
	"CLAUDE_TITLE_MODEL",
	"CODEX_DEFAULT_MODEL",
	"CODEX_TITLE_MODEL",
	"WALLFACER_MAX_PARALLEL",
	"WALLFACER_MAX_TEST_PARALLEL",
	"WALLFACER_OVERSIGHT_INTERVAL",
	"WALLFACER_ARCHIVED_TASKS_PER_PAGE",
	"WALLFACER_AUTO_PUSH",
	"WALLFACER_AUTO_PUSH_THRESHOLD",
	"WALLFACER_PLANNING_WINDOW_DAYS",
	"WALLFACER_DEFAULT_SANDBOX",
	"WALLFACER_SANDBOX_IMPLEMENTATION",
	"WALLFACER_SANDBOX_TESTING",
	"WALLFACER_SANDBOX_REFINEMENT",
	"WALLFACER_SANDBOX_TITLE",
	"WALLFACER_SANDBOX_OVERSIGHT",
	"WALLFACER_SANDBOX_COMMIT_MESSAGE",
	"WALLFACER_SANDBOX_IDEA_AGENT",
	"WALLFACER_SANDBOX_FAST",
	"WALLFACER_HOST_CLAUDE_BINARY",
	"WALLFACER_HOST_CODEX_BINARY",
	"WALLFACER_CONTAINER_NETWORK",
	"WALLFACER_CONTAINER_CPUS",
	"WALLFACER_CONTAINER_MEMORY",
	"WALLFACER_TASK_WORKERS",
	"WALLFACER_DEPENDENCY_CACHES",
	"WALLFACER_TERMINAL_ENABLED",
	"WALLFACER_WORKSPACES",
	"WALLFACER_CLOUD",
}

// Parse reads the env file at path and returns the known configuration values.
// Lines that are blank or start with "#" are ignored. Unknown keys are skipped.
func Parse(path string) (Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	// SandboxFast, TaskWorkers, and TerminalEnabled default to true; only an
	// explicit "false" value in the file disables them. This opt-out semantic
	// means missing keys preserve the safer default (features enabled).
	//
	// PlanningWindowDays defaults to 30 so the planning-cost period picker
	// opens on a sensible "last month" view when the user hasn't configured
	// anything. An explicit 0 in the file still means "all time".
	cfg := Config{
		SandboxFast:        true,
		TaskWorkers:        true,
		TerminalEnabled:    true,
		PlanningWindowDays: 30,
	}
	for line := range strings.SplitSeq(string(raw), "\n") {
		k, v, ok := parseEnvLine(line)
		if !ok {
			continue
		}
		switch k {
		case "CLAUDE_CODE_OAUTH_TOKEN":
			cfg.OAuthToken = v
		case "ANTHROPIC_API_KEY":
			cfg.APIKey = v
		case "ANTHROPIC_AUTH_TOKEN":
			cfg.AuthToken = v
		case "ANTHROPIC_BASE_URL":
			cfg.BaseURL = v
		case "WALLFACER_SERVER_API_KEY":
			cfg.ServerAPIKey = v
		case "CLAUDE_DEFAULT_MODEL":
			cfg.DefaultModel = v
		case "CLAUDE_TITLE_MODEL":
			cfg.TitleModel = v
		case "WALLFACER_MAX_PARALLEL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.MaxParallelTasks = n
			}
		case "WALLFACER_MAX_TEST_PARALLEL":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.MaxTestParallelTasks = n
			}
		case "WALLFACER_OVERSIGHT_INTERVAL":
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				cfg.OversightInterval = n
			}
		case "WALLFACER_ARCHIVED_TASKS_PER_PAGE":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.ArchivedTasksPerPage = n
			}
		case "WALLFACER_AUTO_PUSH":
			cfg.AutoPushEnabled = v == "true"
		case "WALLFACER_AUTO_PUSH_THRESHOLD":
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				cfg.AutoPushThreshold = n
			}
		case "WALLFACER_PLANNING_WINDOW_DAYS":
			// 0 means "all time"; negative values are rejected silently (keeps
			// the initialized default of 30).
			if n, err := strconv.Atoi(v); err == nil && n >= 0 {
				cfg.PlanningWindowDays = n
			}
		case "OPENAI_API_KEY":
			cfg.OpenAIAPIKey = v
		case "OPENAI_BASE_URL":
			cfg.OpenAIBaseURL = v
		case "CODEX_DEFAULT_MODEL":
			cfg.CodexDefaultModel = v
		case "CODEX_TITLE_MODEL":
			cfg.CodexTitleModel = v
		case "WALLFACER_DEFAULT_SANDBOX":
			cfg.DefaultSandbox = sandbox.Normalize(v)
		case "WALLFACER_SANDBOX_IMPLEMENTATION":
			cfg.ImplementationSandbox = sandbox.Normalize(v)
		case "WALLFACER_SANDBOX_TESTING":
			cfg.TestingSandbox = sandbox.Normalize(v)
		case "WALLFACER_SANDBOX_REFINEMENT":
			cfg.RefinementSandbox = sandbox.Normalize(v)
		case "WALLFACER_SANDBOX_TITLE":
			cfg.TitleSandbox = sandbox.Normalize(v)
		case "WALLFACER_SANDBOX_OVERSIGHT":
			cfg.OversightSandbox = sandbox.Normalize(v)
		case "WALLFACER_SANDBOX_COMMIT_MESSAGE":
			cfg.CommitMessageSandbox = sandbox.Normalize(v)
		case "WALLFACER_SANDBOX_IDEA_AGENT":
			cfg.IdeaAgentSandbox = sandbox.Normalize(v)
		case "WALLFACER_SANDBOX_FAST":
			cfg.SandboxFast = v != "false"
		case "WALLFACER_HOST_CLAUDE_BINARY":
			cfg.HostClaudeBinary = v
		case "WALLFACER_HOST_CODEX_BINARY":
			cfg.HostCodexBinary = v
		case "WALLFACER_CONTAINER_NETWORK":
			cfg.ContainerNetwork = v
		case "WALLFACER_CONTAINER_CPUS":
			cfg.ContainerCPUs = v
		case "WALLFACER_CONTAINER_MEMORY":
			cfg.ContainerMemory = v
		case "WALLFACER_TASK_WORKERS":
			cfg.TaskWorkers = v != "false"
		case "WALLFACER_DEPENDENCY_CACHES":
			cfg.DependencyCaches = v == "true"
		case "WALLFACER_TERMINAL_ENABLED":
			cfg.TerminalEnabled = v != "false"
		case "WALLFACER_WORKSPACES":
			cfg.Workspaces = ParseWorkspaces(v)
		case "WALLFACER_CLOUD":
			cfg.Cloud = parseBoolFlag(v)
		}
	}
	return cfg, nil
}

// parseBoolFlag accepts the common truthy spellings used in environment
// variables: "true", "1", and "yes" (all case-insensitive). Everything
// else — including empty, "false", "0", "no" — parses as false. Used by
// flags that are explicitly documented as accepting "true/1/yes", not
// the older WALLFACER_* flags that use `v == "true"` or `v != "false"`.
func parseBoolFlag(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "yes":
		return true
	}
	return false
}

// ParseWorkspaces decodes WALLFACER_WORKSPACES from an OS path-list formatted
// string (':' on Unix, ';' on Windows), trimming empty entries.
func ParseWorkspaces(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := filepath.SplitList(raw)
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// FormatWorkspaces encodes workspaces for WALLFACER_WORKSPACES using the OS
// path-list separator.
func FormatWorkspaces(workspaces []string) string {
	if len(workspaces) == 0 {
		return ""
	}
	return strings.Join(workspaces, string(os.PathListSeparator))
}

// SandboxByActivity returns the per-activity sandbox type overrides derived from config.
func (c Config) SandboxByActivity() map[store.SandboxActivity]sandbox.Type {
	out := map[store.SandboxActivity]sandbox.Type{}
	if c.ImplementationSandbox != "" {
		out[store.SandboxActivityImplementation] = c.ImplementationSandbox
	}
	if c.TestingSandbox != "" {
		out[store.SandboxActivityTesting] = c.TestingSandbox
	}
	if c.RefinementSandbox != "" {
		out[store.SandboxActivityRefinement] = c.RefinementSandbox
	}
	if c.TitleSandbox != "" {
		out[store.SandboxActivityTitle] = c.TitleSandbox
	}
	if c.OversightSandbox != "" {
		out[store.SandboxActivityOversight] = c.OversightSandbox
	}
	if c.CommitMessageSandbox != "" {
		out[store.SandboxActivityCommitMessage] = c.CommitMessageSandbox
	}
	if c.IdeaAgentSandbox != "" {
		out[store.SandboxActivityIdeaAgent] = c.IdeaAgentSandbox
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// parseEnvLine parses a single .env line in a permissive way:
// - trims whitespace
// - ignores blank and comment-only lines
// - accepts leading "export " prefix
// - supports inline comments after quoted/unquoted values
// - preserves literal '#' inside quoted strings
func parseEnvLine(line string) (key, value string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	}

	k, v, hasEquals := strings.Cut(line, "=")
	if !hasEquals {
		return "", "", false
	}

	k = strings.TrimSpace(k)
	v = strings.TrimSpace(stripEnvInlineComment(v))
	return k, unquote(v), true
}

// stripEnvInlineComment removes a trailing # comment from a value string,
// respecting single and double quotes so that hash characters inside quoted
// regions are preserved literally.
func stripEnvInlineComment(v string) string {
	inSingleQuote := false
	inDoubleQuote := false
	escapeNext := false

	for i := 0; i < len(v); i++ {
		c := v[i]

		if escapeNext {
			escapeNext = false
			continue
		}
		if c == '\\' && inDoubleQuote {
			escapeNext = true
			continue
		}

		switch c {
		case '\'':
			if !inDoubleQuote {
				inSingleQuote = !inSingleQuote
			}
		case '"':
			if !inSingleQuote {
				inDoubleQuote = !inDoubleQuote
			}
		case '#':
			if !inSingleQuote && !inDoubleQuote {
				return strings.TrimSpace(v[:i])
			}
		}
	}

	return strings.TrimSpace(v)
}

// Updates holds optional changes for each env key.
//
// Each pointer field controls how the corresponding key is handled:
//   - nil → leave the existing line unchanged
//   - non-nil, non-empty → set to the provided value
//   - non-nil, empty → remove the line (clear the value)
type Updates struct {
	OAuthToken           *string
	APIKey               *string
	BaseURL              *string
	ServerAPIKey         *string
	OpenAIAPIKey         *string
	OpenAIBaseURL        *string
	DefaultModel         *string
	TitleModel           *string
	CodexDefaultModel    *string
	CodexTitleModel      *string
	MaxParallel          *string
	MaxTestParallel      *string
	OversightInterval    *string
	ArchivedTasksPerPage *string
	AutoPush             *string
	AutoPushThreshold    *string
	SandboxFast          *string
	ContainerNetwork     *string
	ContainerCPUs        *string
	ContainerMemory      *string
	TerminalEnabled      *string
	Workspaces           *string
}

// Update merges changes into the env file at path.
// Keys not already present in the file are appended when non-empty.
// Comments and unrecognized keys are preserved verbatim.
func Update(path string, u Updates) error {
	updates := map[string]*string{
		"CLAUDE_CODE_OAUTH_TOKEN":           u.OAuthToken,
		"ANTHROPIC_API_KEY":                 u.APIKey,
		"ANTHROPIC_BASE_URL":                u.BaseURL,
		"WALLFACER_SERVER_API_KEY":          u.ServerAPIKey,
		"OPENAI_API_KEY":                    u.OpenAIAPIKey,
		"OPENAI_BASE_URL":                   u.OpenAIBaseURL,
		"CLAUDE_DEFAULT_MODEL":              u.DefaultModel,
		"CLAUDE_TITLE_MODEL":                u.TitleModel,
		"CODEX_DEFAULT_MODEL":               u.CodexDefaultModel,
		"CODEX_TITLE_MODEL":                 u.CodexTitleModel,
		"WALLFACER_MAX_PARALLEL":            u.MaxParallel,
		"WALLFACER_MAX_TEST_PARALLEL":       u.MaxTestParallel,
		"WALLFACER_OVERSIGHT_INTERVAL":      u.OversightInterval,
		"WALLFACER_ARCHIVED_TASKS_PER_PAGE": u.ArchivedTasksPerPage,
		"WALLFACER_AUTO_PUSH":               u.AutoPush,
		"WALLFACER_AUTO_PUSH_THRESHOLD":     u.AutoPushThreshold,
		"WALLFACER_SANDBOX_FAST":            u.SandboxFast,
		"WALLFACER_CONTAINER_NETWORK":       u.ContainerNetwork,
		"WALLFACER_CONTAINER_CPUS":          u.ContainerCPUs,
		"WALLFACER_CONTAINER_MEMORY":        u.ContainerMemory,
		"WALLFACER_TERMINAL_ENABLED":        u.TerminalEnabled,
		"WALLFACER_WORKSPACES":              u.Workspaces,
	}
	return updateFile(path, updates)
}

// UpdateWorkspaces replaces or clears WALLFACER_WORKSPACES in the env file.
func UpdateWorkspaces(path string, workspaces []string) error {
	encoded := FormatWorkspaces(workspaces)
	return updateFile(path, map[string]*string{
		"WALLFACER_WORKSPACES": &encoded,
	})
}

// UpdateSandboxSettings merges global sandbox-routing settings into the env file.
// defaultSandbox controls WALLFACER_DEFAULT_SANDBOX.
// sandboxByActivity supports keys: implementation, testing, refinement, title,
// oversight, commit_message, idea_agent.
func UpdateSandboxSettings(path string, defaultSandbox *sandbox.Type, sandboxByActivity map[store.SandboxActivity]sandbox.Type) error {
	var impl, test, refine, title, oversight, commit, idea *string
	var defaultSandboxValue *string
	if sandboxByActivity != nil {
		// Two-phase approach: start with all activity pointers set to empty strings,
		// which means "clear the line from the file". Then overwrite only the
		// activities present in the caller's map with their actual values.
		// This ensures that omitted activities are actively removed rather than
		// silently left stale.
		emptyImpl, emptyTest, emptyRefine := "", "", ""
		emptyTitle, emptyOversight, emptyCommit, emptyIdea := "", "", "", ""
		impl, test, refine = &emptyImpl, &emptyTest, &emptyRefine
		title, oversight, commit, idea = &emptyTitle, &emptyOversight, &emptyCommit, &emptyIdea

		if v, ok := sandboxByActivity[store.SandboxActivityImplementation]; ok {
			s := string(sandbox.Normalize(string(v)))
			impl = &s
		}
		if v, ok := sandboxByActivity[store.SandboxActivityTesting]; ok {
			s := string(sandbox.Normalize(string(v)))
			test = &s
		}
		if v, ok := sandboxByActivity[store.SandboxActivityRefinement]; ok {
			s := string(sandbox.Normalize(string(v)))
			refine = &s
		}
		if v, ok := sandboxByActivity[store.SandboxActivityTitle]; ok {
			s := string(sandbox.Normalize(string(v)))
			title = &s
		}
		if v, ok := sandboxByActivity[store.SandboxActivityOversight]; ok {
			s := string(sandbox.Normalize(string(v)))
			oversight = &s
		}
		if v, ok := sandboxByActivity[store.SandboxActivityCommitMessage]; ok {
			s := string(sandbox.Normalize(string(v)))
			commit = &s
		}
		if v, ok := sandboxByActivity[store.SandboxActivityIdeaAgent]; ok {
			s := string(sandbox.Normalize(string(v)))
			idea = &s
		}
	}

	if defaultSandbox != nil {
		s := string(sandbox.Normalize(string(*defaultSandbox)))
		defaultSandboxValue = &s
	}

	// Read the file early so we can pass the raw bytes to updateRawWithUpdates.
	// This avoids a double-read that would otherwise happen via updateFile.
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}

	updates := map[string]*string{
		"WALLFACER_DEFAULT_SANDBOX":        defaultSandboxValue,
		"WALLFACER_SANDBOX_IMPLEMENTATION": impl,
		"WALLFACER_SANDBOX_TESTING":        test,
		"WALLFACER_SANDBOX_REFINEMENT":     refine,
		"WALLFACER_SANDBOX_TITLE":          title,
		"WALLFACER_SANDBOX_OVERSIGHT":      oversight,
		"WALLFACER_SANDBOX_COMMIT_MESSAGE": commit,
		"WALLFACER_SANDBOX_IDEA_AGENT":     idea,
	}
	return updateRawWithUpdates(path, raw, updates)
}

// updateFile reads the env file at path and applies the given updates map.
// It delegates to updateRawWithUpdates after reading the file contents.
func updateFile(path string, updates map[string]*string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}
	return updateRawWithUpdates(path, raw, updates)
}

// updateRawWithUpdates applies a set of key updates to raw env file content.
// It performs a three-phase merge:
//  1. Scan existing lines, updating or clearing matched keys in-place.
//  2. Append any new keys (not already in the file) in knownKeys order.
//  3. Strip blank lines introduced by clearing, then write atomically.
func updateRawWithUpdates(path string, raw []byte, updates map[string]*string) error {
	lines := strings.Split(string(raw), "\n")
	seen := map[string]bool{}

	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") || trimmed == "" {
			continue
		}
		k, _, ok := strings.Cut(trimmed, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		ptr, known := updates[k]
		if !known {
			continue
		}
		seen[k] = true
		if ptr == nil {
			// No change requested.
			continue
		}
		if *ptr == "" {
			// Clear: remove the line by blanking it.
			lines[i] = ""
		} else {
			lines[i] = k + "=" + *ptr
		}
	}

	// Append new keys (in stable order) that weren't already in the file.
	for _, k := range knownKeys {
		ptr, ok := updates[k]
		if !ok {
			continue
		}
		if seen[k] || ptr == nil || *ptr == "" {
			continue
		}
		lines = append(lines, k+"="+*ptr)
	}

	// Remove blank lines introduced by clearing, then ensure a single trailing newline.
	var kept []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" || !isBlankRemovable(l) {
			kept = append(kept, l)
		}
	}
	content := strings.TrimRight(strings.Join(kept, "\n"), "\n") + "\n"

	return atomicfile.Write(path, []byte(content), 0600)
}

// unquote strips matching double or single quotes surrounding a value.
func unquote(v string) string {
	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

// isBlankRemovable returns true for lines that consist only of whitespace.
// These lines are removed during the cleanup phase of updateRawWithUpdates
// to prevent gaps left by cleared keys from accumulating across updates.
func isBlankRemovable(l string) bool {
	return strings.TrimSpace(l) == ""
}

// MaskToken returns a redacted representation of a token for display.
// Short or empty tokens are fully masked.
func MaskToken(v string) string {
	if v == "" {
		return ""
	}
	if len(v) <= 8 {
		return strings.Repeat("*", len(v))
	}
	return v[:4] + "..." + v[len(v)-4:]
}
