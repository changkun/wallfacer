// planning-chat.js — Planning agent chat module.
// Handles sending messages, streaming responses, rendering conversation,
// and slash command autocomplete.

/* global Routes, api, renderMarkdown, specModeState */

var PlanningChat = (function () {
  "use strict";

  var _streaming = false;
  var _eventSource = null;
  var _commandsCache = null;
  var _autocompleteEl = null;
  var _autocompleteIndex = -1;

  // DOM references (set in init).
  var _input = null;
  var _sendBtn = null;
  var _messagesEl = null;
  var _streamEl = null;

  function init() {
    _input = document.getElementById("spec-chat-input");
    _sendBtn = document.getElementById("spec-chat-send");
    _messagesEl = document.getElementById("spec-chat-messages");
    _streamEl = document.getElementById("spec-chat-stream");
    if (!_input || !_messagesEl) return;

    _input.addEventListener("keydown", _onInputKeydown);
    _input.addEventListener("input", _onInputChange);
    if (_sendBtn) {
      _sendBtn.addEventListener("click", function () {
        var text = _input.value.trim();
        if (text) sendMessage(text);
      });
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

    if (e.key === "Enter" && !e.shiftKey) {
      e.preventDefault();
      var text = _input.value.trim();
      if (text) sendMessage(text);
    }
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
    if (_streaming) return; // Will be replaced by queue in ui-message-queue.

    _input.value = "";
    _input.disabled = true;
    if (_sendBtn) _sendBtn.disabled = true;
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
        _enableInput();
        return;
      }
      if (!res.ok) {
        var errText = await res.text();
        _appendSystemMessage("Error: " + errText);
        _enableInput();
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
    var assistantBubble = null;
    var rawChunks = [];

    _eventSource = new EventSource(Routes.planning.messageStream());

    _eventSource.onmessage = function (event) {
      rawChunks.push(event.data);
      if (!assistantBubble) {
        assistantBubble = _createBubble("assistant");
        _messagesEl.appendChild(assistantBubble);
      }
      // Render accumulated text as markdown.
      var fullText = rawChunks.join("");
      var contentEl = assistantBubble.querySelector(
        ".planning-chat-bubble__content",
      );
      if (contentEl) {
        contentEl.innerHTML = renderMarkdown(fullText);
      }
      _scrollToBottom();
    };

    _eventSource.addEventListener("done", function () {
      _stopStreaming();
    });

    _eventSource.onerror = function () {
      _stopStreaming();
    };
  }

  function _stopStreaming() {
    if (_eventSource) {
      _eventSource.close();
      _eventSource = null;
    }
    _streaming = false;
    _enableInput();
  }

  function _enableInput() {
    if (_input) {
      _input.disabled = false;
      _input.focus();
    }
    if (_sendBtn) _sendBtn.disabled = false;
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

  function isStreaming() {
    return _streaming;
  }

  return {
    init: init,
    sendMessage: sendMessage,
    isStreaming: isStreaming,
  };
})();
