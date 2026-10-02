package review

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"latere.ai/x/pkg/atomicfile"

	"latere.ai/x/wallfacer/internal/runner"
)

// The session record. A task's review state directory (beside its worktree,
// never inside it) holds one directory per session:
//
//	<stateDir>/sessions/<id>/session.json      format marker and start time
//	<stateDir>/sessions/<id>/transcript.jsonl  one Record per line, append-only
//	<stateDir>/sessions/<id>/end.json          the End, once the session is over
//
// A session spans the rounds of one review: it stays open while findings go
// back to the task and the task takes its turn, and ends on a verdict, a bound
// or a skip. Its state is replayed from the transcript, so the record is the
// only state the review keeps. Session ids start with a UTC timestamp, so the
// newest session is the one with the greatest id.
//
// Directories without a session.json of this format were written by the
// debate engine the review replaced. They are reported as Legacy and never
// continued.
const (
	sessionsDir    = "sessions"
	sessionFile    = "session.json"
	transcriptFile = "transcript.jsonl"
	endFile        = "end.json"

	// recordFormat marks a session written by this package.
	recordFormat = 2

	// maxRecordLineBytes caps one transcript line on read. A record holds at
	// most a few capped bodies, so a longer line is a damaged transcript.
	maxRecordLineBytes = 8 << 20

	// maxBodyBytes caps the free text a record carries: the feedback sent to
	// the task, the task's reply, and the raw output of an unreadable answer.
	maxBodyBytes = 32 << 10
)

// Roles of a transcript record.
const (
	// RoleReviewer is a reviewer run: an answer, or a failed attempt when
	// ErrorCode is set.
	RoleReviewer = "reviewer"
	// RoleFeedback is the message the findings were sent to the task in.
	RoleFeedback = "feedback"
	// RoleImplementer is the task's reply after the turn the feedback started.
	RoleImplementer = "implementer"
)

// Terminations of a session, recorded in End.Termination.
const (
	// TerminationApproved: the reviewer approved the change.
	TerminationApproved = "approved"
	// TerminationMaxRounds: the last allowed round still requested changes.
	TerminationMaxRounds = "max_rounds"
	// TerminationCostCap: the reviewer token budget is spent.
	TerminationCostCap = "cost_cap"
	// TerminationSkipped: the review did not run; End.Skip says why.
	TerminationSkipped = "skipped"
	// TerminationSuperseded: the task left waiting while the reviewer ran, so
	// the findings were not delivered.
	TerminationSuperseded = "superseded"
	// TerminationUnreadable: the session's transcript could not be read to
	// its end, so its state cannot be replayed and the next round starts a new
	// session instead of failing on it every time.
	TerminationUnreadable = "unreadable"
)

// Record is one transcript line.
type Record struct {
	TS    time.Time `json:"ts"`
	Round int       `json:"round"`
	Role  string    `json:"role"`

	// Reviewer answer.
	Verdict  runner.ReviewVerdict   `json:"verdict,omitempty"`
	Findings []runner.ReviewFinding `json:"findings,omitempty"`
	Summary  string                 `json:"summary,omitempty"`
	Model    string                 `json:"model,omitempty"`
	Harness  string                 `json:"harness,omitempty"`
	Tokens   int                    `json:"tokens,omitempty"` // input plus output tokens of the run
	USD      float64                `json:"usd,omitempty"`

	// Failed reviewer attempt: a stable code, its fixed sentence, and the
	// reviewer's raw output when the answer could not be read.
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
	Raw       string `json:"raw,omitempty"`

	// Feedback or implementer text.
	Body string `json:"body,omitempty"`
}

// Failed reports whether r is a failed reviewer attempt.
func (r Record) Failed() bool { return r.Role == RoleReviewer && r.ErrorCode != "" }

// End is a session's outcome.
type End struct {
	TS          time.Time `json:"ts"`
	Termination string    `json:"termination"`
	Rounds      int       `json:"rounds"`     // rounds the reviewer answered
	Unresolved  int       `json:"unresolved"` // open findings at the end
	Headline    string    `json:"headline,omitempty"`
	Tokens      int       `json:"tokens"`
	USD         float64   `json:"usd"`
	Skip        *Skip     `json:"skip,omitempty"`
}

