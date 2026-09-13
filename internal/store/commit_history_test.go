package store

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestTaskCommitHistoryRoundTripAndConcurrentDeduplication(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "history"})
	if err != nil {
		t.Fatal(err)
	}
	initial, err := s.GetTaskCommits(task.ID)
	if err != nil || initial == nil || len(initial) != 0 {
		t.Fatal("empty history must be an empty array")
	}
	a := TaskCommit{Repository: "/repo-a", Hash: "123", Subject: "first", Patch: "+first", Turn: 1, Attempt: 1}
	if err := s.AppendTaskCommits(task.ID, []TaskCommit{a}); err != nil {
		t.Fatal(err)
	}
	changed := a
	changed.Turn = 9
	changed.Patch = "wrong"
	b := a
	b.Repository = "/repo-b"
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() {
			if err := s.AppendTaskCommits(task.ID, []TaskCommit{changed, b}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	got, err := s.GetTaskCommits(task.ID)
	if err != nil || len(got) != 2 || got[0].Patch != a.Patch || got[0].Turn != 1 {
		t.Fatal("first observations were lost or repositories collapsed")
	}
	if _, err := s.GetTaskCommits(uuid.New()); err == nil {
		t.Fatal("missing task accepted")
	}
	if err := s.AppendTaskCommits(uuid.New(), []TaskCommit{a}); err == nil {
		t.Fatal("write for missing task accepted")
	}
}

type failingHistoryBackend struct {
	StorageBackend
	failRead bool
}

func (b failingHistoryBackend) SaveBlob(uuid.UUID, string, []byte) error {
	return errors.New("disk full")
}
func (b failingHistoryBackend) ReadBlob(id uuid.UUID, key string) ([]byte, error) {
	if b.failRead {
		return nil, errors.New("disk unreadable")
	}
	return b.StorageBackend.ReadBlob(id, key)
}

func TestTaskCommitHistoryFailures(t *testing.T) {
	s := newTestStore(t)
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "history"})
	if err != nil {
		t.Fatal(err)
	}
	base := s.backend
	s.backend = failingHistoryBackend{StorageBackend: base}
	if err := s.AppendTaskCommits(task.ID, []TaskCommit{{Hash: "123"}}); err == nil {
		t.Fatal("write failure ignored")
	}
	s.backend = failingHistoryBackend{StorageBackend: base, failRead: true}
	if _, err := s.GetTaskCommits(task.ID); err == nil {
		t.Fatal("read failure ignored")
	}
	if err := s.AppendTaskCommits(task.ID, []TaskCommit{{Hash: "123"}}); err == nil {
		t.Fatal("update ignored read failure")
	}
	s.backend = base
	if err := s.AppendTaskCommits(task.ID, []TaskCommit{{AuthoredAt: time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)}}); err == nil {
		t.Fatal("invalid date accepted")
	}
	path := filepath.Join(s.DataDir(), task.ID.String(), "commit-history.json")
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetTaskCommits(task.ID); err == nil {
		t.Fatal("corrupt history hidden")
	}
}

func TestCommitHistoryAttemptSurvivesRetryPruning(t *testing.T) {
	s := newTestStore(t)
	s.retryHistoryLimit = 1
	task, err := s.CreateTaskWithOptions(bg(), TaskCreateOptions{Prompt: "history"})
	if err != nil {
		t.Fatal(err)
	}
	for range 4 {
		if err := s.ResetTaskForRetry(bg(), task.ID, "retry", true); err != nil {
			t.Fatal(err)
		}
	}
	reopened, err := NewFileStore(s.DataDir())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	updated, err := reopened.GetTask(bg(), task.ID)
	if err != nil || updated.CurrentAttempt() != 5 || len(updated.RetryHistory) != 1 {
		t.Fatal("attempt count followed pruned retry records")
	}
}
