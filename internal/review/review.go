// Package review runs the review of a waiting task's change: a reviewer on a
// model other than the task's reads the task prompt, the acceptance criteria
// and the diff, and answers with findings and a verdict. Requested changes go
// back to the task as feedback, the task takes its next turn, and the new
// diff is reviewed again, until the reviewer approves or a bound is reached:
// the round limit or the reviewer token budget.
//
// The reviewer runs through the runner's review role, so it runs on whatever
// harness the role resolves to, subprocess or in-process. The diversity a
// review needs comes from the model, not from the harness.
//
// One Verify call is one round. The round state lives in the session record
// on disk (record.go), so the caller holds a task's in-flight slot only while
// a reviewer runs, never across the task's own turn, and a restart continues
// the session where it stopped.
package review

import (
	"context"
	"errors"
	"fmt"
	"time"

	"latere.ai/x/wallfacer/internal/runner"
	"latere.ai/x/wallfacer/internal/store"
)

// Verifier is the handler's post-run review interface; *Reviewer is the
// implementation.
type Verifier interface {
	// Verify runs one review round for a waiting task and reports what the
	// caller must do next. A failed round returns a *Failure and leaves the
	// session open, so a retry runs the same round again.
	Verify(ctx context.Context, in Input) (*Result, error)
}

// Input parameterizes one round.
type Input struct {
	Task          *store.Task
	Diff          string // the task's diff against the default branch, already capped
	StateDir      string // the task's review state directory; sessions are written under it
	MaxRounds     int    // reviewer runs per session; values below 1 mean 1
	CostCapTokens int    // reviewer token budget per session; 0 means unbounded
	// OnStart, when set, is called right before the reviewer launches, with
	// the round number and the round limit. A skipped or bounded round never
	// calls it.
	OnStart func(round, maxRounds int)
}

// Outcome is what the caller must do after a round.
type Outcome string

// The outcomes of a round.
const (
	// OutcomeSkipped: the review did not run. Result.Skip says why. The task's
	// verdict stays unset, so a task whose verdict gates auto-submit waits.
	OutcomeSkipped Outcome = "skipped"
	// OutcomeFeedback: send Result.Feedback to the task through the feedback
	// path. The next round runs after the task's turn, when it waits again.
	OutcomeFeedback Outcome = "feedback"
	// OutcomeFinished: the session is over. Persist Result.Unresolved and
	// Result.Headline on the task.
	OutcomeFinished Outcome = "finished"
)

// Result describes one completed round.
type Result struct {
	Outcome    Outcome
	SessionDir string
	Round      int
	Verdict    runner.ReviewVerdict
	Findings   []runner.ReviewFinding // the round's findings, most severe first

	// OutcomeFeedback.
	Feedback string

	// OutcomeFinished.
	Termination string // one of the Termination* constants
	Unresolved  int    // open findings at the end
	Headline    string // the most severe open finding's claim

	// OutcomeSkipped. SkipRecorded is false when the newest session already
	// records the same skip, so the caller reports it once, not every tick.
	Skip         *Skip
	SkipRecorded bool
}

// Skip is why a review did not run: a stable code, the fixed sentence a user
// sees, and the developer detail (the model names).
type Skip struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

// skipMessages holds the fixed user sentence of each refusal code.
var skipMessages = map[string]string{
	runner.ReviewRefusedModelUnset: "Review did not run: no reviewer model is set. Set WALLFACER_REVIEW_MODEL to a model other than the task's.",
	runner.ReviewRefusedSameModel:  "Review did not run: the reviewer model is the model the task runs on. Set WALLFACER_REVIEW_MODEL to a different model.",
}

// Stable codes of a failed round.
const (
	// CodeReviewerFailed: the reviewer run failed (launch, harness, timeout).
	CodeReviewerFailed = "review_reviewer_failed"
	// CodeOutputInvalid: the reviewer answered, but not with review JSON.
	CodeOutputInvalid = "review_output_invalid"
	// CodeRecordFailed: the session record could not be read or written.
	CodeRecordFailed = "review_record_failed"
)

// failureMessages holds the fixed user sentence of each failure code.
var failureMessages = map[string]string{
	CodeReviewerFailed: "Review: the reviewer run failed; the round will be retried.",
	CodeOutputInvalid:  "Review: the reviewer's answer could not be read; the round will be retried.",
	CodeRecordFailed:   "Review: the review record could not be written; the round will be retried.",
}

// Failure is a round that did not complete. Code is stable, Message is the
// fixed sentence for the task timeline, Err is the developer detail.
type Failure struct {
	Code       string
	Err        error
	SessionDir string // the session the failed attempt is recorded in; empty when none
}

// Message returns the fixed user sentence for f's code.
func (f *Failure) Message() string { return failureMessages[f.Code] }

func (f *Failure) Error() string { return f.Code + ": " + f.Err.Error() }

func (f *Failure) Unwrap() error { return f.Err }