// sessionMeta is session.json.
type sessionMeta struct {
	Format    int       `json:"format"`
	StartedAt time.Time `json:"started_at"`
}

// Session is one review session read from disk.
type Session struct {
	ID        string
	Dir       string
	Legacy    bool // written by the earlier debate engine; Records and End are empty
	StartedAt time.Time
	Records   []Record
	End       *End
	// Truncated is set when the transcript could not be read to its end:
	// Records holds the lines before the failure.
	Truncated bool
}

// CompletedRounds is the number of rounds whose reviewer answered.
func (s *Session) CompletedRounds() int {
	n := 0
	for _, r := range s.Records {
		if r.Role == RoleReviewer && !r.Failed() {
			n++
		}
	}
	return n
}

// LastReview returns the last answered reviewer record, or nil.
func (s *Session) LastReview() *Record {
	for i := len(s.Records) - 1; i >= 0; i-- {
		if r := s.Records[i]; r.Role == RoleReviewer && !r.Failed() {
			return &s.Records[i]
		}
	}
	return nil
}

// lastReply returns the body of the implementer record after the last answered
// reviewer record, or "".
func (s *Session) lastReply() string {
	for _, r := range slices.Backward(s.Records) {
		if r.Role == RoleImplementer {
			return r.Body
		}
		if r.Role == RoleReviewer && !r.Failed() {
			return ""
		}
	}
	return ""
}

// awaitingTurn reports whether findings were sent to the task and its reply is
// not recorded yet. Failed attempts in between do not change that.
func (s *Session) awaitingTurn() bool {
	for _, r := range slices.Backward(s.Records) {
		if !r.Failed() {
			return r.Role == RoleFeedback
		}
	}
	return false
}

// Tokens sums the reviewer tokens of the session, failed attempts excluded
// (their usage is not reported back).
func (s *Session) Tokens() int {
	n := 0
	for _, r := range s.Records {
		n += r.Tokens
	}
	return n
}

// USD sums the reviewer cost of the session.
func (s *Session) USD() float64 {
	var usd float64
	for _, r := range s.Records {
		usd += r.USD
	}
	return usd
}

// Newest returns the session a task's review is on: the format-2 session with
// the greatest id under stateDir, or, when there is none, the most recently
// modified earlier session, reported Legacy. found is false when stateDir holds
// no session. When the newest session's transcript cannot be read to its end,
// the session is returned with Truncated set together with the error.
func Newest(stateDir string) (*Session, bool, error) {
	if stateDir == "" {
		return nil, false, nil
	}
	root := filepath.Join(stateDir, sessionsDir)
	entries, err := os.ReadDir(root)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("list review sessions: %w", err)
	}
	var (
		newest       string
		legacy       string
		legacyMod    int64 = -1
		newestLegacy bool
	)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		if _, ok, err := readMeta(dir); err != nil {
			return nil, false, err
		} else if ok {
			if e.Name() > newest {
				newest = e.Name()
			}
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue // removed between ReadDir and Info
		}
		if m := info.ModTime().UnixNano(); m > legacyMod || (m == legacyMod && e.Name() > legacy) {
			legacyMod, legacy = m, e.Name()
		}
	}
	if newest == "" {
		if legacy == "" {
			return nil, false, nil
		}
		newest, newestLegacy = legacy, true
	}
	if newestLegacy {
		return &Session{ID: newest, Dir: filepath.Join(root, newest), Legacy: true}, true, nil
	}
	s, err := Read(filepath.Join(root, newest))
	return s, s != nil, err
}

