// Package instructions manages workspace instruction files and their keys.
package instructions

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	// InstructionsFilename is the canonical workspace instructions filename.
	InstructionsFilename = "AGENTS.md"
	// LegacyInstructionsFilename is kept for backward compatibility.
	LegacyInstructionsFilename = "CLAUDE.md"
)

// defaultTemplate is the baseline AGENTS.md content written into every new
// workspace instructions file. It provides general guidance for agents
// operating inside a Wallfacer-managed task.
const defaultTemplate = `# Workspace Instructions

This file provides guidance to coding agents when working on tasks in this workspace.

## General Notes

- Complete the task described in the prompt.
- Make focused, well-scoped changes. Prefer editing existing files over creating new ones.
- Avoid over-engineering: only make changes that are directly requested or clearly necessary.
- Run tests if available to verify your changes work correctly.
- Write clear, descriptive commit messages explaining the "why" not just the "what".
- Do not create documentation files or README updates unless explicitly requested.
- Think deliberately, modularize and reuse if possible. Think about higher abstractions instead of narrow minded go ahead with implementation
- Test-driven, write tests to reproduce the feature so that it can be tested reliably and prevent future regression
- Ensure documents are updated accordingly
- When asked to create plans or specs, write them under the ` + "`specs/`" + ` in the respective git repository, never under ` + "`.claude/specs/`" + `.

## Board Context

A read-only board context is mounted at ` + "`/workspace/.tasks/board.json`" + `.
It contains a JSON manifest of all active tasks on the board including their
prompts, statuses, results, and branch names. Your task is marked with
` + "`\"is_self\": true`" + `.

Use this to avoid conflicting changes with sibling tasks or reference
completed work. If sibling worktrees are mounted, they appear under
` + "`/workspace/.tasks/worktrees/<short-id>/<repo>/`" + ` as read-only directories.
`

// workspaceLayoutSection is appended to the default template with the actual
// workspace basenames filled in, so the agent knows exactly where code lives.
const workspaceLayoutSection = `
## Workspace Layout

Workspaces are mounted under ` + "`/workspace/<name>/`" + `. **All file operations
(read, write, create) MUST target paths inside these directories.** Do NOT
create files or directories directly under ` + "`/workspace/`" + `.

`

// Key returns a stable 16-char hex key for a given set of workspace paths.
// The key is derived from the SHA-256 of the sorted, colon-joined absolute paths,
// so the same set of workspaces always maps to the same file regardless of order.
func Key(workspaces []string) string {
	sorted := make([]string, len(workspaces))
	copy(sorted, workspaces)
	sort.Strings(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, ":")))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars
}

// FilePath returns the path to the workspace AGENTS.md for a given set of
// workspace directories. Each unique combination of workspaces has its own file.
func FilePath(configDir string, workspaces []string) string {
	dir := filepath.Join(configDir, "instructions")
	return filepath.Join(dir, Key(workspaces)+".md")
}

// Ensure ensures the AGENTS.md for the given workspace set exists.
// If it does not exist yet it is created from the default template plus any
// AGENTS.md/CLAUDE.md files
// files found in the workspace directories. Returns the path to the file.
func Ensure(configDir string, workspaces []string) (string, error) {
	dir := filepath.Join(configDir, "instructions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create instructions dir: %w", err)
	}

	path := FilePath(configDir, workspaces)

	// Already exists — honour the user's edits, do not overwrite.
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	content := BuildContent(workspaces)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write instructions: %w", err)
	}
	return path, nil
}

// Reinit rebuilds the workspace AGENTS.md from the default template plus any
// per-repo AGENTS.md/CLAUDE.md files, overwriting any existing content.
func Reinit(configDir string, workspaces []string) (string, error) {
	dir := filepath.Join(configDir, "instructions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create instructions dir: %w", err)
	}

	path := FilePath(configDir, workspaces)
	content := BuildContent(workspaces)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write instructions: %w", err)
	}
	return path, nil
}

// BuildContent assembles AGENTS.md content from:
//  1. The default wallfacer instructions template.
//  2. A reference list of per-repo AGENTS.md/CLAUDE.md paths so agents can
//     read them on demand. The instructions file is mounted at
//     /workspace/AGENTS.md so it does not shadow any individual repo's
//     instructions file.
func BuildContent(workspaces []string) string {
	var sb strings.Builder
	sb.WriteString(defaultTemplate)

	// Append workspace layout section so Claude knows where each repo lives.
	sb.WriteString(workspaceLayoutSection)
	for _, ws := range workspaces {
		name := filepath.Base(ws)
		fmt.Fprintf(&sb, "- `/workspace/%s/`\n", name)
	}
	sb.WriteByte('\n')

	// List per-repo AGENTS.md/CLAUDE.md paths so the agent can read them on demand.
	var refs []string
	for _, ws := range workspaces {
		name := filepath.Base(ws)
		if _, err := os.Stat(filepath.Join(ws, InstructionsFilename)); err == nil {
			refs = append(refs, fmt.Sprintf("- `/workspace/%s/%s`", name, InstructionsFilename))
			continue
		}
		if _, err := os.Stat(filepath.Join(ws, LegacyInstructionsFilename)); err == nil {
			refs = append(refs, fmt.Sprintf("- `/workspace/%s/%s`", name, LegacyInstructionsFilename))
		}
	}
	if len(refs) > 0 {
		sb.WriteString("---\n\n## Repo-Specific Instructions\n\n")
		sb.WriteString("The repositories below have their own `AGENTS.md` (or legacy `CLAUDE.md`) with project-specific\n")
		sb.WriteString("instructions. Read the relevant file before working on tasks in that workspace:\n\n")
		for _, ref := range refs {
			sb.WriteString(ref + "\n")
		}
		sb.WriteByte('\n')
	}

	return sb.String()
}
