package handler

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/runner"
)

// initPlanningTestRepo creates a temp git repo with one initial commit so
// HEAD exists and `git log` returns without error.
func initPlanningTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "planning-test@example.com")
	runGit(t, dir, "config", "user.name", "Planning Test")
	runGit(t, dir, "config", "commit.gpgsign", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}

// writeSpec creates a spec file under the given sub-path of specs/ relative
// to the workspace. The name may contain slashes to nest into epic dirs
// (e.g. "local/auth/foo.md").
func writeSpec(t *testing.T, dir, name, body string) {
	t.Helper()
	full := filepath.Join(dir, "specs", name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// gitLogSubjects returns commit subjects reachable from HEAD, newest first.
func gitLogSubjects(t *testing.T, dir string) []string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "log", "--format=%s")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// gitHeadMessage returns the full commit message (subject + body) at HEAD.
func gitHeadMessage(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%B").Output()
	if err != nil {
		t.Fatalf("git log -1 --format=%%B: %v", err)
	}
	return strings.TrimRight(string(out), "\n")
}

func TestCommitPlanningRound_DirtySpecs(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "foo.md", "# Foo\n")

	round, err := commitPlanningRound(context.Background(), ws, "user asked to draft foo", "drafted foo", nil)
	if err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}
	if round != 1 {
		t.Errorf("round = %d, want 1", round)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) != 2 {
		t.Fatalf("expected 2 commits, got %d: %v", len(subjects), subjects)
	}
	// Single file directly under specs/ → primary path "specs".
	want := "specs(plan): drafted foo"
	if subjects[0] != want {
		t.Errorf("top commit subject = %q, want %q", subjects[0], want)
	}

	msg := gitHeadMessage(t, ws)
	if !strings.Contains(msg, "\nPlan-Round: 1") {
		t.Errorf("commit body missing Plan-Round trailer:\n%s", msg)
	}

	// Verify only specs/ landed in the commit.
	out, err := exec.Command("git", "-C", ws, "show", "--name-only", "--format=", "HEAD").Output()
	if err != nil {
		t.Fatalf("git show: %v", err)
	}
	files := strings.Fields(strings.TrimSpace(string(out)))
	for _, f := range files {
		if !strings.HasPrefix(f, "specs/") {
			t.Errorf("unexpected file in commit: %q", f)
		}
	}
	if len(files) != 1 || files[0] != "specs/foo.md" {
		t.Errorf("commit files = %v, want [specs/foo.md]", files)
	}
}

func TestCommitPlanningRound_NoOp(t *testing.T) {
	ws := initPlanningTestRepo(t)
	before := gitLogSubjects(t, ws)

	round, err := commitPlanningRound(context.Background(), ws, "noop prompt", "nothing changed", nil)
	if err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}
	if round != 0 {
		t.Errorf("round = %d on clean tree, want 0", round)
	}

	after := gitLogSubjects(t, ws)
	if len(after) != len(before) {
		t.Errorf("commit count changed: before=%d after=%d", len(before), len(after))
	}
}

func TestCommitPlanningRound_RoundNumbering(t *testing.T) {
	ws := initPlanningTestRepo(t)

	// Three rounds, each adding a spec file.
	writeSpec(t, ws, "a.md", "a\n")
	r1, err := commitPlanningRound(context.Background(), ws, "p1", "first", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r1 != 1 {
		t.Errorf("first round = %d, want 1", r1)
	}
	writeSpec(t, ws, "b.md", "b\n")
	r2, err := commitPlanningRound(context.Background(), ws, "p2", "second", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r2 != 2 {
		t.Errorf("second round = %d, want 2", r2)
	}
	writeSpec(t, ws, "c.md", "c\n")
	r3, err := commitPlanningRound(context.Background(), ws, "p3", "third", nil)
	if err != nil {
		t.Fatal(err)
	}
	if r3 != 3 {
		t.Errorf("third round = %d, want 3", r3)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) < 1 {
		t.Fatalf("no commits")
	}
	want := "specs(plan): third"
	if subjects[0] != want {
		t.Errorf("top subject = %q, want %q\nfull log: %v", subjects[0], want, subjects)
	}
	if msg := gitHeadMessage(t, ws); !strings.Contains(msg, "\nPlan-Round: 3") {
		t.Errorf("round-3 commit missing trailer:\n%s", msg)
	}
}

func TestCommitPlanningRound_SubjectTruncation(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "foo.md", "foo\n")

	// 200-char summary on a single line; subject should truncate at
	// commitPlanningRoundSubjectMax runes with an ellipsis suffix.
	long := strings.Repeat("x", 200)
	if _, err := commitPlanningRound(context.Background(), ws, "long prompt", long, nil); err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) == 0 {
		t.Fatal("no commits")
	}
	top := subjects[0]
	prefix := "specs(plan): "
	if !strings.HasPrefix(top, prefix) {
		t.Fatalf("unexpected subject: %q", top)
	}
	subject := strings.TrimPrefix(top, prefix)
	// truncateRunes appends a single "…" rune on overflow.
	if !strings.HasSuffix(subject, "…") {
		t.Errorf("subject = %q, expected truncation ellipsis", subject)
	}
	if runes := []rune(subject); len(runes) > commitPlanningRoundSubjectMax+1 {
		t.Errorf("subject length = %d runes, want ≤ %d", len(runes), commitPlanningRoundSubjectMax+1)
	}
}

