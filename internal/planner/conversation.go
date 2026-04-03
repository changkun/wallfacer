package planner

import (
	"bufio"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
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
