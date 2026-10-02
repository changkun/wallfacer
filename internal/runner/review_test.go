package runner

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/store"
)

// TestParseReviewAnswer covers the review binding's parser: answers it accepts
// (bare, fenced, with prose, severity synonyms) and malformed output, which
// must come back as a *ReviewOutputError carrying the raw text rather than a
// partial answer.
func TestParseReviewAnswer(t *testing.T) {
	t.Run("approve without findings", func(t *testing.T) {
		got, err := parseReviewAnswer(`{"verdict":"approve","findings":[],"summary":"looks right"}`)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got.Verdict != ReviewApprove || len(got.Findings) != 0 || got.Summary != "looks right" {
			t.Errorf("answer = %+v", got)
		}
	})

	t.Run("changes requested sorts findings most severe first", func(t *testing.T) {
		raw := "Here is my review.\n```json\n" +
			`{"verdict":"Changes_Requested","findings":[` +
			`{"severity":"low","claim":"typo in a comment"},` +
			`{"severity":"critical","claim":"nil map write in Add","location":"store.go:10"},` +
			`{"severity":"medium","claim":"no test for the empty case"}]}` +
			"\n```\nThanks."
		got, err := parseReviewAnswer(raw)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if got.Verdict != ReviewChangesRequested {
			t.Errorf("verdict = %q, want %q", got.Verdict, ReviewChangesRequested)
		}
		sev := make([]ReviewSeverity, len(got.Findings))
		for i, f := range got.Findings {
			sev[i] = f.Severity
		}
		if want := []ReviewSeverity{ReviewSeverityHigh, ReviewSeverityMedium, ReviewSeverityLow}; !slices.Equal(sev, want) {
			t.Errorf("severities = %v, want %v", sev, want)
		}
		if got.Findings[0].Claim != "nil map write in Add" || got.Findings[0].Location != "store.go:10" {
			t.Errorf("first finding = %+v", got.Findings[0])
		}
	})

	malformed := map[string]string{
		"no JSON":                       "I think the change is fine.",
		"invalid JSON":                  `{"verdict": "approve", "findings": [}`,
		"unknown verdict":               `{"verdict":"maybe","findings":[]}`,
		"changes requested, no finding": `{"verdict":"changes_requested","findings":[]}`,
		"unknown severity":              `{"verdict":"changes_requested","findings":[{"severity":"urgent","claim":"x"}]}`,
		"finding without claim":         `{"verdict":"changes_requested","findings":[{"severity":"high","claim":"  "}]}`,
	}
	for name, raw := range malformed {
		t.Run(name, func(t *testing.T) {
			_, err := parseReviewAnswer(raw)
			var oe *ReviewOutputError
			if !errors.As(err, &oe) {
				t.Fatalf("err = %v, want a *ReviewOutputError", err)
			}
			if oe.Raw != raw {
				t.Errorf("Raw = %q, want the reviewer output %q", oe.Raw, raw)
			}
		})
	}
}

// reviewResultLine is one NDJSON result line carrying a reviewer answer and
// its usage, as a subprocess harness prints it.
func reviewResultLine(t *testing.T, answer string) []byte {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"result":         answer,
		"session_id":     "rev-1",
		"stop_reason":    "end_turn",
		"is_error":       false,
		"total_cost_usd": 0.02,
		"usage":          map[string]int{"input_tokens": 1200, "output_tokens": 80},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return line
}

// newReviewTask creates a task with a prompt and acceptance criteria.
func newReviewTask(t *testing.T, s *store.Store) *store.Task {
	t.Helper()
	ctx := context.Background()
	task, err := s.CreateTaskWithOptions(ctx, store.TaskCreateOptions{Prompt: "add a health endpoint", Timeout: 5})
	if err != nil {
		t.Fatalf("CreateTask: %v", err)
	}
	if err := s.UpdateTaskCriteria(ctx, task.ID, "GET /health returns 200"); err != nil {
		t.Fatalf("UpdateTaskCriteria: %v", err)
	}
	fresh, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	return fresh
}

