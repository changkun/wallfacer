package handler

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"changkun.de/x/wallfacer/internal/constants"
	"changkun.de/x/wallfacer/internal/pkg/httpjson"
	"changkun.de/x/wallfacer/internal/pkg/livelog"
	"changkun.de/x/wallfacer/internal/planner"
	"changkun.de/x/wallfacer/internal/prompts"
	"changkun.de/x/wallfacer/internal/sandbox"
	"changkun.de/x/wallfacer/internal/spec"
	"changkun.de/x/wallfacer/internal/store"
)

// selectPlanningSystemPrompt returns the planning-agent prompt prefix
// appropriate for the current workspace state: the "empty" variant when
// no non-archived parseable specs exist across any mounted workspace,
// the "nonempty" variant otherwise. Evaluated per-turn (not cached)
// so archiving or unarchiving a spec takes effect on the very next
// message.
//
// Archived specs do not count toward the non-empty condition: a repo
// where every spec has been archived still reads as "empty" from the
// agent's perspective, keeping the `/spec-new` encouragement active.
// Unparseable spec files (broken frontmatter) are silently dropped by
// BuildTree and therefore also don't count, which matches the
// chat-first-mode spec's definition of an "effectively empty" tree.
func selectPlanningSystemPrompt(workspaces []string) string {
	for _, ws := range workspaces {
		tree, err := spec.BuildTree(filepath.Join(ws, "specs"))
		if err != nil {
			// BuildTree already returns (empty tree, nil) when specs/ is
			// absent (os.IsNotExist), so any error here is a genuine I/O
			// failure (permission denied, EIO, etc.). Defaulting to the
			// empty-variant prompt would falsely signal "no specs" and
			// invite the agent to scaffold against an unknown tree —
			// safer to assume non-empty and surface the failure in logs.
			slog.Warn("planning: spec tree read failed; defaulting to nonempty prompt",
				"workspace", ws, "err", err)
			return prompts.PlanningSystemNonempty()
		}
		for _, node := range tree.All {
			if node.Value == nil {
				continue
			}
			if node.Value.Status == spec.StatusArchived {
				continue
			}
			return prompts.PlanningSystemNonempty()
		}
	}
	return prompts.PlanningSystemEmpty()
}

// assemblePlanningPrompt layers the per-turn system prompts on top of
// the (already user-message-shaped) base. Final layout:
//
//	[planning_system][archivedSpecGuard][base]
//
// archivedSpecGuard sits closest to the base because its rail
// ("don't write to this archived spec") is most relevant the moment
// the model reads the user's request. The planning_system prompt wraps
// the whole turn from the outside. Empty layers are skipped, so an
// unfocused or non-archived spec produces just [planning_system][base].
func assemblePlanningPrompt(workspaces []string, focusedSpec, base string) string {
	out := base
	if guard := archivedSpecGuard(workspaces, focusedSpec); guard != "" {
		out = guard + out
	}
	if prefix := selectPlanningSystemPrompt(workspaces); prefix != "" {
		out = prefix + "\n\n" + out
	}
	return out
}

// archivedSpecGuard returns a system-prompt prefix to prepend when the focused
// spec is archived, instructing the chat agent to refuse writes. Returns the
// empty string when the spec is not archived or cannot be resolved.
func archivedSpecGuard(workspaces []string, focusedSpec string) string {
	if focusedSpec == "" {
		return ""
	}
	abs := findSpecFile(workspaces, focusedSpec)
	if abs == "" {
		return ""
	}
	s, err := spec.ParseFile(abs)
	if err != nil || s == nil {
		return ""
	}
	if s.Status != spec.StatusArchived {
		return ""
	}
	return "\u26A0 This spec is archived (read-only). Do NOT write to or modify " +
		"this spec. If the user requests changes, tell them to unarchive " +
		"the spec first using the Unarchive button in the focused view.\n\n"
}

// GetPlanningStatus reports whether the planning sandbox is running.
func (h *Handler) GetPlanningStatus(w http.ResponseWriter, _ *http.Request) {
	running := false
	if h.planner != nil {
		running = h.planner.IsRunning()
	}
	httpjson.Write(w, http.StatusOK, map[string]any{
		"running": running,
	})
}

