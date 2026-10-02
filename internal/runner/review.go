package runner

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"latere.ai/x/wallfacer/internal/agents"
	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/prompts"
	"latere.ai/x/wallfacer/internal/store"
)

// ReviewVerdict is the reviewer's decision on a task's change.
type ReviewVerdict string

// The two verdicts a reviewer may return. Any other value is malformed output.
const (
	// ReviewApprove means nothing must change before the task is merged.
	ReviewApprove ReviewVerdict = "approve"
	// ReviewChangesRequested means at least one finding must be fixed first.
	ReviewChangesRequested ReviewVerdict = "changes_requested"
)

// ReviewSeverity ranks a finding; the most severe open finding becomes the
// task's review headline.
type ReviewSeverity string

// The three severities a reviewer may assign.
const (
	ReviewSeverityHigh   ReviewSeverity = "high"
	ReviewSeverityMedium ReviewSeverity = "medium"
	ReviewSeverityLow    ReviewSeverity = "low"
)

// reviewSeverityAliases maps the severity words models use unprompted onto the
// three the prompt asks for, so a reviewer that says "critical" or "nit" is not
// recorded as a failed review over a vocabulary difference.
var reviewSeverityAliases = map[string]ReviewSeverity{
	"high":     ReviewSeverityHigh,
	"critical": ReviewSeverityHigh,
	"blocker":  ReviewSeverityHigh,
	"major":    ReviewSeverityHigh,
	"medium":   ReviewSeverityMedium,
	"moderate": ReviewSeverityMedium,
	"low":      ReviewSeverityLow,
	"minor":    ReviewSeverityLow,
	"nit":      ReviewSeverityLow,
}

// Rank orders severities: high is 3, medium 2, low 1, anything else 0.
func (s ReviewSeverity) Rank() int {
	switch s {
	case ReviewSeverityHigh:
		return 3
	case ReviewSeverityMedium:
		return 2
	case ReviewSeverityLow:
		return 1
	}
	return 0
}

// ReviewFinding is one defect the reviewer reports.
type ReviewFinding struct {
	Severity ReviewSeverity `json:"severity"`
	Claim    string         `json:"claim"`              // one sentence: the defect and where it is
	Location string         `json:"location,omitempty"` // optional "path[:line]"
}

// ReviewAnswer is a reviewer's parsed answer. Findings are sorted most severe
// first, so Findings[0] is the headline of a changes_requested answer.
type ReviewAnswer struct {
	Verdict  ReviewVerdict   `json:"verdict"`
	Findings []ReviewFinding `json:"findings"`
	Summary  string          `json:"summary,omitempty"`
}

// maxReviewRawBytes caps the reviewer output a ReviewOutputError carries, so a
// runaway answer does not bloat the review record it is written into.
const maxReviewRawBytes = 16 * 1024

// ReviewOutputError reports reviewer output that is not a valid review answer.
// Raw is the output, truncated to maxReviewRawBytes, so the failed attempt can
// be recorded with what the reviewer actually said.
type ReviewOutputError struct {
	Raw    string
	Reason string
}

func (e *ReviewOutputError) Error() string {
	return "reviewer output is not a valid review answer: " + e.Reason
}

// parseReviewResult is the review binding's result parser. Malformed output is
// a *ReviewOutputError, which runAgent wraps with %w, so callers recover the
// raw output with errors.As.
func parseReviewResult(o *agentOutput) (any, error) {
	return parseReviewAnswer(o.Result)
}

