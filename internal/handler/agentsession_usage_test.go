package handler

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
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
