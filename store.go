package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type TaskUsage struct {
	InputTokens          int     `json:"input_tokens"`
	OutputTokens         int     `json:"output_tokens"`
	CacheReadInputTokens int     `json:"cache_read_input_tokens"`
	CacheCreationTokens  int     `json:"cache_creation_input_tokens"`
	CostUSD              float64 `json:"cost_usd"`
}

type Task struct {
	ID            uuid.UUID `json:"id"`
	Title         string    `json:"title,omitempty"`
	Prompt        string    `json:"prompt"`
	PromptHistory []string  `json:"prompt_history,omitempty"`
	Status        string    `json:"status"`
	Archived      bool      `json:"archived,omitempty"`
	SessionID     *string   `json:"session_id"`
	FreshStart    bool      `json:"fresh_start,omitempty"`
	Result        *string   `json:"result"`
	StopReason    *string   `json:"stop_reason"`
	Turns         int       `json:"turns"`
	Timeout       int       `json:"timeout"`
	Usage         TaskUsage `json:"usage"`
	Position      int       `json:"position"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Worktree isolation fields (populated when task moves to in_progress).
	WorktreePaths    map[string]string `json:"worktree_paths,omitempty"`     // host repoPath → worktree path
	BranchName       string            `json:"branch_name,omitempty"`        // "task/<uuid8>"
	CommitHashes     map[string]string `json:"commit_hashes,omitempty"`      // host repoPath → commit hash after merge
	BaseCommitHashes map[string]string `json:"base_commit_hashes,omitempty"` // host repoPath → defBranch HEAD before merge
}

type TaskEvent struct {
	ID        int64           `json:"id"`
	TaskID    uuid.UUID       `json:"task_id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

type Store struct {
	mu      sync.RWMutex
	dir     string
	tasks   map[uuid.UUID]*Task
	events  map[uuid.UUID][]TaskEvent
	nextSeq map[uuid.UUID]int

	subMu       sync.Mutex
	subscribers map[int]chan struct{}
	nextSubID   int
}

func NewStore(dir string) (*Store, error) {
	s := &Store{
		dir:         dir,
		tasks:       make(map[uuid.UUID]*Task),
		events:      make(map[uuid.UUID][]TaskEvent),
		nextSeq:     make(map[uuid.UUID]int),
		subscribers: make(map[int]chan struct{}),
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	if err := s.loadAll(); err != nil {
		return nil, fmt.Errorf("load store: %w", err)
	}

	return s, nil
}

// loadAll scans the data directory and populates in-memory maps.
func (s *Store) loadAll() error {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, err := uuid.Parse(entry.Name())
		if err != nil {
			continue // skip non-UUID directories
		}

		taskPath := filepath.Join(s.dir, entry.Name(), "task.json")
		raw, err := os.ReadFile(taskPath)
		if err != nil {
			logStore.Warn("skipping task", "name", entry.Name(), "error", err)
			continue
		}
		var task Task
		if err := json.Unmarshal(raw, &task); err != nil {
			logStore.Warn("skipping task", "name", entry.Name(), "error", err)
			continue
		}
		s.tasks[id] = &task

		tracesDir := filepath.Join(s.dir, entry.Name(), "traces")
		traceEntries, err := os.ReadDir(tracesDir)
		if err != nil {
			if os.IsNotExist(err) {
				s.nextSeq[id] = 1
				continue
			}
			return err
		}

		maxSeq := 0
		for _, te := range traceEntries {
			if te.IsDir() || !strings.HasSuffix(te.Name(), ".json") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(tracesDir, te.Name()))
			if err != nil {
				logStore.Warn("skipping trace", "task", entry.Name(), "trace", te.Name(), "error", err)
				continue
			}
			var evt TaskEvent
			if err := json.Unmarshal(raw, &evt); err != nil {
				logStore.Warn("skipping trace", "task", entry.Name(), "trace", te.Name(), "error", err)
				continue
			}
			s.events[id] = append(s.events[id], evt)

			// Extract sequence number from filename.
			base := strings.TrimSuffix(te.Name(), ".json")
			if seq, err := strconv.Atoi(base); err == nil && seq > maxSeq {
				maxSeq = seq
			}
		}

		// Sort events by ID for consistent ordering.
		sort.Slice(s.events[id], func(i, j int) bool {
			return s.events[id][i].ID < s.events[id][j].ID
		})

		s.nextSeq[id] = maxSeq + 1
	}

	return nil
}

