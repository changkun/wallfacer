package handler

import (
	"context"
	"fmt"
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/cmdexec"
)

// commitPlanningRoundSummaryMax caps the summary embedded in a planning
// commit message. Keeps `git log --oneline` readable.
const commitPlanningRoundSummaryMax = 80

// commitPlanningRound stages and commits changes under specs/ in the given
// workspace with a message of the form `plan: round N — <summary>`.
// Returns (N, nil) on success; (0, nil) when specs/ has no pending changes
// or the git status probe itself fails — planning commits are best-effort
// and must not block the HTTP response. Errors are returned only for
// add/commit failures.
//
// Round N is derived by counting existing `plan: round` commits reachable
// from HEAD and adding one. This is monotonically increasing even after
// undo operations, since reverted commits no longer appear in the log.
func commitPlanningRound(ctx context.Context, ws, summary string) (int, error) {
	out, err := cmdexec.Git(ws, "status", "--porcelain", "specs/").WithContext(ctx).Output()
	if err != nil || out == "" {
		return 0, nil
	}

	n := 1
	logOut, logErr := cmdexec.Git(ws, "log", "--format=%s", "--grep=^plan: round").WithContext(ctx).Output()
	if logErr == nil && logOut != "" {
		n = len(strings.Split(logOut, "\n")) + 1
	}

	summary = strings.TrimSpace(summary)
	if len(summary) > commitPlanningRoundSummaryMax {
		summary = summary[:commitPlanningRoundSummaryMax]
	}
	msg := fmt.Sprintf("plan: round %d — %s", n, summary)

	if err := cmdexec.Git(ws, "add", "specs/").WithContext(ctx).Run(); err != nil {
		return 0, fmt.Errorf("git add specs/: %w", err)
	}
	if err := cmdexec.Git(ws, "commit", "-m", msg).WithContext(ctx).Run(); err != nil {
		return 0, fmt.Errorf("git commit: %w", err)
	}
	return n, nil
}