// StartPlanning starts the planning sandbox container.
// If already running, returns 200 with running=true (idempotent).
func (h *Handler) StartPlanning(w http.ResponseWriter, r *http.Request) {
	if h.planner == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	if h.planner.IsRunning() {
		httpjson.Write(w, http.StatusOK, map[string]any{"running": true})
		return
	}
	if err := h.planner.Start(r.Context()); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusAccepted, map[string]any{"running": true})
}

// StopPlanning stops the planning sandbox container.
func (h *Handler) StopPlanning(w http.ResponseWriter, _ *http.Request) {
	if h.planner != nil {
		h.planner.Stop()
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"stopped": true})
}

// GetPlanningMessages returns the planning conversation history as a JSON array.
// Supports optional ?before=<RFC3339> for pagination. The `?thread=<id>`
// query parameter selects which thread's history is returned; when
// omitted, the currently active thread is used.
func (h *Handler) GetPlanningMessages(w http.ResponseWriter, r *http.Request) {
	cs := h.lookupThreadStore(h.threadIDFromRequest(r))
	if cs == nil {
		httpjson.Write(w, http.StatusOK, []any{})
		return
	}

	msgs, err := cs.Messages()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if msgs == nil {
		msgs = []planner.Message{}
	}

	// Optional pagination: filter messages before a given timestamp.
	if before := r.URL.Query().Get("before"); before != "" {
		t, parseErr := time.Parse(time.RFC3339Nano, before)
		if parseErr != nil {
			http.Error(w, "invalid before timestamp", http.StatusBadRequest)
			return
		}
		filtered := make([]planner.Message, 0, len(msgs))
		for _, m := range msgs {
			if m.Timestamp.Before(t) {
				filtered = append(filtered, m)
			}
		}
		msgs = filtered
	}

	httpjson.Write(w, http.StatusOK, msgs)
}

