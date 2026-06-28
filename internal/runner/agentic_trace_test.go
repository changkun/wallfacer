package runner

import (
	"strings"
	"testing"

	"latere.ai/x/wallfacer/internal/agentgraph"
	"latere.ai/x/wallfacer/internal/store"
)

// TestAgenticTraceEvent maps topos trace events to task-timeline events: assistant
// text and tool use become readable system lines labelled by agent; lifecycle
// bookkeeping and empty payloads are filtered out.
func TestAgenticTraceEvent(t *testing.T) {
	cases := []struct {
		name      string
		ev        agentgraph.TraceEvent
		wantOK    bool
		wantType  store.EventType
		wantParts []string // substrings the result line must contain
		wantKind  string   // expected data["kind"] (empty = skip)
	}{
		{
			name:      "assistant text",
			ev:        agentgraph.TraceEvent{Name: "AssistantMessage", Node: "run-x/planner", AgentID: "planner", PayloadJSON: []byte(`{"text":"here is the plan"}`)},
			wantOK:    true,
			wantType:  store.EventTypeSystem,
			wantParts: []string{"planner", "here is the plan"},
			wantKind:  "assistant",
		},
		{
			name:   "assistant empty text filtered",
			ev:     agentgraph.TraceEvent{Name: "AssistantMessage", AgentID: "planner", PayloadJSON: []byte(`{"text":""}`)},
			wantOK: false,
		},
		{
			name:      "delegation",
			ev:        agentgraph.TraceEvent{Name: "SubagentStart", AgentID: "reviewer", PayloadJSON: []byte(`{}`)},
			wantOK:    true,
			wantType:  store.EventTypeSystem,
			wantParts: []string{"reviewer"},
		},
		{
			name:      "tool use",
			ev:        agentgraph.TraceEvent{Name: "PostToolUse", AgentID: "builder", PayloadJSON: []byte(`{"tool_call":{"name":"bash"}}`)},
			wantOK:    true,
			wantType:  store.EventTypeSystem,
			wantParts: []string{"builder", "bash"},
		},
		{
			name:   "lifecycle filtered",
			ev:     agentgraph.TraceEvent{Name: "SessionStart", AgentID: "planner", PayloadJSON: []byte(`{}`)},
			wantOK: false,
		},
		{
			name:      "label falls back to node id",
			ev:        agentgraph.TraceEvent{Name: "AssistantMessage", Node: "run-x/planner", PayloadJSON: []byte(`{"text":"hi"}`)},
			wantOK:    true,
			wantType:  store.EventTypeSystem,
			wantParts: []string{"run-x/planner", "hi"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			etype, data, ok := agenticTraceEvent(tc.ev)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if etype != tc.wantType {
				t.Errorf("type = %q, want %q", etype, tc.wantType)
			}
			for _, part := range tc.wantParts {
				if !strings.Contains(data["result"], part) {
					t.Errorf("result %q missing %q", data["result"], part)
				}
			}
			// Structured fields the Agent Graph view groups on.
			if data["source"] != "agentgraph" {
				t.Errorf("source = %q, want agentgraph", data["source"])
			}
			if tc.wantKind != "" && data["kind"] != tc.wantKind {
				t.Errorf("kind = %q, want %q", data["kind"], tc.wantKind)
			}
		})
	}
}
