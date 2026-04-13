package spec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
)

// ScaffoldOptions controls the frontmatter fields of a new spec. Zero
// values are replaced with sensible defaults (see Scaffold). Path is the
// only required field.
type ScaffoldOptions struct {
	// Path is the target spec file, e.g. "specs/local/auth-refactor.md".
	// Must end in .md and live under a track directory (specs/<track>/...).
	Path string

	// Title defaults to a title-cased version of the base filename.
	Title string

	// Status defaults to [StatusVague].
	Status Status

	// Effort defaults to [EffortMedium].
	Effort Effort

	// Author defaults to the local git user.name, or "unknown" when git
	// is unavailable or has no name configured.
	Author string

	// DependsOn is an optional list of spec paths to seed the
	// frontmatter's depends_on list. Empty renders as "depends_on: []".
	DependsOn []string

	// Now is the timestamp used for the `created` and `updated`
	// frontmatter fields. Zero means time.Now(). Tests inject a fixed
	// value here so generated content is stable.
	Now time.Time

	// Force overwrites an existing file at Path. Without it, Scaffold
	// returns an error when the path is already taken.
	Force bool
}

// Scaffold creates a new spec file at opts.Path with valid frontmatter
// and a minimal body skeleton. It validates the path, resolves defaults,
// creates the parent directory if needed, and writes atomically-enough
// for the common case (single WriteFile). Returns the final path and a
// nil error on success.
//
// Both the CLI (`wallfacer spec new`) and the server-side chat hooks
// consume this function so there is a single source of truth for spec
// frontmatter. The agent never composes frontmatter itself.
func Scaffold(opts ScaffoldOptions) (string, error) {
	if err := ValidateSpecPath(opts.Path); err != nil {
		return "", err
	}

	if opts.Status == "" {
		opts.Status = StatusVague
	}
	if !slices.Contains(ValidStatuses(), opts.Status) {
		return "", fmt.Errorf("invalid status %q (want one of: %s)",
			opts.Status, strings.Join(statusStrings(), ", "))
	}

	if opts.Effort == "" {
		opts.Effort = EffortMedium
	}
	if !slices.Contains(ValidEfforts(), opts.Effort) {
		return "", fmt.Errorf("invalid effort %q (want one of: %s)",
			opts.Effort, strings.Join(effortStrings(), ", "))
	}

	if opts.Title == "" {
		opts.Title = TitleFromFilename(opts.Path)
	}
	if opts.Author == "" {
		opts.Author = ResolveAuthor()
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now()
	}

	content := RenderSkeleton(opts.Title, opts.Status, opts.Effort, opts.Author, opts.DependsOn, opts.Now)

	if err := os.MkdirAll(filepath.Dir(opts.Path), 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(opts.Path), err)
	}

	// Open with O_EXCL when Force is false so the kernel atomically
	// rejects a race where two concurrent Scaffold calls both stat
	// ENOENT and then both write — the second one would silently
	// clobber the first. Force=true falls back to plain WriteFile,
	// which is the documented "overwrite if present" behavior.
	if opts.Force {
		if err := os.WriteFile(opts.Path, []byte(content), 0o644); err != nil {
			return "", fmt.Errorf("write %s: %w", opts.Path, err)
		}
		return opts.Path, nil
	}
	f, err := os.OpenFile(opts.Path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%s already exists (pass Force=true to overwrite)", opts.Path)
		}
		return "", fmt.Errorf("create %s: %w", opts.Path, err)
	}
	defer func() { _ = f.Close() }()
	if _, err := f.Write([]byte(content)); err != nil {
		return "", fmt.Errorf("write %s: %w", opts.Path, err)
	}
	return opts.Path, nil
}

// ValidateSpecPath reports whether path is a legal spec location: must
// end in .md and sit under a non-empty track directory beneath specs/.
// Does not stat the parent directory — callers that write the file
// (e.g. [Scaffold]) are responsible for MkdirAll.
func ValidateSpecPath(path string) error {
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

// TitleFromFilename turns "my-new-feature.md" into "My New Feature".
// Strips the extension, splits on hyphens/underscores, and title-cases
// each word. Non-ASCII input is passed through unchanged aside from
// whitespace handling.
func TitleFromFilename(path string) string {
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

// ResolveAuthor returns the local git user.name. Falls back to
// "unknown" when git is not installed or no name is configured so
// Scaffold remains usable in throwaway environments.
func ResolveAuthor() string {
	out, err := exec.Command("git", "config", "user.name").Output()
	if err == nil {
		if name := strings.TrimSpace(string(out)); name != "" {
			return name
		}
	}
	return "unknown"
}

// RenderSkeleton produces the full file content — frontmatter plus a
// minimal body skeleton. Exposed so callers that want the generated
// bytes without writing to disk (tests, dry-runs) can reuse the same
// renderer. dependsOn nil/empty renders as `depends_on: []`; non-empty
// renders as a YAML list.
func RenderSkeleton(title string, status Status, effort Effort, author string, dependsOn []string, now time.Time) string {
	date := now.Format("2006-01-02")
	var b strings.Builder
	b.WriteString("---\n")
	b.WriteString("title: " + title + "\n")
	b.WriteString("status: " + string(status) + "\n")
	if len(dependsOn) == 0 {
		b.WriteString("depends_on: []\n")
	} else {
		b.WriteString("depends_on:\n")
		for _, d := range dependsOn {
			b.WriteString("  - " + d + "\n")
		}
	}
	b.WriteString("affects: []\n")
	b.WriteString("effort: " + string(effort) + "\n")
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

// statusStrings returns all valid Status values as strings, for use in
// error messages. Kept package-private; external callers who need this
// list should project [ValidStatuses] themselves.
func statusStrings() []string {
	all := ValidStatuses()
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = string(s)
	}
	return out
}

// effortStrings is the Effort analogue of statusStrings.
func effortStrings() []string {
	all := ValidEfforts()
	out := make([]string, len(all))
	for i, e := range all {
		out[i] = string(e)
	}
	return out
}
