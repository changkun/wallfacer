package harness

import (
	"reflect"
	"testing"
)

func TestRegistry_RegisterAndLookup(t *testing.T) {
	prev := snapshotForTest()
	t.Cleanup(func() { restoreForTest(prev) })

	f := &FakeHarness{IDValue: Claude}
	Register(f)

	got, ok := Lookup(Claude)
	if !ok {
		t.Fatal("Lookup(Claude) = !ok")
	}
	if got != f {
		t.Errorf("Lookup returned a different instance")
	}

	if _, ok := Lookup(Codex); ok {
		t.Errorf("Lookup(Codex) = ok, want !ok")
	}
}

func TestRegistry_DuplicateRegisterPanics(t *testing.T) {
	prev := snapshotForTest()
	t.Cleanup(func() { restoreForTest(prev) })

	Register(&FakeHarness{IDValue: Claude})

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate Register")
		}
	}()
	Register(&FakeHarness{IDValue: Claude})
}

func TestRegistry_AllIsSorted(t *testing.T) {
	prev := snapshotForTest()
	t.Cleanup(func() { restoreForTest(prev) })

	Register(&FakeHarness{IDValue: Pi})
	Register(&FakeHarness{IDValue: Claude})
	Register(&FakeHarness{IDValue: Codex})

	got := All()
	want := []ID{Claude, Codex, Pi}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("All() = %v, want %v", got, want)
	}
}

func TestDefault(t *testing.T) {
	if got := Default(); got != Claude {
		t.Errorf("Default() = %q, want %q", got, Claude)
	}
}

func TestCapabilities_ZeroValueIsAllFalse(t *testing.T) {
	var c Capabilities
	if c.SupportsResume || c.SupportsMCP || c.SupportsSystemPrompt ||
		c.EmitsUsage || c.EmitsCost || c.NeedsTTY {
		t.Errorf("zero Capabilities has a true field: %+v", c)
	}
}

func TestFakeHarness_RecordsCalls(t *testing.T) {
	f := &FakeHarness{
		IDValue:      Cursor,
		Argv:         []string{"cursor-agent", "-p", "x"},
		Events:       []Event{{Kind: KindResult, SessionID: "abc"}},
		AuthEnvValue: map[string]string{"CURSOR_API_KEY": "k"},
		Caps:         Capabilities{SupportsResume: true},
	}

	if f.ID() != Cursor {
		t.Errorf("ID() = %q, want %q", f.ID(), Cursor)
	}

	argv, _, err := f.BuildArgv(Request{Prompt: "hi"})
	if err != nil {
		t.Fatalf("BuildArgv: %v", err)
	}
	if !reflect.DeepEqual(argv, []string{"cursor-agent", "-p", "x"}) {
		t.Errorf("BuildArgv argv = %v", argv)
	}
	if len(f.BuildCalls) != 1 || f.BuildCalls[0].Prompt != "hi" {
		t.Errorf("BuildCalls not recorded: %+v", f.BuildCalls)
	}

	evt, err := f.ParseEvent([]byte(`{"x":1}`))
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if evt.Kind != KindResult || evt.SessionID != "abc" {
		t.Errorf("ParseEvent = %+v", evt)
	}
	// Next call drains the queue and returns a zero Event.
	evt, err = f.ParseEvent([]byte(`{}`))
	if err != nil || evt.Kind != KindUnknown {
		t.Errorf("drained ParseEvent = %+v err=%v", evt, err)
	}

	env, err := f.AuthEnv(AuthConfig{CursorAPIKey: "abc"})
	if err != nil {
		t.Fatalf("AuthEnv: %v", err)
	}
	if env["CURSOR_API_KEY"] != "k" {
		t.Errorf("AuthEnv = %v", env)
	}
	if len(f.AuthCalls) != 1 || f.AuthCalls[0].CursorAPIKey != "abc" {
		t.Errorf("AuthCalls not recorded: %+v", f.AuthCalls)
	}

	if !f.Capabilities().SupportsResume {
		t.Errorf("Capabilities not returned: %+v", f.Capabilities())
	}
}
