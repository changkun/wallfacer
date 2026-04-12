// planning-chat.js — Planning agent chat module.
// Handles sending messages, streaming responses, rendering conversation,
// and slash command autocomplete.

/* global Routes, api, renderMarkdown, renderPrettyLogs, startStreamingFetch, escapeHtml, specModeState, withAuthToken, withBearerHeaders, attachMentionAutocomplete, attachAutocomplete, reloadSpecTree */

var PlanningChat = (function () {
  "use strict";

  var _streaming = false;
  var _streamingThreadId = null; // ID of the thread whose exec is in flight
  var _activeStream = null; // handle from startStreamingFetch
  var _commandsCache = null;
  // Send mode: "enter" = Enter sends (Shift+Enter for newline),
  //            "cmd-enter" = Cmd/Ctrl+Enter sends (Enter for newline).
  var _sendMode = localStorage.getItem("wallfacer-chat-send-mode") || "enter";
  var _slashAutocomplete = null; // Handle returned by attachAutocomplete.
  var _nextQueueId = 0;

  // Thread state. _threads maps id -> {id, name, archived, queue, enqueuedAt,
  // lastViewedAt, unread}. enqueuedAt tracks the order of the oldest queued
  // message per thread so the global drain dispatcher can fire in FIFO.
  var _threads = {};
  var _threadOrder = []; // ordered IDs (from manifest)
  var _activeThreadId = null;
  var _archivedList = []; // metadata for archived threads (not in _threadOrder)

  // DOM references (set in init).
  var _input = null;
  var _sendBtn = null;
  var _interruptBtn = null;
  var _messagesEl = null;
  var _streamEl = null;
  var _queueEl = null;
  var _tabsEl = null;

  function init() {
    _input = document.getElementById("spec-chat-input");
    _sendBtn = document.getElementById("spec-chat-send");
    _messagesEl = document.getElementById("spec-chat-messages");
    _streamEl = document.getElementById("spec-chat-stream");
    if (!_input || !_messagesEl) return;

    _input.addEventListener("keydown", _onInputKeydown);
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

    // Attach / slash-command autocomplete via the same shared widget.
    if (typeof attachAutocomplete === "function") {
      _slashAutocomplete = attachAutocomplete(_input, {
        position: "above",
        emptyMessage: null, // Close the dropdown when no command matches.
        triggerOnCursorMove: false,
        shouldActivate: function (textarea) {
          var v = textarea.value;
          if (v.indexOf("\n") !== -1) return null;
          if (v[0] !== "/") return null;
          return { query: v.slice(1), startIdx: 0 };
        },
        fetchItems: async function (match) {
          var commands = await _fetchCommands();
          if (!commands) return [];
          var q = (match.query || "").toLowerCase();
          return commands.filter(function (c) {
            return c.name.toLowerCase().startsWith(q);
          });
        },
        renderItem: function (cmd) {
          var item = document.createElement("div");
          item.className = "mention-item";
          var nameEl = document.createElement("span");
          nameEl.className = "mention-filename";
          nameEl.textContent = "/" + cmd.name;
          var descEl = document.createElement("span");
          descEl.className = "mention-path";
          descEl.textContent = cmd.description || "";
          item.appendChild(nameEl);
          item.appendChild(descEl);
          return item;
        },
        onSelect: function (cmd, textarea) {
          textarea.value = "/" + cmd.name + " ";
          textarea.focus();
        },
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
        _input.dispatchEvent(new Event("input"));
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

    _tabsEl = document.getElementById("spec-chat-tabs");

    _fetchCommands(); // pre-fetch so autocomplete is instant
    // Returned promise lets callers (and tests) wait until the thread
    // manifest has loaded before interacting with the module.
    return _loadThreads().then(function () {
      return _loadHistory();
    });
  }

  // --- Thread state ---

  // _threadUrl substitutes the thread id into a Routes.planning.* URL
  // template that contains the literal "{id}" placeholder. The route
  // generator (internal/apicontract/generate.go) only special-cases
  // /api/tasks/{id} with a nested closure; other {id} routes are emitted
  // as literal templates, so we do the substitution here.
  function _threadUrl(route, id) {
    return route().replace("{id}", encodeURIComponent(id));
  }

  function _activeThread() {
    return _activeThreadId ? _threads[_activeThreadId] : null;
  }

  // _loadThreads fetches the thread list, populates local state, and
  // renders the tab bar. Called once on init and after any CRUD call.
  async function _loadThreads() {
    try {
      var res = await api(
        Routes.planning.listThreads() + "?includeArchived=true",
      );
      var all = (res && res.threads) || [];
      _threadOrder = [];
      _archivedList = [];
      var seen = {};
      all.forEach(function (t) {
        var existing = _threads[t.id] || {};
        _threads[t.id] = {
          id: t.id,
          name: t.name,
          archived: !!t.archived,
          queue: existing.queue || [],
          enqueuedAt: existing.enqueuedAt || 0,
          lastViewedAt: existing.lastViewedAt || 0,
          unread: existing.unread || false,
          scrollTop: existing.scrollTop || 0,
        };
        seen[t.id] = true;
        if (t.archived) {
          _archivedList.push(_threads[t.id]);
        } else {
          _threadOrder.push(t.id);
        }
      });
      // Drop any stale cached thread not in the manifest.
      Object.keys(_threads).forEach(function (id) {
        if (!seen[id]) delete _threads[id];
      });
      _activeThreadId = res && res.active_id ? res.active_id : _threadOrder[0];
      if (!_activeThreadId && _threadOrder.length > 0) {
        _activeThreadId = _threadOrder[0];
      }
      _renderTabs();
    } catch (_) {
      // Swallow — a planner misconfig leaves the tab bar empty.
    }
  }

  // _renderTabs repaints the tab bar from _threadOrder / _activeThreadId.
  function _renderTabs() {
    if (!_tabsEl) return;
    _tabsEl.innerHTML = "";
    _threadOrder.forEach(function (id) {
      var t = _threads[id];
      if (!t) return;
      _tabsEl.appendChild(_buildTabEl(t));
    });
    _tabsEl.appendChild(_buildNewTabButton());
  }

  function _buildTabEl(t) {
    var active = t.id === _activeThreadId;
    var el = document.createElement(active ? "div" : "button");
    el.className = "spec-chat-tab" + (active ? " spec-chat-tab--active" : "");
    el.dataset.threadId = t.id;
    if (active) el.setAttribute("role", "tab");
    var label = document.createElement("span");
    label.className = "spec-chat-tab__label";
    label.textContent = t.name;
    el.appendChild(label);
    if (!active && t.unread) {
      var dot = document.createElement("span");
      dot.className = "spec-chat-tab__unread";
      dot.setAttribute("aria-label", "unread");
      el.appendChild(dot);
    }
    if (active) {
      var pencil = document.createElement("button");
      pencil.type = "button";
      pencil.className = "spec-chat-tab__pencil";
      pencil.title = "Rename";
      pencil.innerHTML = "\u270E"; // ✎
      pencil.addEventListener("click", function (e) {
        e.stopPropagation();
        _startInlineRename(el, t.id);
      });
      el.appendChild(pencil);
      el.addEventListener("dblclick", function () {
        _startInlineRename(el, t.id);
      });
    } else {
      el.addEventListener("click", function () {
        _switchToThread(t.id);
      });
    }
    // Close button (archive) — shown for all tabs except the in-flight one.
    var closeBtn = document.createElement("button");
    closeBtn.type = "button";
    closeBtn.className = "spec-chat-tab__close";
    closeBtn.title = "Archive thread";
    closeBtn.textContent = "\u00d7"; // ×
    closeBtn.addEventListener("click", function (e) {
      e.stopPropagation();
      _archiveThread(t.id);
    });
    el.appendChild(closeBtn);
    return el;
  }

  function _buildNewTabButton() {
    var wrap = document.createElement("span");
    wrap.className = "spec-chat-tab__new-wrap";
    var plus = document.createElement("button");
    plus.type = "button";
    plus.className = "spec-chat-tab__new";
    plus.title = "New thread";
    plus.textContent = "+";
    plus.addEventListener("click", function () {
      _createThread();
    });
    wrap.appendChild(plus);
    if (_archivedList.length > 0) {
      var caret = document.createElement("button");
      caret.type = "button";
      caret.className = "spec-chat-tab__archived-trigger";
      caret.title = "Archived threads";
      caret.textContent = "\u25BE"; // ▾
      caret.addEventListener("click", function (e) {
        e.stopPropagation();
        _toggleArchivedMenu(caret);
      });
      wrap.appendChild(caret);
    }
    return wrap;
  }

  function _toggleArchivedMenu(anchor) {
    var existing = document.querySelector(".spec-chat-archived-menu");
    if (existing) {
      existing.remove();
      return;
    }
    var menu = document.createElement("div");
    menu.className = "spec-chat-archived-menu";
    var header = document.createElement("div");
    header.className = "spec-chat-archived-menu__header";
    header.textContent = "Archived threads";
    menu.appendChild(header);
    _archivedList.forEach(function (t) {
      var row = document.createElement("button");
      row.type = "button";
      row.className = "spec-chat-archived-menu__item";
      row.textContent = t.name;
      row.addEventListener("click", function () {
        menu.remove();
        _unarchiveThread(t.id);
      });
      menu.appendChild(row);
    });
    document.body.appendChild(menu);
    var rect = anchor.getBoundingClientRect();
    menu.style.position = "fixed";
    menu.style.top = rect.bottom + 4 + "px";
    menu.style.left = rect.left + "px";
    // Close on outside click.
    setTimeout(function () {
      function closer(e) {
        if (!menu.contains(e.target)) {
          menu.remove();
          document.removeEventListener("click", closer);
        }
      }
      document.addEventListener("click", closer);
    }, 0);
  }

  async function _createThread() {
    try {
      var t = await api(Routes.planning.createThread(), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: "{}",
      });
      if (t && t.id) {
        await _loadThreads();
        await _switchToThread(t.id);
      }
    } catch (err) {
      _appendSystemMessage("Failed to create thread: " + (err.message || err));
    }
  }

  async function _archiveThread(id) {
    if (!confirm("Archive this thread? You can restore it later.")) return;
    try {
      var res = await fetch(_threadUrl(Routes.planning.archiveThread, id), {
        method: "POST",
        headers: withBearerHeaders({ "Content-Type": "application/json" }),
      });
      if (res.status === 409) {
        _appendSystemMessage("Thread is busy — interrupt it before archiving.");
        return;
      }
      if (!res.ok) {
        _appendSystemMessage("Archive failed: HTTP " + res.status);
        return;
      }
      await _loadThreads();
      // After archive, switch to the (new) active thread so the chat
      // stream stops showing an archived thread's history.
      if (_activeThreadId && _activeThreadId !== id) {
        await _loadHistory();
      } else if (_threadOrder.length > 0) {
        await _switchToThread(_threadOrder[0]);
      } else {
        _messagesEl.innerHTML = "";
      }
    } catch (err) {
      _appendSystemMessage("Archive failed: " + (err.message || err));
    }
  }

  async function _unarchiveThread(id) {
    try {
      await api(_threadUrl(Routes.planning.unarchiveThread, id), {
        method: "POST",
        headers: { "Content-Type": "application/json" },
      });
      await _loadThreads();
      await _switchToThread(id);
    } catch (err) {
      _appendSystemMessage("Restore failed: " + (err.message || err));
    }
  }

  // _switchToThread updates UI state synchronously first (tab bar
  // visibly flips, scroll is saved, stream reader detaches) so clicks
  // always feel instant, then fires the server-side activate POST and
  // history fetch in the background. Concurrent switches from rapid
  // clicks during an agent wrap-up are guarded by a per-switch epoch:
  // only the latest switch's history fetch is allowed to paint.
  var _switchEpoch = 0;
  function _switchToThread(id) {
    if (!id || id === _activeThreadId) return Promise.resolve();
    var outgoing = _activeThread();
    if (outgoing && _messagesEl) {
      outgoing.scrollTop = _messagesEl.scrollTop;
    }
    _activeThreadId = id;
    var t = _threads[id];
    if (t) {
      t.unread = false;
      t.lastViewedAt = Date.now();
    }

    // Detach the local stream reader when leaving the in-flight thread
    // so further chunks don't land in the wrong history. The server
    // exec keeps running.
    if (_streaming && _streamingThreadId !== id) {
      if (_activeStream) {
        _activeStream.abort();
        _activeStream = null;
      }
      _streaming = false;
      if (_interruptBtn) _interruptBtn.style.display = "none";
      if (_sendBtn) _sendBtn.style.display = "";
    }

    _renderTabs();

    var epoch = ++_switchEpoch;
    // Fire-and-forget activate — the UI already reflects the new
    // active thread; this is just a server-side preference save.
    api(_threadUrl(Routes.planning.activateThread, id), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
    }).catch(function () {});

    return _loadHistory().then(function () {
      // A newer switch happened while we were loading — discard our
      // results so we don't paint stale history over the new thread.
      if (epoch !== _switchEpoch) return;
      if (!_streaming && _streamingThreadId === id) {
        _attachStreamToLastBubble();
      }
    });
  }

  async function _startInlineRename(tabEl, id) {
    var t = _threads[id];
    if (!t) return;
    var input = document.createElement("input");
    input.type = "text";
    input.value = t.name;
    input.className = "spec-chat-tab__rename-input";
    var origHtml = tabEl.innerHTML;
    tabEl.innerHTML = "";
    tabEl.appendChild(input);
    input.focus();
    input.select();

    var committed = false;
    async function commit() {
      if (committed) return;
      committed = true;
      var newName = input.value.trim();
      if (!newName || newName === t.name) {
        tabEl.innerHTML = origHtml;
        return;
      }
      try {
        await api(_threadUrl(Routes.planning.renameThread, id), {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ name: newName }),
        });
        t.name = newName;
      } catch (_) {}
      _renderTabs();
    }
    function cancel() {
      if (committed) return;
      committed = true;
      tabEl.innerHTML = origHtml;
    }
    input.addEventListener("keydown", function (e) {
      if (e.key === "Enter") {
        e.preventDefault();
        commit();
      } else if (e.key === "Escape") {
        e.preventDefault();
        cancel();
      }
    });
    input.addEventListener("blur", commit);
  }

  // _attachStreamToLastBubble re-attaches the SSE reader to the last
  // assistant bubble in the currently rendered thread. Used when
  // switching into the in-flight thread mid-exec.
  function _attachStreamToLastBubble() {
    // The server will respond 204 if the thread isn't actually in
    // flight, so this is safe as a best-effort attach.
    _startStreaming();
  }

  function _onInputKeydown(e) {
    // The shared autocomplete widget handles its own ArrowUp/Down/Enter/Tab/
    // Escape when its dropdown is open. Skip send-on-Enter so selecting an
    // autocomplete row doesn't also submit the message.
    var autocompleteOpen =
      (_slashAutocomplete && _slashAutocomplete.isOpen()) ||
      !!document.querySelector(".mention-dropdown");
    if (autocompleteOpen) return;

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

  async function _loadHistory() {
    if (!_messagesEl) return;
    // Clear the pane before rendering so switching threads doesn't
    // stack old bubbles. _messagesEl is always repainted here so callers
    // don't need to clear it themselves.
    _messagesEl.innerHTML = "";
    _renderQueue();
    if (!_activeThreadId) return;
    // Capture the active thread at fetch start; if the user switches
    // tabs while the response is in flight (rapid clicks during a
    // stream wrap-up), we drop this response on the floor so we don't
    // paint stale messages into the new thread.
    var fetchedThreadId = _activeThreadId;
    var url =
      Routes.planning.messages() +
      "?thread=" +
      encodeURIComponent(fetchedThreadId);
    try {
      var msgs = await api(url);
      if (fetchedThreadId !== _activeThreadId) return;
      if (msgs && msgs.length > 0) {
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
      }
      // Restore scroll position for this thread (or scroll to bottom by
      // default).
      var t = _activeThread();
      if (t && t.scrollTop > 0) {
        _messagesEl.scrollTop = t.scrollTop;
      } else {
        _scrollToBottom(true);
      }
    } catch (_) {
      // Ignore — history may not be available yet.
    }
  }

  async function sendMessage(text, opts) {
    opts = opts || {};
    var threadID = opts.threadID || _activeThreadId;
    if (!threadID) {
      _appendSystemMessage("No active thread — create one first.");
      return;
    }
    if (_streaming) {
      _enqueue(text, threadID);
      return;
    }

    _input.value = "";
    _input.style.height = "auto";
    if (_slashAutocomplete) _slashAutocomplete.close();

    // Render user message immediately only when sending to the active
    // thread (otherwise the bubble would appear in the wrong history).
    if (threadID === _activeThreadId) {
      _appendMessageBubble("user", text, new Date().toISOString());
      _userScrolledUp = false;
      _scrollToBottom(true);
    }

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
          thread: threadID,
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

      _streamingThreadId = threadID;
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
      var streamUrl =
        Routes.planning.messageStream() +
        (_streamingThreadId
          ? "?thread=" + encodeURIComponent(_streamingThreadId)
          : "");
      _activeStream = startStreamingFetch({
        url: streamUrl,
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
    var finishedThreadId = _streamingThreadId;
    _streamingThreadId = null;
    if (_interruptBtn) _interruptBtn.style.display = "none";
    if (_sendBtn) _sendBtn.style.display = "";

    if (interrupted && _messagesEl) {
      var indicator = document.createElement("div");
      indicator.className = "planning-chat-interrupted";
      indicator.textContent = "interrupted";
      _messagesEl.appendChild(indicator);
    }

    _enableInput();

    // Global drain dispatcher: pick the thread with the oldest queued
    // message across all threads. This is what lets a queued message
    // in a background tab fire without the user switching back to it.
    _drainNextQueuedThread();

    // Refetch history on a clean stop so the streaming bubble picks up
    // its server-attributed plan_round (for the per-message undo
    // button). Only reload the UI if the finished thread is still the
    // active one; otherwise mark the background thread as unread.
    if (!interrupted) {
      if (finishedThreadId && finishedThreadId !== _activeThreadId) {
        var t = _threads[finishedThreadId];
        if (t) t.unread = true;
        _renderTabs();
      } else {
        _loadHistory();
      }
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
  function _appendMessageBubbleWithActivity(
    content,
    rawOutput,
    timestamp,
    planRound,
  ) {
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
      var n = Number.parseInt(bubbles[i].getAttribute("data-round"), 10);
      if (!Number.isNaN(n) && n > latestRound) latestRound = n;
    }
    bubbles.forEach(function (b) {
      var btn = b.querySelector(".planning-chat-bubble__undo");
      if (!btn) return;
      var n = Number.parseInt(b.getAttribute("data-round"), 10);
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
      var undoUrl = Routes.planning.undo();
      if (_activeThreadId) {
        undoUrl += "?thread=" + encodeURIComponent(_activeThreadId);
      }
      var res = await fetch(undoUrl, {
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
    el.className = "planning-chat-system planning-chat-system--undo";
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
    if (status === 409 && err.indexOf("revert conflict") !== -1) {
      msg =
        "⚠ Undo ran into a merge conflict — a concurrent thread edited the same spec. Resolve manually before retrying.";
    } else if (status === 409 && err.indexOf("stash pop conflict") !== -1) {
      msg =
        "⚠ Undo partially applied: your working-tree edits couldn't be reapplied cleanly. Your changes are preserved in the stash — run `git stash list` to recover.";
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
  //
  // Dropdown UI, keyboard nav, and lifecycle are owned by the shared
  // attachAutocomplete widget (ui/js/lib/autocomplete.ts). This module only
  // supplies the command source via _fetchCommands below; the widget is
  // attached in init() with slash-specific shouldActivate/renderItem/onSelect.

  async function _fetchCommands() {
    if (_commandsCache) return _commandsCache;
    try {
      _commandsCache = await api(Routes.planning.commands());
      return _commandsCache;
    } catch (_) {
      return [];
    }
  }

  // --- Message queue ---
  //
  // Each thread keeps its own queue (_threads[id].queue). The global
  // drain dispatcher picks the thread with the oldest queued message
  // across all threads — not just the active tab — so a queued message
  // in a background tab still fires when the in-flight exec finishes.

  function _currentQueue() {
    var t = _activeThread();
    return t ? t.queue : [];
  }

  function _enqueue(text, threadID) {
    var id = _nextQueueId++;
    var t = _threads[threadID || _activeThreadId];
    if (!t) return;
    if (t.queue.length === 0) t.enqueuedAt = Date.now();
    t.queue.push({ id: id, text: text });
    _renderQueue();
  }

  function _removeFromQueue(id) {
    var t = _activeThread();
    if (!t) return;
    t.queue = t.queue.filter(function (item) {
      return item.id !== id;
    });
    if (t.queue.length === 0) t.enqueuedAt = 0;
    _renderQueue();
  }

  function _editQueueItem(id) {
    var queue = _currentQueue();
    var item = queue.find(function (q) {
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
    var queue = _currentQueue();
    queue.forEach(function (item) {
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

  // _drainNextQueuedThread picks the thread whose oldest queued message
  // is the oldest across all threads and fires that message. This is
  // the global FIFO dispatcher described in the spec — without it,
  // background tabs with queued messages would never drain until the
  // user switched to them.
  function _drainNextQueuedThread() {
    if (_streaming) return;
    var bestId = null;
    var bestTs = Infinity;
    Object.keys(_threads).forEach(function (id) {
      var t = _threads[id];
      if (!t || t.queue.length === 0) return;
      if (t.enqueuedAt < bestTs) {
        bestTs = t.enqueuedAt;
        bestId = id;
      }
    });
    if (!bestId) return;
    var t = _threads[bestId];
    var next = t.queue.shift();
    t.enqueuedAt = t.queue.length > 0 ? Date.now() : 0;
    _renderQueue();
    sendMessage(next.text, { threadID: bestId });
  }

  // --- Interrupt ---

  async function _onInterrupt() {
    var url = Routes.planning.interruptMessage();
    if (_streamingThreadId) {
      url += "?thread=" + encodeURIComponent(_streamingThreadId);
    }
    try {
      await fetch(url, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
      });
    } catch (_) {
      // Ignore errors — the stream will end regardless.
    }
    _stopStreaming(true);
  }

  async function clearHistory() {
    var url = Routes.planning.clearMessages();
    if (_activeThreadId) {
      url += "?thread=" + encodeURIComponent(_activeThreadId);
    }
    try {
      await fetch(url, {
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
    return _currentQueue().slice();
  }

  return {
    init: init,
    sendMessage: sendMessage,
    clearHistory: clearHistory,
    isStreaming: isStreaming,
    getQueue: getQueue,
  };
})();
