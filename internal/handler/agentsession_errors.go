package handler

import (
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"latere.ai/x/wallfacer/internal/agentsession"
)

// writeAgentFailure emits the same terminal error shape the chat parser uses
// for provider errors. The leading newline separates any partial agent frame.
func writeAgentFailure(log io.Writer, code, message, detail string) []byte {
	frame, _ := json.Marshal(map[string]any{
		"type": "result", "subtype": "error", "is_error": true,
		"code": code, "message": message, "detail": detail, "result": message,
	})
	frame = append(append([]byte{'\n'}, frame...), '\n')
	_, _ = log.Write(frame)
	return frame
}

// persistAgentFailure keeps errors visible after fast failures close the live
// log before the browser attaches, and after the conversation is reopened.
func persistAgentFailure(cs *agentsession.ConversationStore, raw []byte, focusedSpec, focusedTask string) {
	if err := cs.AppendMessage(agentsession.Message{
		Role: "assistant", Content: agentsession.ExtractResultText(raw),
		RawOutput: string(raw), Timestamp: time.Now().UTC(),
		FocusedSpec: focusedSpec, FocusedTask: focusedTask,
	}); err != nil {
		slog.Error("persist agent failure", "error", err)
	}
}
