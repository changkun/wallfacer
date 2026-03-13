package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexEntrypointPreservesUsageFromStream(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not installed")
	}

	tempDir := t.TempDir()
	fakeCodexPath := filepath.Join(tempDir, "codex")
	argsPath := filepath.Join(tempDir, "codex.args")
	fakeCodex := `#!/bin/bash
set -euo pipefail

printf '%s\n' "$@" > "` + argsPath + `"

LAST_MSG=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --output-last-message)
            LAST_MSG="$2"
            shift 2
            ;;
        *)
            shift
            ;;
    esac
done

cat <<'EOF'
{"type":"item.completed","item":{"id":"item_65","type":"agent_message","text":"working"}}
{"type":"turn.completed","session_id":"sess-codex-123","stop_reason":"end_turn","total_cost_usd":0.125,"usage":{"input_tokens":321,"cached_input_tokens":111,"output_tokens":45}}
EOF
printf 'final answer from codex' > "$LAST_MSG"
`
	if err := os.WriteFile(fakeCodexPath, []byte(fakeCodex), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	// The entrypoint path is relative to the repo root. Since this test file
	// lives in the repo root package, the default working directory is correct
	// when running via `go test`. We avoid hardcoding /workspace/wallfacer
	// which only exists inside containers.
	cmd := exec.Command("/bin/bash", "sandbox/codex/entrypoint.sh", "-p", "test prompt", "--verbose", "--output-format", "stream-json")
	cmd.Env = append(os.Environ(), "PATH="+tempDir+":"+os.Getenv("PATH"))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run entrypoint: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected streamed events plus final envelope, got %q", string(out))
	}

	var envelope struct {
		Result       string  `json:"result"`
		SessionID    string  `json:"session_id"`
		StopReason   string  `json:"stop_reason"`
		IsError      bool    `json:"is_error"`
		TotalCostUSD float64 `json:"total_cost_usd"`
		Usage        struct {
			InputTokens              int `json:"input_tokens"`
			OutputTokens             int `json:"output_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &envelope); err != nil {
		t.Fatalf("unmarshal final envelope: %v\nraw: %s", err, lines[len(lines)-1])
	}

	if envelope.Result != "final answer from codex" {
		t.Fatalf("Result = %q, want final answer from codex", envelope.Result)
	}
	if envelope.SessionID != "sess-codex-123" {
		t.Fatalf("SessionID = %q, want sess-codex-123", envelope.SessionID)
	}
	if envelope.StopReason != "end_turn" {
		t.Fatalf("StopReason = %q, want end_turn", envelope.StopReason)
	}
	if envelope.IsError {
		t.Fatal("IsError = true, want false")
	}
	if envelope.TotalCostUSD != 0.125 {
		t.Fatalf("TotalCostUSD = %v, want 0.125", envelope.TotalCostUSD)
	}
	if envelope.Usage.InputTokens != 321 {
		t.Fatalf("InputTokens = %d, want 321", envelope.Usage.InputTokens)
	}
	if envelope.Usage.OutputTokens != 45 {
		t.Fatalf("OutputTokens = %d, want 45", envelope.Usage.OutputTokens)
	}
	if envelope.Usage.CacheReadInputTokens != 111 {
		t.Fatalf("CacheReadInputTokens = %d, want 111", envelope.Usage.CacheReadInputTokens)
	}
	if envelope.Usage.CacheCreationInputTokens != 0 {
		t.Fatalf("CacheCreationInputTokens = %d, want 0", envelope.Usage.CacheCreationInputTokens)
	}

	argsRaw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if !strings.Contains(string(argsRaw), "--config\nmodel_reasoning_effort=\"low\"\n") {
		t.Fatalf("expected /fast codex config in args, got:\n%s", string(argsRaw))
	}
}

func TestCodexEntrypointSkipsFastConfigWhenDisabled(t *testing.T) {
	tempDir := t.TempDir()
	fakeCodexPath := filepath.Join(tempDir, "codex")
	argsPath := filepath.Join(tempDir, "codex.args")
	fakeCodex := `#!/bin/bash
set -euo pipefail
printf '%s\n' "$@" > "` + argsPath + `"
LAST_MSG=""
while [[ $# -gt 0 ]]; do
    case "$1" in
        --output-last-message)
            LAST_MSG="$2"
            shift 2
            ;;
        *)
            shift
            ;;
    esac
done
printf 'ok' > "$LAST_MSG"
printf '{"type":"turn.completed","session_id":"sess","stop_reason":"end_turn","usage":{"input_tokens":1,"output_tokens":1}}\n'
`
	if err := os.WriteFile(fakeCodexPath, []byte(fakeCodex), 0o755); err != nil {
		t.Fatalf("write fake codex: %v", err)
	}

	cmd := exec.Command("/bin/bash", "sandbox/codex/entrypoint.sh", "-p", "test prompt")
	cmd.Env = append(os.Environ(),
		"PATH="+tempDir+":"+os.Getenv("PATH"),
		"WALLFACER_SANDBOX_FAST=false",
	)
	if _, err := cmd.Output(); err != nil {
		t.Fatalf("run entrypoint: %v", err)
	}

	argsRaw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatalf("read args: %v", err)
	}
	if strings.Contains(string(argsRaw), "model_reasoning_effort=\"low\"") {
		t.Fatalf("did not expect fast config in args, got:\n%s", string(argsRaw))
	}
}
