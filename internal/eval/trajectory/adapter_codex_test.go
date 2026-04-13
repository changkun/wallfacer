package trajectory

import (
	"strings"
	"testing"
)

const codexSampleNDJSON = `{"type":"thread.started","thread_id":"thr-abc"}
{"type":"turn.started"}
{"type":"item.started","item":{"id":"item-1","type":"agent_message","text":""}}
{"type":"item.updated","item":{"id":"item-1","type":"agent_message","text":"reading files..."}}
{"type":"item.completed","item":{"id":"item-1","type":"agent_message","text":"done"}}
{"type":"item.completed","item":{"id":"item-2","type":"command_execution","command":"go test ./...","aggregated_output":"PASS\n","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item-3","type":"file_change","changes":[{"path":"foo.go","kind":"update"}],"status":"completed"}}
{"type":"turn.completed","usage":{"input_tokens":42,"cached_input_tokens":10,"output_tokens":7}}
`

func TestCodexAdapter_Parse(t *testing.T) {
	t.Parallel()

	tr, err := NewCodexAdapter().Parse([]byte(codexSampleNDJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := tr.Provider, ProviderCodex; got != want {
		t.Errorf("Provider = %q, want %q", got, want)
	}
	if got, want := tr.ProviderVersion, ""; got != want {
		t.Errorf("ProviderVersion = %q, want empty (codex does not advertise version in-stream)", got)
	}
	if got, want := len(tr.Events), 8; got != want {
		t.Fatalf("len(Events) = %d, want %d", got, want)
	}

	wantTypes := []string{
		CodexTypeThreadStarted,
		CodexTypeTurnStarted,
		CodexTypeItemStarted,
		CodexTypeItemUpdated,
		CodexTypeItemCompleted,
		CodexTypeItemCompleted,
		CodexTypeItemCompleted,
		CodexTypeTurnCompleted,
	}
	for i, want := range wantTypes {
		if got := tr.Events[i].Type; got != want {
			t.Errorf("Events[%d].Type = %q, want %q", i, got, want)
		}
		if len(tr.Events[i].Raw) == 0 {
			t.Errorf("Events[%d].Raw is empty", i)
		}
	}
}

func TestCodexAdapter_TypedDecode(t *testing.T) {
	t.Parallel()

	tr, err := NewCodexAdapter().Parse([]byte(codexSampleNDJSON))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}

	// thread.started
	var started ThreadStartedEvent
	if err := tr.Events[0].Decode(&started); err != nil {
		t.Fatalf("Decode thread.started: %v", err)
	}
	if started.ThreadID != "thr-abc" {
		t.Errorf("ThreadID = %q, want %q", started.ThreadID, "thr-abc")
	}

	// turn.completed
	var completed TurnCompletedEvent
	if err := tr.Events[7].Decode(&completed); err != nil {
		t.Fatalf("Decode turn.completed: %v", err)
	}
	if completed.Usage.InputTokens != 42 || completed.Usage.CachedInputTokens != 10 || completed.Usage.OutputTokens != 7 {
		t.Errorf("usage = %+v, want (42 in, 10 cached, 7 out)", completed.Usage)
	}

	// item.completed carrying a command_execution — two-step decode:
	// first the ItemCompletedEvent wrapper, then DecodeDetails on the
	// contained ThreadItem into the typed variant.
	var itemEv ItemCompletedEvent
	if err := tr.Events[5].Decode(&itemEv); err != nil {
		t.Fatalf("Decode item.completed: %v", err)
	}
	if itemEv.Item.ID != "item-2" || itemEv.Item.Type != CodexItemCommandExecution {
		t.Errorf("item wrapper = {id:%q type:%q}, want {id:item-2 type:command_execution}", itemEv.Item.ID, itemEv.Item.Type)
	}

	var cmd CommandExecutionItem
	if err := itemEv.Item.DecodeDetails(&cmd); err != nil {
		t.Fatalf("DecodeDetails command_execution: %v", err)
	}
	if cmd.Command != "go test ./..." {
		t.Errorf("cmd.Command = %q", cmd.Command)
	}
	if cmd.ExitCode == nil || *cmd.ExitCode != 0 {
		t.Errorf("cmd.ExitCode = %v, want pointer to 0", cmd.ExitCode)
	}
	if cmd.Status != CommandCompleted {
		t.Errorf("cmd.Status = %q, want %q", cmd.Status, CommandCompleted)
	}

	// item.completed carrying a file_change
	var fileEv ItemCompletedEvent
	if err := tr.Events[6].Decode(&fileEv); err != nil {
		t.Fatalf("Decode item.completed (file_change): %v", err)
	}
	var fc FileChangeItem
	if err := fileEv.Item.DecodeDetails(&fc); err != nil {
		t.Fatalf("DecodeDetails file_change: %v", err)
	}
	if fc.Status != PatchCompleted {
		t.Errorf("fc.Status = %q, want %q", fc.Status, PatchCompleted)
	}
	if len(fc.Changes) != 1 || fc.Changes[0].Path != "foo.go" || fc.Changes[0].Kind != PatchUpdate {
		t.Errorf("fc.Changes = %+v, want [{foo.go update}]", fc.Changes)
	}
}

func TestCodexAdapter_MalformedLine(t *testing.T) {
	t.Parallel()

	input := `{"type":"thread.started","thread_id":"t1"}` + "\n" + `not json` + "\n"
	_, err := NewCodexAdapter().Parse([]byte(input))
	if err == nil {
		t.Fatalf("Parse returned nil on malformed line")
	}
	if !strings.Contains(err.Error(), "line 2") {
		t.Errorf("error = %q, want line 2", err.Error())
	}
}

func TestCodexAdapter_UnknownTypePreserved(t *testing.T) {
	t.Parallel()

	// A hypothetical future event; adapter must keep it, not drop it.
	input := `{"type":"turn.interrupted","reason":"user_cancel"}` + "\n"
	tr, err := NewCodexAdapter().Parse([]byte(input))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got, want := len(tr.Events), 1; got != want {
		t.Fatalf("len(Events) = %d, want %d", got, want)
	}
	if tr.Events[0].Type != "turn.interrupted" {
		t.Errorf("Type = %q, want %q", tr.Events[0].Type, "turn.interrupted")
	}
}
