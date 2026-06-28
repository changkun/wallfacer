package flow

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUserFlows_MissingDirIsNotError(t *testing.T) {
	flows, err := LoadUserFlows(filepath.Join(t.TempDir(), "does-not-exist"))
	if err != nil {
		t.Fatalf("LoadUserFlows: %v", err)
	}
	if len(flows) != 0 {
		t.Errorf("flows = %v, want empty", flows)
	}
}

func TestLoadUserFlows_ReadsValidYAML(t *testing.T) {
	dir := t.TempDir()
	yaml := `slug: tdd-loop
name: TDD Loop
description: Test first, implement second.
steps:
  - agent_slug: test
  - agent_slug: impl
    input_from: test
`
	if err := os.WriteFile(filepath.Join(dir, "tdd.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	flows, err := LoadUserFlows(dir)
	if err != nil {
		t.Fatalf("LoadUserFlows: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("flows = %d, want 1", len(flows))
	}
	f := flows[0]
	if f.Slug != "tdd-loop" || len(f.Steps) != 2 {
		t.Errorf("parsed flow mismatch: %+v", f)
	}
	if f.Steps[1].InputFrom != "test" {
		t.Errorf("input_from = %q, want test", f.Steps[1].InputFrom)
	}
}

func TestLoadUserFlows_RejectsMissingSteps(t *testing.T) {
	dir := t.TempDir()
	body := "slug: empty-flow\nname: Empty\n"
	if err := os.WriteFile(filepath.Join(dir, "empty.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadUserFlows(dir); err == nil {
		t.Fatal("expected error for empty steps")
	}
}

func TestLoadUserFlows_RejectsDuplicateAgentSlug(t *testing.T) {
	dir := t.TempDir()
	// AgentSlug is the engine's unique key for result wiring and parallel
	// grouping; two steps on the same agent must be rejected at load.
	body := "slug: dup-flow\nname: Dup\nsteps:\n  - agent_slug: impl\n  - agent_slug: impl\n"
	if err := os.WriteFile(filepath.Join(dir, "dup.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadUserFlows(dir); err == nil {
		t.Fatal("expected error for duplicate agent_slug across steps")
	}
}

func TestLoadUserFlows_RejectsStepWithoutAgentSlug(t *testing.T) {
	dir := t.TempDir()
	body := "slug: bad-step\nname: Bad\nsteps:\n  - optional: true\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadUserFlows(dir); err == nil {
		t.Fatal("expected error for step missing agent_slug")
	}
}

func TestLoadUserFlows_ReadsAgenticDynamicFields(t *testing.T) {
	dir := t.TempDir()
	yaml := `slug: mesh-flow
name: Mesh Flow
agentic: true
dynamic: true
topology: mesh
max_handoff_depth: 4
steps:
  - agent_slug: planner
  - agent_slug: builder
`
	if err := os.WriteFile(filepath.Join(dir, "mesh.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	flows, err := LoadUserFlows(dir)
	if err != nil {
		t.Fatalf("LoadUserFlows: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("flows = %d, want 1", len(flows))
	}
	f := flows[0]
	if !f.Agentic || !f.Dynamic {
		t.Errorf("agentic=%v dynamic=%v, want both true", f.Agentic, f.Dynamic)
	}
	if f.Topology != TopologyMesh {
		t.Errorf("topology = %q, want mesh", f.Topology)
	}
	if f.MaxHandoffDepth != 4 {
		t.Errorf("max_handoff_depth = %d, want 4", f.MaxHandoffDepth)
	}
}

func TestLoadUserFlows_RejectsUnknownTopology(t *testing.T) {
	dir := t.TempDir()
	body := "slug: bad-topo\nname: Bad\ntopology: clique\nsteps:\n  - agent_slug: impl\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := LoadUserFlows(dir); err == nil {
		t.Fatal("expected error for an unknown topology value")
	}
}

func TestWriteAndLoadUserFlow_AgenticRoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := Flow{
		Slug:            "mesh-flow",
		Name:            "Mesh Flow",
		Agentic:         true,
		Dynamic:         true,
		Topology:        TopologyMesh,
		MaxHandoffDepth: 3,
		Steps:           []Step{{AgentSlug: "planner"}, {AgentSlug: "builder"}},
	}
	if err := WriteUserFlow(dir, f); err != nil {
		t.Fatalf("WriteUserFlow: %v", err)
	}
	flows, err := LoadUserFlows(dir)
	if err != nil {
		t.Fatalf("LoadUserFlows: %v", err)
	}
	if len(flows) != 1 {
		t.Fatalf("flows = %d, want 1", len(flows))
	}
	got := flows[0]
	if !got.Agentic || !got.Dynamic || got.Topology != TopologyMesh || got.MaxHandoffDepth != 3 {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestNewMergedRegistry_IncludesBuiltinAndUser(t *testing.T) {
	dir := t.TempDir()
	body := "slug: custom-flow\nname: Custom\nsteps:\n  - agent_slug: impl\n"
	if err := os.WriteFile(filepath.Join(dir, "custom.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	reg, err := NewMergedRegistry(dir)
	if err != nil {
		t.Fatalf("NewMergedRegistry: %v", err)
	}
	if _, ok := reg.Get("custom-flow"); !ok {
		t.Error("custom-flow missing")
	}
	if _, ok := reg.Get("implement"); !ok {
		t.Error("built-in implement flow missing")
	}
}

func TestNewMergedRegistry_RejectsBuiltinShadow(t *testing.T) {
	dir := t.TempDir()
	body := "slug: implement\nname: Shadowed\nsteps:\n  - agent_slug: impl\n"
	if err := os.WriteFile(filepath.Join(dir, "implement.yaml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := NewMergedRegistry(dir); err == nil {
		t.Fatal("expected error for built-in slug shadow")
	}
}

func TestWriteAndDeleteUserFlow_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	f := Flow{
		Slug:  "tdd-loop",
		Name:  "TDD Loop",
		Steps: []Step{{AgentSlug: "test"}, {AgentSlug: "impl"}},
	}
	if err := WriteUserFlow(dir, f); err != nil {
		t.Fatalf("WriteUserFlow: %v", err)
	}
	flows, err := LoadUserFlows(dir)
	if err != nil {
		t.Fatalf("LoadUserFlows: %v", err)
	}
	if len(flows) != 1 || flows[0].Slug != "tdd-loop" {
		t.Fatalf("roundtrip: got %v", flows)
	}
	if err := DeleteUserFlow(dir, "tdd-loop"); err != nil {
		t.Fatalf("DeleteUserFlow: %v", err)
	}
	if err := DeleteUserFlow(dir, "tdd-loop"); err != nil {
		t.Errorf("second delete: %v", err)
	}
}

func TestIsBuiltin_Flow(t *testing.T) {
	if !IsBuiltin("implement") {
		t.Error("implement should be built-in")
	}
	if IsBuiltin("implement-extended") {
		t.Error("implement-extended should not be built-in")
	}
}
