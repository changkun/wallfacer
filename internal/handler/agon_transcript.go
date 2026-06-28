package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/pkg/httpjson"
)

// maxAgonRoundBytes caps a single round body in the transcript response so one
// pathological round can't bloat the payload.
const maxAgonRoundBytes = 256 * 1024

// agonRound is one proposer-or-critic turn in a fork's debate.
type agonRound struct {
	Round int    `json:"round"`
	Role  string `json:"role"` // "critic" | "proposer"
	Body  string `json:"body"` // round markdown
	TS    string `json:"ts"`
}

// agonFork is one critic fork's ordered rounds.
type agonFork struct {
	Index  int         `json:"index"`
	Rounds []agonRound `json:"rounds"`
}

// agonRunConfig describes how this task's agon run is configured (what the
// trigger actually does): critic fork count, per-fork round cap, token budget,
// and the harnesses driving each role.
type agonRunConfig struct {
	Forks         int      `json:"forks"`
	MaxRounds     int      `json:"max_rounds"`
	CostCap       int      `json:"cost_cap"`
	ProposerModel string   `json:"proposer_model"`
	CriticModels  []string `json:"critic_models"`
}

// agonOutcome is the terminal result of a finished run, from end.json.
type agonOutcome struct {
	Termination  string         `json:"termination"`   // steady_state | cost_cap | max_turn | ...
	TotalAttacks int            `json:"total_attacks"` // distinct attacks raised
	ByStatus     map[string]int `json:"by_status"`     // open/conceded/rebutted/...
	WallSeconds  int            `json:"wall_seconds"`
	Tokens       int            `json:"tokens"`
}

// agonTranscriptResp is the GET /api/tasks/{id}/agon/transcript body.
type agonTranscriptResp struct {
	SessionID string         `json:"session_id"`
	Running   bool           `json:"running"`
	Config    *agonRunConfig `json:"config,omitempty"`
	Outcome   *agonOutcome   `json:"outcome,omitempty"`
	Forks     []agonFork     `json:"forks"`
}

// agonTranscriptLine mirrors the subset of agon's state.TranscriptRecord we read
// from <session>/transcript.jsonl.
type agonTranscriptLine struct {
	TS    string `json:"ts"`
	Fork  int    `json:"fork"`
	Round int    `json:"round"`
	Role  string `json:"role"`
	Path  string `json:"path"`
}

// AgonTranscript returns the live trajectory of the most recent agon
// verification run for a task: each critic fork's proposer/critic rounds with
// their markdown bodies, read from agon's incrementally-written session dir.
// The frontend polls this while a run is in flight.
func (h *Handler) AgonTranscript(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	s, ok := h.requireStore(w)
	if !ok {
		return
	}
	task, err := s.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}

	stateDir := agonStateDir(primaryWorktree(task.WorktreePaths))
	sessionDir, sessionID, found := newestAgonSession(stateDir)
	if !found {
		http.Error(w, "no agon run for this task", http.StatusNotFound)
		return
	}

	forks, rounds, costCap := h.agonTuning()
	resp := agonTranscriptResp{
		SessionID: sessionID,
		// Authoritative: the live in-flight set, not an on-disk signal.
		Running: h.isAgonRunning(id),
		Config: &agonRunConfig{
			Forks:         forks,
			MaxRounds:     rounds,
			CostCap:       costCap,
			ProposerModel: string(agonProposerHarness),
			CriticModels:  agonCriticHarnessNames(),
		},
		Forks:   readAgonTranscript(sessionDir),
		Outcome: readAgonOutcome(sessionDir),
	}
	httpjson.Write(w, http.StatusOK, resp)
}

// readAgonOutcome reads the terminal stats from <sessionDir>/end.json. Returns
// nil while the run is still in flight (no end.json yet).
func readAgonOutcome(sessionDir string) *agonOutcome {
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
	return &agonOutcome{
		Termination:  ef.Termination.Reason,
		TotalAttacks: ef.Stats.TotalAttacks,
		ByStatus:     ef.Stats.ByStatus,
		WallSeconds:  ef.Stats.WallSeconds,
		Tokens:       ef.Stats.TokensUsed,
	}
}

// newestAgonSession returns the most-recently-modified session dir under
// <stateDir>/sessions/ (the current or last run). found is false when no
// session exists.
func newestAgonSession(stateDir string) (dir, id string, found bool) {
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

// readAgonTranscript parses <sessionDir>/transcript.jsonl and reads each
// referenced round file, grouped by fork in append order. Returns nil when the
// transcript does not exist yet (a run that just started).
func readAgonTranscript(sessionDir string) []agonFork {
	f, err := os.Open(filepath.Join(sessionDir, "transcript.jsonl"))
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }()

	byFork := map[int]*agonFork{}
	var order []int
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var rec agonTranscriptLine
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		body := readRoundBody(sessionDir, rec.Path)
		fk := byFork[rec.Fork]
		if fk == nil {
			fk = &agonFork{Index: rec.Fork}
			byFork[rec.Fork] = fk
			order = append(order, rec.Fork)
		}
		fk.Rounds = append(fk.Rounds, agonRound{
			Round: rec.Round, Role: rec.Role, Body: body, TS: rec.TS,
		})
	}

	forks := make([]agonFork, 0, len(order))
	for _, idx := range order {
		forks = append(forks, *byFork[idx])
	}
	return forks
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
	if len(b) > maxAgonRoundBytes {
		b = append(b[:maxAgonRoundBytes], "\n… (truncated)"...)
	}
	return string(b)
}