// FailureMessage returns the fixed user sentence for an error Verify returned:
// the failure's own sentence, or the reviewer-failed sentence for any other
// error.
func FailureMessage(err error) string {
	var f *Failure
	if errors.As(err, &f) && f.Message() != "" {
		return f.Message()
	}
	return failureMessages[CodeReviewerFailed]
}

// reviewerRunner is the part of the runner a review needs.
type reviewerRunner interface {
	RunReviewer(ctx context.Context, task *store.Task, in runner.ReviewerInput) (*runner.ReviewerResult, error)
}

// Reviewer implements Verifier on the runner's review role.
type Reviewer struct {
	runner reviewerRunner
	now    func() time.Time
}

// New returns a Reviewer that runs reviewers through r.
func New(r reviewerRunner) *Reviewer {
	return &Reviewer{runner: r, now: time.Now}
}

// Verify runs one round. It continues the open session of the task's state
// directory, or starts a new one when the newest session has ended. On a
// continued session it first records the task's reply to the findings it was
// sent, then holds the session to its bounds, and only then runs the
// reviewer, so a round that would exceed the limit or the budget is never
// started.
func (v *Reviewer) Verify(ctx context.Context, in Input) (*Result, error) {
	if in.Task == nil {
		return nil, errors.New("review: task is required")
	}
	maxRounds := max(in.MaxRounds, 1)
	now := v.now()

	newest, found, err := Newest(in.StateDir)
	if err != nil && (newest == nil || !newest.Truncated) {
		return nil, &Failure{Code: CodeRecordFailed, Err: err}
	}
	if err != nil {
		// The newest session's transcript is damaged part way. Its state
		// cannot be replayed, so it is closed and this round starts a new
		// session; failing on it would stall the task's review for good.
		if newest.End == nil {
			if ferr := newest.finish(End{TS: now.UTC(), Termination: TerminationUnreadable, Rounds: newest.CompletedRounds(), Tokens: newest.Tokens(), USD: newest.USD()}); ferr != nil {
				return nil, &Failure{Code: CodeRecordFailed, Err: errors.Join(err, ferr), SessionDir: newest.Dir}
			}
		}
		found = false
	}
	var open *Session
	if found && !newest.Legacy && newest.End == nil {
		open = newest
	}

	round := 1
	var prior []runner.ReviewFinding
	var reply string
	if open != nil {
		if open.awaitingTurn() {
			body := ""
			if in.Task.Result != nil {
				body = capBody(*in.Task.Result)
			}
			if err := open.append(Record{TS: now, Round: open.CompletedRounds(), Role: RoleImplementer, Body: body}); err != nil {
				return nil, &Failure{Code: CodeRecordFailed, Err: err, SessionDir: open.Dir}
			}
		}
		if last := open.LastReview(); last != nil {
			if open.CompletedRounds() >= maxRounds {
				return v.finish(open, TerminationMaxRounds, *last, now)
			}
			if in.CostCapTokens > 0 && open.Tokens() >= in.CostCapTokens {
				return v.finish(open, TerminationCostCap, *last, now)
			}
			prior = last.Findings
			reply = open.lastReply()
		}
		round = open.CompletedRounds() + 1
	}

	res, err := v.runner.RunReviewer(ctx, in.Task, runner.ReviewerInput{
		Round:            round,
		MaxRounds:        maxRounds,
		Diff:             in.Diff,
		PriorFindings:    prior,
		ImplementerReply: reply,
		OnStart: func() {
			if in.OnStart != nil {
				in.OnStart(round, maxRounds)
			}
		},
	})
	if refusal := (*runner.ReviewRefusal)(nil); errors.As(err, &refusal) {
		return v.skip(in.StateDir, newest, open, refusal, now)
	}
	if err != nil {
		return nil, v.recordFailedAttempt(in.StateDir, open, round, err, now)
	}

	s := open
	if s == nil {
		if s, err = newSession(in.StateDir, now); err != nil {
			return nil, &Failure{Code: CodeRecordFailed, Err: err}
		}
	}
	rec := Record{
		TS:       now,
		Round:    round,
		Role:     RoleReviewer,
		Verdict:  res.Answer.Verdict,
		Findings: res.Answer.Findings,
		Summary:  res.Answer.Summary,
		Model:    res.Model,
		Harness:  string(res.Harness),
		Tokens:   res.Usage.InputTokens + res.Usage.OutputTokens,
		USD:      res.Usage.CostUSD,
	}
	if err := s.append(rec); err != nil {
		return nil, &Failure{Code: CodeRecordFailed, Err: err, SessionDir: s.Dir}
	}
	switch {
	case rec.Verdict == runner.ReviewApprove:
		return v.finish(s, TerminationApproved, rec, now)
	case round >= maxRounds:
		return v.finish(s, TerminationMaxRounds, rec, now)
	case in.CostCapTokens > 0 && s.Tokens() >= in.CostCapTokens:
		return v.finish(s, TerminationCostCap, rec, now)
	}

	feedback := feedbackMessage(round, maxRounds, rec.Findings)
	if err := s.append(Record{TS: now, Round: round, Role: RoleFeedback, Body: capBody(feedback)}); err != nil {
		return nil, &Failure{Code: CodeRecordFailed, Err: err, SessionDir: s.Dir}
	}
	return &Result{
		Outcome:    OutcomeFeedback,
		SessionDir: s.Dir,
		Round:      round,
		Verdict:    rec.Verdict,
		Findings:   rec.Findings,
		Feedback:   feedback,
	}, nil
}

