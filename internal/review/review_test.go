package review

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
)

// fakeRunner answers reviewer runs from a script and records their inputs.
type fakeRunner struct {
	answers []func(in runner.ReviewerInput) (*runner.ReviewerResult, error)
	inputs  []runner.ReviewerInput
}

func (f *fakeRunner) RunReviewer(_ context.Context, _ *store.Task, in runner.ReviewerInput) (*runner.ReviewerResult, error) {
	f.inputs = append(f.inputs, in)
	i := min(len(f.inputs), len(f.answers)) - 1
	res, err := f.answers[i](in)
	if err == nil && in.OnStart != nil {
		in.OnStart()
	}
	return res, err
}

func answer(verdict runner.ReviewVerdict, tokens int, findings ...runner.ReviewFinding) func(runner.ReviewerInput) (*runner.ReviewerResult, error) {
	return func(runner.ReviewerInput) (*runner.ReviewerResult, error) {
		return &runner.ReviewerResult{
			Answer:  runner.ReviewAnswer{Verdict: verdict, Findings: findings, Summary: "s"},
			Model:   "reviewer-model",
			Harness: "claude",
			Usage:   store.TaskUsage{InputTokens: tokens, CostUSD: 0.01},
		}, nil
	}
}

func fail(err error) func(runner.ReviewerInput) (*runner.ReviewerResult, error) {
	return func(runner.ReviewerInput) (*runner.ReviewerResult, error) { return nil, err }
}

var (
	high = runner.ReviewFinding{Severity: runner.ReviewSeverityHigh, Claim: "Add writes to a nil map"}
	low  = runner.ReviewFinding{Severity: runner.ReviewSeverityLow, Claim: "typo in a comment"}
)

// newTask returns a task whose last turn reported result.
func newTask(result string) *store.Task {
	return &store.Task{Prompt: "p", Result: &result}
}

// verify runs one round with the given bounds.
func verify(t *testing.T, v *Reviewer, task *store.Task, stateDir string, rounds, costCap int) (*Result, error) {
	t.Helper()
	return v.Verify(context.Background(), Input{Task: task, Diff: "+x", StateDir: stateDir, MaxRounds: rounds, CostCapTokens: costCap})
}

// newestSession reads the newest session or fails the test.
func newestSession(t *testing.T, stateDir string) *Session {
	t.Helper()
	s, found, err := Newest(stateDir)
	if err != nil || !found {
		t.Fatalf("Newest: found=%v err=%v", found, err)
	}
	return s
}

func TestVerify_ApproveFinishesClean(t *testing.T) {
	dir := t.TempDir()
	v := New(&fakeRunner{answers: []func(runner.ReviewerInput) (*runner.ReviewerResult, error){answer(runner.ReviewApprove, 100, low)}})

	res, err := verify(t, v, newTask(""), dir, 3, 0)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Outcome != OutcomeFinished || res.Termination != TerminationApproved || res.Unresolved != 0 || res.Headline != "" {
		t.Errorf("result = %+v, want finished approved with nothing open", res)
	}
	s := newestSession(t, dir)
	if s.End == nil || s.End.Termination != TerminationApproved || s.End.Rounds != 1 || s.End.Tokens != 100 {
		t.Errorf("end = %+v", s.End)
	}
	if res.SessionDir != s.Dir {
		t.Errorf("SessionDir = %q, want %q", res.SessionDir, s.Dir)
	}
}

