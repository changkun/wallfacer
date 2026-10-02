package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"latere.ai/x/pkg/httpjson"

	"latere.ai/x/wallfacer/internal/logger"
)

// maxReviewRoundBytes caps a single round body in the transcript response so one
// pathological round can't bloat the payload.
const maxReviewRoundBytes = 256 * 1024

// maxReviewTranscriptLineBytes caps one record of transcript.jsonl. A record is
// a timestamp, a fork and round number, a role and a relative path, so the cap
// is far above anything review writes; a longer line is a damaged transcript.
const maxReviewTranscriptLineBytes = 8 * 1024 * 1024

// reviewRound is one proposer-or-critic turn in a fork's debate.
type reviewRound struct {
	Round int    `json:"round"`
	Role  string `json:"role"` // "critic" | "proposer"
	Body  string `json:"body"` // round markdown
	TS    string `json:"ts"`
}

// reviewFork is one critic fork's ordered rounds.
type reviewFork struct {
	Index  int           `json:"index"`
	Rounds []reviewRound `json:"rounds"`
}

// reviewRunConfig describes how this task's review run is configured (what the
// trigger actually does): critic fork count, per-fork round cap, token budget,
// and the harnesses driving each role.
type reviewRunConfig struct {
	Forks         int      `json:"forks"`
	MaxRounds     int      `json:"max_rounds"`
	CostCap       int      `json:"cost_cap"`
	ProposerModel string   `json:"proposer_model"`
	CriticModels  []string `json:"critic_models"`
}

// reviewOutcome is the terminal result of a finished run, from end.json.
type reviewOutcome struct {
	Termination  string         `json:"termination"`   // steady_state | cost_cap | max_turn | ...
	TotalAttacks int            `json:"total_attacks"` // distinct attacks raised
	ByStatus     map[string]int `json:"by_status"`     // open/conceded/rebutted/...
	WallSeconds  int            `json:"wall_seconds"`
	Tokens       int            `json:"tokens"`
}

// reviewTranscriptResp is the GET /api/tasks/{id}/review/transcript body.
// Truncated is set when the transcript could not be read to its end: Forks then
// holds the rounds read before the failure and later rounds are missing.
type reviewTranscriptResp struct {
	SessionID string           `json:"session_id"`
	Running   bool             `json:"running"`
	Config    *reviewRunConfig `json:"config,omitempty"`
	Outcome   *reviewOutcome   `json:"outcome,omitempty"`
	Forks     []reviewFork     `json:"forks"`
	Truncated bool             `json:"truncated,omitempty"`
}

// reviewTranscriptLine mirrors the subset of review's state.TranscriptRecord we read
// from <session>/transcript.jsonl.
type reviewTranscriptLine struct {
	TS    string `json:"ts"`
	Fork  int    `json:"fork"`
	Round int    `json:"round"`
	Role  string `json:"role"`
	Path  string `json:"path"`
}

// ReviewTranscript returns the live trajectory of the most recent review
// verification run for a task: each critic fork's proposer/critic rounds with
// their markdown bodies, read from review's incrementally-written session dir.
// The frontend polls this while a run is in flight.
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

	stateDir := reviewStateDir(primaryWorktree(task.WorktreePaths))
	sessionDir, sessionID, found := newestReviewSession(stateDir)
	if !found {
		http.Error(w, "no review run for this task", http.StatusNotFound)
		return
	}

	// A transcript that stops being readable part way still has rounds worth
	// showing, so the read error marks the response instead of failing it.
	transcript, err := readReviewTranscript(sessionDir)
	if err != nil {
		logger.Handler.WarnContext(r.Context(), "review transcript: read stopped before the end",
			"task", id, "session_dir", sessionDir, "error", err)
	}

	forks, rounds, costCap := h.reviewTuning()
	resp := reviewTranscriptResp{
		SessionID: sessionID,
		// Authoritative: the live in-flight set, not an on-disk signal.
		Running: h.isReviewRunning(id),
		Config: &reviewRunConfig{
			Forks:         forks,
			MaxRounds:     rounds,
			CostCap:       costCap,
			ProposerModel: string(reviewProposerHarness),
			CriticModels:  reviewCriticHarnessNames(),
		},
		Forks:     transcript,
		Outcome:   readReviewOutcome(sessionDir),
		Truncated: err != nil,
	}
	httpjson.Write(w, http.StatusOK, resp)
}

