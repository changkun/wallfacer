package runner

import (
	"context"
	"fmt"
	"math"
	"os/exec"
	"sort"
	"strings"
)

func (r *Runner) collectWorkspaceChurnSignals(ctx context.Context) []string {
	var signals []string
	for _, workspace := range r.workspacesForRunner() {
		sig := r.collectWorkspaceChurnSignalsForWorkspace(ctx, workspace)
		signals = append(signals, sig...)
	}
	if len(signals) <= maxIdeationChurnSignals {
		return signals
	}
	return signals[:maxIdeationChurnSignals]
}

func (r *Runner) collectWorkspaceChurnSignalsForWorkspace(ctx context.Context, workspace string) []string {
	raw, err := r.runWorkspaceGitCommand(ctx, workspace, "log", "--name-only", "--pretty=format:", "-n", "30")
	if err != nil {
		return nil
	}
	s := strings.TrimSpace(string(raw))
	if s == "" {
		return nil
	}
	counts := make(map[string]int)
	for _, line := range strings.Split(s, "\n") {
		file := strings.TrimSpace(line)
		if file == "" {
			continue
		}
		counts[file]++
	}
	type item struct {
		path  string
		count int
	}
	var list []item
	for path, count := range counts {
		list = append(list, item{path, count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count == list[j].count {
			return list[i].path < list[j].path
		}
		return list[i].count > list[j].count
	})
	maxItems := int(math.Min(float64(maxIdeationChurnSignals), float64(len(list))))
	out := make([]string, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		out = append(out, fmt.Sprintf("%s (%d commits)", list[i].path, list[i].count))
	}
	return out
}

func (r *Runner) collectWorkspaceTodoSignals(ctx context.Context) []string {
	var signals []string
	for _, workspace := range r.workspacesForRunner() {
		sig := r.collectWorkspaceTodoSignalsForWorkspace(ctx, workspace)
		signals = append(signals, sig...)
	}
	if len(signals) <= maxIdeationTodoSignals {
		return signals
	}
	return signals[:maxIdeationTodoSignals]
}

func (r *Runner) collectWorkspaceTodoSignalsForWorkspace(ctx context.Context, workspace string) []string {
	raw, err := r.runWorkspaceGitCommand(ctx, workspace, "grep", "-n", "-E", "TODO|FIXME|XXX", "--", ".")
	if err != nil {
		return nil
	}
	counts := make(map[string]int)
	for _, line := range strings.Split(string(raw), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		before, _, found := strings.Cut(trimmed, ":")
		if !found || before == "" {
			continue
		}
		counts[before]++
	}
	type item struct {
		path  string
		count int
	}
	var list []item
	for path, count := range counts {
		list = append(list, item{path, count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count == list[j].count {
			return list[i].path < list[j].path
		}
		return list[i].count > list[j].count
	})
	maxItems := int(math.Min(float64(maxIdeationTodoSignals), float64(len(list))))
	out := make([]string, 0, maxItems)
	for i := 0; i < maxItems; i++ {
		out = append(out, fmt.Sprintf("%s (%d markers)", list[i].path, list[i].count))
	}
	return out
}

func (r *Runner) runWorkspaceGitCommand(parentCtx context.Context, workspace string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(parentCtx, workspaceIdeationCommandTTL)
	defer cancel()
	command := exec.CommandContext(ctx, "git", append([]string{"-C", workspace}, args...)...)
	return command.Output()
}

func (r *Runner) workspacesForRunner() []string {
	var ws []string
	for _, raw := range strings.Fields(r.workspaces) {
		clean := strings.TrimSpace(raw)
		if clean == "" {
			continue
		}
		ws = append(ws, clean)
	}
	return ws
}
