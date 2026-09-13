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
)

type errorSessionBackend struct {
	executor.Backend
	ready     chan struct{}
	release   chan struct{}
	launchErr error
	output    string
	exitCode  int
}

func (b *errorSessionBackend) Launch(context.Context, executor.ContainerSpec) (executor.Handle, error) {
	close(b.ready)
	<-b.release
	if b.launchErr != nil {
		return nil, b.launchErr
	}
	return &errorSessionHandle{output: b.output, code: b.exitCode}, nil
}

type errorSessionHandle struct {
	output string
	code   int
}

func (*errorSessionHandle) State() executor.BackendState { return executor.StateRunning }
func (h *errorSessionHandle) Stdout() io.ReadCloser      { return io.NopCloser(strings.NewReader(h.output)) }
func (*errorSessionHandle) Stderr() io.ReadCloser {
	return io.NopCloser(strings.NewReader("authentication failed"))
}
func (h *errorSessionHandle) Wait() (int, error) { return h.code, nil }
func (*errorSessionHandle) Kill() error          { return nil }
func (*errorSessionHandle) Name() string         { return "test-error-session" }

func TestAgentSessionFailuresAreStreamedAndPersisted(t *testing.T) {
	for _, scenario := range []string{"launch", "nonzero-exit", "provider-error"} {
		t.Run(scenario, func(t *testing.T) {
			h := newTestHandler(t)
			b := &errorSessionBackend{ready: make(chan struct{}), release: make(chan struct{})}
			switch scenario {
			case "launch":
				b.launchErr = errors.New("harness binary unavailable")
			case "nonzero-exit":
				b.exitCode = 1
			case "provider-error":
				b.output = "{\"type\":\"result\",\"is_error\":true,\"result\":\"API credentials rejected\"}\n"
			}
			h.agentSession = agentsession.New(agentsession.Config{Backend: b, ConfigDir: t.TempDir(), Fingerprint: "errors"})
			w := httptest.NewRecorder()
			h.SendAgentMessage(w, httptest.NewRequest(http.MethodPost, "/api/agent/messages", strings.NewReader(`{"message":"hello","harness":"claude"}`)))
			if w.Code != 202 {
				t.Fatalf("send: %d %s", w.Code, w.Body.String())
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			select {
			case <-b.ready:
			case <-ctx.Done():
				t.Fatal("agent did not launch")
			}
			reader := h.agentSession.LogReader("")
			if reader == nil {
				t.Fatal("live stream not initialized")
			}
			close(b.release)
			var raw []byte
			for {
				chunk, err := reader.ReadChunk(ctx)
				raw = append(raw, chunk...)
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if !agentsession.IsErrorResult(raw) {
				t.Fatal("failed agent produced no visible error frame")
			}
			messages, err := activeThreadStore(t, h.agentSession).Messages()
			if err != nil {
				t.Fatal(err)
			}
			if len(messages) != 2 || messages[1].Role != "assistant" || !agentsession.IsErrorResult([]byte(messages[1].RawOutput)) {
				t.Fatal("error disappears when conversation reloads")
			}
		})
	}
}