// readReviewOutcome reads the terminal stats from <sessionDir>/end.json. Returns
// nil while the run is still in flight (no end.json yet).
func readReviewOutcome(sessionDir string) *reviewOutcome {
	b, err := os.ReadFile(filepath.Join(sessionDir, "end.json"))
	if err != nil {
		return nil
	}
	var ef struct {
		Termination struct {
			Reason string `json:"reason"`
		} `json:"termination"`
		Stats struct {
			TotalAttacks int            `json:"total_attacks"`
			ByStatus     map[string]int `json:"by_status"`
			TokensUsed   int            `json:"tokens_used"`
			WallSeconds  int            `json:"wall_seconds"`
		} `json:"stats"`
	}
	if err := json.Unmarshal(b, &ef); err != nil {
		return nil
	}
	return &reviewOutcome{
		Termination:  ef.Termination.Reason,
		TotalAttacks: ef.Stats.TotalAttacks,
		ByStatus:     ef.Stats.ByStatus,
		WallSeconds:  ef.Stats.WallSeconds,
		Tokens:       ef.Stats.TokensUsed,
	}
}

// newestReviewSession returns the most-recently-modified session dir under
// <stateDir>/sessions/ (the current or last run). found is false when no
// session exists.
func newestReviewSession(stateDir string) (dir, id string, found bool) {
	if stateDir == "" {
		return "", "", false
	}
	root := filepath.Join(stateDir, "sessions")
	entries, err := os.ReadDir(root)
	if err != nil {
		return "", "", false
	}
	var newestMod int64 = -1
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if m := info.ModTime().UnixNano(); m > newestMod {
			newestMod = m
			id = e.Name()
			dir = filepath.Join(root, e.Name())
		}
	}
	return dir, id, dir != ""
}

// readReviewTranscript parses <sessionDir>/transcript.jsonl and reads each
// referenced round file, grouped by fork in append order. Returns nil, nil when
// the transcript does not exist yet (a run that just started). When the file
// cannot be opened, or the read stops before the end (an I/O error, or a line
// over maxReviewTranscriptLineBytes, which ends the scan), it returns the
// rounds read up to that point together with the error.
func readReviewTranscript(sessionDir string) ([]reviewFork, error) {
	path := filepath.Join(sessionDir, "transcript.jsonl")
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open review transcript: %w", err)
	}
	defer func() { _ = f.Close() }()

	byFork := map[int]*reviewFork{}
	var order []int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxReviewTranscriptLineBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec reviewTranscriptLine
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		body := readRoundBody(sessionDir, rec.Path)
		fk := byFork[rec.Fork]
		if fk == nil {
			fk = &reviewFork{Index: rec.Fork}
			byFork[rec.Fork] = fk
			order = append(order, rec.Fork)
		}
		fk.Rounds = append(fk.Rounds, reviewRound{
			Round: rec.Round, Role: rec.Role, Body: body, TS: rec.TS,
		})
	}

	forks := make([]reviewFork, 0, len(order))
	for _, idx := range order {
		forks = append(forks, *byFork[idx])
	}
	if err := sc.Err(); err != nil {
		return forks, fmt.Errorf("read review transcript %s: %w", path, err)
	}
	return forks, nil
}

// readRoundBody reads a round markdown file referenced by a transcript record.
// rel must be a relative path inside the session dir; absolute paths or any
// ".." escape are rejected so a crafted transcript cannot read outside it.
func readRoundBody(sessionDir, rel string) string {
	if rel == "" || filepath.IsAbs(rel) {
		return ""
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(sessionDir, clean))
	if err != nil {
		return ""
	}
	if len(b) > maxReviewRoundBytes {
		return strings.ToValidUTF8(string(b[:maxReviewRoundBytes]), "") + "\n… (truncated)"
	}
	return string(b)
}
