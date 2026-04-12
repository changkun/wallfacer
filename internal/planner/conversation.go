package planner

import (
	"bufio"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"changkun.de/x/wallfacer/internal/pkg/atomicfile"
)

// Message is a single entry in the planning conversation log.
type Message struct {
	Role        string    `json:"role"`                   // "user" or "assistant"
	Content     string    `json:"content"`                // message text
	Timestamp   time.Time `json:"timestamp"`              // when the message was recorded
	FocusedSpec string    `json:"focused_spec,omitempty"` // spec path focused at the time
	RawOutput   string    `json:"raw_output,omitempty"`   // raw NDJSON output (assistant only)
	// PlanRound, when non-zero, identifies the planning git commit this
	// assistant message produced ("plan: round N — ..."). Zero means the
	// round wrote nothing to specs/ (or the message is from a user).
	// Undo affordances key off this field.
	PlanRound int `json:"plan_round,omitempty"`
}

// SessionInfo tracks the active Claude Code session for --resume.
type SessionInfo struct {
	SessionID   string    `json:"session_id"`             // Claude Code session ID
	LastActive  time.Time `json:"last_active"`            // last interaction timestamp
	FocusedSpec string    `json:"focused_spec,omitempty"` // last focused spec path
}

// ConversationStore persists planning chat messages and session state
// to ~/.wallfacer/planning/<fingerprint>/. Messages are stored as
// newline-delimited JSON for append efficiency; session info is stored
// as a single JSON file written atomically.
type ConversationStore struct {
	dir string
	mu  sync.Mutex
}

const (
	messagesFile = "messages.jsonl"
	sessionFile  = "session.json"
)

// NewConversationStore creates a store rooted at configDir/planning/fingerprint/.
// The directory is created if it does not exist.
func NewConversationStore(configDir, fingerprint string) (*ConversationStore, error) {
	dir := filepath.Join(configDir, "planning", fingerprint)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &ConversationStore{dir: dir}, nil
}

// AppendMessage appends a message to the JSONL conversation log.
func (s *ConversationStore) AppendMessage(msg Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	line, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	f, err := os.OpenFile(filepath.Join(s.dir, messagesFile), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	_, writeErr := f.Write(line)
	closeErr := f.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

// Messages reads all messages from the JSONL log in order.
// Malformed lines are skipped with a log warning.
func (s *ConversationStore) Messages() ([]Message, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(filepath.Join(s.dir, messagesFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var msgs []Message
	sc := bufio.NewScanner(f)
	lineNum := 0
	for sc.Scan() {
		lineNum++
		var m Message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			slog.Warn("conversation: skipping malformed line",
				"line", lineNum, "file", messagesFile, "err", err)
			continue
		}
		msgs = append(msgs, m)
	}
	if err := sc.Err(); err != nil {
		return msgs, err
	}
	return msgs, nil
}

// Clear removes the message log and session file, starting a fresh conversation.
func (s *ConversationStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var errs []error
	for _, name := range []string{messagesFile, sessionFile} {
		if err := os.Remove(filepath.Join(s.dir, name)); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// SaveSession atomically writes the session info file.
func (s *ConversationStore) SaveSession(info SessionInfo) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return atomicfile.WriteJSON(filepath.Join(s.dir, sessionFile), info, 0o644)
}

// LoadSession reads the session info file. Returns a zero-value
// SessionInfo and nil error if the file does not exist.
func (s *ConversationStore) LoadSession() (SessionInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	data, err := os.ReadFile(filepath.Join(s.dir, sessionFile))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return SessionInfo{}, nil
		}
		return SessionInfo{}, err
	}
	var info SessionInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return SessionInfo{}, err
	}
	return info, nil
}

// BuildHistoryContext creates a text summary of prior conversation messages
// that can be prepended to a prompt when starting a fresh session (e.g. after
// a stale session is cleared). Returns empty string if no history exists.
func (s *ConversationStore) BuildHistoryContext() string {
	msgs, err := s.Messages()
	if err != nil || len(msgs) == 0 {
		return ""
	}

	// Cap to last 20 messages to avoid blowing the context window.
	if len(msgs) > 20 {
		msgs = msgs[len(msgs)-20:]
	}

	var b strings.Builder
	b.WriteString("[Previous conversation context — session was reset]\n\n")
	for _, m := range msgs {
		if m.Role == "user" {
			b.WriteString("User: ")
		} else {
			b.WriteString("Assistant: ")
		}
		content := m.Content
		if len(content) > 500 {
			content = content[:500] + "..."
		}
		b.WriteString(content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// ExtractSessionID scans NDJSON output for the first session_id or thread_id
// field. Returns empty string if not found.
func ExtractSessionID(raw []byte) string {
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var obj struct {
			SessionID string `json:"session_id"`
			ThreadID  string `json:"thread_id"`
		}
		if json.Unmarshal([]byte(line), &obj) == nil {
			if obj.SessionID != "" {
				return obj.SessionID
			}
			if obj.ThreadID != "" {
				return obj.ThreadID
			}
		}
	}
	return ""
}

// ExtractResultText scans NDJSON output for the response text.
// It checks both the "result" line (type=result) and "assistant" lines
// (type=assistant with message.content[].text). Returns the result text
// if found, otherwise concatenated assistant message text.
func ExtractResultText(raw []byte) string {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")

	// First pass: look for a "result" line (most reliable).
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var obj struct {
			Type   string `json:"type"`
			Result string `json:"result"`
		}
		if json.Unmarshal([]byte(line), &obj) == nil && obj.Type == "result" && obj.Result != "" {
			return obj.Result
		}
	}

	// Fallback: extract text from assistant message content blocks.
	var text strings.Builder
	for _, rawLine := range lines {
		line := strings.TrimSpace(rawLine)
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var obj struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(line), &obj) == nil && obj.Type == "assistant" {
			for _, c := range obj.Message.Content {
				if c.Type == "text" && c.Text != "" {
					text.WriteString(c.Text)
				}
			}
		}
	}
	return text.String()
}

// IsStaleSessionError checks if the NDJSON error result indicates a missing
// or expired session by inspecting the structured error fields.
func IsStaleSessionError(raw []byte) bool {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var obj struct {
			Type    string   `json:"type"`
			IsError bool     `json:"is_error"`
			Subtype string   `json:"subtype"`
			Errors  []string `json:"errors"`
			Result  string   `json:"result"`
		}
		if json.Unmarshal([]byte(line), &obj) != nil || obj.Type != "result" || !obj.IsError {
			continue
		}
		// Check errors array and result text for session-related failures.
		for _, e := range obj.Errors {
			if strings.Contains(e, "session ID") || strings.Contains(e, "session") {
				return true
			}
		}
		if strings.Contains(obj.Result, "session ID") {
			return true
		}
	}
	return false
}

// IsErrorResult checks if the NDJSON output contains an error result.
func IsErrorResult(raw []byte) bool {
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if len(line) == 0 || line[0] != '{' {
			continue
		}
		var obj struct {
			Type    string `json:"type"`
			IsError bool   `json:"is_error"`
		}
		if json.Unmarshal([]byte(line), &obj) == nil && obj.Type == "result" {
			return obj.IsError
		}
	}
	return false
}