// parseReviewAnswer extracts the review JSON object from reviewer output,
// tolerating a code fence or prose around it, and validates it: the verdict is
// one of the two values, each finding has a known severity and a claim, and a
// changes_requested verdict carries at least one finding.
func parseReviewAnswer(raw string) (ReviewAnswer, error) {
	fail := func(format string, args ...any) (ReviewAnswer, error) {
		return ReviewAnswer{}, &ReviewOutputError{Raw: truncate(raw, maxReviewRawBytes), Reason: fmt.Sprintf(format, args...)}
	}
	s := strings.TrimSpace(raw)
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < start {
		return fail("no JSON object")
	}
	var wire struct {
		Verdict  string `json:"verdict"`
		Findings []struct {
			Severity string `json:"severity"`
			Claim    string `json:"claim"`
			Location string `json:"location"`
		} `json:"findings"`
		Summary string `json:"summary"`
	}
	if err := json.Unmarshal([]byte(s[start:end+1]), &wire); err != nil {
		return fail("invalid JSON: %v", err)
	}
	ans := ReviewAnswer{
		Verdict:  ReviewVerdict(strings.ToLower(strings.TrimSpace(wire.Verdict))),
		Findings: make([]ReviewFinding, 0, len(wire.Findings)),
		Summary:  strings.TrimSpace(wire.Summary),
	}
	if ans.Verdict != ReviewApprove && ans.Verdict != ReviewChangesRequested {
		return fail("verdict %q is neither %q nor %q", wire.Verdict, ReviewApprove, ReviewChangesRequested)
	}
	for i, f := range wire.Findings {
		sev, ok := reviewSeverityAliases[strings.ToLower(strings.TrimSpace(f.Severity))]
		if !ok {
			return fail("finding %d has unknown severity %q", i+1, f.Severity)
		}
		claim := strings.TrimSpace(f.Claim)
		if claim == "" {
			return fail("finding %d has no claim", i+1)
		}
		ans.Findings = append(ans.Findings, ReviewFinding{Severity: sev, Claim: claim, Location: strings.TrimSpace(f.Location)})
	}
	if ans.Verdict == ReviewChangesRequested && len(ans.Findings) == 0 {
		return fail("verdict %q lists no findings", ReviewChangesRequested)
	}
	slices.SortStableFunc(ans.Findings, func(a, b ReviewFinding) int {
		return cmp.Compare(b.Severity.Rank(), a.Severity.Rank())
	})
	return ans, nil
}

// Stable codes of a review the runner refuses to start. The review loop maps
// each to the fixed sentence a user sees.
const (
	// ReviewRefusedModelUnset: WALLFACER_REVIEW_MODEL is empty.
	ReviewRefusedModelUnset = "review_model_unset"
	// ReviewRefusedSameModel: the reviewer model is a model the task runs on.
	ReviewRefusedSameModel = "review_model_same"
)

// ReviewRefusal is returned by RunReviewer when the review must not run. The
// point of the review is a second model, so a missing reviewer model or one
// equal to the task's is refused before anything launches. Code is one of the
// ReviewRefused* constants; Detail names the models for logs and the record.
type ReviewRefusal struct {
	Code   string
	Detail string
}

func (e *ReviewRefusal) Error() string { return "review refused: " + e.Code + ": " + e.Detail }

// ReviewerInput is what one reviewer run reads besides the task itself.
type ReviewerInput struct {
	Round            int // 1-based
	MaxRounds        int
	Diff             string
	PriorFindings    []ReviewFinding // the previous round's findings; empty on round 1
	ImplementerReply string          // the task's result after its last turn; empty on round 1
}

// ReviewerResult is one completed reviewer run: the answer, the model and
// harness it ran on, and the usage it reported. The usage is already added to
// the task's review breakdown by runAgent; it is returned so the caller can
// hold the review to its token budget.
type ReviewerResult struct {
	Answer  ReviewAnswer
	Model   string
	Harness harness.ID
	Usage   store.TaskUsage
}