func (s *Store) Close() {}

// subscribe registers a channel that receives a signal whenever task state changes.
// The caller must call unsubscribe with the returned ID when done.
func (s *Store) subscribe() (int, <-chan struct{}) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	id := s.nextSubID
	s.nextSubID++
	ch := make(chan struct{}, 1)
	s.subscribers[id] = ch
	return id, ch
}

func (s *Store) unsubscribe(id int) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	delete(s.subscribers, id)
}

// notify wakes all SSE subscribers. Non-blocking: if a subscriber's buffer is
// already full it already has a pending signal, so no additional send is needed.
func (s *Store) notify() {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	for _, ch := range s.subscribers {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

func (s *Store) ListTasks(_ context.Context, includeArchived bool) ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if !includeArchived && t.Archived {
			continue
		}
		tasks = append(tasks, *t)
	}
	sort.Slice(tasks, func(i, j int) bool {
		if tasks[i].Position != tasks[j].Position {
			return tasks[i].Position < tasks[j].Position
		}
		return tasks[i].CreatedAt.Before(tasks[j].CreatedAt)
	})
	return tasks, nil
}

func (s *Store) SetTaskArchived(_ context.Context, id uuid.UUID, archived bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Archived = archived
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) GetTask(_ context.Context, id uuid.UUID) (*Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	t, ok := s.tasks[id]
	if !ok {
		return nil, fmt.Errorf("task not found: %s", id)
	}
	copy := *t
	return &copy, nil
}

func (s *Store) CreateTask(_ context.Context, prompt string, timeout int) (*Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	maxPos := -1
	for _, t := range s.tasks {
		if t.Status == "backlog" && t.Position > maxPos {
			maxPos = t.Position
		}
	}

	if timeout <= 0 {
		timeout = 5
	}
	if timeout > 1440 {
		timeout = 1440
	}

	now := time.Now()
	task := &Task{
		ID:        uuid.New(),
		Prompt:    prompt,
		Status:    "backlog",
		Turns:     0,
		Timeout:   timeout,
		Position:  maxPos + 1,
		CreatedAt: now,
		UpdatedAt: now,
	}

	taskDir := filepath.Join(s.dir, task.ID.String())
	tracesDir := filepath.Join(taskDir, "traces")
	if err := os.MkdirAll(tracesDir, 0755); err != nil {
		return nil, err
	}

	if err := s.saveTask(task.ID, task); err != nil {
		return nil, err
	}

	s.tasks[task.ID] = task
	s.events[task.ID] = nil
	s.nextSeq[task.ID] = 1
	s.notify()

	ret := *task
	return &ret, nil
}

