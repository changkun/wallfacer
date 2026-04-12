package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

	"changkun.de/x/wallfacer/internal/spec"
)

// RunSpec dispatches `wallfacer spec <subcommand> [args]`. Currently supports
// only `validate`, but structured as a subcommand hub so more spec tooling
// (status, impact, diff) can land alongside without reshaping the CLI.
func RunSpec(_ string, args []string) {
	if len(args) == 0 {
		specUsage(os.Stderr)
		os.Exit(2)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "validate":
		runSpecValidate(rest)
	case "new":
		runSpecNew(rest)
	case "-h", "-help", "--help":
		specUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "wallfacer spec: unknown subcommand %q\n\n", sub)
		specUsage(os.Stderr)
		os.Exit(2)
	}
}

func specUsage(w *os.File) {
	_, _ = fmt.Fprint(w, "Usage: wallfacer spec <subcommand> [flags] [args...]\n\n"+
		"Subcommands:\n"+
		"  new        Create a new spec file with valid frontmatter defaults\n"+
		"  validate   Validate one or more spec files (or the whole specs/ tree)\n\n"+
		"Run 'wallfacer spec <subcommand> -h' for flags.\n")
}

// runSpecValidate implements `wallfacer spec validate [flags] [paths...]`.
//
// With no positional paths, it walks the `specs/` directory under the
// current working directory and reports every issue. When paths are given,
// validation still runs across the entire tree (cross-spec checks like
// cycle detection and unique-dispatch need the full graph) but the output
// is filtered to results for the requested files.
//
// Exit codes: 0 on clean runs (no errors; warnings are non-fatal), 1 when
// any error is reported, 2 on usage errors or tree build failure.
func runSpecValidate(args []string) {
	fs := flag.NewFlagSet("spec validate", flag.ExitOnError)
	specsDir := fs.String("specs-dir", "specs", "path to the specs/ root to validate")
	jsonOut := fs.Bool("json", false, "emit machine-readable JSON instead of the human report")
	warnings := fs.Bool("warnings", true, "include warnings (errors are always shown)")
	_ = fs.Parse(args)

	tree, err := spec.BuildTree(*specsDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallfacer spec validate: build tree from %s: %v\n", *specsDir, err)
		os.Exit(2)
	}

	results := spec.ValidateTree(tree, ".")

	// Parse-level errors are carried on the tree but not on the results
	// list; surface them too so typos in frontmatter don't go silent.
	for _, e := range tree.Errs {
		results = append(results, spec.Result{
			Severity: spec.SeverityError,
			Rule:     "parse",
			Message:  e.Error(),
		})
	}

	filter := pathFilter(fs.Args(), *specsDir)
	if filter != nil {
		results = filterResults(results, filter)
	}

	if !*warnings {
		results = filterSeverity(results, spec.SeverityError)
	}

	errCount, warnCount := countSeverities(results)

	if *jsonOut {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{
			"specs_dir":  *specsDir,
			"spec_count": len(tree.All),
			"errors":     errCount,
			"warnings":   warnCount,
			"results":    results,
		})
	} else {
		printValidateReport(tree, results, errCount, warnCount, len(fs.Args()) == 0)
	}

	if errCount > 0 {
		os.Exit(1)
	}
}

// pathFilter canonicalizes the user-provided paths so each can be compared
// against the `Path` field on spec.Result (which is stored as a path
// relative to the repo root, e.g. "specs/local/foo.md"). It accepts paths
// that are relative to CWD, already-relative-to-repo-root, or absolute;
// returns nil when no paths are provided.
func pathFilter(userPaths []string, specsDir string) map[string]bool {
	if len(userPaths) == 0 {
		return nil
	}
	wanted := make(map[string]bool, len(userPaths))
	cwd, _ := os.Getwd()
	for _, p := range userPaths {
		candidates := []string{p}
		if !filepath.IsAbs(p) {
			// Try the path verbatim plus as-resolved from CWD.
			if cwd != "" {
				candidates = append(candidates, filepath.Join(cwd, p))
			}
		}
		for _, c := range candidates {
			// Normalise: any form reduces to a forward-slash path rooted
			// at specsDir (matching what spec.Path records).
			if rel, err := filepath.Rel(filepath.Dir(specsDir), c); err == nil && !strings.HasPrefix(rel, "..") {
				wanted[filepath.ToSlash(rel)] = true
			}
		}
		// Also accept the user's input verbatim.
		wanted[filepath.ToSlash(p)] = true
	}
	return wanted
}

