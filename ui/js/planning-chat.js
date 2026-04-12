// planning-chat.js — Planning agent chat module.
// Handles sending messages, streaming responses, rendering conversation,
// and slash command autocomplete.

/* global Routes, api, renderMarkdown, renderPrettyLogs, startStreamingFetch, escapeHtml, specModeState, withAuthToken, withBearerHeaders, attachMentionAutocomplete, reloadSpecTree */

var PlanningChat = (function () {
  "use strict";

  var _streaming = false;
  var _activeStream = null; // handle from startStreamingFetch
  var _commandsCache = null;
  // Send mode: "enter" = Enter sends (Shift+Enter for newline),
  //            "cmd-enter" = Cmd/Ctrl+Enter sends (Enter for newline).
  var _sendMode = localStorage.getItem("wallfacer-chat-send-mode") || "enter";
  var _autocompleteEl = null;
  var _autocompleteIndex = -1;
  var _queue = []; // Array of {id, text}
  var _nextQueueId = 0;

  // DOM references (set in init).
  var _input = null;
  var _sendBtn = null;
  var _interruptBtn = null;
  var _messagesEl = null;
  var _streamEl = null;
  var _queueEl = null;

  function init() {
    _input = document.getElementById("spec-chat-input");
    _sendBtn = document.getElementById("spec-chat-send");
    _messagesEl = document.getElementById("spec-chat-messages");
    _streamEl = document.getElementById("spec-chat-stream");
    if (!_input || !_messagesEl) return;

    _input.addEventListener("keydown", _onInputKeydown);
    _input.addEventListener("input", _onInputChange);
    _input.addEventListener("input", _autoGrow);
    if (_sendBtn) {
      _sendBtn.addEventListener("click", function () {
        var text = _input.value.trim();
        if (text) sendMessage(text);
      });
    }

    // Attach @-mention file autocomplete.
    if (typeof attachMentionAutocomplete === "function") {
      attachMentionAutocomplete(_input, {
        position: "above",
        priorityPrefix: "specs/",
      });
    }

    // Wire clear button.
    var clearBtn = document.getElementById("spec-chat-clear");
    if (clearBtn) {
      clearBtn.addEventListener("click", clearHistory);
    }

    // Wire send-mode toggle and hint.
    var modeBtn = document.getElementById("spec-chat-send-mode");
    var hintEl = document.getElementById("spec-chat-send-hint");
    if (modeBtn) {
      _updateSendHint(hintEl);
      modeBtn.addEventListener("click", function () {
        _sendMode = _sendMode === "enter" ? "cmd-enter" : "enter";
        localStorage.setItem("wallfacer-chat-send-mode", _sendMode);
        _updateSendHint(hintEl);
      });
    }

    // Wire / and @ shortcut buttons.
    var slashBtn = document.getElementById("spec-chat-slash-hint");
    if (slashBtn) {
      slashBtn.addEventListener("click", function () {
        _input.value = "/";
        _input.focus();
        _onInputChange();
      });
    }
    var atBtn = document.getElementById("spec-chat-at-hint");
    if (atBtn) {
      atBtn.addEventListener("click", function () {
        _input.value += "@";
        _input.focus();
        _input.dispatchEvent(new Event("input"));
      });
    }

    // Create interrupt button (hidden by default), placed in the send group.
    _interruptBtn = document.createElement("button");
    _interruptBtn.className =
      "spec-chat-composer__send planning-chat-interrupt-btn";
    _interruptBtn.innerHTML = "&#x25A0;"; // stop square
    _interruptBtn.title = "Interrupt";
    _interruptBtn.style.display = "none";
    _interruptBtn.addEventListener("click", _onInterrupt);
    var sendGroup = _sendBtn ? _sendBtn.parentElement : null;
    if (sendGroup) {
      sendGroup.insertBefore(_interruptBtn, _sendBtn);
    }

    // Create queue container above the composer.
    _queueEl = document.createElement("div");
    _queueEl.className = "planning-chat-queue";
    var composer = document.querySelector(".spec-chat-composer");
    if (composer && composer.parentElement) {
      composer.parentElement.insertBefore(_queueEl, composer);
    }

    // Track scroll position to suppress auto-scroll when user reads history.
    // The scrollable element is _messagesEl, not the outer _streamEl container.
    if (_messagesEl) {
      _messagesEl.addEventListener("scroll", function () {
        _userScrolledUp =
          _messagesEl.scrollTop + _messagesEl.clientHeight <
          _messagesEl.scrollHeight - 40;
      });
    }

    _loadHistory();
    _fetchCommands(); // pre-fetch so autocomplete is instant
  }

  function _onInputKeydown(e) {
    // If the @-mention dropdown is open (managed by mention.js) and our own
    // slash dropdown is not open, let mention.js handle the keys.
    if (!_autocompleteEl && document.querySelector(".mention-dropdown")) return;

    // If user presses arrow key while typing a slash command but autocomplete
    // hasn't rendered yet, trigger it synchronously from cache.
    if (
      !_autocompleteEl &&
      _input.value.startsWith("/") &&
      (e.key === "ArrowDown" || e.key === "ArrowUp")
    ) {
      _showAutocompleteSync(_input.value);
    }

    if (_autocompleteEl) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        var len = _autocompleteEl.children.length;
        _autocompleteIndex = len > 0 ? (_autocompleteIndex + 1) % len : 0;
        _highlightAutocomplete();
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        var len2 = _autocompleteEl.children.length;
        _autocompleteIndex =
          len2 > 0 ? (_autocompleteIndex - 1 + len2) % len2 : 0;
        _highlightAutocomplete();
        return;
      }
      if (e.key === "Enter" || e.key === "Tab") {
        if (_autocompleteIndex >= 0) {
          e.preventDefault();
          _selectAutocomplete(_autocompleteIndex);
          return;
        }
      }
      if (e.key === "Escape") {
        _closeAutocomplete();
        return;
      }
    }

    if (e.key === "Enter") {
      var shouldSend = false;
      if (_sendMode === "cmd-enter") {
        // Cmd/Ctrl+Enter sends; plain Enter inserts newline.
        shouldSend = e.metaKey || e.ctrlKey;
      } else {
        // Enter sends; Shift+Enter inserts newline. Cmd/Ctrl+Enter also sends.
        shouldSend = !e.shiftKey || e.metaKey || e.ctrlKey;
      }
      if (shouldSend) {
        e.preventDefault();
        var text = _input.value.trim();
        if (text) sendMessage(text);
      }
    }
  }

  function _updateSendHint(hintEl) {
    if (!hintEl) return;
    var isMac =
      typeof navigator !== "undefined" && /Mac/.test(navigator.platform);
    var mod = isMac ? "\u2318" : "Ctrl";
    if (_sendMode === "cmd-enter") {
      hintEl.textContent = mod + "+Return to send";
    } else {
      hintEl.textContent = "Shift+Return for new line";
    }
  }

  function _autoGrow() {
    if (!_input) return;
    _input.style.height = "auto";
    _input.style.height = Math.min(_input.scrollHeight, 200) + "px";
  }

  function _onInputChange() {
    var val = _input.value;
    if (val.startsWith("/") && val.indexOf("\n") === -1) {
      _showAutocomplete(val);
    } else {
      _closeAutocomplete();
    }
  }

  async function _loadHistory() {
    try {
      var msgs = await api(Routes.planning.messages());
      if (msgs && msgs.length > 0) {
        _messagesEl.innerHTML = "";
        msgs.forEach(function (m) {
          if (m.role === "assistant" && m.raw_output) {
            _appendMessageBubbleWithActivity(
              m.content,
              m.raw_output,
              m.timestamp,
              m.plan_round || 0,
            );
          } else {
            _appendMessageBubble(
              m.role,
              m.content,
              m.timestamp,
              m.plan_round || 0,
            );
          }
        });
        _updateUndoButtonStates();
        _scrollToBottom();
      }
    } catch (_) {
      // Ignore — history may not be available yet.
    }
  }

  async function sendMessage(text) {
    if (_streaming) {
      _enqueue(text);
      return;
    }

    _input.value = "";
    _input.style.height = "auto";
    _closeAutocomplete();

    // Render user message immediately and force-scroll to show it.
    _appendMessageBubble("user", text, new Date().toISOString());
    _userScrolledUp = false;
    _scrollToBottom(true);

    // Get focused spec from spec mode state.
    var focusedSpec = "";
    if (typeof specModeState !== "undefined" && specModeState.focusedSpecPath) {
      focusedSpec = specModeState.focusedSpecPath;
    }

    try {
      var res = await fetch(Routes.planning.sendMessage(), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          message: text,
          focused_spec: focusedSpec,
        }),
      });

      if (res.status === 409) {
        _appendSystemMessage("Agent is busy — try again shortly.");
        _input.focus();
        return;
      }
      if (!res.ok) {
        var errText = await res.text();
        _appendSystemMessage("Error: " + errText);
        _input.focus();
        return;
      }

      // Start streaming the response.
      _startStreaming();
    } catch (err) {
      _appendSystemMessage("Error: " + err.message);
      _enableInput();
    }
  }

  function _startStreaming() {
    _streaming = true;
    if (_interruptBtn) _interruptBtn.style.display = "";
    if (_sendBtn) _sendBtn.style.display = "none";

    var bubble = _createBubble("assistant");
    _messagesEl.appendChild(bubble);
    var contentEl = bubble.querySelector(".planning-chat-bubble__content");
    // Show thinking indicator until first content arrives.
    if (contentEl) {
      contentEl.innerHTML =
        '<span class="planning-chat-thinking"><span class="planning-chat-thinking__dots"><span>.</span><span>.</span><span>.</span></span></span>';
    }
    _scrollToBottom();
    var rawBuffer = "";
    var assistantText = "";
    var receivedContent = false;

    var retried = false;

    function _connectStream() {
      _activeStream = startStreamingFetch({
        url: Routes.planning.messageStream(),
        onChunk: function (chunk) {
          rawBuffer += chunk;
          assistantText = _extractAssistantText(rawBuffer);
          if (contentEl) {
            if (
              !receivedContent &&
              (assistantText || _hasToolActivity(rawBuffer))
            ) {
              receivedContent = true;
            }
            if (receivedContent) {
              _renderChatResponse(contentEl, assistantText, rawBuffer, true);
            }
          }
          _scrollToBottom();
        },
        onDone: function (hadData) {
          if (!hadData && !retried) {
            // Stream returned empty (204 or exec not ready yet) — retry once.
            retried = true;
            setTimeout(_connectStream, 500);
            return;
          }
          assistantText = _extractAssistantText(rawBuffer);
          if (contentEl) {
            _renderChatResponse(contentEl, assistantText, rawBuffer, false);
          }
          _stopStreaming(false);
        },
        onError: function () {
          if (!retried) {
            retried = true;
            setTimeout(_connectStream, 500);
            return;
          }
          _stopStreaming(false);
        },
      });
    }
    _connectStream();
  }

  // _extractAssistantText pulls all assistant text from raw NDJSON output.
  function _extractAssistantText(raw) {
    var text = "";
    var lines = raw.split("\n");
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (!line || line[0] !== "{") continue;
      try {
        var obj = JSON.parse(line);
        if (obj.type === "assistant" && obj.message && obj.message.content) {
          for (var j = 0; j < obj.message.content.length; j++) {
            var block = obj.message.content[j];
            if (block.type === "text" && block.text) {
              text += block.text;
            }
          }
        }
      } catch (_) {}
    }
    return text;
  }

  // _hasToolActivity checks if the NDJSON contains tool calls or thinking.
  function _hasToolActivity(raw) {
    var lines = raw.split("\n");
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (!line || line[0] !== "{") continue;
      try {
        var obj = JSON.parse(line);
        if (obj.type === "assistant" && obj.message && obj.message.content) {
          for (var j = 0; j < obj.message.content.length; j++) {
            var t = obj.message.content[j].type;
            if (t === "tool_use" || t === "thinking") return true;
          }
        }
        if (obj.type === "user") return true; // tool results
      } catch (_) {}
    }
    return false;
  }

  // _extractError pulls the error message from NDJSON output, if any.
  function _extractError(raw) {
    var lines = raw.split("\n");
    for (var i = lines.length - 1; i >= 0; i--) {
      var line = lines[i].trim();
      if (!line || line[0] !== "{") continue;
      try {
        var obj = JSON.parse(line);
        if (obj.type === "result" && obj.is_error && obj.result) {
          return obj.result;
        }
      } catch (_) {}
    }
    return "";
  }

  // _renderChatResponse renders the assistant text as markdown with an
  // optional collapsible activity log showing tool calls. Errors are
  // shown as styled error blocks. When streaming is true the activity
  // section stays open so the user can follow along live; it collapses
  // once the response is complete.
  function _renderChatResponse(el, text, rawBuffer, streaming) {
    var html = "";
    var errorMsg = _extractError(rawBuffer);
    if (errorMsg) {
      html +=
        '<div class="planning-chat-error">' +
        _escapeForHtml(errorMsg) +
        "</div>";
    }
    if (text) {
      html += renderMarkdown(text);
    }
    // Show tool activity in a collapsible details element.
    if (_hasToolActivity(rawBuffer) && typeof renderPrettyLogs === "function") {
      var openAttr = streaming ? " open" : "";
      html +=
        '<details class="planning-chat-activity"' +
        openAttr +
        "><summary>Agent activity</summary>" +
        '<div class="planning-chat-activity__log">' +
        renderPrettyLogs(rawBuffer) +
        "</div></details>";
    }
    if (!html) {
      html = '<span class="planning-chat-empty">No response</span>';
    }
    el.innerHTML = html;

    // Auto-scroll the activity log to the bottom during streaming.
    if (streaming) {
      var logEl = el.querySelector(".planning-chat-activity__log");
      if (logEl) logEl.scrollTop = logEl.scrollHeight;
    }
  }

  function _escapeForHtml(s) {
    var el = document.createElement("span");
    el.textContent = s;
    return el.innerHTML;
  }

  function _stopStreaming(interrupted) {
    if (_activeStream) {
      _activeStream.abort();
      _activeStream = null;
    }
    _streaming = false;
    if (_interruptBtn) _interruptBtn.style.display = "none";
    if (_sendBtn) _sendBtn.style.display = "";

    if (interrupted) {
      // Mark the last assistant bubble as interrupted.
      var bubbles = _messagesEl.querySelectorAll
        ? _messagesEl.children.filter
          ? []
          : []
        : [];
      // Simple approach: append an interrupted indicator.
      var indicator = document.createElement("div");
      indicator.className = "planning-chat-interrupted";
      indicator.textContent = "interrupted";
      _messagesEl.appendChild(indicator);
    }

    _enableInput();
    _drainQueue();

    // Refetch history on a clean stop so the streaming bubble picks up
    // its server-attributed plan_round (for the per-message undo button).
    // Interrupted streams skip this — there's no committed round to
    // attribute.
    if (!interrupted) {
      _loadHistory();
    }
  }

  // no-op kept for backward compat with tests
  function _enableInput() {
    if (_input) _input.focus();
  }

  function _appendMessageBubble(role, content, timestamp, planRound) {
    var bubble = _createBubble(role);
    var contentEl = bubble.querySelector(".planning-chat-bubble__content");
    if (contentEl) {
      if (role === "assistant") {
        contentEl.innerHTML = renderMarkdown(content);
      } else {
        contentEl.textContent = content;
      }
    }
    _applyTimestamp(bubble, timestamp);
    _attachUndoIfRound(bubble, role, planRound);
    _messagesEl.appendChild(bubble);
  }

  // _appendMessageBubbleWithActivity renders an assistant bubble with
  // the collapsible agent activity section restored from raw NDJSON output.
  function _appendMessageBubbleWithActivity(content, rawOutput, timestamp, planRound) {
    var bubble = _createBubble("assistant");
    var contentEl = bubble.querySelector(".planning-chat-bubble__content");
    if (contentEl) {
      _renderChatResponse(contentEl, content, rawOutput, false);
    }
    _applyTimestamp(bubble, timestamp);
    _attachUndoIfRound(bubble, "assistant", planRound);
    _messagesEl.appendChild(bubble);
  }

  function _applyTimestamp(bubble, timestamp) {
    if (!timestamp) return;
    var timeEl = bubble.querySelector(".planning-chat-bubble__time");
    if (!timeEl) return;
    var d = new Date(timestamp);
    timeEl.textContent = d.toLocaleTimeString(undefined, {
      hour: "2-digit",
      minute: "2-digit",
    });
  }

  // _attachUndoIfRound decorates an assistant bubble with its round number
  // and a tiny undo button when planRound > 0. Only assistant bubbles that
  // wrote to specs/ get the affordance; user bubbles and no-op rounds stay
  // plain. Whether the button is enabled or disabled is decided later by
  // _updateUndoButtonStates() — only the latest-round bubble's button is
  // active at any moment.
  function _attachUndoIfRound(bubble, role, planRound) {
    if (role !== "assistant" || !planRound) return;
    bubble.setAttribute("data-round", String(planRound));
    var actions = document.createElement("div");
    actions.className = "planning-chat-bubble__actions";
    var btn = document.createElement("button");
    btn.type = "button";
    btn.className = "planning-chat-bubble__undo";
    btn.innerHTML = "&#x21BA;"; // ↺
    btn.setAttribute("aria-label", "Undo round " + planRound);
    btn.title = "Undo round " + planRound;
    btn.disabled = true; // promoted to enabled by _updateUndoButtonStates
    btn.addEventListener("click", function () {
      if (btn.disabled) return;
      _onUndo(bubble, planRound, btn);
    });
    actions.appendChild(btn);
    // Insert actions before the time stamp so the icon sits at the top
    // of the bubble on hover, matching the CSS layout.
    var timeEl = bubble.querySelector(".planning-chat-bubble__time");
    if (timeEl) {
      bubble.insertBefore(actions, timeEl);
    } else {
      bubble.appendChild(actions);
    }
  }

  // _updateUndoButtonStates enables the undo button on the latest-round
  // assistant bubble and disables all older ones. Called after any change
  // that adds, removes, or reverts a round-bearing bubble.
  function _updateUndoButtonStates() {
    if (!_messagesEl) return;
    var bubbles = _messagesEl.querySelectorAll(
      ".planning-chat-bubble--assistant[data-round]",
    );
    var latestRound = -1;
    for (var i = 0; i < bubbles.length; i++) {
      var n = parseInt(bubbles[i].getAttribute("data-round"), 10);
      if (!isNaN(n) && n > latestRound) latestRound = n;
    }
    bubbles.forEach(function (b) {
      var btn = b.querySelector(".planning-chat-bubble__undo");
      if (!btn) return;
      var n = parseInt(b.getAttribute("data-round"), 10);
      if (n === latestRound) {
        btn.disabled = false;
        btn.title = "Undo round " + n;
      } else {
        btn.disabled = true;
        btn.title = "Only the most recent round can be undone";
      }
    });
  }

  async function _onUndo(bubble, round, btn) {
    btn.disabled = true;
    var originalTitle = btn.title;
    btn.title = "Undoing…";
    try {
      var res = await fetch(Routes.planning.undo(), {
        method: "POST",
        headers: withBearerHeaders({ "Content-Type": "application/json" }),
      });
      var body = null;
      try {
        body = await res.json();
      } catch (_) {
        body = {};
      }
      if (!res.ok) {
        _appendUndoWarning(res.status, body);
        btn.title = originalTitle;
        _updateUndoButtonStates();
        return;
      }
      // Mark the originating bubble as reverted and strip its undo button.
      bubble.classList.add("planning-chat-bubble--reverted");
      bubble.removeAttribute("data-round");
      var actions = bubble.querySelector(".planning-chat-bubble__actions");
      if (actions) actions.remove();
      // Append the system announcement.
      _appendUndoSystemBubble(body);
      _updateUndoButtonStates();
      // Best-effort spec tree refresh.
      if (typeof reloadSpecTree === "function") {
        try {
          reloadSpecTree();
        } catch (_) {}
      }
    } catch (err) {
      _appendSystemMessage(
        "Undo failed: " + (err && err.message ? err.message : "network error"),
      );
      btn.title = originalTitle;
      _updateUndoButtonStates();
    }
  }

  function _appendUndoSystemBubble(body) {
    var round = body && body.round ? body.round : "?";
    var summary = body && body.summary ? body.summary : "";
    var files = body && body.files_reverted ? body.files_reverted : [];
    var el = document.createElement("div");
    el.className =
      "planning-chat-system planning-chat-system--undo";
    var text = "↺ Undid round " + round;
    if (summary) text += " — " + summary;
    el.textContent = text;
    if (files.length > 0) {
      var list = document.createElement("ul");
      list.className = "planning-chat-system__files";
      files.forEach(function (f) {
        var li = document.createElement("li");
        li.textContent = f;
        list.appendChild(li);
      });
      el.appendChild(list);
    }
    _messagesEl.appendChild(el);
    _scrollToBottom();
  }

  function _appendUndoWarning(status, body) {
    var err = (body && body.error) || "";
    var msg;
    if (status === 409 && err.indexOf("not at HEAD") !== -1) {
      msg =
        "⚠ Can't undo: you have unrelated commits since the last planning round. Resolve manually before using undo.";
    } else if (status === 409 && err.indexOf("stash pop conflict") !== -1) {
      msg =
        "⚠ Undo partially applied: git reset succeeded but your working-tree edits couldn't be reapplied cleanly. Your changes are preserved in the stash — run `git stash list` to recover.";
    } else if (status === 409) {
      msg = "⚠ Nothing to undo right now.";
    } else {
      msg = "Undo failed (HTTP " + status + ")" + (err ? ": " + err : "");
    }
    _appendSystemMessage(msg);
  }

  function _createBubble(role) {
    var bubble = document.createElement("div");
    bubble.className = "planning-chat-bubble planning-chat-bubble--" + role;
    var contentClass = "planning-chat-bubble__content";
    if (role === "assistant") contentClass += " prose-content";
    bubble.innerHTML =
      '<div class="' +
      contentClass +
      '"></div>' +
      '<div class="planning-chat-bubble__time"></div>';
    return bubble;
  }

  function _appendSystemMessage(text) {
    var el = document.createElement("div");
    el.className = "planning-chat-system";
    el.textContent = text;
    _messagesEl.appendChild(el);
    _scrollToBottom();
  }

  // _userScrolledUp is true when the user has manually scrolled away from
  // the bottom. Auto-scroll is suppressed until they scroll back down.
  var _userScrolledUp = false;

  function _scrollToBottom(force) {
    if (!_messagesEl) return;
    if (force || !_userScrolledUp) {
      _messagesEl.scrollTop = _messagesEl.scrollHeight;
    }
  }

  // --- Slash command autocomplete ---

  async function _fetchCommands() {
    if (_commandsCache) return _commandsCache;
    try {
      _commandsCache = await api(Routes.planning.commands());
      return _commandsCache;
    } catch (_) {
      return [];
    }
  }

  // Synchronous version for keydown handler — uses cached commands only.
  function _showAutocompleteSync(text) {
    if (!_commandsCache || _commandsCache.length === 0) return;
    _renderAutocomplete(text, _commandsCache);
  }

  async function _showAutocomplete(text) {
    var commands = await _fetchCommands();
    _renderAutocomplete(text, commands);
  }

  function _renderAutocomplete(text, commands) {
    if (!commands || commands.length === 0) {
      _closeAutocomplete();
      return;
    }

    var query = text.slice(1).toLowerCase(); // remove leading /
    var matches = commands.filter(function (c) {
      return c.name.toLowerCase().startsWith(query);
    });

    if (matches.length === 0) {
      _closeAutocomplete();
      return;
    }

    if (!_autocompleteEl) {
      _autocompleteEl = document.createElement("div");
      _autocompleteEl.className = "mention-dropdown";
      // Position above the composer input.
      var rect = _input.getBoundingClientRect();
      _autocompleteEl.style.position = "fixed";
      _autocompleteEl.style.left = rect.left + "px";
      _autocompleteEl.style.width = Math.max(320, rect.width) + "px";
      _autocompleteEl.style.bottom = window.innerHeight - rect.top + 4 + "px";
      _autocompleteEl.style.top = "auto";
      document.body.appendChild(_autocompleteEl);
    }

    _autocompleteEl.innerHTML = "";
    _autocompleteIndex = 0;

    matches.forEach(function (cmd, i) {
      var item = document.createElement("div");
      item.className = "mention-item";
      var nameEl = document.createElement("span");
      nameEl.className = "mention-filename";
      nameEl.textContent = "/" + cmd.name;
      var descEl = document.createElement("span");
      descEl.className = "mention-path";
      descEl.textContent = cmd.description;
      item.appendChild(nameEl);
      item.appendChild(descEl);
      item.addEventListener("mousedown", function (e) {
        e.preventDefault();
        _selectAutocomplete(i);
      });
      _autocompleteEl.appendChild(item);
    });

    _highlightAutocomplete();
  }

  function _highlightAutocomplete() {
    if (!_autocompleteEl) return;
    // Clamp index to valid range when results shrink.
    if (_autocompleteIndex >= _autocompleteEl.children.length) {
      _autocompleteIndex = Math.max(0, _autocompleteEl.children.length - 1);
    }
    var items = _autocompleteEl.children;
    for (var i = 0; i < items.length; i++) {
      items[i].classList.toggle(
        "mention-item-selected",
        i === _autocompleteIndex,
      );
    }
    // Scroll active item into view.
    if (
      _autocompleteIndex >= 0 &&
      items[_autocompleteIndex] &&
      typeof items[_autocompleteIndex].scrollIntoView === "function"
    ) {
      items[_autocompleteIndex].scrollIntoView({ block: "nearest" });
    }
  }

  function _selectAutocomplete(index) {
    if (!_autocompleteEl) return;
    var items = _autocompleteEl.children;
    if (index < 0 || index >= items.length) return;
    var nameEl = items[index].querySelector(".mention-filename");
    var name = nameEl ? nameEl.textContent : "";
    _input.value = name + " ";
    _input.focus();
    _closeAutocomplete();
  }

  function _closeAutocomplete() {
    if (_autocompleteEl) {
      _autocompleteEl.remove();
      _autocompleteEl = null;
    }
    _autocompleteIndex = -1;
  }

  function _escapeHtml(s) {
    var el = document.createElement("span");
    el.textContent = s;
    return el.innerHTML;
  }

  // --- Message queue ---

  function _enqueue(text) {
    var id = _nextQueueId++;
    _queue.push({ id: id, text: text });
    _renderQueue();
  }

  function _removeFromQueue(id) {
    _queue = _queue.filter(function (item) {
      return item.id !== id;
    });
    _renderQueue();
  }

  function _editQueueItem(id) {
    var item = _queue.find(function (q) {
      return q.id === id;
    });
    if (!item || !_queueEl) return;

    // Find the chip element and replace with an input.
    var chips = _queueEl.children;
    for (var i = 0; i < chips.length; i++) {
      if (parseInt(chips[i].dataset.queueId, 10) === id) {
        var editInput = document.createElement("input");
        editInput.className = "planning-chat-queue__edit";
        editInput.value = item.text;
        editInput.addEventListener("keydown", function (e) {
          if (e.key === "Enter") {
            item.text = editInput.value.trim() || item.text;
            _renderQueue();
          }
          if (e.key === "Escape") {
            _renderQueue();
          }
        });
        editInput.addEventListener("blur", function () {
          item.text = editInput.value.trim() || item.text;
          _renderQueue();
        });
        chips[i].innerHTML = "";
        chips[i].appendChild(editInput);
        editInput.focus();
        break;
      }
    }
  }

  function _renderQueue() {
    if (!_queueEl) return;
    _queueEl.innerHTML = "";
    _queue.forEach(function (item) {
      var chip = document.createElement("div");
      chip.className = "planning-chat-queue__chip";
      chip.dataset.queueId = item.id;

      var textSpan = document.createElement("span");
      textSpan.className = "planning-chat-queue__text";
      textSpan.textContent = item.text;
      textSpan.addEventListener("click", function () {
        _editQueueItem(item.id);
      });
      chip.appendChild(textSpan);

      var removeBtn = document.createElement("button");
      removeBtn.className = "planning-chat-queue__remove";
      removeBtn.textContent = "\u00d7"; // ×
      removeBtn.addEventListener("click", function () {
        _removeFromQueue(item.id);
      });
      chip.appendChild(removeBtn);

      _queueEl.appendChild(chip);
    });
  }

  function _drainQueue() {
    if (_queue.length > 0 && !_streaming) {
      var next = _queue.shift();
      _renderQueue();
      sendMessage(next.text);
    }
  }

  // --- Interrupt ---

  async function _onInterrupt() {
    try {
      await fetch(Routes.planning.interruptMessage(), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
      });
    } catch (_) {
      // Ignore errors — the stream will end regardless.
    }
    _stopStreaming(true);
  }

  async function clearHistory() {
    try {
      await fetch(Routes.planning.clearMessages(), {
        method: "DELETE",
        headers: { "Content-Type": "application/json" },
      });
    } catch (_) {}
    if (_messagesEl) _messagesEl.innerHTML = "";
  }

  function isStreaming() {
    return _streaming;
  }

  function getQueue() {
    return _queue.slice();
  }

  return {
    init: init,
    sendMessage: sendMessage,
    clearHistory: clearHistory,
    isStreaming: isStreaming,
    getQueue: getQueue,
  };
})();
