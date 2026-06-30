package harness

import (
	"errors"
	"testing"
)

// TestToposRegistered verifies the native harness is a registry citizen so the
// UI selector, default resolution, and per-task pinning treat it uniformly.
func TestToposRegistered(t *testing.T) {
	h, ok := Lookup(Topos)
	if !ok {
		t.Fatal("Topos harness not registered")
	}
	if h.ID() != Topos {
		t.Errorf("ID() = %q, want %q", h.ID(), Topos)
	}
	if !Topos.IsValid() {
		t.Error("Topos.IsValid() = false, want true (registration-based)")
	}
}

// TestToposInUse verifies All() and the InProcess predicate surface Topos so
// callers can route it through the in-process seam.
func TestToposInUse(t *testing.T) {
	found := false
	for _, id := range All() {
		if id == Topos {
			found = true
		}
	}
	if !found {
		t.Error("All() does not include Topos")
	}
	if !InProcess(Topos) {
		t.Error("InProcess(Topos) = false, want true")
	}
	if InProcess(Claude) {
		t.Error("InProcess(Claude) = true, want false")
	}
}

// TestToposBuildArgvIsInProcess verifies BuildArgv refuses with ErrInProcess so
// an accidental subprocess-path dispatch fails loudly rather than launching a
// bogus process.
func TestToposBuildArgvIsInProcess(t *testing.T) {
	h, _ := Lookup(Topos)
	argv, stdin, err := h.BuildArgv(Request{Prompt: "hi"})
	if !errors.Is(err, ErrInProcess) {
		t.Errorf("BuildArgv err = %v, want ErrInProcess", err)
	}
	if argv != nil || stdin != nil {
		t.Errorf("BuildArgv argv=%v stdin=%v, want nil, nil", argv, stdin)
	}
}

// TestToposParseEventUnknown verifies ParseEvent records rather than errors,
// matching the schema-drift contract the CLI harnesses follow.
func TestToposParseEventUnknown(t *testing.T) {
	h, _ := Lookup(Topos)
	ev, err := h.ParseEvent([]byte(`{"x":1}`))
	if err != nil {
		t.Fatalf("ParseEvent err = %v, want nil", err)
	}
	if ev.Kind != KindUnknown {
		t.Errorf("ParseEvent kind = %v, want KindUnknown", ev.Kind)
	}
	if string(ev.Raw) != `{"x":1}` {
		t.Errorf("ParseEvent raw = %q, want preserved", ev.Raw)
	}
}

// TestToposCapabilities pins the declared capability matrix.
func TestToposCapabilities(t *testing.T) {
	h, _ := Lookup(Topos)
	caps := h.Capabilities()
	if !caps.SupportsSystemPrompt {
		t.Error("want SupportsSystemPrompt true")
	}
	if !caps.EmitsUsage {
		t.Error("want EmitsUsage true")
	}
	if caps.NeedsTTY {
		t.Error("want NeedsTTY false (in-process)")
	}
}

// TestToposAuthEnvEmpty verifies the native harness injects no subprocess env
// (credentials are resolved in-process through the model gateway).
func TestToposAuthEnvEmpty(t *testing.T) {
	h, _ := Lookup(Topos)
	env, err := h.AuthEnv(AuthConfig{AnthropicAPIKey: "sk-test"})
	if err != nil {
		t.Fatalf("AuthEnv err = %v", err)
	}
	if len(env) != 0 {
		t.Errorf("AuthEnv = %v, want empty", env)
	}
}
