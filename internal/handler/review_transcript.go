package handler

import (
	"net/http"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/logger"
	"latere.ai/x/wallfacer/internal/review"
	"latere.ai/x/wallfacer/internal/runner"
)

// reviewTranscriptResp is the GET /api/tasks/{id}/review/transcript body: the
// task's newest review session, round by round.
//
// Legacy is set for a session recorded by the debate engine the review
// replaced: its rounds are not in a form this route reads, so Rounds is empty
// and the panel says so. Truncated is set when the transcript could not be
// read to its end: Rounds then holds the rounds read before the failure.
type reviewTranscriptResp struct {
	SessionID string           `json:"session_id"`
	Running   bool             `json:"running"`
	Legacy    bool             `json:"legacy,omitempty"`
	Config    *reviewRunConfig `json:"config,omitempty"`
	Outcome   *reviewOutcome   `json:"outcome,omitempty"`
	Rounds    []reviewRound    `json:"rounds"`
	Truncated bool             `json:"truncated,omitempty"`
}

// reviewRunConfig is how a review is configured now: the round limit, the
// reviewer token budget, and the reviewer model ("" when unset).
type reviewRunConfig struct {
	MaxRounds     int    `json:"max_rounds"`
	CostCap       int    `json:"cost_cap"`
	ReviewerModel string `json:"reviewer_model"`
}

// reviewOutcome is the end of a finished session.
type reviewOutcome struct {
	Termination string       `json:"termination"` // approved | max_rounds | cost_cap | skipped | superseded
	Rounds      int          `json:"rounds"`
	Unresolved  int          `json:"unresolved"`
	Headline    string       `json:"headline,omitempty"`
	Tokens      int          `json:"tokens"`
	USD         float64      `json:"usd"`
	Skip        *review.Skip `json:"skip,omitempty"`
}

// reviewRound is one round: the reviewer's answer, any failed attempts before
// it, the feedback the findings were sent to the task in, and the task's
// reply after its turn.
type reviewRound struct {
	Round          int                   `json:"round"`
	Reviewer       *reviewerAnswer       `json:"reviewer,omitempty"`
	FailedAttempts []reviewFailedAttempt `json:"failed_attempts,omitempty"`
	Feedback       string                `json:"feedback,omitempty"`
	Reply          string                `json:"reply,omitempty"`
}

// reviewerAnswer is a reviewer's answer in one round.
type reviewerAnswer struct {
	Verdict  runner.ReviewVerdict   `json:"verdict"`
	Findings []runner.ReviewFinding `json:"findings"`
	Summary  string                 `json:"summary,omitempty"`
	Model    string                 `json:"model,omitempty"`
	Harness  string                 `json:"harness,omitempty"`
	Tokens   int                    `json:"tokens,omitempty"`
	TS       time.Time              `json:"ts"`
}

// reviewFailedAttempt is a reviewer run that produced no answer: its stable
// code, the fixed sentence, and the reviewer's raw output when it answered in
// a form that could not be read.
type reviewFailedAttempt struct {
	Code    string    `json:"code"`
	Message string    `json:"message"`
	Raw     string    `json:"raw,omitempty"`
	TS      time.Time `json:"ts"`
}

// ReviewTranscript returns the task's newest review session: its rounds, its
// outcome once it has ended, and whether a round is running now. The frontend
// polls this while a round is in flight.
func (h *Handler) ReviewTranscript(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	// A transcript that stops being readable part way still has rounds worth
	// showing, so a read error with a session marks the response truncated
	// instead of failing it.
	sess, _, err := review.Newest(reviewStateDir(primaryWorktree(task.WorktreePaths)))
	if err != nil {
		logger.Handler.WarnContext(r.Context(), "review transcript: read stopped before the end",
			"task", id, "error", err)
	}
	if sess == nil {
		if err != nil {
			http.Error(w, "the review record could not be read", http.StatusInternalServerError)
			return
		}
		http.Error(w, "no review run for this task", http.StatusNotFound)
		return
	}

	rounds, costCap := h.reviewTuning()
	resp := reviewTranscriptResp{
		SessionID: sess.ID,
		// Authoritative: the live in-flight set, not an on-disk signal.
		Running: h.isReviewRunning(id),
		Legacy:  sess.Legacy,
		Config: &reviewRunConfig{
			MaxRounds:     rounds,
			CostCap:       costCap,
			ReviewerModel: h.reviewModelSetting(),
		},
		Rounds:    reviewRounds(sess.Records),
		Truncated: sess.Truncated,
	}
	if e := sess.End; e != nil {
		resp.Outcome = &reviewOutcome{
			Termination: e.Termination,
			Rounds:      e.Rounds,
			Unresolved:  e.Unresolved,
			Headline:    e.Headline,
			Tokens:      e.Tokens,
			USD:         e.USD,
			Skip:        e.Skip,
		}
	}
	httpjson.Write(w, http.StatusOK, resp)
}

// reviewModelSetting returns the configured reviewer model, or "" when it is
// unset or the env file cannot be read.
func (h *Handler) reviewModelSetting() string {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil {
		return ""
	}
	return cfg.ReviewModel
}

// reviewRounds groups transcript records by round, in the order the rounds
// first appear. Never nil, so the body always carries a rounds array.
func reviewRounds(records []review.Record) []reviewRound {
	out := make([]reviewRound, 0, len(records))
	index := map[int]int{}
	at := func(round int) *reviewRound {
		i, ok := index[round]
		if !ok {
			i = len(out)
			index[round] = i
			out = append(out, reviewRound{Round: round})
		}
		return &out[i]
	}
	for _, rec := range records {
		rd := at(rec.Round)
		switch rec.Role {
		case review.RoleReviewer:
			if rec.Failed() {
				rd.FailedAttempts = append(rd.FailedAttempts, reviewFailedAttempt{
					Code: rec.ErrorCode, Message: rec.Error, Raw: rec.Raw, TS: rec.TS,
				})
				continue
			}
			findings := rec.Findings
			if findings == nil {
				findings = []runner.ReviewFinding{}
			}
			rd.Reviewer = &reviewerAnswer{
				Verdict: rec.Verdict, Findings: findings, Summary: rec.Summary,
				Model: rec.Model, Harness: rec.Harness, Tokens: rec.Tokens, TS: rec.TS,
			}
		case review.RoleFeedback:
			rd.Feedback = rec.Body
		case review.RoleImplementer:
			rd.Reply = rec.Body
		}
	}
	return out
}