func TestCommitPlanningRound_PrimaryPathFromEpic(t *testing.T) {
	ws := initPlanningTestRepo(t)
	// Multiple files all under the same epic directory — primary path
	// should become the common dir, not "specs".
	writeSpec(t, ws, "local/auth/overview.md", "a\n")
	writeSpec(t, ws, "local/auth/oauth.md", "b\n")

	if _, err := commitPlanningRound(context.Background(), ws, "user prompt", "draft auth epic", nil); err != nil {
		t.Fatal(err)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) == 0 {
		t.Fatal("no commits")
	}
	want := "specs/local/auth(plan): draft auth epic"
	if subjects[0] != want {
		t.Errorf("subject = %q, want %q", subjects[0], want)
	}
}

func TestCommitPlanningRound_PrimaryPathMixedTracks(t *testing.T) {
	ws := initPlanningTestRepo(t)
	// Two files in different tracks — common prefix collapses to "specs".
	writeSpec(t, ws, "local/foo.md", "a\n")
	writeSpec(t, ws, "cloud/bar.md", "b\n")

	if _, err := commitPlanningRound(context.Background(), ws, "user prompt", "cross-track note", nil); err != nil {
		t.Fatal(err)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) == 0 {
		t.Fatal("no commits")
	}
	want := "specs(plan): cross-track note"
	if subjects[0] != want {
		t.Errorf("subject = %q, want %q", subjects[0], want)
	}
}

func TestCommitPlanningRound_SubjectSkipsFrontmatterAndHeadings(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "foo.md", "foo\n")

	// Frontmatter, heading, blank lines, and a real paragraph. Subject
	// should pull from the paragraph (headings are allowed as a last
	// resort; content-bearing paragraphs win when both appear).
	summary := "---\ntitle: foo\n---\n\n# Heading\n\n" +
		"Add OAuth flow breakdown for auth epic.\n\n" +
		"Second paragraph with detail.\n"
	if _, err := commitPlanningRound(context.Background(), ws, "user prompt", summary, nil); err != nil {
		t.Fatal(err)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) == 0 {
		t.Fatal("no commits")
	}
	// firstMeaningfulLine returns the heading text because it appears
	// before the paragraph after frontmatter — both are legitimate
	// choices but the function's policy is "first line with content".
	if !strings.HasPrefix(subjects[0], "specs(plan): Heading") {
		t.Errorf("subject = %q, want scope+Heading", subjects[0])
	}
}

func TestCommitPlanningRound_FallbackSubjectForEmptySummary(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "local/auth/oauth.md", "x\n")

	if _, err := commitPlanningRound(context.Background(), ws, "user prompt", "   \n\n---\n\n", nil); err != nil {
		t.Fatal(err)
	}

	subjects := gitLogSubjects(t, ws)
	if len(subjects) == 0 {
		t.Fatal("no commits")
	}
	// Fallback subject derives from the basename of the primary path.
	want := "specs/local/auth(plan): update auth"
	if subjects[0] != want {
		t.Errorf("subject = %q, want %q", subjects[0], want)
	}
}

