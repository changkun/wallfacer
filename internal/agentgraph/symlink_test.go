package agentgraph_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/agentgraph"
)

func TestRunAgentRejectsFileToolSymlinkEscape(t *testing.T) {
	worktree, outside := t.TempDir(), t.TempDir()
	const privateData = "outside-worktree-private-data"
	if err := os.WriteFile(filepath.Join(outside, "note.txt"), []byte(privateData), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(worktree, "escape")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	var calls atomic.Int32
	resultRequest := make(chan string, 1)
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		frame := func(kind, data string) { _, _ = io.WriteString(w, "event: "+kind+"\ndata: "+data+"\n\n") }
		frame("message_start", `{"type":"message_start","id":"msg","model":"test"}`)
		if calls.Add(1) == 1 {
			frame("block_start", `{"type":"block_start","index":0,"block":{"type":"tool_use","tool_use":{"id":"read-1","name":"read_file"}}}`)
			frame("args_delta", `{"type":"args_delta","index":0,"delta":"{\"path\":\"escape/note.txt\"}"}`)
			frame("block_stop", `{"type":"block_stop","index":0}`)
			frame("message_delta", `{"type":"message_delta","stop_reason":"tool_use"}`)
		} else {
			resultRequest <- string(body)
			frame("message_delta", `{"type":"message_delta","stop_reason":"end_turn"}`)
		}
		frame("message_stop", `{"type":"message_stop"}`)
	}))
	defer model.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	_, err := agentgraph.RunAgent(ctx, "symlink-run", agentgraph.ModelConfig{
		Mode: agentgraph.ModelModeLux, BaseURL: model.URL, APIKey: "test-key", Model: "test",
	}, "reader", "", "read the file", worktree, nil)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case request := <-resultRequest:
		if strings.Contains(request, privateData) {
			t.Error("file tool sent outside-worktree data to the model")
		}
		if !strings.Contains(request, "path escapes sandbox directory") {
			t.Error("tool result did not report a path refusal")
		}
	default:
		t.Fatal("model never received the file tool result")
	}
}