func (s *Store) UpdateTaskStatus(_ context.Context, id uuid.UUID, status string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Status = status
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) UpdateTaskTitle(_ context.Context, id uuid.UUID, title string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Title = title
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) UpdateTaskResult(_ context.Context, id uuid.UUID, result, sessionID, stopReason string, turns int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Result = &result
	t.SessionID = &sessionID
	t.StopReason = &stopReason
	t.Turns = turns
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) AccumulateTaskUsage(_ context.Context, id uuid.UUID, delta TaskUsage) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Usage.InputTokens += delta.InputTokens
	t.Usage.OutputTokens += delta.OutputTokens
	t.Usage.CacheReadInputTokens += delta.CacheReadInputTokens
	t.Usage.CacheCreationTokens += delta.CacheCreationTokens
	t.Usage.CostUSD += delta.CostUSD
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) UpdateTaskPosition(_ context.Context, id uuid.UUID, position int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.Position = position
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) UpdateTaskBacklog(_ context.Context, id uuid.UUID, prompt *string, timeout *int, freshStart *bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	if prompt != nil {
		t.Prompt = *prompt
	}
	if timeout != nil {
		v := *timeout
		if v <= 0 {
			v = 5
		}
		if v > 1440 {
			v = 1440
		}
		t.Timeout = v
	}
	if freshStart != nil {
		t.FreshStart = *freshStart
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) ResetTaskForRetry(_ context.Context, id uuid.UUID, newPrompt string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	t.PromptHistory = append(t.PromptHistory, t.Prompt)
	t.Prompt = newPrompt
	// Preserve SessionID so the task can resume from where it left off.
	// FreshStart=false (default) means it will resume; user can toggle via UI.
	t.FreshStart = false
	t.Result = nil
	t.StopReason = nil
	t.Turns = 0
	t.Status = "backlog"
	// Clear worktree fields so fresh worktrees are created on next run.
	t.WorktreePaths = nil
	t.BranchName = ""
	t.CommitHashes = nil
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) UpdateTaskWorktrees(_ context.Context, id uuid.UUID, worktreePaths map[string]string, branchName string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.WorktreePaths = worktreePaths
	t.BranchName = branchName
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

func (s *Store) UpdateTaskCommitHashes(_ context.Context, id uuid.UUID, hashes map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.CommitHashes = hashes
	t.UpdatedAt = time.Now()
	return s.saveTask(id, t)
}

func (s *Store) UpdateTaskBaseCommitHashes(_ context.Context, id uuid.UUID, hashes map[string]string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}
	t.BaseCommitHashes = hashes
	t.UpdatedAt = time.Now()
	return s.saveTask(id, t)
}

func (s *Store) ResumeTask(_ context.Context, id uuid.UUID, timeout *int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks[id]
	if !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	t.Status = "in_progress"
	if timeout != nil {
		v := *timeout
		if v <= 0 {
			v = 5
		}
		if v > 1440 {
			v = 1440
		}
		t.Timeout = v
	}
	t.UpdatedAt = time.Now()
	if err := s.saveTask(id, t); err != nil {
		return err
	}
	s.notify()
	return nil
}

// SaveTurnOutput persists raw stdout/stderr for a given turn to the outputs directory.
func (s *Store) SaveTurnOutput(taskID uuid.UUID, turn int, stdout, stderr []byte) error {
	outputsDir := filepath.Join(s.dir, taskID.String(), "outputs")
	if err := os.MkdirAll(outputsDir, 0755); err != nil {
		return fmt.Errorf("create outputs dir: %w", err)
	}

	name := fmt.Sprintf("turn-%04d.json", turn)
	if err := os.WriteFile(filepath.Join(outputsDir, name), stdout, 0644); err != nil {
		return fmt.Errorf("write stdout: %w", err)
	}

	if len(stderr) > 0 {
		stderrName := fmt.Sprintf("turn-%04d.stderr.txt", turn)
		if err := os.WriteFile(filepath.Join(outputsDir, stderrName), stderr, 0644); err != nil {
			return fmt.Errorf("write stderr: %w", err)
		}
	}

	return nil
}

func (s *Store) DeleteTask(_ context.Context, id uuid.UUID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[id]; !ok {
		return fmt.Errorf("task not found: %s", id)
	}

	taskDir := filepath.Join(s.dir, id.String())
	if err := os.RemoveAll(taskDir); err != nil {
		return fmt.Errorf("remove task dir: %w", err)
	}

	delete(s.tasks, id)
	delete(s.events, id)
	delete(s.nextSeq, id)
	s.notify()
	return nil
}

func (s *Store) InsertEvent(_ context.Context, taskID uuid.UUID, eventType string, data any) error {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.tasks[taskID]; !ok {
		return fmt.Errorf("task not found: %s", taskID)
	}

	seq := s.nextSeq[taskID]
	event := TaskEvent{
		ID:        int64(seq),
		TaskID:    taskID,
		EventType: eventType,
		Data:      jsonData,
		CreatedAt: time.Now(),
	}

	if err := s.saveEvent(taskID, seq, event); err != nil {
		return err
	}

	s.events[taskID] = append(s.events[taskID], event)
	s.nextSeq[taskID] = seq + 1
	return nil
}

func (s *Store) GetEvents(_ context.Context, taskID uuid.UUID) ([]TaskEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	events := s.events[taskID]
	out := make([]TaskEvent, len(events))
	copy(out, events)
	return out, nil
}

// saveTask atomically writes a task's metadata to its task.json file.
func (s *Store) saveTask(id uuid.UUID, task *Task) error {
	path := filepath.Join(s.dir, id.String(), "task.json")
	return atomicWriteJSON(path, task)
}

// saveEvent writes a single event to the task's traces directory.
func (s *Store) saveEvent(taskID uuid.UUID, seq int, event TaskEvent) error {
	tracesDir := filepath.Join(s.dir, taskID.String(), "traces")
	if err := os.MkdirAll(tracesDir, 0755); err != nil {
		return err
	}
	path := filepath.Join(tracesDir, fmt.Sprintf("%04d.json", seq))
	return atomicWriteJSON(path, event)
}

// atomicWriteJSON marshals v to JSON and writes it atomically via temp+rename.
func atomicWriteJSON(path string, v any) error {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
