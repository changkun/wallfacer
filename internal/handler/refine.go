package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"changkun.de/wallfacer/internal/envconfig"
	"changkun.de/wallfacer/internal/logger"
	"changkun.de/wallfacer/internal/store"
	"github.com/google/uuid"
)

const refineSystemPrompt = `You are a deliberate task refinement assistant. Your role is to help users clarify and improve their task descriptions through thoughtful, focused conversation.

Given a task description, you will:
1. Analyze it for ambiguities, unstated assumptions, and missing details
2. Ask ONE clear, specific question at a time — never multiple questions in one message
3. After each user answer, integrate it and ask the next most important open question
4. Build progressively toward a complete, unambiguous specification

Ask about things like:
- Expected inputs, outputs, and success criteria
- Edge cases and error handling expectations
- Technology preferences or constraints
- Integration points with existing systems
- Testing and verification requirements
- Scope boundaries (what is explicitly out of scope)

When you have gathered enough information to write a truly comprehensive and unambiguous task description, output a refined version. Format it exactly as:

REFINED PROMPT:
<the complete improved task description here>

Do not output the refined prompt until you have asked enough questions to substantially improve the original. Be deliberate and thorough.`

// refineMessage is the wire format for a single chat turn shared between
// the frontend and the Claude Messages API.
type refineMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// llmRequest is the payload sent to the Messages API.
type llmRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	System    string          `json:"system"`
	Messages  []refineMessage `json:"messages"`
}

// llmResponse is the relevant subset of the Messages API response.
type llmResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Usage *struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// llmUsage holds token counts returned from a Messages API call.
type llmUsage struct {
	InputTokens              int
	OutputTokens             int
	CacheReadInputTokens     int
	CacheCreationInputTokens int
}

// buildMessagesURL returns the full endpoint URL for the Messages API,
// choosing the appropriate host and path based on which credential is configured.
//
// OAuth tokens must target api.claude.ai (/api/messages), whereas API keys and
// gateway auth tokens use api.anthropic.com (/v1/messages). An explicit
// ANTHROPIC_BASE_URL always takes precedence and uses /v1/messages.
func buildMessagesURL(baseURL, apiKey, authToken, oauthToken string) string {
	path := "/v1/messages"
	if baseURL == "" {
		if oauthToken != "" && apiKey == "" && authToken == "" {
			// OAuth tokens authenticate against api.claude.ai,
			// not api.anthropic.com. The former uses the /api/messages path.
			baseURL = "https://api.claude.ai"
			path = "/api/messages"
		} else {
			baseURL = "https://api.anthropic.com"
		}
	}
	return strings.TrimRight(baseURL, "/") + path
}

