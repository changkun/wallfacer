package cli

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/pkg/cmdexec"
)

// RunDoctor implements the `wallfacer doctor` subcommand.
// It displays configuration paths, checks prerequisites, and reports
// whether credentials, agent backends, and git are ready. Items marked
// [!] need attention; [ ] are optional.
func RunDoctor(configDir string, args []string) {
	fs := flag.NewFlagSet("doctor", flag.ExitOnError)
	_ = fs.Parse(args)

	v := Version
	if v == "" {
		v = "dev"
	}
	fmt.Printf("wallfacer doctor (%s)\n\n", v)

	issues := 0
	envFile := envOrDefault("ENV_FILE", filepath.Join(configDir, ".env"))

	// --- Paths ---
	fmt.Printf("Config directory:  %s\n", configDir)
	fmt.Printf("Data directory:    %s\n", envOrDefault("DATA_DIR", filepath.Join(configDir, "data")))
	fmt.Printf("Env file:          %s\n", envFile)
	fmt.Printf("Prompts dir:       %s\n", filepath.Join(configDir, "prompts"))
	fmt.Println()

	// --- Config directory ---
	if info, err := os.Stat(configDir); err != nil {
		fmt.Printf("[!] Config directory missing: %s\n", configDir)
		fmt.Printf("    Run 'wallfacer run' once to auto-create it.\n")
		issues++
	} else if !info.IsDir() {
		fmt.Printf("[!] %s exists but is not a directory\n", configDir)
		issues++
	} else {
		fmt.Printf("[ok] Config directory exists\n")
	}

	// --- .env file ---
	raw, err := os.ReadFile(envFile)
	if err != nil {
		fmt.Printf("[!] Env file not found: %s\n", envFile)
		fmt.Printf("    Run 'wallfacer run' once to auto-create it.\n")
		issues++
	} else {
		fmt.Printf("[ok] Env file exists\n")
	}

	// --- Parse env values ---
	vals := map[string]string{}
	if raw != nil {
		for line := range strings.SplitSeq(string(raw), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "#") || line == "" {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}

	// --- Claude Code sandbox credentials ---
	fmt.Println()
	fmt.Println("Claude Code sandbox:")
	oauthToken := vals["CLAUDE_CODE_OAUTH_TOKEN"]
	apiKey := vals["ANTHROPIC_API_KEY"]
	switch {
	case oauthToken != "" && oauthToken != "your-oauth-token-here":
		fmt.Printf("[ok] CLAUDE_CODE_OAUTH_TOKEN is set (%s)\n", envconfig.MaskToken(oauthToken))
	case apiKey != "":
		fmt.Printf("[ok] ANTHROPIC_API_KEY is set (%s)\n", envconfig.MaskToken(apiKey))
	default:
		fmt.Printf("[!] No Claude credential (CLAUDE_CODE_OAUTH_TOKEN or ANTHROPIC_API_KEY)\n")
		fmt.Printf("    Set one in Settings → API Configuration.\n")
		issues++
	}
	printOptionalVar(vals, "ANTHROPIC_BASE_URL", "using default")
	printOptionalVar(vals, "CLAUDE_DEFAULT_MODEL", "using Claude Code default")
	printOptionalVar(vals, "CLAUDE_TITLE_MODEL", "falls back to default model")

	// --- OpenAI Codex sandbox credentials ---
	fmt.Println()
	fmt.Println("OpenAI Codex sandbox:")
	if openAIKey := vals["OPENAI_API_KEY"]; openAIKey != "" {
		fmt.Printf("[ok] OPENAI_API_KEY is set (%s)\n", envconfig.MaskToken(openAIKey))
	} else {
		fmt.Printf("[ ] OPENAI_API_KEY not set\n")
	}
	printOptionalVar(vals, "OPENAI_BASE_URL", "using OpenAI default")
	printOptionalVar(vals, "CODEX_DEFAULT_MODEL", "using Codex default")
	printOptionalVar(vals, "CODEX_TITLE_MODEL", "falls back to CODEX_DEFAULT_MODEL")

	fmt.Println()
	issues += checkHostBackend(vals)

	// --- Git ---
	fmt.Println()
	if gitPath, err := exec.LookPath("git"); err != nil {
		fmt.Printf("[!] Git not found\n")
		fmt.Printf("    Git is needed for worktrees, diffs, and auto-push.\n")
		issues++
	} else {
		out, _ := cmdexec.New(gitPath, "--version").Output()
		fmt.Printf("[ok] %s\n", out)
	}

	// --- Summary ---
	fmt.Println()
	if issues == 0 {
		fmt.Printf("All checks passed. Ready to run.\n")
	} else {
		fmt.Printf("%d issue(s) found. Fix the items marked [!] above.\n", issues)
	}
}

// printOptionalVar prints the value of an optional env variable or a
// "not set" note with the given fallback description.
func printOptionalVar(vals map[string]string, key, fallback string) {
	if v := vals[key]; v != "" {
		fmt.Printf("[ok] %s = %s\n", key, v)
	} else {
		fmt.Printf("[ ] %s not set (%s)\n", key, fallback)
	}
}

// checkHostBackend resolves the claude and codex binaries and probes each
// with --version. Returns the number of problems found (0 when all green).
// Claude is required; codex is optional (tasks routed to codex fail if
// missing, but claude-only hosts are still valid).
func checkHostBackend(vals map[string]string) int {
	issues := 0
	claude, claudeErr := resolveHostBinary(vals["WALLFACER_HOST_CLAUDE_BINARY"], "claude")
	if claudeErr != nil {
		fmt.Printf("[!] %s\n", claudeErr)
		fmt.Printf("    Install with: npm i -g @anthropic-ai/claude-code\n")
		issues++
	} else {
		fmt.Printf("[ok] Claude binary: %s\n", claude)
		if ver, err := cliVersion(claude); err == nil {
			fmt.Printf("     %s\n", strings.TrimSpace(ver))
		} else {
			fmt.Printf("[!] Claude --version failed: %v\n", err)
			issues++
		}
	}

	codex, codexErr := resolveHostBinary(vals["WALLFACER_HOST_CODEX_BINARY"], "codex")
	if codexErr != nil {
		fmt.Printf("[ ] codex binary not found (optional; codex-typed tasks will fail)\n")
		fmt.Printf("    Install with: npm i -g @openai/codex\n")
	} else {
		fmt.Printf("[ok] Codex binary: %s\n", codex)
		if ver, err := cliVersion(codex); err == nil {
			fmt.Printf("     %s\n", strings.TrimSpace(ver))
		} else {
			// Codex --version failure is a soft warning — the binary is on
			// disk but not responding. Treat like a missing codex for UX.
			fmt.Printf("[ ] Codex --version failed: %v\n", err)
		}
	}

	return issues
}

// resolveHostBinary mirrors executor.NewHostBackend's resolver, but returns a
// descriptive error instead of crashing. Used by doctor for readiness
// reporting so the user sees the same hint they'd get from `wallfacer run`
// failing at startup.
func resolveHostBinary(explicit, name string) (string, error) {
	if explicit != "" {
		if _, err := os.Stat(explicit); err != nil {
			return "", fmt.Errorf("%s binary not found at %q: %v", name, explicit, err)
		}
		return explicit, nil
	}
	path, err := exec.LookPath(name)
	if err != nil {
		return "", fmt.Errorf("%s binary not found in $PATH", name)
	}
	return path, nil
}

// cliVersion runs `<bin> --version` with a short timeout so a hung binary
// can't stall doctor. Returns stdout on success.
func cliVersion(bin string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := cmdexec.New(bin, "--version").WithContext(ctx).Output()
	if err != nil {
		return "", err
	}
	return out, nil
}
