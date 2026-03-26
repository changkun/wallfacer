package sandbox

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseContainerListPodmanJSON(t *testing.T) {
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
	result, err := ParseContainerList(data)
	if err != nil {
		t.Fatalf("ParseContainerList: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("got %d containers, want 1", len(result))
	}
	if result[0].Name != "wallfacer-myslug-12345678" {
		t.Errorf("name = %q, want %q", result[0].Name, "wallfacer-myslug-12345678")
	}
	if result[0].CreatedAt != 1711150800 {
		t.Errorf("createdAt = %d, want 1711150800", result[0].CreatedAt)
	}
	if result[0].TaskID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("taskID = %q, want %q", result[0].TaskID, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	}
}

func TestParseContainerListDockerNDJSON(t *testing.T) {
	line1 := `{"Id":"def456","Names":"wallfacer-slug-aabbccdd","Image":"sandbox-claude:latest","State":"running","Status":"Up 2 minutes","Labels":{"wallfacer.task.id":"11111111-2222-3333-4444-555555555555"}}`
	line2 := `{"Id":"ghi789","Names":"/wallfacer-other-11223344","Image":"sandbox-codex:latest","State":"exited","Status":"Exited (0) 1 minute ago","Labels":{}}`
	data := []byte(line1 + "\n" + line2 + "\n")

	result, err := ParseContainerList(data)
	if err != nil {
		t.Fatalf("ParseContainerList: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("got %d containers, want 2", len(result))
	}
	if result[1].Name != "wallfacer-other-11223344" {
		t.Errorf("name = %q, want %q", result[1].Name, "wallfacer-other-11223344")
	}
}

func TestParseContainerListEmpty(t *testing.T) {
	for _, input := range []string{"", "null", "  \n  "} {
		result, err := ParseContainerList([]byte(input))
		if err != nil {
			t.Errorf("ParseContainerList(%q): %v", input, err)
		}
		if len(result) != 0 {
			t.Errorf("ParseContainerList(%q): got %d, want 0", input, len(result))
		}
	}
}

func TestParseContainerListTaskIDFallback(t *testing.T) {
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
	result, err := ParseContainerList(data)
	if err != nil {
		t.Fatalf("ParseContainerList: %v", err)
	}
	if result[0].TaskID != "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee" {
		t.Errorf("taskID = %q, want %q", result[0].TaskID, "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	}
}

func TestIsUUID(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", true},
		{"12345678-1234-1234-1234-123456789abc", true},
		{"AABBCCDD-EEFF-0011-2233-445566778899", true},
		{"not-a-uuid", false},
		{"", false},
		{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeee", false},   // too short
		{"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeeee", false}, // too long
		{"aaaaaaaa_bbbb-cccc-dddd-eeeeeeeeeeee", false},  // underscore
		{"gaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", false},  // non-hex
	}
	for _, tt := range tests {
		if got := IsUUID(tt.input); got != tt.want {
			t.Errorf("IsUUID(%q) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBackendStateString(t *testing.T) {
	tests := []struct {
		state BackendState
		want  string
	}{
		{StateCreating, "creating"},
		{StateRunning, "running"},
		{StateStreaming, "streaming"},
		{StateStopping, "stopping"},
		{StateStopped, "stopped"},
		{StateFailed, "failed"},
		{BackendState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("BackendState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

func TestContainerSpecBuild(t *testing.T) {
	spec := ContainerSpec{
		Runtime: "podman",
		Name:    "test",
		Image:   "alpine:latest",
		Network: "none",
		Cmd:     []string{"echo", "hello"},
	}
	args := spec.Build()
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "--network=none") {
		t.Errorf("args missing --network=none: %v", args)
	}
	if !strings.Contains(joined, "--name test") {
		t.Errorf("args missing --name test: %v", args)
	}
	if !strings.Contains(joined, "alpine:latest") {
		t.Errorf("args missing image: %v", args)
	}
}
