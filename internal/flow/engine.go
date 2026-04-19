package flow

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/sync/errgroup"

	"changkun.de/x/wallfacer/internal/store"
)

// Engine walks a Flow's steps linearly, fanning out parallel-sibling
// groups, and drives each step through an AgentLauncher. It holds no
// per-execution state: callers can reuse a single Engine across many
// Execute calls concurrently.
type Engine struct {
	launcher AgentLauncher
}

// NewEngine returns an Engine bound to the given launcher.
func NewEngine(l AgentLauncher) *Engine {
	return &Engine{launcher: l}
}

// Execute walks f's steps and dispatches each through the launcher.
//
// Semantics:
//   - Steps with no InputFrom receive task.Prompt.
//   - Steps with InputFrom: "<slug>" receive fmt.Sprint of that
//     step's parsed result.
//   - Sibling steps sharing RunInParallelWith edges run concurrently
//     in a single errgroup; the first error cancels the rest.
//   - Optional steps that fail produce a warning log and the engine
//     continues with the next step (or group).
//   - A mandatory step's failure returns immediately.
//   - Unknown step slugs in RunInParallelWith are ignored when
//     grouping; they surface at dispatch as a launcher error.
//
// The Flow is deep-copied on entry so concurrent registry edits
// cannot mutate the execution's view of the plan.
func (e *Engine) Execute(ctx context.Context, f Flow, task *store.Task) error {
	f = cloneFlow(f)
	groups := buildParallelGroups(f.Steps)
	results := make(map[string]any)
	var resultsMu sync.Mutex

	readResult := func(slug string) (any, bool) {
		resultsMu.Lock()
		defer resultsMu.Unlock()
		v, ok := results[slug]
		return v, ok
	}
	writeResult := func(slug string, v any) {
		resultsMu.Lock()
		defer resultsMu.Unlock()
		results[slug] = v
	}

	basePrompt := ""
	if task != nil {
		basePrompt = task.Prompt
	}

	for _, group := range groups {
		if err := ctx.Err(); err != nil {
			return err
		}

		if len(group) == 1 {
			step := group[0]
			prompt := resolvePrompt(step, basePrompt, readResult)
			parsed, err := e.launcher.RunAgent(ctx, step.AgentSlug, task, prompt)
			if err != nil {
				if step.Optional {
					slog.Warn("flow: optional step failed, continuing",
						"slug", step.AgentSlug, "err", err)
					continue
				}
				return fmt.Errorf("flow step %q: %w", step.AgentSlug, err)
			}
			writeResult(step.AgentSlug, parsed)
			continue
		}

		// Parallel group. Snapshot prompts before fanning out so
		// siblings cannot see each other's results mid-group.
		prompts := make([]string, len(group))
		for i, step := range group {
			prompts[i] = resolvePrompt(step, basePrompt, readResult)
		}

		g, gctx := errgroup.WithContext(ctx)
		parsedBySlug := make(map[string]any, len(group))
		var parsedMu sync.Mutex
		var skipMu sync.Mutex
		var skipped []string
		for i, step := range group {
			step := step
			prompt := prompts[i]
			g.Go(func() error {
				parsed, err := e.launcher.RunAgent(gctx, step.AgentSlug, task, prompt)
				if err != nil {
					if step.Optional {
						slog.Warn("flow: optional parallel step failed, continuing",
							"slug", step.AgentSlug, "err", err)
						skipMu.Lock()
						skipped = append(skipped, step.AgentSlug)
						skipMu.Unlock()
						return nil
					}
					return fmt.Errorf("flow step %q: %w", step.AgentSlug, err)
				}
				parsedMu.Lock()
				parsedBySlug[step.AgentSlug] = parsed
				parsedMu.Unlock()
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
		}
		for slug, parsed := range parsedBySlug {
			writeResult(slug, parsed)
		}
	}

	return nil
}

// resolvePrompt decides what text feeds a step: the task's base
// prompt by default, or the stringified parsed result of a prior
// step when InputFrom is set. Missing references fall through to
// the base prompt so optional upstream failures don't break the
// chain.
func resolvePrompt(step Step, base string, read func(string) (any, bool)) string {
	if step.InputFrom == "" {
		return base
	}
	v, ok := read(step.InputFrom)
	if !ok {
		return base
	}
	if s, isStr := v.(string); isStr {
		return s
	}
	return fmt.Sprint(v)
}

// buildParallelGroups walks steps in declared order and merges each
// step into the same group as any sibling it lists in
// RunInParallelWith — transitively. The three-way mutual-reference
// shape used by the "implement" flow's terminal triple
// (commit-msg/title/oversight each listing the other two) collapses
// to a single group because of the transitive closure.
//
// The returned groups preserve the appearance order of each group's
// first member so sequential flows keep their authored order.
func buildParallelGroups(steps []Step) [][]Step {
	if len(steps) == 0 {
		return nil
	}

	indexBySlug := make(map[string]int, len(steps))
	for i, s := range steps {
		indexBySlug[s.AgentSlug] = i
	}

	// Build adjacency via RunInParallelWith. Unknown siblings are
	// dropped silently — the launcher will surface them as a
	// dispatch error if they're actually referenced.
	adj := make([][]int, len(steps))
	for i, s := range steps {
		for _, peer := range s.RunInParallelWith {
			if j, ok := indexBySlug[peer]; ok && j != i {
				adj[i] = append(adj[i], j)
			}
		}
	}

	assigned := make([]int, len(steps))
	for i := range assigned {
		assigned[i] = -1
	}
	var groups [][]Step
	for i := range steps {
		if assigned[i] != -1 {
			continue
		}
		// BFS closure of i over adj.
		groupID := len(groups)
		queue := []int{i}
		assigned[i] = groupID
		members := []int{i}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			for _, n := range adj[cur] {
				if assigned[n] == -1 {
					assigned[n] = groupID
					members = append(members, n)
					queue = append(queue, n)
				}
			}
		}
		// Keep authored order within the group.
		sortAscending(members)
		group := make([]Step, len(members))
		for k, idx := range members {
			group[k] = steps[idx]
		}
		groups = append(groups, group)
	}
	return groups
}

func sortAscending(a []int) {
	// Tiny insertion sort — groups are small (typically 1-3 members).
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}