// TestRunReviewer_RunsOnReviewerModel runs the reviewer through the subprocess
// path: the launch names the reviewer model, the prompt carries the task, the
// criteria, the diff and the previous round's findings, the answer is parsed,
// and the usage lands in the task's review breakdown.
func TestRunReviewer_RunsOnReviewerModel(t *testing.T) {
	mock := &MockSandboxBackend{}
	s, r := setupRunnerWithMockBackend(t, nil, mock)
	r.envFile = writeEnvFile(t, "WALLFACER_DEFAULT_SANDBOX=claude\nCLAUDE_DEFAULT_MODEL=task-model\nWALLFACER_REVIEW_MODEL=reviewer-model\n")
	mock.responses = []ContainerResponse{{Stdout: reviewResultLine(t,
		`{"verdict":"changes_requested","findings":[{"severity":"high","claim":"the handler returns 500 on HEAD"}]}`)}}
	task := newReviewTask(t, s)

	started := 0
	res, err := r.RunReviewer(context.Background(), task, ReviewerInput{
		Round: 2, MaxRounds: 3, Diff: "+func health() {}",
		PriorFindings:    []ReviewFinding{{Severity: ReviewSeverityMedium, Claim: "no test for /health"}},
		ImplementerReply: "added the test",
		OnStart:          func() { started++ },
	})
	if err != nil {
		t.Fatalf("RunReviewer: %v", err)
	}
	if started != 1 {
		t.Errorf("OnStart called %d times, want 1", started)
	}
	if res.Model != "reviewer-model" {
		t.Errorf("Model = %q, want reviewer-model", res.Model)
	}
	if res.Answer.Verdict != ReviewChangesRequested || len(res.Answer.Findings) != 1 {
		t.Errorf("answer = %+v", res.Answer)
	}
	if res.Usage.InputTokens != 1200 || res.Usage.OutputTokens != 80 {
		t.Errorf("usage = %+v, want 1200 in / 80 out", res.Usage)
	}

	calls := mock.RunArgsCalls()
	if len(calls) != 1 {
		t.Fatalf("launches = %d, want 1", len(calls))
	}
	args := strings.Join(calls[0].Args, "\n")
	if !strings.Contains(args, "--model\nreviewer-model") {
		t.Errorf("launch does not name the reviewer model: %q", calls[0].Args)
	}
	for _, want := range []string{"add a health endpoint", "GET /health returns 200", "+func health() {}", "- [medium] no test for /health", "added the test", "round 2 of at most 3"} {
		if !strings.Contains(args, want) {
			t.Errorf("reviewer prompt lacks %q", want)
		}
	}

	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if bd := got.UsageBreakdown[store.SandboxActivityReview]; bd.InputTokens != 1200 || bd.CostUSD != 0.02 {
		t.Errorf("review usage breakdown = %+v, want the reviewer's usage", bd)
	}
}

// TestRunReviewer_RefusesWithoutASecondModel covers the two refusals: no
// reviewer model, and a reviewer model equal to a model the task runs on
// (its configured default, its pin, or the model the harness reported). A
// refusal launches nothing.
func TestRunReviewer_RefusesWithoutASecondModel(t *testing.T) {
	cases := []struct {
		name    string
		env     string
		prepare func(*store.Task)
		code    string
	}{
		{
			name: "unset",
			env:  "CLAUDE_DEFAULT_MODEL=task-model\n",
			code: ReviewRefusedModelUnset,
		},
		{
			name: "same as the default model",
			env:  "CLAUDE_DEFAULT_MODEL=task-model\nWALLFACER_REVIEW_MODEL=Task-Model\n",
			code: ReviewRefusedSameModel,
		},
		{
			name: "same as the task's pin",
			env:  "CLAUDE_DEFAULT_MODEL=other\nWALLFACER_REVIEW_MODEL=pinned-model\n",
			prepare: func(task *store.Task) {
				m := "pinned-model"
				task.ModelOverride = &m
			},
			code: ReviewRefusedSameModel,
		},
		{
			name: "same as the reported model",
			env:  "CLAUDE_DEFAULT_MODEL=task-model\nWALLFACER_REVIEW_MODEL=task-model-20260101\n",
			prepare: func(task *store.Task) {
				task.Environment = &store.ExecutionEnvironment{ModelName: "task-model-20260101"}
			},
			code: ReviewRefusedSameModel,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mock := &MockSandboxBackend{}
			s, r := setupRunnerWithMockBackend(t, nil, mock)
			r.envFile = writeEnvFile(t, "WALLFACER_DEFAULT_SANDBOX=claude\n"+tc.env)
			task := newReviewTask(t, s)
			if tc.prepare != nil {
				tc.prepare(task)
			}

			started := 0
			_, err := r.RunReviewer(context.Background(), task, ReviewerInput{Round: 1, MaxRounds: 3, OnStart: func() { started++ }})
			if started != 0 {
				t.Errorf("OnStart called %d times for a refused review, want 0", started)
			}
			var refusal *ReviewRefusal
			if !errors.As(err, &refusal) {
				t.Fatalf("err = %v, want a *ReviewRefusal", err)
			}
			if refusal.Code != tc.code {
				t.Errorf("code = %q, want %q", refusal.Code, tc.code)
			}
			if n := len(mock.RunArgsCalls()); n != 0 {
				t.Errorf("launches = %d, want none for a refused review", n)
			}
		})
	}
}

