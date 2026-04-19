package flow

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"changkun.de/x/wallfacer/internal/store"
)

type fakeCall struct {
	slug   string
	prompt string
}

type fakeLauncher struct {
	mu      sync.Mutex
	calls   []fakeCall
	results map[string]any
	errs    map[string]error
	// hooks fires after recording the call; tests use it to
	// observe concurrency or inject delays.
	hooks map[string]func(ctx context.Context)
}

func newFakeLauncher() *fakeLauncher {
	return &fakeLauncher{
		results: map[string]any{},
		errs:    map[string]error{},
		hooks:   map[string]func(context.Context){},
	}
}

func (f *fakeLauncher) RunAgent(ctx context.Context, slug string, _ *store.Task, prompt string) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, fakeCall{slug: slug, prompt: prompt})
	hook := f.hooks[slug]
	f.mu.Unlock()
	if hook != nil {
		hook(ctx)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := f.errs[slug]; err != nil {
		return nil, err
	}
	return f.results[slug], nil
}

func (f *fakeLauncher) callSlugs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.calls))
	for i, c := range f.calls {
		out[i] = c.slug
	}
	return out
}

func (f *fakeLauncher) promptFor(slug string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, c := range f.calls {
		if c.slug == slug {
			return c.prompt
		}
	}
	return ""
}

func TestExecute_LinearChain(t *testing.T) {
	f := Flow{Slug: "t", Steps: []Step{{AgentSlug: "a"}, {AgentSlug: "b"}, {AgentSlug: "c"}}}
	l := newFakeLauncher()
	task := &store.Task{Prompt: "go"}
	if err := NewEngine(l).Execute(context.Background(), f, task); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := l.callSlugs()
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("calls: got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("calls[%d]: got %q want %q", i, got[i], want[i])
		}
	}
}

func TestExecute_InputFromThreading(t *testing.T) {
	f := Flow{Steps: []Step{
		{AgentSlug: "a"},
		{AgentSlug: "b", InputFrom: "a"},
	}}
	l := newFakeLauncher()
	l.results["a"] = "refined prompt"
	task := &store.Task{Prompt: "original"}
	if err := NewEngine(l).Execute(context.Background(), f, task); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := l.promptFor("a"); got != "original" {
		t.Errorf("step a prompt: got %q want %q", got, "original")
	}
	if got := l.promptFor("b"); got != "refined prompt" {
		t.Errorf("step b prompt: got %q want %q", got, "refined prompt")
	}
}