// RunReviewer runs the review role for task on the reviewer model. The model
// is WALLFACER_REVIEW_MODEL; the harness resolves like any other role's
// (per-task review override, the task's harness, the env default), so by
// default the reviewer runs on the task's harness with a different model. It
// returns a *ReviewRefusal without launching when the reviewer model is unset
// or equals a model the task runs on, and an error wrapping a
// *ReviewOutputError when the reviewer's answer cannot be parsed.
func (r *Runner) RunReviewer(ctx context.Context, task *store.Task, in ReviewerInput) (*ReviewerResult, error) {
	if task == nil {
		return nil, errors.New("review: task is required")
	}
	model, err := r.reviewModel()
	if err != nil {
		return nil, err
	}
	if model == "" {
		return nil, &ReviewRefusal{Code: ReviewRefusedModelUnset, Detail: "WALLFACER_REVIEW_MODEL is empty"}
	}
	for _, tm := range r.taskModels(task) {
		if strings.EqualFold(tm, model) {
			return nil, &ReviewRefusal{Code: ReviewRefusedSameModel, Detail: fmt.Sprintf("reviewer model %q is the task's model", model)}
		}
	}

	prompt := r.promptsMgr.Review(prompts.ReviewData{
		Prompt:           task.Prompt,
		Criteria:         strings.TrimSpace(task.Criteria),
		Diff:             strings.TrimSpace(in.Diff),
		Round:            in.Round,
		MaxRounds:        in.MaxRounds,
		PriorFindings:    FormatReviewFindings(in.PriorFindings),
		ImplementerReply: strings.TrimSpace(in.ImplementerReply),
	})
	res, err := r.runAgent(ctx, agents.Review, task, prompt, runAgentOpts{
		EmitSpanEvents: true,
		TrackUsage:     true,
		Turn:           1,
		ModelOverride:  model,
	})
	if err != nil {
		return nil, err
	}
	ans, ok := res.Parsed.(ReviewAnswer)
	if !ok {
		return nil, fmt.Errorf("review: reviewer returned no answer")
	}
	out := &ReviewerResult{Answer: ans, Model: model, Harness: res.SandboxUsed}
	if o := res.Output; o != nil {
		out.Usage = store.TaskUsage{
			InputTokens:          o.Usage.InputTokens,
			OutputTokens:         o.Usage.OutputTokens,
			CacheReadInputTokens: o.Usage.CacheReadInputTokens,
			CacheCreationTokens:  o.Usage.CacheCreationInputTokens,
			CostUSD:              o.TotalCostUSD,
		}
	}
	return out, nil
}

// reviewModel reads WALLFACER_REVIEW_MODEL from the env file. An unconfigured
// env file means no reviewer model; an unreadable one is an error, not a
// refusal, so the attempt is retried rather than recorded as a skip.
func (r *Runner) reviewModel() (string, error) {
	if r.envFile == "" {
		return "", nil
	}
	cfg, err := envconfig.Parse(r.envFile)
	if err != nil {
		return "", fmt.Errorf("review: read env file: %w", err)
	}
	return strings.TrimSpace(cfg.ReviewModel), nil
}

// taskModels lists every model the task is known to run on: its per-task pin
// or, without one, the env default for its implementation harness, plus the
// model the harness reported during the run. The reported name can differ in
// spelling from the configured one (a dated suffix, for example), so the
// reviewer model is refused if it equals any of them.
func (r *Runner) taskModels(task *store.Task) []string {
	var out []string
	add := func(m string) {
		m = strings.TrimSpace(m)
		if m != "" && !slices.Contains(out, m) {
			out = append(out, m)
		}
	}
	if pin := task.EffectiveModel(); pin != "" {
		add(pin)
	} else {
		add(r.modelFromEnvForSandbox(r.sandboxForTask(task)))
	}
	if task.Environment != nil {
		add(task.Environment.ModelName)
	}
	return out
}

// FormatReviewFindings renders findings one per line as
// "- [severity] claim (location)", the form the reviewer reads its earlier
// findings in and the implementer reads them as feedback.
func FormatReviewFindings(findings []ReviewFinding) string {
	var b strings.Builder
	for _, f := range findings {
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- [")
		b.WriteString(string(f.Severity))
		b.WriteString("] ")
		b.WriteString(f.Claim)
		if f.Location != "" {
			b.WriteString(" (")
			b.WriteString(f.Location)
			b.WriteString(")")
		}
	}
	return b.String()
}