func filterResults(results []spec.Result, wanted map[string]bool) []spec.Result {
	out := make([]spec.Result, 0, len(results))
	for _, r := range results {
		if wanted[r.Path] {
			out = append(out, r)
		}
	}
	return out
}

func filterSeverity(results []spec.Result, minSeverity spec.Severity) []spec.Result {
	if minSeverity != spec.SeverityError {
		return results
	}
	out := make([]spec.Result, 0, len(results))
	for _, r := range results {
		if r.Severity == spec.SeverityError {
			out = append(out, r)
		}
	}
	return out
}

func countSeverities(results []spec.Result) (errors, warnings int) {
	for _, r := range results {
		switch r.Severity {
		case spec.SeverityError:
			errors++
		case spec.SeverityWarning:
			warnings++
		}
	}
	return errors, warnings
}

// printValidateReport writes a human-readable summary grouped by spec path.
// Errors render first, then warnings, both sorted by (path, rule). fullScope
// is true when the user didn't restrict the run to specific paths — it
// adjusts the summary line to report the full tree size.
func printValidateReport(tree *spec.Tree, results []spec.Result, errCount, warnCount int, fullScope bool) {
	if len(results) == 0 {
		if fullScope {
			fmt.Printf("%s✓%s %d specs, no issues\n", ansiGreen(), ansiReset, len(tree.All))
		} else {
			fmt.Printf("%s✓%s no issues\n", ansiGreen(), ansiReset)
		}
		return
	}

	byPath := make(map[string][]spec.Result, len(results))
	for _, r := range results {
		byPath[r.Path] = append(byPath[r.Path], r)
	}
	paths := make([]string, 0, len(byPath))
	for p := range byPath {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for _, p := range paths {
		rs := byPath[p]
		sort.SliceStable(rs, func(i, j int) bool {
			if rs[i].Severity != rs[j].Severity {
				return rs[i].Severity == spec.SeverityError
			}
			return rs[i].Rule < rs[j].Rule
		})
		label := p
		if label == "" {
			label = "(tree)"
		}
		fmt.Printf("%s%s%s\n", ansiBold, label, ansiReset)
		for _, r := range rs {
			tag := ansiRed() + "error  "
			if r.Severity == spec.SeverityWarning {
				tag = ansiYellow() + "warning"
			}
			fmt.Printf("  %s%s  %-22s  %s\n", tag, ansiReset, r.Rule, r.Message)
		}
	}

	fmt.Println()
	fmt.Printf("%d error(s), %d warning(s)", errCount, warnCount)
	if fullScope {
		fmt.Printf(" across %d specs", len(tree.All))
	}
	fmt.Println()
}

// ANSI helpers kept as functions (not constants) so tests can swap them
// out or strip them without mutating package-level constants.
func ansiGreen() string  { return "\033[32m" }
func ansiRed() string    { return "\033[31m" }
func ansiYellow() string { return "\033[33m" }

// runSpecNew implements `wallfacer spec new <path> [flags]`. Writes a
// minimal spec file with valid frontmatter defaults so the author can
// start editing immediately. The default status is `vague`, which
// suppresses the body-not-empty validation warning — the skeleton body
// is a placeholder, not a finished spec.
func runSpecNew(args []string) {
	fs := flag.NewFlagSet("spec new", flag.ExitOnError)
	title := fs.String("title", "", "spec title (default: Title Case of the file name)")
	status := fs.String("status", "vague", "initial status (one of: "+strings.Join(statusesForHelp(), ", ")+")")
	effort := fs.String("effort", "medium", "initial effort estimate (one of: "+strings.Join(effortsForHelp(), ", ")+")")
	author := fs.String("author", "", "spec author (default: git config user.name, else 'unknown')")
	force := fs.Bool("force", false, "overwrite the file if it already exists")
	_ = fs.Parse(args)

	rest := fs.Args()
	if len(rest) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: wallfacer spec new [flags] <path>")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Path must be under a track directory, e.g. specs/local/my-feature.md.")
		fmt.Fprintln(os.Stderr, "Flags must come before the path (standard Go flag parsing).")
		os.Exit(2)
	}
	path := rest[0]

	if err := validateSpecPath(path); err != nil {
		fmt.Fprintf(os.Stderr, "wallfacer spec new: %v\n", err)
		os.Exit(2)
	}
	if !slices.Contains(stringStatuses(), *status) {
		fmt.Fprintf(os.Stderr, "wallfacer spec new: invalid -status %q (want one of: %s)\n",
			*status, strings.Join(statusesForHelp(), ", "))
		os.Exit(2)
	}
	if !slices.Contains(stringEfforts(), *effort) {
		fmt.Fprintf(os.Stderr, "wallfacer spec new: invalid -effort %q (want one of: %s)\n",
			*effort, strings.Join(effortsForHelp(), ", "))
		os.Exit(2)
	}

	if _, err := os.Stat(path); err == nil && !*force {
		fmt.Fprintf(os.Stderr, "wallfacer spec new: %s already exists (use -force to overwrite)\n", path)
		os.Exit(1)
	}

	effectiveTitle := *title
	if effectiveTitle == "" {
		effectiveTitle = titleFromFilename(path)
	}
	effectiveAuthor := *author
	if effectiveAuthor == "" {
		effectiveAuthor = resolveAuthor()
	}

	content := renderSpecSkeleton(effectiveTitle, *status, *effort, effectiveAuthor, time.Now())

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "wallfacer spec new: mkdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "wallfacer spec new: write: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("%s✓%s created %s\n", ansiGreen(), ansiReset, path)
}