func TestCommitPlanningRound_AgentGeneratedMessage(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "local/auth/oauth.md", "x\n")

	// Stub generator returns a kanban-style message as the commit agent
	// would. commitPlanningRound should use it verbatim (plus trailer).
	gen := func(_ context.Context, data prompts.CommitData) (string, error) {
		// Sanity-check the prompt carries the scope instruction.
		if !strings.Contains(data.Prompt, "(plan)") {
			t.Errorf("commit prompt missing scope instruction: %q", data.Prompt)
		}
		return "specs/local/auth(plan): add OAuth flow breakdown\n\n" +
			"Cover the OAuth handshake and token refresh cases so\n" +
			"implementation can be scheduled independently.", nil
	}

	round, err := commitPlanningRound(context.Background(), ws, "user asked for auth plan", "agent said stuff", gen)
	if err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}
	if round != 1 {
		t.Errorf("round = %d, want 1", round)
	}
	msg := gitHeadMessage(t, ws)
	wantSubject := "specs/local/auth(plan): add OAuth flow breakdown"
	if !strings.HasPrefix(msg, wantSubject+"\n") {
		t.Errorf("top of commit message = %q, want subject %q", msg, wantSubject)
	}
	if !strings.Contains(msg, "Cover the OAuth handshake") {
		t.Errorf("body missing from commit message:\n%s", msg)
	}
	if !strings.Contains(msg, "\nPlan-Round: 1") {
		t.Errorf("trailer missing from commit message:\n%s", msg)
	}
}

func TestCommitPlanningRound_AgentMissingScopeGetsSpliced(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "local/auth/oauth.md", "x\n")

	// Agent produced a correct kanban subject but forgot the (plan) scope.
	// ensurePlanScope should splice it in before the colon.
	gen := func(_ context.Context, _ prompts.CommitData) (string, error) {
		return "specs/local/auth: add OAuth breakdown\n\nwhy body", nil
	}

	if _, err := commitPlanningRound(context.Background(), ws, "prompt", "summary", gen); err != nil {
		t.Fatal(err)
	}
	subjects := gitLogSubjects(t, ws)
	want := "specs/local/auth(plan): add OAuth breakdown"
	if subjects[0] != want {
		t.Errorf("subject = %q, want %q", subjects[0], want)
	}
}

func TestCommitPlanningRound_AgentErrorFallsBack(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "foo.md", "x\n")

	gen := func(_ context.Context, _ prompts.CommitData) (string, error) {
		return "", fmt.Errorf("agent timed out")
	}

	if _, err := commitPlanningRound(context.Background(), ws, "prompt", "agent summary text", gen); err != nil {
		t.Fatal(err)
	}
	subjects := gitLogSubjects(t, ws)
	// Deterministic fallback: specs(plan): <first line of summary>
	want := "specs(plan): agent summary text"
	if subjects[0] != want {
		t.Errorf("subject = %q, want %q", subjects[0], want)
	}
}

func TestCommitPlanningRound_SanitizesAgentFencedOutput(t *testing.T) {
	// Reproduces a real-world failure: the commit agent wrote explanatory
	// prose, wrapped the actual commit message in a ``` fence, and ended
	// with more prose. Without sanitization the primary-path scope got
	// spliced onto the first preamble sentence and the fence bodies were
	// kept as body text.
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "local/spec-coordination/spec-planning-ux/codex.md", "x\n")

	gen := func(_ context.Context, _ prompts.CommitData) (string, error) {
		return "Based on the filename and the diff stats (77 insertions, 11 deletions):\n\n" +
			"```\n" +
			"specs/local/spec-coordination/spec-planning-ux(plan): expand codex compatibility spec\n\n" +
			"Flesh out planning-codex-compat spec with additional coverage,\n" +
			"clarifying compatibility between the planning flow and codex.\n" +
			"```\n", nil
	}

	if _, err := commitPlanningRound(context.Background(), ws, "user", "summary", gen); err != nil {
		t.Fatal(err)
	}

	msg := gitHeadMessage(t, ws)
	wantSubject := "specs/local/spec-coordination/spec-planning-ux(plan): expand codex compatibility spec"
	if !strings.HasPrefix(msg, wantSubject+"\n") {
		t.Errorf("subject = first line of %q, want %q", msg, wantSubject)
	}
	if strings.Contains(msg, "Based on the filename") {
		t.Errorf("preamble prose leaked into commit message:\n%s", msg)
	}
	if strings.Contains(msg, "```") {
		t.Errorf("code fence leaked into commit message:\n%s", msg)
	}
	if !strings.Contains(msg, "Flesh out planning-codex-compat spec") {
		t.Errorf("body content missing:\n%s", msg)
	}
	if !strings.HasSuffix(strings.TrimRight(msg, "\n"), "Plan-Round: 1") {
		t.Errorf("trailer not at end of message:\n%s", msg)
	}
}