// TestVerify_FeedbackThenNextRound covers the round trip: changes requested
// produce feedback and keep the session open; the next call records the task's
// reply and gives the reviewer the prior findings and that reply.
func TestVerify_FeedbackThenNextRound(t *testing.T) {
	dir := t.TempDir()
	f := &fakeRunner{answers: []func(runner.ReviewerInput) (*runner.ReviewerResult, error){
		answer(runner.ReviewChangesRequested, 100, high, low),
		answer(runner.ReviewApprove, 100),
	}}
	v := New(f)

	res, err := verify(t, v, newTask("first turn"), dir, 3, 0)
	if err != nil {
		t.Fatalf("round 1: %v", err)
	}
	if res.Outcome != OutcomeFeedback || res.Round != 1 {
		t.Fatalf("round 1 result = %+v, want feedback", res)
	}
	for _, want := range []string{"round 1 of at most 3", "- [high] Add writes to a nil map", "- [low] typo in a comment"} {
		if !strings.Contains(res.Feedback, want) {
			t.Errorf("feedback lacks %q: %s", want, res.Feedback)
		}
	}
	if s := newestSession(t, dir); s.End != nil || !s.awaitingTurn() {
		t.Fatalf("session after round 1: end=%+v awaiting=%v, want open and awaiting the task's turn", s.End, s.awaitingTurn())
	}

	res, err = verify(t, v, newTask("fixed the nil map"), dir, 3, 0)
	if err != nil {
		t.Fatalf("round 2: %v", err)
	}
	if res.Outcome != OutcomeFinished || res.Termination != TerminationApproved || res.Round != 2 {
		t.Errorf("round 2 result = %+v, want approved in round 2", res)
	}
	in := f.inputs[1]
	if in.Round != 2 || len(in.PriorFindings) != 2 || in.ImplementerReply != "fixed the nil map" {
		t.Errorf("round 2 input = %+v", in)
	}
	roles := make([]string, 0, 4)
	for _, r := range newestSession(t, dir).Records {
		roles = append(roles, r.Role)
	}
	if got, want := strings.Join(roles, ","), "reviewer,feedback,implementer,reviewer"; got != want {
		t.Errorf("record roles = %s, want %s", got, want)
	}
}

// TestVerify_LastRoundLeavesFindingsOpen: the last allowed round still
// requesting changes ends the session with its findings open, the most severe
// as the headline, and sends nothing back.
func TestVerify_LastRoundLeavesFindingsOpen(t *testing.T) {
	dir := t.TempDir()
	v := New(&fakeRunner{answers: []func(runner.ReviewerInput) (*runner.ReviewerResult, error){answer(runner.ReviewChangesRequested, 100, high, low)}})

	res, err := verify(t, v, newTask(""), dir, 1, 0)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if res.Outcome != OutcomeFinished || res.Termination != TerminationMaxRounds {
		t.Fatalf("result = %+v, want finished on max rounds", res)
	}
	if res.Unresolved != 2 || res.Headline != high.Claim {
		t.Errorf("open = %d %q, want 2 with the high finding as headline", res.Unresolved, res.Headline)
	}
	if res.Feedback != "" {
		t.Error("the last round must not produce feedback")
	}
}

// TestVerify_FailedAttemptKeepsTheRound is the retry bound: a failed attempt
// is recorded, leaves the session open, and the retry runs the same round, so
// failures never add rounds or feedback turns.
func TestVerify_FailedAttemptKeepsTheRound(t *testing.T) {
	dir := t.TempDir()
	malformed := fmt.Errorf("review: %w", &runner.ReviewOutputError{Raw: "looks fine", Reason: "no JSON object"})
	f := &fakeRunner{answers: []func(runner.ReviewerInput) (*runner.ReviewerResult, error){
		fail(malformed),
		fail(errors.New("launch review container: exit status 1")),
		answer(runner.ReviewApprove, 10),
	}}
	v := New(f)

	_, err := verify(t, v, newTask(""), dir, 3, 0)
	var failure *Failure
	if !errors.As(err, &failure) || failure.Code != CodeOutputInvalid {
		t.Fatalf("err = %v, want a %s failure", err, CodeOutputInvalid)
	}
	if FailureMessage(err) != failureMessages[CodeOutputInvalid] {
		t.Errorf("FailureMessage = %q", FailureMessage(err))
	}
	_, err = verify(t, v, newTask(""), dir, 3, 0)
	if !errors.As(err, &failure) || failure.Code != CodeReviewerFailed {
		t.Fatalf("err = %v, want a %s failure", err, CodeReviewerFailed)
	}

	s := newestSession(t, dir)
	if s.End != nil || len(s.Records) != 2 || !s.Records[0].Failed() || s.Records[0].Raw != "looks fine" {
		t.Fatalf("session after failures = %+v, want open with two failed attempts and the raw answer", s)
	}

	res, err := verify(t, v, newTask(""), dir, 3, 0)
	if err != nil {
		t.Fatalf("Verify after failures: %v", err)
	}
	if res.Round != 1 {
		t.Errorf("round = %d, want 1: failed attempts do not consume rounds", res.Round)
	}
	for i, in := range f.inputs {
		if in.Round != 1 {
			t.Errorf("attempt %d ran round %d, want 1", i+1, in.Round)
		}
	}
	if newestSession(t, dir).ID != s.ID {
		t.Error("the retry must continue the session the failures are recorded in")
	}
}

