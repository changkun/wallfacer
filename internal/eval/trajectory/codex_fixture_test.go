package trajectory

import (
	"testing"
)

// TestCodexAdapter_RealFixture parses a real captured turn from a
// Codex-sandbox task and asserts the adapter produces the expected
// event mix. Aggregated_output strings on command_execution items
// were trimmed before check-in to keep the fixture compact; the event
// shape is otherwise unchanged from the on-disk original.
func TestCodexAdapter_RealFixture(t *testing.T) {
	t.Parallel()

	raw := mustRead(t, "testdata/codex/sample-turn-with-tools.jsonl")
	tr, err := NewCodexAdapter().Parse(raw)
	if err != nil {
		t.Fatalf("Parse real fixture: %v", err)
	}
	if got, want := tr.Provider, ProviderCodex; got != want {
		t.Errorf("Provider = %q, want %q", got, want)
	}
	if tr.ProviderVersion != "" {
		t.Errorf("ProviderVersion = %q, want empty (Codex does not advertise in-stream)", tr.ProviderVersion)
	}

	counts := map[string]int{}
	for _, ev := range tr.Events {
		counts[ev.Type]++
	}
	// Sanity — the fixture has every top-level event kind Codex emits.
	for _, want := range []string{
		CodexTypeThreadStarted,
		CodexTypeTurnStarted,
		CodexTypeTurnCompleted,
		CodexTypeItemStarted,
		CodexTypeItemCompleted,
		CodexTypeItemUpdated,
		CodexTypeError,
	} {
		if counts[want] == 0 {
			t.Errorf("expected at least one %q event, got none", want)
		}
	}

	// First event must be thread.started and carry a thread_id.
	var started ThreadStartedEvent
	if err := tr.Events[0].Decode(&started); err != nil {
		t.Fatalf("Decode thread.started: %v", err)
	}
	if started.ThreadID == "" {
		t.Errorf("thread.started ThreadID empty")
	}

	// Every item.* event must carry a ThreadItem with ID and Type set,
	// and its DecodeDetails must succeed into the right typed variant.
	itemCounts := map[string]int{}
	for i, ev := range tr.Events {
		switch ev.Type {
		case CodexTypeItemStarted, CodexTypeItemUpdated, CodexTypeItemCompleted:
			var wrapper ItemStartedEvent // shape identical across started/updated/completed
			if err := ev.Decode(&wrapper); err != nil {
				t.Fatalf("Events[%d] decode: %v", i, err)
			}
			if wrapper.Item.ID == "" {
				t.Errorf("Events[%d] item.ID empty", i)
			}
			itemCounts[wrapper.Item.Type]++

			switch wrapper.Item.Type {
			case CodexItemAgentMessage:
				var m AgentMessageItem
				if err := wrapper.Item.DecodeDetails(&m); err != nil {
					t.Errorf("Events[%d] agent_message details: %v", i, err)
				}
			case CodexItemCommandExecution:
				var c CommandExecutionItem
				if err := wrapper.Item.DecodeDetails(&c); err != nil {
					t.Errorf("Events[%d] command_execution details: %v", i, err)
				}
				if c.Command == "" {
					t.Errorf("Events[%d] command_execution.Command empty", i)
				}
			case CodexItemFileChange:
				var f FileChangeItem
				if err := wrapper.Item.DecodeDetails(&f); err != nil {
					t.Errorf("Events[%d] file_change details: %v", i, err)
				}
			case CodexItemTodoList:
				var tl TodoListItem
				if err := wrapper.Item.DecodeDetails(&tl); err != nil {
					t.Errorf("Events[%d] todo_list details: %v", i, err)
				}
			}
		}
	}
	// Every item type the fixture carries must have been exercised at
	// least once — otherwise a regression in the adapter (e.g. a
	// dropped item.started line) could hide here.
	for _, want := range []string{
		CodexItemAgentMessage,
		CodexItemCommandExecution,
		CodexItemFileChange,
		CodexItemTodoList,
	} {
		if itemCounts[want] == 0 {
			t.Errorf("no %q items decoded from fixture", want)
		}
	}
}