// callLLM sends a conversation to the Messages API and returns the assistant
// reply along with token usage. It reads credentials from the env file.
func (h *Handler) callLLM(messages []refineMessage) (string, llmUsage, error) {
	cfg, err := envconfig.Parse(h.envFile)
	if err != nil {
		return "", llmUsage{}, fmt.Errorf("read env config: %w", err)
	}

	apiKey := cfg.APIKey
	authToken := cfg.AuthToken
	oauthToken := cfg.OAuthToken
	if apiKey == "" && authToken == "" && oauthToken == "" {
		return "", llmUsage{}, fmt.Errorf("no API key configured; set ANTHROPIC_API_KEY in the env file")
	}

	model := cfg.DefaultModel
	if model == "" {
		model = "claude-haiku-4-5"
	}

	payload := llmRequest{
		Model:     model,
		MaxTokens: 1024,
		System:    refineSystemPrompt,
		Messages:  messages,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return "", llmUsage{}, fmt.Errorf("marshal request: %w", err)
	}

	messagesURL := buildMessagesURL(cfg.BaseURL, apiKey, authToken, oauthToken)
	req, err := http.NewRequest("POST", messagesURL, bytes.NewReader(body))
	if err != nil {
		return "", llmUsage{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-version", "2023-06-01")

	switch {
	case apiKey != "":
		req.Header.Set("x-api-key", apiKey)
	case authToken != "":
		req.Header.Set("Authorization", "Bearer "+authToken)
	case oauthToken != "":
		req.Header.Set("Authorization", "Bearer "+oauthToken)
	}

	client := &http.Client{Timeout: 60 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", llmUsage{}, fmt.Errorf("call LLM API: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", llmUsage{}, fmt.Errorf("read response: %w", err)
	}

	var cr llmResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return "", llmUsage{}, fmt.Errorf("parse response: %w", err)
	}
	if cr.Error != nil {
		return "", llmUsage{}, fmt.Errorf("LLM API error (%s): %s", cr.Error.Type, cr.Error.Message)
	}
	if len(cr.Content) == 0 {
		return "", llmUsage{}, fmt.Errorf("empty response from LLM")
	}

	var usage llmUsage
	if cr.Usage != nil {
		usage = llmUsage{
			InputTokens:              cr.Usage.InputTokens,
			OutputTokens:             cr.Usage.OutputTokens,
			CacheReadInputTokens:     cr.Usage.CacheReadInputTokens,
			CacheCreationInputTokens: cr.Usage.CacheCreationInputTokens,
		}
	}

	var parts []string
	for _, c := range cr.Content {
		if c.Type == "text" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n"), usage, nil
}

// RefineChatRequest is the body for POST /api/tasks/{id}/refine.
type RefineChatRequest struct {
	// Message is the latest user message (empty on the first call to get the opening question).
	Message      string          `json:"message"`
	Conversation []refineMessage `json:"conversation"`
}

// RefineChatResponse is returned by POST /api/tasks/{id}/refine.
type RefineChatResponse struct {
	Message        string `json:"message"`
	RefinedPrompt  string `json:"refined_prompt,omitempty"` // non-empty when Claude has proposed a refinement
}

// RefineChat handles a single chat turn in a refinement session.
// POST /api/tasks/{id}/refine
// Body: { message: string, conversation: [{role, content}] }
func (h *Handler) RefineChat(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "backlog" {
		http.Error(w, "task is not in backlog", http.StatusBadRequest)
		return
	}

	var req RefineChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}

	// Build conversation history to send to Claude.
	// On the first call, prime with the task prompt as the first user message.
	messages := req.Conversation
	if len(messages) == 0 {
		messages = []refineMessage{
			{
				Role:    "user",
				Content: "I have a task I'd like to refine. Here is the current description:\n\n" + task.Prompt,
			},
		}
	} else if req.Message != "" {
		messages = append(messages, refineMessage{Role: "user", Content: req.Message})
	}

	reply, usage, err := h.callLLM(messages)
	if err != nil {
		logger.Handler.Error("refine chat: call LLM", "task", id, "error", err)
		http.Error(w, "failed to get AI response: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Accumulate token usage for refinement (Messages API does not expose cost_usd).
	if usage.InputTokens > 0 || usage.OutputTokens > 0 {
		if accErr := h.store.AccumulateSubAgentUsage(r.Context(), id, "refinement", store.TaskUsage{
			InputTokens:          usage.InputTokens,
			OutputTokens:         usage.OutputTokens,
			CacheReadInputTokens: usage.CacheReadInputTokens,
			CacheCreationTokens:  usage.CacheCreationInputTokens,
		}); accErr != nil {
			logger.Handler.Warn("refine chat: accumulate usage failed", "task", id, "error", accErr)
		}
	}

	// Check if Claude has proposed a refined prompt.
	var refinedPrompt string
	const marker = "REFINED PROMPT:"
	if idx := strings.Index(reply, marker); idx >= 0 {
		refinedPrompt = strings.TrimSpace(reply[idx+len(marker):])
		// Trim the marker section from the displayed message.
		reply = strings.TrimSpace(reply[:idx])
		if reply == "" {
			reply = "I've drafted a refined version of the task prompt. You can review and edit it, then click Apply to save it."
		}
	}

	writeJSON(w, http.StatusOK, RefineChatResponse{
		Message:       reply,
		RefinedPrompt: refinedPrompt,
	})
}

// RefineApplyRequest is the body for POST /api/tasks/{id}/refine/apply.
type RefineApplyRequest struct {
	Prompt       string          `json:"prompt"`
	Conversation []refineMessage `json:"conversation"`
}

// RefineApply persists the refined prompt, recording the full conversation in
// RefineSessions and moving the old prompt to PromptHistory.
// POST /api/tasks/{id}/refine/apply
func (h *Handler) RefineApply(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, "task not found", http.StatusNotFound)
		return
	}
	if task.Status != "backlog" {
		http.Error(w, "task is not in backlog", http.StatusBadRequest)
		return
	}

	var req RefineApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		http.Error(w, "prompt is required", http.StatusBadRequest)
		return
	}

	// Convert wire messages to store model.
	now := time.Now()
	storeMessages := make([]store.RefinementMessage, 0, len(req.Conversation))
	for _, m := range req.Conversation {
		storeMessages = append(storeMessages, store.RefinementMessage{
			Role:      m.Role,
			Content:   m.Content,
			CreatedAt: now,
		})
	}

	session := store.RefinementSession{
		ID:          uuid.New().String(),
		CreatedAt:   now,
		StartPrompt: task.Prompt,
		Messages:    storeMessages,
	}

	if err := h.store.ApplyRefinement(r.Context(), id, req.Prompt, session); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Regenerate title for the updated prompt.
	go h.runner.GenerateTitle(id, req.Prompt)

	updated, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, updated)
}