// TestRunReviewer_MalformedAnswer proves a reviewer answer that is not review
// JSON fails the run with a *ReviewOutputError holding the raw answer, and the
// usage of the failed run is still attributed to the task.
func TestRunReviewer_MalformedAnswer(t *testing.T) {
	mock := &MockSandboxBackend{}
	s, r := setupRunnerWithMockBackend(t, nil, mock)
	r.envFile = writeEnvFile(t, "WALLFACER_DEFAULT_SANDBOX=claude\nCLAUDE_DEFAULT_MODEL=task-model\nWALLFACER_REVIEW_MODEL=reviewer-model\n")
	mock.responses = []ContainerResponse{{Stdout: reviewResultLine(t, "The change looks fine to me.")}}
	task := newReviewTask(t, s)

	_, err := r.RunReviewer(context.Background(), task, ReviewerInput{Round: 1, MaxRounds: 3})
	var oe *ReviewOutputError
	if !errors.As(err, &oe) {
		t.Fatalf("err = %v, want a wrapped *ReviewOutputError", err)
	}
	if oe.Raw != "The change looks fine to me." {
		t.Errorf("Raw = %q", oe.Raw)
	}
	got, err := s.GetTask(context.Background(), task.ID)
	if err != nil {
		t.Fatalf("GetTask: %v", err)
	}
	if bd := got.UsageBreakdown[store.SandboxActivityReview]; bd.InputTokens != 1200 {
		t.Errorf("review usage breakdown = %+v, want the failed run's usage", bd)
	}
}

// TestRunReviewer_InProcessHarness proves a task on the in-process harness is
// reviewed in-process: the request goes to the scripted model and names the
// reviewer model, and no subprocess is launched.
func TestRunReviewer_InProcessHarness(t *testing.T) {
	mock := &MockSandboxBackend{}
	s, r := setupRunnerWithMockBackend(t, nil, mock)
	envFile, requests := scriptedModel(t, `{"verdict":"approve","findings":[]}`)
	appendEnv(t, envFile, "WALLFACER_REVIEW_MODEL=reviewer-model\n")
	r.envFile = envFile
	task := newToposPinnedTask(t, s, "add a health endpoint")

	res, err := r.RunReviewer(context.Background(), task, ReviewerInput{Round: 1, MaxRounds: 3})
	if err != nil {
		t.Fatalf("RunReviewer: %v", err)
	}
	if res.Answer.Verdict != ReviewApprove {
		t.Errorf("verdict = %q, want approve", res.Answer.Verdict)
	}
	got := requests()
	if len(got) != 1 || !strings.Contains(got[0], "reviewer-model") {
		t.Errorf("model requests = %v, want one naming reviewer-model", got)
	}
	if n := len(mock.RunArgsCalls()); n != 0 {
		t.Errorf("subprocess launches = %d, want none", n)
	}
}

// TestFormatReviewFindings pins the one-line form findings are fed back in.
func TestFormatReviewFindings(t *testing.T) {
	got := FormatReviewFindings([]ReviewFinding{
		{Severity: ReviewSeverityHigh, Claim: "nil map write", Location: "a.go:3"},
		{Severity: ReviewSeverityLow, Claim: "typo"},
	})
	want := "- [high] nil map write (a.go:3)\n- [low] typo"
	if got != want {
		t.Errorf("FormatReviewFindings = %q, want %q", got, want)
	}
	if FormatReviewFindings(nil) != "" {
		t.Error("no findings should format as empty")
	}
}