func TestExecute_ParallelSiblings_RunConcurrently(t *testing.T) {
	// Three-way mutual-ref shape, matching the "implement" flow's
	// terminal triple. The transitive closure must collapse this
	// into a single group.
	f := Flow{Steps: []Step{
		{AgentSlug: "x", RunInParallelWith: []string{"y", "z"}},
		{AgentSlug: "y", RunInParallelWith: []string{"x", "z"}},
		{AgentSlug: "z", RunInParallelWith: []string{"x", "y"}},
	}}
	l := newFakeLauncher()

	var inflight atomic.Int32
	var peak atomic.Int32
	barrier := make(chan struct{})
	var once sync.Once
	releaseOnReady := func(ctx context.Context) {
		n := inflight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		if n == 3 {
			once.Do(func() { close(barrier) })
		}
		select {
		case <-barrier:
		case <-ctx.Done():
		case <-time.After(2 * time.Second):
		}
		inflight.Add(-1)
	}
	l.hooks["x"] = releaseOnReady
	l.hooks["y"] = releaseOnReady
	l.hooks["z"] = releaseOnReady

	if err := NewEngine(l).Execute(context.Background(), f, &store.Task{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got := peak.Load(); got != 3 {
		t.Fatalf("peak in-flight: got %d want 3 (siblings did not run concurrently)", got)
	}
	if len(l.callSlugs()) != 3 {
		t.Fatalf("expected 3 calls, got %v", l.callSlugs())
	}
}

func TestExecute_MandatoryStepError_StopsChain(t *testing.T) {
	f := Flow{Steps: []Step{{AgentSlug: "a"}, {AgentSlug: "b"}, {AgentSlug: "c"}}}
	l := newFakeLauncher()
	boom := errors.New("boom")
	l.errs["b"] = boom
	err := NewEngine(l).Execute(context.Background(), f, &store.Task{})
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("Execute err: got %v want wrap of %v", err, boom)
	}
	got := l.callSlugs()
	want := []string{"a", "b"}
	if len(got) != len(want) {
		t.Fatalf("calls: got %v want %v (c must not run)", got, want)
	}
}

func TestExecute_OptionalStepError_Continues(t *testing.T) {
	f := Flow{Steps: []Step{
		{AgentSlug: "a", Optional: true},
		{AgentSlug: "b"},
	}}
	l := newFakeLauncher()
	l.errs["a"] = errors.New("skip-me")
	if err := NewEngine(l).Execute(context.Background(), f, &store.Task{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := l.callSlugs()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("calls: got %v want [a b]", got)
	}
}

func TestExecute_CancellationAbortsInFlight(t *testing.T) {
	f := Flow{Steps: []Step{
		{AgentSlug: "x", RunInParallelWith: []string{"y"}},
		{AgentSlug: "y", RunInParallelWith: []string{"x"}},
	}}
	l := newFakeLauncher()

	ctx, cancel := context.WithCancel(context.Background())
	l.hooks["x"] = func(c context.Context) {
		cancel()
		select {
		case <-c.Done():
		case <-time.After(time.Second):
			t.Error("x: ctx not cancelled")
		}
	}
	l.hooks["y"] = func(c context.Context) {
		select {
		case <-c.Done():
		case <-time.After(time.Second):
			t.Error("y: ctx not cancelled")
		}
	}
	err := NewEngine(l).Execute(ctx, f, &store.Task{})
	if err == nil {
		t.Fatalf("Execute: expected error on cancellation, got nil")
	}
}

func TestExecute_FlowSnapshotIsStable(t *testing.T) {
	// The engine must deep-copy Flow at entry so mutating the
	// caller-held Flow mid-execution doesn't change what runs.
	started := make(chan struct{})
	block := make(chan struct{})
	l := newFakeLauncher()
	l.hooks["a"] = func(ctx context.Context) {
		close(started)
		select {
		case <-block:
		case <-ctx.Done():
		}
	}
	l.hooks["b"] = func(context.Context) {}

	f := Flow{Steps: []Step{{AgentSlug: "a"}, {AgentSlug: "b"}}}
	done := make(chan error, 1)
	go func() { done <- NewEngine(l).Execute(context.Background(), f, &store.Task{}) }()

	// Wait until the engine is past cloneFlow (i.e. already
	// dispatching step "a"), then mutate the caller's Flow.
	<-started
	f.Steps[1].AgentSlug = "mutated"
	close(block)

	if err := <-done; err != nil {
		t.Fatalf("Execute: %v", err)
	}
	got := l.callSlugs()
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("calls: got %v want [a b] (engine observed caller mutation)", got)
	}
}

func TestBuildParallelGroups_TransitiveClosure(t *testing.T) {
	// The implement flow's terminal triple: each lists the other
	// two. A naive grouping would produce overlapping groups; we
	// must collapse into one.
	steps := []Step{
		{AgentSlug: "impl"},
		{AgentSlug: "commit-msg", RunInParallelWith: []string{"title", "oversight"}},
		{AgentSlug: "title", RunInParallelWith: []string{"commit-msg", "oversight"}},
		{AgentSlug: "oversight", RunInParallelWith: []string{"commit-msg", "title"}},
	}
	groups := buildParallelGroups(steps)
	if len(groups) != 2 {
		t.Fatalf("groups: got %d want 2 (impl, then triple)", len(groups))
	}
	if len(groups[0]) != 1 || groups[0][0].AgentSlug != "impl" {
		t.Fatalf("group[0]: got %+v", groups[0])
	}
	if len(groups[1]) != 3 {
		t.Fatalf("group[1] size: got %d want 3", len(groups[1]))
	}
	wantOrder := []string{"commit-msg", "title", "oversight"}
	for i, w := range wantOrder {
		if groups[1][i].AgentSlug != w {
			t.Errorf("group[1][%d]: got %q want %q", i, groups[1][i].AgentSlug, w)
		}
	}
}
