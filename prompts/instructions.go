package prompts

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const (
	// CodexInstructionsFilename is the instructions filename for the Codex sandbox.
	CodexInstructionsFilename = "AGENTS.md"
	// ClaudeInstructionsFilename is the instructions filename for the Claude sandbox.
	ClaudeInstructionsFilename = "CLAUDE.md"
)

// InstructionsKey returns a stable 16-char hex key for a given set of workspace paths.
// The key is derived from the SHA-256 of the sorted, colon-joined absolute paths,
// so the same set of workspaces always maps to the same file regardless of order.
func InstructionsKey(workspaces []string) string {
	sorted := slices.Clone(workspaces)
	slices.Sort(sorted)
	h := sha256.Sum256([]byte(strings.Join(sorted, ":")))
	return fmt.Sprintf("%x", h[:8]) // 16 hex chars
}

// InstructionsFilePath returns the path to the workspace AGENTS.md for a given set of
// workspace directories. Each unique combination of workspaces has its own file.
func InstructionsFilePath(configDir string, workspaces []string) string {
	dir := filepath.Join(configDir, "instructions")
	return filepath.Join(dir, InstructionsKey(workspaces)+".md")
}

// EnsureInstructions ensures the AGENTS.md for the given workspace set exists.
// If it does not exist yet it is created from the default template plus any
// AGENTS.md/CLAUDE.md files found in the workspace directories.
// Returns the path to the file.
func EnsureInstructions(configDir string, workspaces []string) (string, error) {
	dir := filepath.Join(configDir, "instructions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create instructions dir: %w", err)
	}

	path := InstructionsFilePath(configDir, workspaces)

	// Already exists — honour the user's edits, do not overwrite.
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	content := BuildInstructionsContent(workspaces)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write instructions: %w", err)
	}
	return path, nil
}

// ReinitInstructions rebuilds the workspace AGENTS.md from the default template plus any
// per-repo AGENTS.md/CLAUDE.md files, overwriting any existing content.
func ReinitInstructions(configDir string, workspaces []string) (string, error) {
	dir := filepath.Join(configDir, "instructions")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("create instructions dir: %w", err)
	}

	path := InstructionsFilePath(configDir, workspaces)
	content := BuildInstructionsContent(workspaces)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("write instructions: %w", err)
	}
	return path, nil
}

// BuildInstructionsContent assembles AGENTS.md content by rendering the
// instructions template with workspace data. It scans workspaces for
// per-repo AGENTS.md/CLAUDE.md files and includes them as references.
func BuildInstructionsContent(workspaces []string) string {
	data := buildInstructionsData(workspaces)
	return Default.Instructions(data)
}

// buildInstructionsData scans workspace paths and builds the template data,
// detecting per-repo AGENTS.md or CLAUDE.md files for reference listing.
func buildInstructionsData(workspaces []string) InstructionsData {
	var data InstructionsData
	for _, ws := range workspaces {
		name := filepath.Base(ws)
		data.Workspaces = append(data.Workspaces, InstructionsWorkspace{Name: name})

		if _, err := os.Stat(filepath.Join(ws, CodexInstructionsFilename)); err == nil {
			data.RepoInstructionRefs = append(data.RepoInstructionRefs, InstructionsRepoRef{
				Workspace: name,
				Filename:  CodexInstructionsFilename,
			})
			continue
		}
		if _, err := os.Stat(filepath.Join(ws, ClaudeInstructionsFilename)); err == nil {
			data.RepoInstructionRefs = append(data.RepoInstructionRefs, InstructionsRepoRef{
				Workspace: name,
				Filename:  ClaudeInstructionsFilename,
			})
		}
	}
	return data
}