// TestVerify_CostCap covers both checks of the token budget: after a round
// that requests changes, and before a round of a continued session.
func TestVerify_CostCap(t *testing.T) {
	t.Run("after a round", func(t *testing.T) {
		dir := t.TempDir()
		v := New(&fakeRunner{answers: []func(runner.ReviewerInput) (*runner.ReviewerResult, error){
			answer(runner.ReviewChangesRequested, 600, high),
			answer(runner.ReviewChangesRequested, 600, high),
		}})
		res, err := verify(t, v, newTask(""), dir, 5, 1000)
		if err != nil || res.Outcome != OutcomeFeedback {
			t.Fatalf("round 1 = %+v, %v; want feedback while under budget", res, err)
		}
		res, err = verify(t, v, newTask(""), dir, 5, 1000)
		if err != nil {
			t.Fatalf("round 2: %v", err)
		}
		if res.Outcome != OutcomeFinished || res.Termination != TerminationCostCap || res.Unresolved != 1 {
			t.Errorf("round 2 = %+v, want finished on the budget with one open finding", res)
		}
	})

	t.Run("before a round", func(t *testing.T) {
		dir := t.TempDir()
		f := &fakeRunner{answers: []func(runner.ReviewerInput) (*runner.ReviewerResult, error){answer(runner.ReviewChangesRequested, 600, high)}}
		v := New(f)
		if res, err := verify(t, v, newTask(""), dir, 5, 0); err != nil || res.Outcome != OutcomeFeedback {
			t.Fatalf("round 1 = %+v, %v", res, err)
		}
		// The budget is lowered under the open session: the next call ends it
		// without running a reviewer.
		res, err := verify(t, v, newTask("reply"), dir, 5, 500)
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		if res.Termination != TerminationCostCap || res.Unresolved != 1 || res.Headline != high.Claim {
			t.Errorf("result = %+v, want finished on the budget with the sent finding open", res)
		}
		if len(f.inputs) != 1 {
			t.Errorf("reviewer runs = %d, want 1", len(f.inputs))
		}
	})
}