// TestCodexAdapter_RealFixture_StrictDecode is the schema-drift guard
// for Codex. Every event in the fixture is strictly decoded into its
// typed variant with DisallowUnknownFields — and for item.* events,
// the wrapped ThreadItem is also strictly decoded into the matching
// item variant.
//
// When this test breaks: read the upstream schema at
// codex-rs/exec/src/exec_events.rs in openai/codex, port the new
// field(s) into codex_types.go, then re-run. Do not silence the test.
func TestCodexAdapter_RealFixture_StrictDecode(t *testing.T) {
	t.Parallel()

	raw := mustRead(t, "testdata/codex/sample-turn-with-tools.jsonl")
	tr, err := NewCodexAdapter().Parse(raw)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	for i, ev := range tr.Events {
		v, ok := codexVariantFor(ev)
		if !ok {
			continue // forward-compat passthrough
		}
		if err := strictDecode(ev.Raw, v); err != nil {
			t.Errorf("Events[%d] (type=%q) strict decode: %v", i, ev.Type, err)
			continue
		}

		// For item.* events, strict-decode the nested ThreadItem.
		// ThreadItem uses a custom UnmarshalJSON that preserves Raw,
		// so we go through DecodeDetails on the contained item.
		switch ev.Type {
		case CodexTypeItemStarted, CodexTypeItemUpdated, CodexTypeItemCompleted:
			// Re-decode to get a fresh wrapper (v above held a zero
			// pointer that Decode mutated — but we want clean access
			// to Item.Raw here).
			var wrapper ItemStartedEvent
			if err := ev.Decode(&wrapper); err != nil {
				t.Fatalf("Events[%d] wrapper decode: %v", i, err)
			}
			iv, ok := codexItemVariantFor(wrapper.Item)
			if !ok {
				continue
			}
			if err := strictDecode(wrapper.Item.Raw, iv); err != nil {
				t.Errorf("Events[%d] item (%q) strict decode: %v", i, wrapper.Item.Type, err)
			}
		}
	}
}

// codexVariantFor returns a zero-valued pointer to the typed struct
// that maps to the top-level event's type, or (nil, false) when the
// event is not one of the modeled variants.
func codexVariantFor(ev StreamEvent) (any, bool) {
	switch ev.Type {
	case CodexTypeThreadStarted:
		return new(ThreadStartedEvent), true
	case CodexTypeTurnStarted:
		return new(TurnStartedEvent), true
	case CodexTypeTurnCompleted:
		return new(TurnCompletedEvent), true
	case CodexTypeTurnFailed:
		return new(TurnFailedEvent), true
	case CodexTypeItemStarted:
		return new(ItemStartedEvent), true
	case CodexTypeItemUpdated:
		return new(ItemUpdatedEvent), true
	case CodexTypeItemCompleted:
		return new(ItemCompletedEvent), true
	case CodexTypeError:
		return new(ThreadErrorEvent), true
	}
	return nil, false
}

// codexItemVariantFor returns a zero-valued pointer to the typed
// struct for a ThreadItem's variant, or (nil, false) when the item
// type is not one of the modeled variants.
func codexItemVariantFor(item ThreadItem) (any, bool) {
	switch item.Type {
	case CodexItemAgentMessage:
		return new(AgentMessageItem), true
	case CodexItemReasoning:
		return new(ReasoningItem), true
	case CodexItemCommandExecution:
		return new(CommandExecutionItem), true
	case CodexItemFileChange:
		return new(FileChangeItem), true
	case CodexItemMcpToolCall:
		return new(McpToolCallItem), true
	case CodexItemCollabToolCall:
		return new(CollabToolCallItem), true
	case CodexItemWebSearch:
		return new(WebSearchItem), true
	case CodexItemTodoList:
		return new(TodoListItem), true
	case CodexItemError:
		return new(ErrorItem), true
	}
	return nil, false
}