// SendPlanningMessage sends a user message to the planning agent.
// The agent exec runs in a background goroutine; returns 202 immediately.
// Returns 409 if an exec is already in flight. The `?thread=<id>` query
// parameter (or body field) selects which thread receives the message;
// when omitted, the active thread is used.
func (h *Handler) SendPlanningMessage(w http.ResponseWriter, r *http.Request) {
	if h.planner == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	if h.planner.IsBusy() {
		httpjson.Write(w, http.StatusConflict, map[string]any{
			"error": "agent is busy",
		})
		return
	}

	req, ok := httpjson.DecodeBody[struct {
		Message     string `json:"message"`
		FocusedSpec string `json:"focused_spec"`
		Thread      string `json:"thread"`
	}](w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(req.Message) == "" {
		http.Error(w, "message is required", http.StatusBadRequest)
		return
	}

	threadID := strings.TrimSpace(req.Thread)
	if threadID == "" {
		threadID = h.threadIDFromRequest(r)
	}
	cs := h.lookupThreadStore(threadID)
	if cs == nil {
		http.Error(w, "thread not found", http.StatusNotFound)
		return
	}

	// Append user message to conversation store.
	userMsg := planner.Message{
		Role:        "user",
		Content:     req.Message,
		Timestamp:   time.Now().UTC(),
		FocusedSpec: req.FocusedSpec,
	}
	if err := cs.AppendMessage(userMsg); err != nil {
		http.Error(w, "failed to persist message", http.StatusInternalServerError)
		return
	}
	if tm := h.threadsManager(); tm != nil {
		tm.Touch(threadID)
	}

	// Expand slash commands before building exec args.
	prompt := req.Message
	if h.commandRegistry != nil {
		if expanded, ok := h.commandRegistry.Expand(req.Message, req.FocusedSpec); ok {
			prompt = expanded
		}
	}
	// Slash commands can expand to a leading `/spec-new <path>` line
	// (see create-command-expansion). When they do, scaffold the file
	// server-side immediately so the agent sees an already-created spec
	// instead of having to echo the directive. Errors here are user-
	// fixable (empty title, collision-loop exhausted, invalid path),
	// so surface a 400 rather than spinning the agent.
	if strings.HasPrefix(strings.TrimSpace(prompt), "/spec-new") {
		workspaces := h.currentWorkspaces()
		scaffoldWs := ""
		if len(workspaces) > 0 {
			scaffoldWs = workspaces[0]
		}
		next, createdPath, serr := applySlashSpecNew(prompt, scaffoldWs, time.Now().UTC())
		if serr != nil {
			http.Error(w, "slash command: "+serr.Error(), http.StatusBadRequest)
			return
		}
		prompt = next
		if createdPath != "" {
			// Thread the just-scaffolded spec through to the agent: the
			// directive line has been stripped, so without this hint the
			// agent would not know what path to populate.
			prompt = "[Focused spec: " + createdPath + "]\n\n" + prompt
			if req.FocusedSpec == "" {
				req.FocusedSpec = createdPath
			}
		}
	}
	if req.FocusedSpec != "" && !strings.HasPrefix(req.Message, "/") {
		prompt = "[Focused spec: " + req.FocusedSpec + "]\n\n" + prompt
	}
	prompt = assemblePlanningPrompt(h.currentWorkspaces(), req.FocusedSpec, prompt)
	cmd := []string{"-p", prompt, "--verbose", "--output-format", "stream-json"}

	// Resume existing session if available.
	sess, _ := cs.LoadSession()
	if sess.SessionID != "" {
		cmd = append(cmd, "--resume", sess.SessionID)
	}

	// Auto-start the planner if not already running.
	if !h.planner.IsRunning() {
		if err := h.planner.Start(r.Context()); err != nil {
			http.Error(w, "failed to start planner: "+err.Error(), http.StatusInternalServerError)
			return
		}
	}

	h.planner.SetBusy(true, threadID)
	ll := h.planner.StartLiveLog()

	// Run exec in background goroutine. Use a detached context because the
	// HTTP request context is cancelled as soon as the 202 response is sent.
	go func() {
		defer func() {
			// SetBusy must be cleared before CloseLiveLog so that when the
			// frontend receives the stream EOF and immediately drains its
			// message queue, the backend is already ready to accept the next
			// request (otherwise the queued message races and gets a 409).
			h.planner.SetBusy(false, "")
			h.planner.CloseLiveLog()
			if rec := recover(); rec != nil {
				slog.Error("planning exec panic", "recover", rec)
			}
		}()

		handle, err := h.planner.Exec(context.Background(), cmd)
		if err != nil {
			slog.Error("planning exec failed", "error", err)
			return
		}

		// Tee stdout into the live log so SSE consumers can stream it.
		tee := io.TeeReader(handle.Stdout(), ll)
		rawStdout, _ := io.ReadAll(tee)
		_, _ = io.ReadAll(handle.Stderr())
		_, _ = handle.Wait()

		// Extract session ID and save for future --resume calls.
		sessionID := planner.ExtractSessionID(rawStdout)
		if sessionID != "" {
			_ = cs.SaveSession(planner.SessionInfo{
				SessionID:   sessionID,
				LastActive:  time.Now().UTC(),
				FocusedSpec: req.FocusedSpec,
			})
		}

		// If the error is a stale session, clear session (not history) and retry
		// with conversation history prepended to give the agent prior context.
		if planner.IsErrorResult(rawStdout) && planner.IsStaleSessionError(rawStdout) {
			slog.Warn("planning: stale session, retrying with history context")
			_ = cs.SaveSession(planner.SessionInfo{}) // clear session ID only
			historyCtx := cs.BuildHistoryContext()
			retryPrompt := prompt
			if historyCtx != "" {
				retryPrompt = historyCtx + retryPrompt
			}
			ll2 := h.planner.StartLiveLog()
			retryCmd := []string{"-p", retryPrompt, "--verbose", "--output-format", "stream-json"}
			retryHandle, retryErr := h.planner.Exec(context.Background(), retryCmd)
			if retryErr != nil {
				slog.Error("planning retry exec failed", "error", retryErr)
				return
			}
			retryTee := io.TeeReader(retryHandle.Stdout(), ll2)
			rawStdout, _ = io.ReadAll(retryTee)
			_, _ = io.ReadAll(retryHandle.Stderr())
			_, _ = retryHandle.Wait()
			h.planner.CloseLiveLog()

			sessionID = planner.ExtractSessionID(rawStdout)
			if sessionID != "" {
				_ = cs.SaveSession(planner.SessionInfo{
					SessionID:   sessionID,
					LastActive:  time.Now().UTC(),
					FocusedSpec: req.FocusedSpec,
				})
			}
		}

		// Persist round usage before building the assistant message so the
		// stats/usage dashboards reflect the round even if the commit
		// pipeline below produces a warning. Best-effort: errors are logged
		// and never fail the round.
		h.persistPlanningRoundUsage(rawStdout)

		// Parse response text and append assistant message (skip errors).
		if !planner.IsErrorResult(rawStdout) {
			resultText := planner.ExtractResultText(rawStdout)
			// Scan the agent's assistant-text blocks for /spec-new
			// directives. Scaffold each one into the first mounted
			// workspace so the file is present before commitPlanningRound
			// runs (and therefore included in the round's git commit).
			// Scaffold errors surface as `system`-role messages; the
			// agent's original text still flows through to the assistant
			// log untouched.
			dirScanner := &DirectiveScanner{}
			for _, line := range extractAssistantLines(rawStdout) {
				dirScanner.ScanLine(line)
			}
			directives := dirScanner.Directives()
			if len(directives) > 0 {
				workspaces := h.currentWorkspaces()
				var scaffoldWs string
				if len(workspaces) > 0 {
					scaffoldWs = workspaces[0]
				}
				now := time.Now().UTC()
				for _, sysMsg := range processDirectives(
					scaffoldWs, directives, req.FocusedSpec, now,
				) {
					_ = cs.AppendMessage(sysMsg)
				}
			}
			// Commit any spec writes from this round to git so the undo
			// stack has a distinct commit per round. Best-effort: log and
			// continue on failure, never block the conversation log. The
			// max round across workspaces attributes the assistant message
			// for UI undo affordances.
			commitCtx := context.Background()
			planRound := 0
			// h.runner may be nil in narrow test setups; a nil generator
			// makes commitPlanningRound fall back to its deterministic path.
			var genCommit commitMessageGenerator
			if h.runner != nil {
				genCommit = h.runner.GenerateCommitMessage
			}
			for _, ws := range h.currentWorkspaces() {
				n, cerr := commitPlanningRound(commitCtx, ws, req.Message, resultText, genCommit, threadID)
				if cerr != nil {
					slog.Warn("planning commit failed", "workspace", ws, "err", cerr)
					continue
				}
				if n > planRound {
					planRound = n
				}
				// Auto-push after a successful planning commit, mirroring the
				// behaviour of the task "mark as done" flow.
				if n > 0 && h.runner != nil {
					h.runner.MaybeAutoPushWorkspace(commitCtx, ws)
				}
			}
			if resultText != "" {
				_ = cs.AppendMessage(planner.Message{
					Role:        "assistant",
					Content:     resultText,
					Timestamp:   time.Now().UTC(),
					FocusedSpec: req.FocusedSpec,
					RawOutput:   string(rawStdout),
					PlanRound:   planRound,
				})
				// Touch the thread so the UI can sort by recent activity.
				if tm := h.threadsManager(); tm != nil {
					tm.Touch(threadID)
				}
			}
		}
	}()

	httpjson.Write(w, http.StatusAccepted, map[string]any{"status": "accepted"})
}

// StreamPlanningMessages streams the current planning exec's raw stdout.
// Uses the same plain-text streaming pattern as task log streaming
// (streamLiveLog) so the frontend can reuse renderPrettyLogs().
// Returns 204 No Content if no exec is in flight, or if the `?thread=<id>`
// query parameter does not match the thread that owns the exec.
func (h *Handler) StreamPlanningMessages(w http.ResponseWriter, r *http.Request) {
	if h.planner == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	// Resolve the requested thread. An explicit ?thread= must match the
	// in-flight thread exactly; otherwise the client is either polling
	// for its own thread (which isn't the one running) or looking at a
	// different workspace group.
	threadID := strings.TrimSpace(r.URL.Query().Get("thread"))

	// Poll briefly for the live log — there's a race between the client
	// connecting here and the exec goroutine creating the live log.
	var lr *livelog.Reader
	for range 20 { // up to ~2s
		lr = h.planner.LogReader(threadID)
		if lr != nil {
			break
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(100 * time.Millisecond):
		}
	}
	if lr == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := make(chan []byte, 4)
	go func() {
		defer close(ch)
		for {
			data, err := lr.ReadChunk(r.Context())
			if len(data) > 0 {
				ch <- data
			}
			if err != nil {
				return
			}
		}
	}()

	keepalive := time.NewTicker(constants.SSEKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case data, ok := <-ch:
			if !ok {
				return
			}
			if _, err := w.Write(data); err != nil {
				return
			}
			flusher.Flush()
		case <-keepalive.C:
			if _, err := w.Write([]byte("\n")); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// ClearPlanningMessages clears a thread's conversation history and
// session. The `?thread=<id>` query parameter selects which thread;
// when omitted the active thread is used.
func (h *Handler) ClearPlanningMessages(w http.ResponseWriter, r *http.Request) {
	cs := h.lookupThreadStore(h.threadIDFromRequest(r))
	if cs == nil {
		httpjson.Write(w, http.StatusOK, map[string]any{"status": "cleared"})
		return
	}
	if err := cs.Clear(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"status": "cleared"})
}

// InterruptPlanningMessage interrupts the current agent turn. When a
// `?thread=<id>` is supplied it must match the in-flight thread,
// otherwise the request is rejected (409). Returns 409 if no exec is
// in flight.
func (h *Handler) InterruptPlanningMessage(w http.ResponseWriter, r *http.Request) {
	if h.planner == nil {
		http.Error(w, "planning not configured", http.StatusServiceUnavailable)
		return
	}
	if threadID := strings.TrimSpace(r.URL.Query().Get("thread")); threadID != "" {
		owner := h.planner.BusyThreadID()
		if owner != "" && owner != threadID {
			httpjson.Write(w, http.StatusConflict, map[string]any{
				"error": "a different thread is currently running",
			})
			return
		}
	}
	if err := h.planner.Interrupt(); err != nil {
		httpjson.Write(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	httpjson.Write(w, http.StatusOK, map[string]any{"status": "interrupted"})
}

// persistPlanningRoundUsage parses token and cost usage from a round's
// raw stdout and appends it to the planning-usage log for the current
// workspace group. Failed rounds, missing usage, and missing workspace
// configuration short-circuit silently. Append errors are logged so a
// persistence failure never fails the user-facing round.
func (h *Handler) persistPlanningRoundUsage(raw []byte) {
	if planner.IsErrorResult(raw) {
		return
	}
	workspaces := h.currentWorkspaces()
	if len(workspaces) == 0 || h.configDir == "" {
		return
	}
	usage, ok := planner.ExtractUsage(raw)
	if !ok {
		return
	}
	groupKey := store.PlanningGroupKey(workspaces)
	existing, _ := store.ReadPlanningUsage(h.configDir, groupKey, time.Time{})
	rec := store.TurnUsageRecord{
		Turn:                 len(existing) + 1,
		Timestamp:            time.Now().UTC(),
		InputTokens:          usage.InputTokens,
		OutputTokens:         usage.OutputTokens,
		CacheReadInputTokens: usage.CacheReadInputTokens,
		CacheCreationTokens:  usage.CacheCreationInputTokens,
		CostUSD:              usage.CostUSD,
		StopReason:           usage.StopReason,
		Sandbox:              sandbox.Claude,
		SubAgent:             store.SandboxActivityPlanning,
	}
	if err := store.AppendPlanningUsage(h.configDir, groupKey, rec); err != nil {
		slog.Warn("planning: failed to append round usage", "error", err)
	}
}

// GetPlanningCommands returns the list of available slash commands.
func (h *Handler) GetPlanningCommands(w http.ResponseWriter, _ *http.Request) {
	if h.commandRegistry == nil {
		httpjson.Write(w, http.StatusOK, []any{})
		return
	}
	httpjson.Write(w, http.StatusOK, h.commandRegistry.Commands())
}