// Read reads the session in dir. A directory without a session.json of this
// format is returned Legacy. When the transcript cannot be read to its end,
// the session is returned with Truncated set together with the error.
func Read(dir string) (*Session, error) {
	s := &Session{ID: filepath.Base(dir), Dir: dir}
	meta, ok, err := readMeta(dir)
	if err != nil {
		return nil, err
	}
	if !ok {
		s.Legacy = true
		return s, nil
	}
	s.StartedAt = meta.StartedAt
	end, err := readEnd(dir)
	if err != nil {
		return nil, err
	}
	s.End = end
	s.Records, err = readRecords(dir)
	if err != nil {
		s.Truncated = true
		return s, err
	}
	return s, nil
}

// readMeta reads session.json. ok is false when the file is absent or names
// another format, which marks a session of the earlier engine.
func readMeta(dir string) (sessionMeta, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, sessionFile))
	if errors.Is(err, fs.ErrNotExist) {
		return sessionMeta{}, false, nil
	}
	if err != nil {
		return sessionMeta{}, false, fmt.Errorf("read review session %s: %w", dir, err)
	}
	var m sessionMeta
	if err := json.Unmarshal(b, &m); err != nil || m.Format != recordFormat {
		return sessionMeta{}, false, nil
	}
	return m, true, nil
}

// readEnd reads end.json; nil while the session is open.
func readEnd(dir string) (*End, error) {
	path := filepath.Join(dir, endFile)
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read review end %s: %w", path, err)
	}
	var e End
	if err := json.Unmarshal(b, &e); err != nil {
		return nil, fmt.Errorf("parse review end %s: %w", path, err)
	}
	return &e, nil
}

// readRecords parses transcript.jsonl. A missing transcript is an empty one.
// It returns the records read before a failure together with the error; a
// line that is not a record is skipped.
func readRecords(dir string) ([]Record, error) {
	path := filepath.Join(dir, transcriptFile)
	f, err := os.Open(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open review transcript: %w", err)
	}
	defer func() { _ = f.Close() }()

	var out []Record
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxRecordLineBytes)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r Record
		if err := json.Unmarshal(line, &r); err != nil {
			continue
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("read review transcript %s: %w", path, err)
	}
	return out, nil
}

// newSession creates a session directory with its session.json.
func newSession(stateDir string, now time.Time) (*Session, error) {
	if stateDir == "" {
		return nil, errors.New("review: no state directory")
	}
	id := now.UTC().Format("20060102T150405.000000000Z") + "-" + uuid.NewString()[:8]
	dir := filepath.Join(stateDir, sessionsDir, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create review session: %w", err)
	}
	meta := sessionMeta{Format: recordFormat, StartedAt: now.UTC()}
	if err := atomicfile.WriteJSON(filepath.Join(dir, sessionFile), meta, 0o644); err != nil {
		return nil, fmt.Errorf("write review session: %w", err)
	}
	return &Session{ID: id, Dir: dir, StartedAt: meta.StartedAt}, nil
}

// append writes r as the next transcript line and adds it to s.Records.
func (s *Session) append(r Record) error {
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Errorf("encode review record: %w", err)
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, transcriptFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open review transcript: %w", err)
	}
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		return fmt.Errorf("append review record: %w", werr)
	}
	s.Records = append(s.Records, r)
	return nil
}

// finish writes e as the session's end.json.
func (s *Session) finish(e End) error {
	if err := atomicfile.WriteJSON(filepath.Join(s.Dir, endFile), e, 0o644); err != nil {
		return fmt.Errorf("write review end: %w", err)
	}
	s.End = &e
	return nil
}

// Supersede ends the open session in dir as superseded: the task left waiting
// while its reviewer ran, so the findings were not delivered and the next
// review starts a new session. A session that has already ended is left as is.
func Supersede(dir string, now time.Time) error {
	s, err := Read(dir)
	if err != nil {
		return err
	}
	if s.Legacy || s.End != nil {
		return nil
	}
	return s.finish(End{
		TS:          now.UTC(),
		Termination: TerminationSuperseded,
		Rounds:      s.CompletedRounds(),
		Tokens:      s.Tokens(),
		USD:         s.USD(),
	})
}

// capBody truncates free text to maxBodyBytes on a rune boundary.
func capBody(s string) string {
	if len(s) <= maxBodyBytes {
		return s
	}
	cut := maxBodyBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "\n… (truncated)"
}
