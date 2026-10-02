package agentgraph_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"latere.ai/x/topos"

	"latere.ai/x/wallfacer/internal/agentgraph"
)

// testModel names the runtime's deterministic test model. Tests that execute a
// run pass it explicitly; a config that does not name it and carries no
// credential is refused.
var testModel = agentgraph.ModelConfig{Mode: agentgraph.ModelModeFake}

// TestRunOptions_SessionAndModel asserts the built topos.Options carry the
// session id and the model the config selects, and that a config selecting no
// model yields no options at all. The mapping is read structurally; only the
// seam names topos.
func TestRunOptions_SessionAndModel(t *testing.T) {
	opts, err := agentgraph.RunOptions("run-x", testModel)
	if err != nil {
		t.Fatalf("RunOptions: %v", err)
	}
	if opts.SessionID != "run-x" {
		t.Errorf("SessionID = %q, want run-x", opts.SessionID)
	}
	if got := string(opts.Model.Kind); got != "fake" {
		t.Errorf("Model.Kind = %q, want fake", got)
	}

	if _, err := agentgraph.RunOptions("run-x", agentgraph.ModelConfig{}); !errors.Is(err, agentgraph.ErrNoModelCredential) {
		t.Errorf("RunOptions with an unconfigured model: err = %v, want ErrNoModelCredential", err)
	}
}

// TestNewRunner_RefusesOptionsWithoutModel covers the constructor guard: topos
// options that name no model are refused instead of being handed to the
// runtime, which would resolve them to its test model.
func TestNewRunner_RefusesOptionsWithoutModel(t *testing.T) {
	if _, err := agentgraph.NewRunner(topos.Options{SessionID: "run-x"}); err == nil {
		t.Fatal("NewRunner accepted options that select no model")
	}
	if _, err := agentgraph.NewRunner(topos.Options{SessionID: "run-x", Model: topos.ModelOptions{Kind: topos.ModelFake}}); err != nil {
		t.Fatalf("NewRunner with the test model named explicitly: %v", err)
	}
}

// TestRunAgent_SingleNode exercises the native-harness entry point: a single
// agent runs as a one-node pinned region with the deterministic test model,
// producing a non-empty final text, exactly one trace node (<session>/<name>,
// status done), no edges, and live observer events that join to that node.
func TestRunAgent_SingleNode(t *testing.T) {
	var got []agentgraph.Event
	res, err := agentgraph.RunAgent(
		context.Background(), "run-native", testModel, "implement", "you implement", "do the thing", "",
		func(ev agentgraph.Event) { got = append(got, ev) },
	)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if res.Final == "" {
		t.Error("final text is empty")
	}
	if len(res.Trace.Nodes) != 1 {
		t.Fatalf("nodes = %+v, want exactly 1", res.Trace.Nodes)
	}
	n := res.Trace.Nodes[0]
	if n.ID != "run-native/implement" {
		t.Errorf("node id = %q, want run-native/implement", n.ID)
	}
	if n.Status != "done" {
		t.Errorf("node status = %q, want done", n.Status)
	}
	if len(res.Trace.Edges) != 0 {
		t.Errorf("edges = %+v, want none (single agent, no delegation)", res.Trace.Edges)
	}

	names := map[string]bool{}
	for _, ev := range got {
		names[ev.Name] = true
		if ev.Node != "" && ev.Node != n.ID {
			t.Errorf("event Node %q is not the single trace node %q", ev.Node, n.ID)
		}
	}
	for _, want := range []string{"SessionStart", "AssistantMessage", "SessionEnd"} {
		if !names[want] {
			t.Errorf("missing event %q; got %v", want, names)
		}
	}
}

// TestRunAgent_WithWorktreeExecutesInRepo proves end-to-end worktree execution:
// with a worktree set, the run's tools execute in that directory. The test
// model's scripted tool call writes marker.txt, and the file lands in the
// worktree, demonstrating the native harness edits the real repo (via topos
// Options.Workdir). The observer sees the tool call as a PostToolUse event,
// the event the runner maps onto the task timeline as tool use.
func TestRunAgent_WithWorktreeExecutesInRepo(t *testing.T) {
	worktree := t.TempDir()
	var sawToolUse bool
	_, err := agentgraph.RunAgent(
		context.Background(), "run-wt", testModel, "implement", "", "hi > marker.txt", worktree,
		func(ev agentgraph.Event) {
			if ev.Name == "PostToolUse" {
				sawToolUse = true
			}
		},
	)
	if err != nil {
		t.Fatalf("RunAgent with worktree: %v", err)
	}
	if !sawToolUse {
		t.Error("observer received no PostToolUse event for the scripted tool call")
	}
	got, err := os.ReadFile(filepath.Join(worktree, "marker.txt"))
	if err != nil {
		t.Fatalf("marker.txt not created in the worktree: %v", err)
	}
	if !strings.Contains(string(got), "hi") {
		t.Errorf("marker.txt = %q, want it to contain the echoed prompt", got)
	}
}

// TestRunAgent_DefaultName falls back to a stable node name when none is given,
// and works with a nil observer.
func TestRunAgent_DefaultName(t *testing.T) {
	res, err := agentgraph.RunAgent(context.Background(), "run-x", testModel, "", "", "hi", "", nil)
	if err != nil {
		t.Fatalf("RunAgent: %v", err)
	}
	if len(res.Trace.Nodes) != 1 || res.Trace.Nodes[0].ID != "run-x/agent" {
		t.Fatalf("nodes = %+v, want one node run-x/agent", res.Trace.Nodes)
	}
}