// TestVerify_SkipIsRecordedOnce: a refusal writes a session that holds the
// skip; the same refusal again writes nothing and says it was not recorded;
// a different refusal is recorded.
func TestVerify_SkipIsRecordedOnce(t *testing.T) {
	dir := t.TempDir()
	same := &runner.ReviewRefusal{Code: runner.ReviewRefusedSameModel, Detail: "m"}
	unset := &runner.ReviewRefusal{Code: runner.ReviewRefusedModelUnset, Detail: "empty"}
	started := 0
	f := &fakeRunner{answers: []func(runner.ReviewerInput) (*runner.ReviewerResult, error){fail(same), fail(same), fail(unset)}}
	v := New(f)
	run := func() *Result {
		t.Helper()
		res, err := v.Verify(context.Background(), Input{Task: newTask(""), StateDir: dir, MaxRounds: 3, OnStart: func(int, int) { started++ }})
		if err != nil {
			t.Fatalf("Verify: %v", err)
		}
		return res
	}

	first := run()
	if first.Outcome != OutcomeSkipped || !first.SkipRecorded || first.Skip.Message != skipMessages[runner.ReviewRefusedSameModel] {
		t.Fatalf("first = %+v, want a recorded skip with its fixed sentence", first)
	}
	second := run()
	if second.SkipRecorded {
		t.Error("the same skip must not be recorded twice")
	}
	third := run()
	if !third.SkipRecorded || third.Skip.Code != runner.ReviewRefusedModelUnset {
		t.Errorf("third = %+v, want the new reason recorded", third)
	}
	entries, err := os.ReadDir(filepath.Join(dir, sessionsDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Errorf("sessions = %d, want 2 (one per distinct skip)", len(entries))
	}
	if started != 0 {
		t.Errorf("OnStart called %d times for skipped reviews, want 0", started)
	}
}

// TestVerify_StartsAfreshAfterLegacyOrSuperseded: a session of the earlier
// engine is never continued, and a superseded session is over, so both lead
// to a new session at round 1.
func TestVerify_StartsAfreshAfterLegacyOrSuperseded(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, sessionsDir, "zzzz-legacy")
	if err := os.MkdirAll(legacy, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacy, transcriptFile), []byte(`{"fork":1,"round":1,"role":"critic","path":"r1.md"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := &fakeRunner{answers: []func(runner.ReviewerInput) (*runner.ReviewerResult, error){answer(runner.ReviewChangesRequested, 1, high)}}
	v := New(f)

	res, err := verify(t, v, newTask(""), dir, 3, 0)
	if err != nil || res.Round != 1 || res.SessionDir == legacy {
		t.Fatalf("after legacy = %+v, %v; want round 1 in a new session", res, err)
	}
	if err := Supersede(res.SessionDir, time.Now()); err != nil {
		t.Fatalf("Supersede: %v", err)
	}
	next, err := verify(t, v, newTask(""), dir, 3, 0)
	if err != nil || next.Round != 1 || next.SessionDir == res.SessionDir {
		t.Fatalf("after supersede = %+v, %v; want round 1 in a new session", next, err)
	}
}

// TestNewest_PrefersCurrentFormat: the newest session is the current-format
// one with the greatest id, whatever the earlier engine left beside it.
func TestNewest_PrefersCurrentFormat(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	a, err := newSession(dir, now)
	if err != nil {
		t.Fatal(err)
	}
	b, err := newSession(dir, now.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, sessionsDir, "legacy"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := newestSession(t, dir)
	if s.ID != b.ID || s.Legacy {
		t.Errorf("newest = %s (legacy %v), want %s; %s is older", s.ID, s.Legacy, b.ID, a.ID)
	}

	only := t.TempDir()
	if err := os.MkdirAll(filepath.Join(only, sessionsDir, "old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if s := newestSession(t, only); !s.Legacy {
		t.Errorf("a directory without session.json should be Legacy: %+v", s)
	}
	if _, found, err := Newest(t.TempDir()); found || err != nil {
		t.Errorf("empty state dir: found=%v err=%v", found, err)
	}
}

// TestRead_TruncatedTranscript: a line over the read cap stops the scan; the
// records before it come back with Truncated set and the error.
func TestRead_TruncatedTranscript(t *testing.T) {
	s, err := newSession(t.TempDir(), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.append(Record{Round: 1, Role: RoleReviewer, Verdict: runner.ReviewApprove}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, transcriptFile), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"role":"feedback","body":"` + strings.Repeat("a", maxRecordLineBytes) + "\"}\n" + `{"role":"implementer"}` + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	got, err := Read(s.Dir)
	if err == nil || got == nil || !got.Truncated || len(got.Records) != 1 {
		t.Fatalf("Read = %+v, %v; want the first record, Truncated and an error", got, err)
	}
}

func TestCapBody(t *testing.T) {
	if capBody("short") != "short" {
		t.Error("short text must pass through")
	}
	long := strings.Repeat("é", maxBodyBytes)
	got := capBody(long)
	if !strings.HasSuffix(got, "(truncated)") || len(got) > maxBodyBytes+32 {
		t.Errorf("capBody kept %d bytes", len(got))
	}
	if !strings.HasPrefix(got, "é") || strings.ContainsRune(got[:len(got)-len("\n… (truncated)")], '�') {
		t.Error("capBody must cut on a rune boundary")
	}
}
