package runner

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/pkg/cmdexec"
	"latere.ai/x/pkg/gitutil"

	"latere.ai/x/wallfacer/internal/store"
)

const maxCommitPatchBytes = 2 * 1024 * 1024

// boundedPatch keeps git output memory bounded while draining the entire pipe.
type boundedPatch struct {
	data      []byte
	truncated bool
}

func (w *boundedPatch) Write(p []byte) (int, error) {
	n := min(len(p), maxCommitPatchBytes-len(w.data))
	w.data = append(w.data, p[:n]...)
	w.truncated = w.truncated || n != len(p)
	return len(p), nil
}

func discoverTaskCommits(ctx context.Context, task *store.Task, paths map[string]string, turn int, saved []store.TaskCommit) ([]store.TaskCommit, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	known := make(map[string]bool, len(saved))
	for _, c := range saved {
		known[c.Repository+"\x00"+c.Hash] = true
	}
	repos := make([]string, 0, len(paths))
	for repo := range paths {
		repos = append(repos, repo)
	}
	slices.Sort(repos)
	commits := []store.TaskCommit{}
	for _, repo := range repos {
		wt := paths[repo]
		if _, err := os.Stat(wt); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		var base string
		if gitutil.IsGitRepo(repo) && gitutil.HasCommits(repo) {
			branch, err := gitutil.DefaultBranch(repo)
			if err != nil {
				return nil, err
			}
			base, err = cmdexec.Git(wt, "merge-base", "HEAD", branch).WithContext(ctx).Output()
			if err != nil {
				return nil, fmt.Errorf("commit history merge base: %w", err)
			}
		} else {
			var err error
			base, err = cmdexec.Git(wt, "rev-list", "--max-parents=0", "HEAD").WithContext(ctx).Output()
			if err != nil {
				return nil, fmt.Errorf("commit history snapshot root: %w", err)
			}
			if strings.Contains(base, "\n") {
				return nil, fmt.Errorf("commit history snapshot has multiple roots")
			}
		}
		hashes, err := cmdexec.Git(wt, "rev-list", "--reverse", base+"..HEAD", "--").WithContext(ctx).Output()
		if err != nil {
			return nil, fmt.Errorf("list task commits: %w", err)
		}
		for hash := range strings.FieldsSeq(hashes) {
			if known[repo+"\x00"+hash] {
				continue
			}
			metadata, err := cmdexec.Git(wt, "show", "-s", "--format=%s%x00%an%x00%aI", hash, "--").WithContext(ctx).Output()
			if err != nil {
				return nil, fmt.Errorf("read commit metadata: %w", err)
			}
			fields := strings.Split(metadata, "\x00")
			if len(fields) != 3 {
				return nil, fmt.Errorf("invalid commit metadata")
			}
			date, err := time.Parse(time.RFC3339, fields[2])
			if err != nil {
				return nil, fmt.Errorf("invalid commit date: %w", err)
			}
			patch := &boundedPatch{}
			cmd := exec.CommandContext(ctx, "git", "-C", wt, "show", "--format=", "--no-ext-diff", "--no-textconv", "--no-color", "--first-parent", hash, "--")
			cmd.Stdout = patch
			if err := cmd.Run(); err != nil {
				return nil, fmt.Errorf("read commit patch: %w", err)
			}
			commits = append(commits, store.TaskCommit{Repository: repo, Hash: hash, Subject: fields[0], Author: fields[1], AuthoredAt: date, Attempt: task.CurrentAttempt(), Turn: max(turn, 1), Patch: string(patch.data), PatchTruncated: patch.truncated})
		}
	}
	return commits, nil
}

func (r *Runner) captureTaskCommits(ctx context.Context, id uuid.UUID, turn int, paths map[string]string) error {
	s := r.taskStore(id)
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return err
	}
	if paths == nil {
		paths = task.WorktreePaths
	}
	saved, err := s.GetTaskCommits(id)
	if err != nil {
		return err
	}
	commits, err := discoverTaskCommits(ctx, task, paths, turn, saved)
	if err != nil {
		return err
	}
	if err := s.AppendTaskCommits(id, commits); err != nil {
		return fmt.Errorf("save commit history: %w", err)
	}
	return nil
}

// TaskCommits adds read-only discovery to saved history while worktrees exist.
func TaskCommits(ctx context.Context, s *store.Store, id uuid.UUID) ([]store.TaskCommit, error) {
	task, err := s.GetTask(ctx, id)
	if err != nil {
		return nil, err
	}
	saved, err := s.GetTaskCommits(id)
	if err != nil {
		return nil, err
	}
	live, err := discoverTaskCommits(ctx, task, task.WorktreePaths, task.Turns, saved)
	if err != nil {
		return nil, err
	}
	return store.MergeTaskCommits(saved, live), nil
}

func (r *Runner) failCommitHistory(ctx context.Context, id uuid.UUID, err error) {
	message := "Could not preserve task commit history; worktrees were kept for retry. " + err.Error()
	s := r.taskStore(id)
	_ = s.InsertEvent(ctx, id, store.EventTypeError, map[string]string{"error": message})
	_ = s.UpdateTaskStatus(ctx, id, store.TaskStatusFailed)
	_ = s.UpdateTaskResult(ctx, id, message, "", "", 0)
}
