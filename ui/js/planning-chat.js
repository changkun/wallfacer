// planning-chat.js — Planning agent chat module.
// Handles sending messages, streaming responses, rendering conversation,
// and slash command autocomplete.

/* global Routes, api, renderMarkdown, renderPrettyLogs, startStreamingFetch, escapeHtml, specModeState, withAuthToken, withBearerHeaders, attachMentionAutocomplete */

var PlanningChat = (function () {
  "use strict";

  var _streaming = false;
  var _activeStream = null; // handle from startStreamingFetch
  var _commandsCache = null;
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

    // Attach @-mention file autocomplete (reuses the task board's mention module).
    if (typeof attachMentionAutocomplete === "function") {
      attachMentionAutocomplete(_input);
    }

    // Wire clear button.
    var clearBtn = document.getElementById("spec-chat-clear");
    if (clearBtn) {
      clearBtn.addEventListener("click", clearHistory);
    }

    // Create interrupt button (hidden by default).
    _interruptBtn = document.createElement("button");
    _interruptBtn.className = "planning-chat-interrupt-btn";
    _interruptBtn.textContent = "Interrupt";
    _interruptBtn.style.display = "none";
    _interruptBtn.addEventListener("click", _onInterrupt);
    if (_sendBtn && _sendBtn.parentElement) {
      _sendBtn.parentElement.insertBefore(_interruptBtn, _sendBtn.nextSibling);
    }

    // Create queue container (below the input area).
    _queueEl = document.createElement("div");
    _queueEl.className = "planning-chat-queue";
    if (_input.parentElement) {
      _input.parentElement.appendChild(_queueEl);
    }

    _loadHistory();
  }

  function _onInputKeydown(e) {
    if (_autocompleteEl) {
      if (e.key === "ArrowDown") {
        e.preventDefault();
        _autocompleteIndex = Math.min(
          _autocompleteIndex + 1,
          _autocompleteEl.children.length - 1,
        );
        _highlightAutocomplete();
        return;
      }
      if (e.key === "ArrowUp") {
        e.preventDefault();
        _autocompleteIndex = Math.max(_autocompleteIndex - 1, 0);
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

    // Enter or Cmd+Enter sends; Shift+Enter inserts newline.
    if (e.key === "Enter" && (e.metaKey || e.ctrlKey || !e.shiftKey)) {
      e.preventDefault();
      var text = _input.value.trim();
      if (text) sendMessage(text);
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
          _appendMessageBubble(m.role, m.content, m.timestamp);
        });
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

    // Render user message immediately.
    _appendMessageBubble("user", text, new Date().toISOString());
    _scrollToBottom();

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

    var bubble = _createBubble("assistant");
    _messagesEl.appendChild(bubble);
    var contentEl = bubble.querySelector(".planning-chat-bubble__content");
    // Show thinking indicator until first content arrives.
    if (contentEl) {
      contentEl.innerHTML = '<span class="planning-chat-thinking"><span class="planning-chat-thinking__dots"><span>.</span><span>.</span><span>.</span></span></span>';
    }
    _scrollToBottom();
    var rawBuffer = "";
    var assistantText = "";
    var receivedContent = false;

    _activeStream = startStreamingFetch({
      url: Routes.planning.messageStream(),
      onChunk: function (chunk) {
        rawBuffer += chunk;
        assistantText = _extractAssistantText(rawBuffer);
        if (contentEl) {
          // Replace thinking indicator on first real content.
          if (!receivedContent && (assistantText || _hasToolActivity(rawBuffer))) {
            receivedContent = true;
          }
          if (receivedContent) {
            _renderChatResponse(contentEl, assistantText, rawBuffer);
          }
        }
        _scrollToBottom();
      },
      onDone: function () {
        // Final render with complete buffer.
        assistantText = _extractAssistantText(rawBuffer);
        if (contentEl) {
          _renderChatResponse(contentEl, assistantText, rawBuffer);
        }
        _stopStreaming(false);
      },
      onError: function () {
        _stopStreaming(false);
      },
    });
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

  // _hasToolActivity checks if the NDJSON contains tool calls.
  function _hasToolActivity(raw) {
    var lines = raw.split("\n");
    for (var i = 0; i < lines.length; i++) {
      var line = lines[i].trim();
      if (!line || line[0] !== "{") continue;
      try {
        var obj = JSON.parse(line);
        if (obj.type === "assistant" && obj.message && obj.message.content) {
          for (var j = 0; j < obj.message.content.length; j++) {
            if (obj.message.content[j].type === "tool_use") return true;
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
  // shown as styled error blocks.
  function _renderChatResponse(el, text, rawBuffer) {
    var html = "";
    var errorMsg = _extractError(rawBuffer);
    if (errorMsg) {
      html += '<div class="planning-chat-error">' + _escapeForHtml(errorMsg) + "</div>";
    }
    if (text) {
      html += renderMarkdown(text);
    }
    // Show tool activity in a collapsible details element.
    if (_hasToolActivity(rawBuffer) && typeof renderPrettyLogs === "function") {
      html +=
        '<details class="planning-chat-activity"><summary>Agent activity</summary>' +
        '<div class="planning-chat-activity__log">' +
        renderPrettyLogs(rawBuffer) +
        "</div></details>";
    }
    if (!html) {
      html = '<span class="planning-chat-empty">No response</span>';
    }
    el.innerHTML = html;
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
  }

  // no-op kept for backward compat with tests
  function _enableInput() {
    if (_input) _input.focus();
  }

  function _appendMessageBubble(role, content, timestamp) {
    var bubble = _createBubble(role);
    var contentEl = bubble.querySelector(".planning-chat-bubble__content");
    if (contentEl) {
      if (role === "assistant") {
        contentEl.innerHTML = renderMarkdown(content);
      } else {
        contentEl.textContent = content;
      }
    }
    if (timestamp) {
      var timeEl = bubble.querySelector(".planning-chat-bubble__time");
      if (timeEl) {
        var d = new Date(timestamp);
        timeEl.textContent = d.toLocaleTimeString(undefined, {
          hour: "2-digit",
          minute: "2-digit",
        });
      }
    }
    _messagesEl.appendChild(bubble);
  }

  function _createBubble(role) {
    var bubble = document.createElement("div");
    bubble.className =
      "planning-chat-bubble planning-chat-bubble--" + role;
    bubble.innerHTML =
      '<div class="planning-chat-bubble__content"></div>' +
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

  function _scrollToBottom() {
    if (_streamEl) {
      _streamEl.scrollTop = _streamEl.scrollHeight;
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

  async function _showAutocomplete(text) {
    var commands = await _fetchCommands();
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
      _autocompleteEl.className = "planning-chat-autocomplete";
      _input.parentElement.appendChild(_autocompleteEl);
    }

    _autocompleteEl.innerHTML = "";
    _autocompleteIndex = 0;

    matches.forEach(function (cmd, i) {
      var item = document.createElement("div");
      item.className = "planning-chat-autocomplete__item";
      item.innerHTML =
        "<strong>/" +
        _escapeHtml(cmd.name) +
        "</strong> <span>" +
        _escapeHtml(cmd.description) +
        "</span>";
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
    var items = _autocompleteEl.children;
    for (var i = 0; i < items.length; i++) {
      items[i].classList.toggle(
        "planning-chat-autocomplete__item--active",
        i === _autocompleteIndex,
      );
    }
  }

  function _selectAutocomplete(index) {
    if (!_autocompleteEl) return;
    var items = _autocompleteEl.children;
    if (index < 0 || index >= items.length) return;
    var name = items[index].querySelector("strong").textContent;
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
    _queue = _queue.filter(function (item) { return item.id !== id; });
    _renderQueue();
  }

  function _editQueueItem(id) {
    var item = _queue.find(function (q) { return q.id === id; });
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