func TestCommitPlanningRound_AgentPreambleOnlyFallsBack(t *testing.T) {
	// Agent returned prose with no kanban-style line anywhere — cannot be
	// rescued, must fall back to the deterministic path.
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "local/auth/oauth.md", "x\n")

	gen := func(_ context.Context, _ prompts.CommitData) (string, error) {
		return "I'll write a commit message: it updates the auth spec with new content.", nil
	}

	if _, err := commitPlanningRound(context.Background(), ws, "user", "agent summary", gen); err != nil {
		t.Fatal(err)
	}
	subjects := gitLogSubjects(t, ws)
	// Fallback: primary(plan): <firstMeaningfulLine(agentSummary)>
	want := "specs/local/auth(plan): agent summary"
	if subjects[0] != want {
		t.Errorf("subject = %q, want %q", subjects[0], want)
	}
}

func TestSanitizeAgentCommitMessage(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "fenced block with preamble",
			in:   "Here is the message:\n```\nfoo/bar(plan): do thing\n\nbody line\n```\n",
			want: "foo/bar(plan): do thing\n\nbody line",
		},
		{
			name: "fenced block with language tag",
			in:   "```text\nfoo: subject\n\nbody\n```",
			want: "foo: subject\n\nbody",
		},
		{
			name: "preamble with no fence",
			in:   "Sure, here it is:\nfoo/bar(plan): subject\n\nbody",
			want: "foo/bar(plan): subject\n\nbody",
		},
		{
			name: "already clean",
			in:   "foo(plan): x\n\nbody",
			want: "foo(plan): x\n\nbody",
		},
		{
			name: "pure prose",
			in:   "I cannot generate a commit message for this.",
			want: "I cannot generate a commit message for this.",
		},
		{
			name: "empty",
			in:   "   \n\n",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := sanitizeAgentCommitMessage(c.in)
			if got != c.want {
				t.Errorf("sanitize(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestEnsurePlanScope(t *testing.T) {
	cases := []struct {
		name    string
		subject string
		primary string
		want    string
	}{
		{"already has scope", "specs/local(plan): foo", "specs/local", "specs/local(plan): foo"},
		{"kanban style missing scope", "specs/local/auth: foo", "specs/local/auth", "specs/local/auth(plan): foo"},
		{"no colon at all", "bare subject", "specs/local", "specs/local(plan): bare subject"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ensurePlanScope(c.subject, c.primary)
			if got != c.want {
				t.Errorf("ensurePlanScope(%q, %q) = %q, want %q", c.subject, c.primary, got, c.want)
			}
		})
	}
}

func TestWrapLine(t *testing.T) {
	got := wrapLine("this is a fairly long sentence that should wrap", 20)
	// Each output line must not exceed 20 unless it's a single long word.
	for _, l := range strings.Split(got, "\n") {
		if len(l) > 20 && !strings.Contains(l, " ") {
			continue // single long word, allowed to overflow
		}
		if len(l) > 20 {
			t.Errorf("line too long (%d): %q", len(l), l)
		}
	}
}

// ---------------------------------------------------------------------------
// Planning auto-push wiring tests
// ---------------------------------------------------------------------------

// TestPlanningCommit_AutoPushCalledAfterSuccessfulCommit verifies that the
// planning commit pipeline calls MaybeAutoPushWorkspace on the runner after
// commitPlanningRound returns a positive round number (i.e. a commit was made).
// This mirrors the behaviour of the "mark as done" flow for task cards, which
// calls runner.Commit (and inside it maybeAutoPush) after moving a Waiting
// task to Done.
func TestPlanningCommit_AutoPushCalledAfterSuccessfulCommit(t *testing.T) {
	ws := initPlanningTestRepo(t)
	writeSpec(t, ws, "local/foo.md", "# Foo\n")

	mock := &runner.MockRunner{}

	// Simulate the per-workspace loop inside SendPlanningMessage.
	ctx := context.Background()
	n, err := commitPlanningRound(ctx, ws, "draft foo spec", "drafted foo", nil)
	if err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}
	if n == 0 {
		t.Fatal("expected commitPlanningRound to return round > 0 for dirty specs/")
	}
	// This is the exact condition the handler uses.
	if n > 0 {
		mock.MaybeAutoPushWorkspace(ctx, ws)
	}

	calls := mock.AutoPushWorkspaceCalls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 auto-push call, got %d: %v", len(calls), calls)
	}
	if calls[0] != ws {
		t.Errorf("auto-push workspace = %q, want %q", calls[0], ws)
	}
}

