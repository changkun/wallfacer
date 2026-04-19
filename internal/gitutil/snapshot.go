package gitutil

import (
	"context"
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// SnapshotDiff computes a unified diff of all changes in a snapshot repository
// relative to its initial commit (HEAD~1). If the repo has only one commit,
// it diffs against git's well-known empty-tree hash instead. Untracked files
// are appended as new-file diffs so the output reflects the full set of
// agent-made changes.
//
// Intended for repos created by [InitLocalRepo], where the initial commit
// captures the original workspace and any later commits capture agent changes.
func SnapshotDiff(ctx context.Context, snapshotPath string) string {
	out, err := cmdexec.Git(snapshotPath, "diff", "HEAD~1").WithContext(ctx).Output()
	if err != nil {
		// HEAD~1 doesn't exist (only the initial snapshot commit has been made,
		// no agent-changes commit on top). Nothing to diff against — return
		// uncommitted changes only, matching the semantic of "agent-made changes".
		out, _ = cmdexec.Git(snapshotPath, "diff", "HEAD").WithContext(ctx).Output()
	}

	if untrackedRaw, err := cmdexec.Git(snapshotPath,
		"ls-files", "--others", "--exclude-standard").WithContext(ctx).Output(); err == nil {
		for file := range strings.SplitSeq(untrackedRaw, "\n") {
			if file == "" {
				continue
			}
			fd, _ := cmdexec.Git(snapshotPath,
				"diff", "--no-index", "/dev/null", file).WithContext(ctx).Output()
			out += fd
		}
	}
	return out
}
