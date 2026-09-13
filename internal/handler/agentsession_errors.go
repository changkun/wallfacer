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
	frame, _ := json.Marshal(struct {
		Type    string `json:"type"`
		Subtype string `json:"subtype"`
		IsError bool   `json:"is_error"`
		Code    string `json:"code"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
		Result  string `json:"result"`
	}{
		Type: "result", Subtype: "error", IsError: true,
		Code: code, Message: message, Detail: detail, Result: message,
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