// finish ends s with termination after last, the session's last answered
// round. The open findings are last's findings unless the reviewer approved;
// when the session ends on a bound before the next review, they are the
// findings the task was last sent, not re-checked after its turn.
func (v *Reviewer) finish(s *Session, termination string, last Record, now time.Time) (*Result, error) {
	unresolved, headline := 0, ""
	if termination != TerminationApproved && last.Verdict == runner.ReviewChangesRequested && len(last.Findings) > 0 {
		unresolved, headline = len(last.Findings), last.Findings[0].Claim
	}
	end := End{
		TS:          now.UTC(),
		Termination: termination,
		Rounds:      s.CompletedRounds(),
		Unresolved:  unresolved,
		Headline:    headline,
		Tokens:      s.Tokens(),
		USD:         s.USD(),
	}
	if err := s.finish(end); err != nil {
		return nil, &Failure{Code: CodeRecordFailed, Err: err, SessionDir: s.Dir}
	}
	return &Result{
		Outcome:     OutcomeFinished,
		SessionDir:  s.Dir,
		Round:       last.Round,
		Verdict:     last.Verdict,
		Findings:    last.Findings,
		Termination: termination,
		Unresolved:  unresolved,
		Headline:    headline,
	}, nil
}

// skip records a refused review. A refusal in the middle of a session (the
// reviewer model changed under it) ends that session. Otherwise a new session
// holding only the skip is written, unless the newest session already records
// a skip with the same code, in which case nothing is written and the result
// says so.
func (v *Reviewer) skip(stateDir string, newest, open *Session, refusal *runner.ReviewRefusal, now time.Time) (*Result, error) {
	sk := &Skip{Code: refusal.Code, Message: skipMessages[refusal.Code], Detail: refusal.Detail}
	if sk.Message == "" {
		sk.Message = "Review did not run."
	}
	s := open
	if s == nil {
		if newest != nil && !newest.Legacy && newest.End != nil && newest.End.Termination == TerminationSkipped &&
			newest.End.Skip != nil && newest.End.Skip.Code == sk.Code {
			return &Result{Outcome: OutcomeSkipped, SessionDir: newest.Dir, Skip: sk}, nil
		}
		var err error
		if s, err = newSession(stateDir, now); err != nil {
			return nil, &Failure{Code: CodeRecordFailed, Err: err}
		}
	}
	end := End{
		TS:          now.UTC(),
		Termination: TerminationSkipped,
		Rounds:      s.CompletedRounds(),
		Tokens:      s.Tokens(),
		USD:         s.USD(),
		Skip:        sk,
	}
	if err := s.finish(end); err != nil {
		return nil, &Failure{Code: CodeRecordFailed, Err: err, SessionDir: s.Dir}
	}
	return &Result{Outcome: OutcomeSkipped, SessionDir: s.Dir, Skip: sk, SkipRecorded: true}, nil
}

// recordFailedAttempt records a reviewer run that did not produce an answer
// and returns the *Failure for it. The attempt does not consume the round and
// does not end the session: the retry runs the same round, so failures never
// widen the round limit.
func (v *Reviewer) recordFailedAttempt(stateDir string, open *Session, round int, runErr error, now time.Time) error {
	code, raw := CodeReviewerFailed, ""
	var oe *runner.ReviewOutputError
	if errors.As(runErr, &oe) {
		code, raw = CodeOutputInvalid, oe.Raw
	}
	s := open
	if s == nil {
		var err error
		if s, err = newSession(stateDir, now); err != nil {
			return &Failure{Code: code, Err: errors.Join(runErr, err)}
		}
	}
	rec := Record{TS: now, Round: round, Role: RoleReviewer, ErrorCode: code, Error: failureMessages[code], Raw: capBody(raw)}
	if err := s.append(rec); err != nil {
		return &Failure{Code: code, Err: errors.Join(runErr, err), SessionDir: s.Dir}
	}
	return &Failure{Code: code, Err: runErr, SessionDir: s.Dir}
}

// feedbackMessage is the message the findings are sent to the task in.
func feedbackMessage(round, maxRounds int, findings []runner.ReviewFinding) string {
	return fmt.Sprintf("A review of this change on a second model requested changes (round %d of at most %d). "+
		"Address each finding below, or explain in your reply why it does not apply.\n\n%s",
		round, maxRounds, runner.FormatReviewFindings(findings))
}