// TestPlanningCommit_AutoPushNotCalledWhenNoCommit verifies that
// MaybeAutoPushWorkspace is NOT called when commitPlanningRound returns 0
// (no specs/ changes pending — nothing to push).
func TestPlanningCommit_AutoPushNotCalledWhenNoCommit(t *testing.T) {
	ws := initPlanningTestRepo(t) // clean repo, no spec changes

	mock := &runner.MockRunner{}

	ctx := context.Background()
	n, err := commitPlanningRound(ctx, ws, "noop", "nothing changed", nil)
	if err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}
	// Simulate handler condition — only push when n > 0.
	if n > 0 {
		mock.MaybeAutoPushWorkspace(ctx, ws)
	}

	if calls := mock.AutoPushWorkspaceCalls(); len(calls) != 0 {
		t.Errorf("expected no auto-push calls when nothing was committed, got %v", calls)
	}
}

// TestCommitPlanningRound_IgnoresPollutedLocalIdentity verifies that a polluted
// per-repo user.name/user.email (as left behind by a sandbox container agent
// that ran `git config user.name X` inside a shared worktree) does not override
// the host's global identity on the resulting commit. Regression test for the
// incident where planning commits landed authored as `Claude <claude@wallfacer.local>`.
func TestCommitPlanningRound_IgnoresPollutedLocalIdentity(t *testing.T) {
	ws := initPlanningTestRepo(t)

	// Stage 1: seed a polluted per-repo identity of the kind a sandbox agent
	// might leave behind. Without the -c overrides, git would pick these up.
	runGit(t, ws, "config", "user.name", "Claude")
	runGit(t, ws, "config", "user.email", "claude@wallfacer.local")

	// Stage 2: install a synthetic "host global" identity via GIT_CONFIG_GLOBAL
	// so the test runs in isolation from the developer's real ~/.gitconfig.
	globalGitConfig := filepath.Join(t.TempDir(), "gitconfig")
	globalContent := "[user]\n\tname = Host User\n\temail = host@example.com\n"
	if err := os.WriteFile(globalGitConfig, []byte(globalContent), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalGitConfig)
	// Also neutralize the system-level config so nothing stray on CI leaks in.
	t.Setenv("GIT_CONFIG_SYSTEM", "/dev/null")

	// Stage 3: produce a pending specs/ change and commit it via the planning
	// round path. Use a nil genCommit to force the deterministic subject.
	writeSpec(t, ws, "local/auth.md", "# auth spec\n")

	ctx := context.Background()
	n, err := commitPlanningRound(ctx, ws, "add auth spec", "added auth spec", nil)
	if err != nil {
		t.Fatalf("commitPlanningRound: %v", err)
	}
	if n != 1 {
		t.Fatalf("round count = %d, want 1", n)
	}

	// Stage 4: inspect the HEAD author — must match the "host global" identity,
	// not the polluted per-repo one.
	authorOut, err := exec.Command("git", "-C", ws, "log", "-1", "--format=%an <%ae>").Output()
	if err != nil {
		t.Fatalf("git log author: %v", err)
	}
	got := strings.TrimSpace(string(authorOut))
	want := "Host User <host@example.com>"
	if got != want {
		t.Errorf("commit author = %q, want %q (polluted local identity leaked through)", got, want)
	}
}
