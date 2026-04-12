package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

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
	case "-h", "-help", "--help":
		specUsage(os.Stdout)
	default:
		fmt.Fprintf(os.Stderr, "wallfacer spec: unknown subcommand %q\n\n", sub)
		specUsage(os.Stderr)
		os.Exit(2)
	}
}

func specUsage(w *os.File) {
	_, _ = fmt.Fprint(w, "Usage: wallfacer spec <subcommand> [flags] [paths...]\n\n"+
		"Subcommands:\n"+
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
