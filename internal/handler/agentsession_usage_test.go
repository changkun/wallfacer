package handler

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/agentsession"
	"latere.ai/x/wallfacer/internal/executor"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/prompts"
	"latere.ai/x/wallfacer/internal/store"
)

// usageSessionBackend serves one chat round: it records the harness the
// runtime asked for and returns the scripted stdout.
type usageSessionBackend struct {
	executor.Backend
	output  string
	agentCh chan string
}

func (b *usageSessionBackend) Launch(_ context.Context, spec executor.ContainerSpec) (executor.Handle, error) {
	b.agentCh <- spec.Env["WALLFACER_AGENT"]
	return &errorSessionHandle{output: b.output}, nil
}

// TestSendAgentMessage_AttributesUsageToTheTurnHarness drives one chat round
// per harness and reads the usage log. The round is recorded under the harness
// the turn ran on. Claude's result line carries its usage; the Codex and
// OpenCode streams are not read for usage, so their rounds record zero tokens
// and zero cost rather than numbers taken from fields that mean something else.
func TestSendAgentMessage_AttributesUsageToTheTurnHarness(t *testing.T) {
	cases := []struct {
		harness    harness.ID
		output     string
		wantInput  int
		wantOutput int
		wantCost   float64
	}{
		{
			harness:    harness.Claude,
			output:     `{"type":"result","result":"","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0.0123,"usage":{"input_tokens":120,"output_tokens":40}}`,
			wantInput:  120,
			wantOutput: 40,
			wantCost:   0.0123,
		},
		{
			// The terminal record the host backend appends to a Codex stream.
			harness: harness.Codex,
			output:  `{"type":"turn.completed","result":"","session_id":"s1","stop_reason":"end_turn","is_error":false,"total_cost_usd":0,"usage":{"input_tokens":90,"output_tokens":30}}`,
		},
		{
			// The terminal record the host backend appends to an OpenCode stream.
			harness: harness.OpenCode,
			output:  `{"type":"result","sessionID":"s1","result":"","is_error":false,"stop_reason":"end_turn","usage":{"input":70,"output":20,"reasoning":0,"cache":{"read":0,"write":0}},"cost":0.004}`,
		},
	}
	for _, tc := range cases {
		t.Run(string(tc.harness), func(t *testing.T) {
			ws := t.TempDir()
			h := newStaticWorkspaceHandler(t, []string{ws})
			b := &usageSessionBackend{output: tc.output + "\n", agentCh: make(chan string, 2)}
			h.agentSession = agentsession.New(agentsession.Config{Backend: b, ConfigDir: t.TempDir(), Fingerprint: "usage"})

			w := httptest.NewRecorder()
			body := strings.NewReader(`{"message":"hello","harness":"` + string(tc.harness) + `"}`)
			h.SendAgentMessage(w, httptest.NewRequest(http.MethodPost, "/api/agent/messages", body))
			if w.Code != http.StatusAccepted {
				t.Fatalf("send: %d %s", w.Code, w.Body.String())
			}

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			select {
			case agent := <-b.agentCh:
				if agent != string(tc.harness) {
					t.Fatalf("runtime launched %q, want %q", agent, tc.harness)
				}
			case <-ctx.Done():
				t.Fatal("agent did not launch")
			}
			// The live log closes when the round's goroutine returns, after
			// usage has been persisted.
			reader := h.agentSession.LogReader("")
			if reader == nil {
				t.Fatal("live stream not initialized")
			}
			for {
				_, err := reader.ReadChunk(ctx)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}

			recs, err := store.ReadAgentSessionUsage(h.configDir, prompts.WorkspaceDataKey([]string{ws}), time.Time{})
			if err != nil {
				t.Fatalf("ReadAgentSessionUsage: %v", err)
			}
			if len(recs) != 1 {
				t.Fatalf("usage records = %d, want 1 for the round", len(recs))
			}
			got := recs[0]
			if got.Sandbox != tc.harness {
				t.Errorf("sandbox = %q, want %q", got.Sandbox, tc.harness)
			}
			if got.SubAgent != store.SandboxActivityAgentSession {
				t.Errorf("sub_agent = %q, want %q", got.SubAgent, store.SandboxActivityAgentSession)
			}
			if got.InputTokens != tc.wantInput || got.OutputTokens != tc.wantOutput || got.CostUSD != tc.wantCost {
				t.Errorf("usage = (%d in, %d out, $%v), want (%d in, %d out, $%v)",
					got.InputTokens, got.OutputTokens, got.CostUSD, tc.wantInput, tc.wantOutput, tc.wantCost)
			}
		})
	}
}

// TestPersistAgentRoundUsage_LogsUnreadableLog asserts a usage log that cannot
// be read is reported, not taken as empty: the round's turn index is the count
// of records already logged, so an unreadable log is logged as a warning and
// the round is not appended under a turn index it cannot know.
func TestPersistAgentRoundUsage_LogsUnreadableLog(t *testing.T) {
	ws := t.TempDir()
	h := newStaticWorkspaceHandler(t, []string{ws})
	key := prompts.WorkspaceDataKey([]string{ws})

	// One logged round, then a line longer than the reader's line limit, so
	// the log exists and cannot be read.
	if err := store.AppendAgentSessionUsage(h.configDir, key, store.TurnUsageRecord{Turn: 1, Sandbox: harness.Claude}); err != nil {
		t.Fatalf("AppendAgentSessionUsage: %v", err)
	}
	path := store.AgentSessionUsagePath(h.configDir, key)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open usage log: %v", err)
	}
	if _, err := f.WriteString(strings.Repeat("x", 128*1024) + "\n"); err != nil {
		t.Fatalf("write oversized line: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close usage log: %v", err)
	}
	if _, err := store.ReadAgentSessionUsage(h.configDir, key, time.Time{}); err == nil {
		t.Fatal("fixture: usage log is readable, want a read error")
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read usage log: %v", err)
	}

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h.persistAgentRoundUsage(agentRoundStdout(10, 5, 0, 0, 0.001), harness.Claude)

	if !strings.Contains(logs.String(), "read round usage") {
		t.Errorf("no warning for the unreadable usage log; logs:\n%s", logs.String())
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read usage log: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Errorf("usage log grew by %d bytes; want no record appended under an unknown turn index", len(after)-len(before))
	}
}