// validateSpecPath checks that path looks like a valid spec location —
// lives under a specs/<track>/ directory and ends in .md. Does not
// check the parent directory exists on disk; MkdirAll handles that.
func validateSpecPath(path string) error {
	clean := filepath.ToSlash(filepath.Clean(path))
	if !strings.HasSuffix(clean, ".md") {
		return fmt.Errorf("path must end in .md, got %q", path)
	}
	parts := strings.Split(clean, "/")
	if len(parts) < 3 || parts[0] != "specs" {
		return fmt.Errorf("path must be under a track directory, e.g. specs/local/my-feature.md (got %q)", path)
	}
	if strings.TrimSpace(parts[1]) == "" {
		return fmt.Errorf("track directory (specs/<track>/...) is required")
	}
	return nil
}

// titleFromFilename turns "my-new-feature.md" into "My New Feature".
// Strips the extension, splits on hyphens/underscores, and Title-Cases
// each word. Non-ASCII input is passed through unchanged.
func titleFromFilename(path string) string {
	base := filepath.Base(path)
	base = strings.TrimSuffix(base, filepath.Ext(base))
	parts := strings.FieldsFunc(base, func(r rune) bool {
		return r == '-' || r == '_'
	})
	for i, p := range parts {
		if p == "" {
			continue
		}
		runes := []rune(p)
		runes[0] = unicode.ToUpper(runes[0])
		parts[i] = string(runes)
	}
	if len(parts) == 0 {
		return base
	}
	return strings.Join(parts, " ")
}

// resolveAuthor asks git for the current user.name; falls back to
// "unknown" when git is not installed or no name is configured. Keeps
// `spec new` usable in throwaway environments.
func resolveAuthor() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name
		}
	}
	return "unknown"
}

// renderSpecSkeleton produces the full file content — frontmatter plus
// a minimal body skeleton. All dates use the provided `now` so tests
// can inject a fixed timestamp.
func renderSpecSkeleton(title, status, effort, author string, now time.Time) string {
	date := now.Format("2006-01-02")
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + title + "\n")
	b.WriteString("status: " + status + "\n")
	b.WriteString("depends_on: []\n")
	b.WriteString("affects: []\n")
	b.WriteString("effort: " + effort + "\n")
	b.WriteString("created: " + date + "\n")
	b.WriteString("updated: " + date + "\n")
	b.WriteString("author: " + author + "\n")
	b.WriteString("dispatched_task_id: null\n")
	b.WriteString("---\n\n")
	b.WriteString("# " + title + "\n\n")
	b.WriteString("## Problem\n\n")
	b.WriteString("<!-- What problem does this spec address? Why now? -->\n\n")
	b.WriteString("## Design\n\n")
	b.WriteString("<!-- High-level approach. Key decisions and trade-offs. -->\n\n")
	b.WriteString("## Acceptance\n\n")
	b.WriteString("<!-- How will we know this is done? Tests, behaviour changes, files touched. -->\n")
	return b.String()
}

func stringStatuses() []string {
	all := spec.ValidStatuses()
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = string(s)
	}
	return out
}

func stringEfforts() []string {
	all := spec.ValidEfforts()
	out := make([]string, len(all))
	for i, e := range all {
		out[i] = string(e)
	}
	return out
}

// statusesForHelp / effortsForHelp wrap their string-slice counterparts
// so the flag-help strings don't need to re-do the conversion inline.
func statusesForHelp() []string { return stringStatuses() }
func effortsForHelp() []string  { return stringEfforts() }
