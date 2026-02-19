package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Task struct {
	ID            uuid.UUID `json:"id"`
	Prompt        string    `json:"prompt"`
	PromptHistory []string  `json:"prompt_history,omitempty"`
	Status        string    `json:"status"`
	SessionID     *string   `json:"session_id"`
	Result        *string   `json:"result"`
	StopReason    *string   `json:"stop_reason"`
	Turns         int       `json:"turns"`
	Timeout       int       `json:"timeout"`
	Position      int       `json:"position"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type TaskEvent struct {
	ID        int64           `json:"id"`
	TaskID    uuid.UUID       `json:"task_id"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// legacyStoreData is the old monolithic format used for migration.
type legacyStoreData struct {
	Tasks       []Task      `json:"tasks"`
	Events      []TaskEvent `json:"events"`
	NextEventID int64       `json:"next_event_id"`
}

type Store struct {
	mu      sync.RWMutex
	dir     string
	tasks   map[uuid.UUID]*Task
	events  map[uuid.UUID][]TaskEvent
	nextSeq map[uuid.UUID]int
}

func NewStore(dir string) (*Store, error) {
	s := &Store{
		dir:     dir,
		tasks:   make(map[uuid.UUID]*Task),
		events:  make(map[uuid.UUID][]TaskEvent),
		nextSeq: make(map[uuid.UUID]int),
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	if err := s.migrateFromLegacy(); err != nil {
		return nil, fmt.Errorf("legacy migration: %w", err)
	}

	if err := s.loadAll(); err != nil {
		return nil, fmt.Errorf("load store: %w", err)
	}

	return s, nil
}

// migrateFromLegacy converts a data.json file to the per-task directory layout.
func (s *Store) migrateFromLegacy() error {
	legacyPath := "data.json"
	raw, err := os.ReadFile(legacyPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read legacy file: %w", err)
	}

	var old legacyStoreData
	if err := json.Unmarshal(raw, &old); err != nil {
		return fmt.Errorf("parse legacy file: %w", err)
	}

	log.Printf("migrating %d tasks from data.json to %s/", len(old.Tasks), s.dir)

	// Index events by task ID.
	eventsByTask := make(map[uuid.UUID][]TaskEvent)
	for _, e := range old.Events {
		eventsByTask[e.TaskID] = append(eventsByTask[e.TaskID], e)
	}

	for _, task := range old.Tasks {
		taskDir := filepath.Join(s.dir, task.ID.String())
		tracesDir := filepath.Join(taskDir, "traces")
		if err := os.MkdirAll(tracesDir, 0755); err != nil {
			return err
		}

		if err := atomicWriteJSON(filepath.Join(taskDir, "task.json"), task); err != nil {
			return err
		}

		events := eventsByTask[task.ID]
		sort.Slice(events, func(i, j int) bool { return events[i].ID < events[j].ID })
		for seq, evt := range events {
			name := fmt.Sprintf("%04d.json", seq+1)
			if err := atomicWriteJSON(filepath.Join(tracesDir, name), evt); err != nil {
				return err
			}
		}
	}

	if err := os.Rename(legacyPath, legacyPath+".bak"); err != nil {
		return fmt.Errorf("rename legacy file: %w", err)
	}
	log.Printf("migration complete; data.json renamed to data.json.bak")
	return nil
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
			log.Printf("skipping %s: %v", entry.Name(), err)
			continue
		}
		var task Task
		if err := json.Unmarshal(raw, &task); err != nil {
			log.Printf("skipping %s: %v", entry.Name(), err)
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
				log.Printf("skipping trace %s/%s: %v", entry.Name(), te.Name(), err)
				continue
			}
			var evt TaskEvent
			if err := json.Unmarshal(raw, &evt); err != nil {
				log.Printf("skipping trace %s/%s: %v", entry.Name(), te.Name(), err)
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

func (s *Store) ListTasks(_ context.Context) ([]Task, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
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
	return s.saveTask(id, t)
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
	return s.saveTask(id, t)
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
	return s.saveTask(id, t)
}

func (s *Store) UpdateTaskBacklog(_ context.Context, id uuid.UUID, prompt *string, timeout *int) error {
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
	t.UpdatedAt = time.Now()
	return s.saveTask(id, t)
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
	t.SessionID = nil
	t.Result = nil
	t.StopReason = nil
	t.Turns = 0
	t.Status = "backlog"
	t.UpdatedAt = time.Now()
	return s.saveTask(id, t)
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
