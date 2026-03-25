package runner

import (
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"testing"
)

// findRuntime returns the first available container runtime binary, or
// skips the test if none is found.
func findRuntime(t *testing.T) string {
	t.Helper()
	for _, name := range []string{"podman", "docker"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	t.Skip("no container runtime (podman/docker) found")
	return ""
}

// ---------- LocalBackend.Launch tests ----------

func TestLocalBackend_LaunchAndWait(t *testing.T) {
	runtime := findRuntime(t)
	b := NewLocalBackend(runtime)

	spec := ContainerSpec{
		Runtime: runtime,
		Name:    "wallfacer-test-launch",
		Image:   "alpine:latest",
		Network: "none",
		Cmd:     []string{"echo", "hello sandbox"},
	}

	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if s := h.State(); s != SandboxRunning {
		t.Errorf("state after launch = %v, want Running", s)
	}
	if h.Name() != spec.Name {
		t.Errorf("Name() = %q, want %q", h.Name(), spec.Name)
	}

	out, err := io.ReadAll(h.Stdout())
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if !strings.Contains(string(out), "hello sandbox") {
		t.Errorf("stdout = %q, want substring %q", out, "hello sandbox")
	}

	code, err := h.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if s := h.State(); s != SandboxStopped {
		t.Errorf("state after wait = %v, want Stopped", s)
	}
}

func TestLocalBackend_LaunchNonZeroExit(t *testing.T) {
	runtime := findRuntime(t)
	b := NewLocalBackend(runtime)

	spec := ContainerSpec{
		Runtime: runtime,
		Name:    "wallfacer-test-exit1",
		Image:   "alpine:latest",
		Network: "none",
		Cmd:     []string{"sh", "-c", "exit 42"},
	}

	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	// Drain stdout so Wait() doesn't block.
	_, _ = io.ReadAll(h.Stdout())

	code, err := h.Wait()
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if code != 42 {
		t.Errorf("exit code = %d, want 42", code)
	}
	if s := h.State(); s != SandboxStopped {
		t.Errorf("state after non-zero exit = %v, want Stopped", s)
	}
}

func TestLocalBackend_Kill(t *testing.T) {
	runtime := findRuntime(t)
	b := NewLocalBackend(runtime)

	spec := ContainerSpec{
		Runtime: runtime,
		Name:    "wallfacer-test-kill",
		Image:   "alpine:latest",
		Network: "none",
		Cmd:     []string{"sleep", "300"},
	}

	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	if err := h.Kill(); err != nil {
		t.Fatalf("Kill: %v", err)
	}

	if s := h.State(); s != SandboxStopped {
		t.Errorf("state after kill = %v, want Stopped", s)
	}
}

func TestLocalBackend_LaunchBadImage(t *testing.T) {
	runtime := findRuntime(t)
	b := NewLocalBackend(runtime)

	spec := ContainerSpec{
		Runtime: runtime,
		Name:    "wallfacer-test-bad",
		Image:   "wallfacer-nonexistent-image:never",
		Network: "none",
		Cmd:     []string{"true"},
	}

	// Launch may fail immediately (if the runtime rejects the image before
	// Start returns) or succeed with a handle that fails on Wait.
	h, err := b.Launch(context.Background(), spec)
	if err != nil {
		// Expected: runtime rejected the image at Start().
		return
	}

	// Drain stdout.
	_, _ = io.ReadAll(h.Stdout())
	code, _ := h.Wait()
	if code == 0 {
		t.Error("expected non-zero exit code for bad image, got 0")
	}
}

// ---------- LocalBackend.List tests ----------

func TestLocalBackend_ListParsesPodmanJSON(t *testing.T) {
	// Unit test: exercises parseContainerList with Podman-format JSON array.
	containers := []map[string]any{
		{
			"Id":      "abc123",
			"Names":   []string{"wallfacer-myslug-12345678"},
			"Image":   "sandbox-claude:latest",
			"State":   "running",
			"Status":  "Up 5 minutes",
			"Created": 1711150800,
			"Labels": map[string]string{
				"wallfacer.task.id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
			},
		},
	}
	data, _ := json.Marshal(containers)
	raw, err := parseContainerList(data)
	if err != nil {
		t.Fatalf("parseContainerList: %v", err)
	}
	if len(raw) != 1 {
		t.Fatalf("got %d containers, want 1", len(raw))
	}
	name, err := raw[0].name()
	if err != nil {
		t.Fatalf("name: %v", err)
	}
	if name != "wallfacer-myslug-12345678" {
		t.Errorf("name = %q, want %q", name, "wallfacer-myslug-12345678")
	}
	if raw[0].createdUnix() != 1711150800 {
		t.Errorf("createdUnix = %d, want 1711150800", raw[0].createdUnix())
	}
}

func TestLocalBackend_ListParsesDockerNDJSON(t *testing.T) {
	// Unit test: exercises parseContainerList with Docker NDJSON format.
	line1 := `{"Id":"def456","Names":"wallfacer-slug-aabbccdd","Image":"sandbox-claude:latest","State":"running","Status":"Up 2 minutes","Labels":{"wallfacer.task.id":"11111111-2222-3333-4444-555555555555"}}`
	line2 := `{"Id":"ghi789","Names":"/wallfacer-other-11223344","Image":"sandbox-codex:latest","State":"exited","Status":"Exited (0) 1 minute ago","Labels":{}}`
	data := []byte(line1 + "\n" + line2 + "\n")

	raw, err := parseContainerList(data)
	if err != nil {
		t.Fatalf("parseContainerList: %v", err)
	}
	if len(raw) != 2 {
		t.Fatalf("got %d containers, want 2", len(raw))
	}

	// Docker Names is a string — verify prefix stripping.
	name, err := raw[1].name()
	if err != nil {
		t.Fatalf("name: %v", err)
	}
	if name != "wallfacer-other-11223344" {
		t.Errorf("name = %q, want %q", name, "wallfacer-other-11223344")
	}
}

func TestLocalBackend_ListEmpty(t *testing.T) {
	for _, input := range []string{"", "null", "  \n  "} {
		raw, err := parseContainerList([]byte(input))
		if err != nil {
			t.Errorf("parseContainerList(%q): %v", input, err)
		}
		if len(raw) != 0 {
			t.Errorf("parseContainerList(%q): got %d, want 0", input, len(raw))
		}
	}
}

func TestLocalBackend_ListTaskIDFallback(t *testing.T) {
	// Verify the old name-based UUID fallback when no label is present.
	containers := []map[string]any{
		{
			"Id":      "xyz999",
			"Names":   []string{"wallfacer-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
			"Image":   "sandbox-claude:latest",
			"State":   "running",
			"Status":  "Up 1 minute",
			"Created": 1711150800,
			"Labels":  map[string]string{},
		},
	}
	data, _ := json.Marshal(containers)
	raw, err := parseContainerList(data)
	if err != nil {
		t.Fatalf("parseContainerList: %v", err)
	}

	// Simulate the List() task ID extraction logic.
	name, _ := raw[0].name()
	taskID := ""
	if raw[0].Labels != nil {
		taskID = raw[0].Labels["wallfacer.task.id"]
	}
	if taskID == "" {
		candidate := strings.TrimPrefix(name, "wallfacer-")
		if candidate != name && isUUID(candidate) {
			taskID = candidate
		}
	}
	if taskID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("taskID = %q, want %q", taskID, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	}
}
