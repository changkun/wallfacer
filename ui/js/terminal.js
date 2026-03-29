// --- Terminal: xterm.js integration with WebSocket PTY relay ---

var _term = null;
var _fitAddon = null;
var _termWs = null;
var _termReconnectTimer = null;
var _termReconnectDelay = 1000;
var _termTabCounter = 0;
var _onTabClick = function () {};
var _onTabClose = function () {};
var _onTabAdd = function () {};
var _sessions = {}; // sessionId → { buffer: [Uint8Array...] }
var _activeSessionId = null;
var _sessionBufferLimit = 100000; // ~100KB per session

// In desktop mode (Wails), the reverse proxy can't forward WebSocket upgrades.
// Discover the real server port via /api/desktop-port and connect directly.
var _desktopServerHost = (function () {
  try {
    var xhr = new XMLHttpRequest();
    xhr.open("GET", "/api/desktop-port", false);
    xhr.send();
    if (xhr.status === 200 && xhr.responseText) {
      return "localhost:" + xhr.responseText.trim();
    }
  } catch (_) {}
  return null;
})();

function _getCSSVar(name) {
  return getComputedStyle(document.documentElement)
    .getPropertyValue(name)
    .trim();
}

/**
 * initTerminal — create xterm.js instance and mount into the panel.
 * Does NOT connect; connection happens on first panel open.
 */
// ANSI color palettes tuned for dark and light backgrounds.
// Modeled after VSCode's default terminal themes.
var _darkAnsiColors = {
  black: "#3c3c3c",
  red: "#f14c4c",
  green: "#23d18b",
  yellow: "#f5f543",
  blue: "#3b8eea",
  magenta: "#d670d6",
  cyan: "#29b8db",
  white: "#cccccc",
  brightBlack: "#666666",
  brightRed: "#f14c4c",
  brightGreen: "#23d18b",
  brightYellow: "#f5f543",
  brightBlue: "#3b8eea",
  brightMagenta: "#d670d6",
  brightCyan: "#29b8db",
  brightWhite: "#e5e5e5",
};
var _lightAnsiColors = {
  black: "#3c3c3c",
  red: "#cd3131",
  green: "#00bc70",
  yellow: "#949800",
  blue: "#0451a5",
  magenta: "#bc05bc",
  cyan: "#0598bc",
  white: "#555555",
  brightBlack: "#666666",
  brightRed: "#cd3131",
  brightGreen: "#14ce14",
  brightYellow: "#b5ba00",
  brightBlue: "#0451a5",
  brightMagenta: "#bc05bc",
  brightCyan: "#0598bc",
  brightWhite: "#3c3c3c",
};

function _buildTermTheme() {
  var bg = _getCSSVar("--bg") || "#1a1917";
  var fg = _getCSSVar("--text") || "#cccccc";
  // Detect light vs dark by checking luminance of the background.
  var isLight = _isLightColor(bg);
  var ansi = isLight ? _lightAnsiColors : _darkAnsiColors;
  return {
    background: bg,
    foreground: fg,
    cursor: _getCSSVar("--accent") || "#d97757",
    selectionBackground: isLight
      ? "rgba(0,0,0,0.15)"
      : "rgba(78,140,255,0.3)",
    black: ansi.black,
    red: ansi.red,
    green: ansi.green,
    yellow: ansi.yellow,
    blue: ansi.blue,
    magenta: ansi.magenta,
    cyan: ansi.cyan,
    white: ansi.white,
    brightBlack: ansi.brightBlack,
    brightRed: ansi.brightRed,
    brightGreen: ansi.brightGreen,
    brightYellow: ansi.brightYellow,
    brightBlue: ansi.brightBlue,
    brightMagenta: ansi.brightMagenta,
    brightCyan: ansi.brightCyan,
    brightWhite: ansi.brightWhite,
  };
}

function _isLightColor(hex) {
  hex = hex.replace("#", "");
  var r = parseInt(hex.substring(0, 2), 16);
  var g = parseInt(hex.substring(2, 4), 16);
  var b = parseInt(hex.substring(4, 6), 16);
  // Relative luminance threshold.
  return (0.299 * r + 0.587 * g + 0.114 * b) > 128;
}

function initTerminal() {
  if (_term) return;
  if (typeof Terminal === "undefined") return;

  _term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: '"SF Mono", Menlo, Monaco, "Courier New", monospace',
    theme: _buildTermTheme(),
    macOptionIsMeta: true,
  });

  _fitAddon = new FitAddon.FitAddon();
  _term.loadAddon(_fitAddon);

  var canvas = document.getElementById("terminal-canvas");
  if (canvas) {
    _term.open(canvas);
    // Fit after open so dimensions are calculated correctly.
    try {
      _fitAddon.fit();
    } catch (_) {
      /* panel may be hidden */
    }
  }

  // Re-fit when the canvas resizes (drag handle, window resize).
  if (canvas && typeof ResizeObserver !== "undefined") {
    var panel = document.getElementById("status-bar-panel");
    new ResizeObserver(function () {
      if (!panel || !panel.classList.contains("hidden")) {
        try {
          _fitAddon.fit();
        } catch (_) {
          /* ignore */
        }
      }
    }).observe(canvas);
  }

  // Re-apply terminal theme when light/dark mode changes.
  if (typeof MutationObserver !== "undefined") {
    new MutationObserver(function () {
      if (_term) _term.options.theme = _buildTermTheme();
    }).observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["data-theme"],
    });
  }

  // Wire the "+" button to the tab-add callback.
  // Prevent mousedown from stealing focus from xterm.
  var addBtn = document.getElementById("terminal-tab-add");
  if (addBtn) {
    addBtn.addEventListener("mousedown", function (e) {
      e.preventDefault();
    });
    addBtn.addEventListener("click", function () {
      _onTabAdd();
    });
  }

  // Wire the container picker button.
  var containerBtn = document.getElementById("terminal-container-btn");
  if (containerBtn) {
    containerBtn.addEventListener("mousedown", function (e) {
      e.preventDefault();
    });
    containerBtn.addEventListener("click", function () {
      _showContainerPicker();
    });
  }

  // Wire tab callbacks to send WebSocket session messages.
  // Each callback defers _term.focus() to the next frame so the browser's
  // click-focus handling finishes first and our focus call wins.
  setTabClickHandler(function (id) {
    if (_termWs && _termWs.readyState === WebSocket.OPEN) {
      _termWs.send(JSON.stringify({ type: "switch_session", session: id }));
    }
    _deferTermFocus();
  });
  setTabCloseHandler(function (id) {
    if (_termWs && _termWs.readyState === WebSocket.OPEN) {
      _termWs.send(JSON.stringify({ type: "close_session", session: id }));
    }
    _deferTermFocus();
  });
  setTabAddHandler(function () {
    if (_termWs && _termWs.readyState === WebSocket.OPEN) {
      _termWs.send(JSON.stringify({ type: "create_session" }));
    }
    _deferTermFocus();
  });

  // Intercept Cmd+Backspace on macOS and send Ctrl+U (kill line backward).
  // The browser eats Cmd+key combos before xterm sees them, so we catch
  // the raw DOM keydown event and inject the escape sequence manually.
  _term.attachCustomKeyEventHandler(function (e) {
    if (e.type !== "keydown") return true;
    if (e.metaKey && e.key === "Backspace") {
      // Ctrl+U = \x15 (kill from cursor to start of line).
      if (_termWs && _termWs.readyState === WebSocket.OPEN) {
        _termWs.send(JSON.stringify({ type: "input", data: btoa("\x15") }));
      }
      e.preventDefault();
      return false;
    }
    if (e.metaKey && e.key === "k") {
      // Ctrl+K = \x0b (kill from cursor to end of line).
      if (_termWs && _termWs.readyState === WebSocket.OPEN) {
        _termWs.send(JSON.stringify({ type: "input", data: btoa("\x0b") }));
      }
      e.preventDefault();
      return false;
    }
    return true;
  });

  // Wire terminal events once — they check _termWs on each invocation.
  _term.onData(function (data) {
    if (_termWs && _termWs.readyState === WebSocket.OPEN) {
      _termWs.send(JSON.stringify({ type: "input", data: btoa(data) }));
    }
  });
  _term.onResize(function (size) {
    if (_termWs && _termWs.readyState === WebSocket.OPEN) {
      _termWs.send(
        JSON.stringify({ type: "resize", cols: size.cols, rows: size.rows }),
      );
    }
  });
}

/**
 * connectTerminal — open WebSocket to the backend PTY.
 * If already connected, just re-fit and focus.
 */
function connectTerminal() {
  if (!_term) return;

  // Re-apply theme in case user switched light/dark since init.
  _term.options.theme = _buildTermTheme();

  if (_termWs && _termWs.readyState === WebSocket.OPEN) {
    try {
      _fitAddon.fit();
    } catch (_) {
      /* ignore */
    }
    _term.focus();
    return;
  }

  var cols = _term.cols || 80;
  var rows = _term.rows || 24;
  // In desktop mode, connect WebSocket directly to the real server since the
  // Wails reverse proxy cannot forward WebSocket upgrades.
  var wsHost = _desktopServerHost || location.host;
  var proto = _desktopServerHost
    ? "ws:"
    : location.protocol === "https:"
      ? "wss:"
      : "ws:";
  var url = proto + "//" + wsHost + "/api/terminal/ws";
  url += "?cols=" + cols + "&rows=" + rows;
  var token =
    typeof getWallfacerToken === "function" ? getWallfacerToken() : "";
  if (token) url += "&token=" + encodeURIComponent(token);

  var ws = new WebSocket(url);
  ws.binaryType = "arraybuffer";
  _termWs = ws;

  ws.onopen = function () {
    _termReconnectDelay = 1000;
    try {
      _fitAddon.fit();
    } catch (_) {
      /* ignore */
    }
    _term.focus();
  };

  ws.onmessage = function (event) {
    if (event.data instanceof ArrayBuffer) {
      var bytes = new Uint8Array(event.data);
      _term.write(bytes);
      // Buffer output for the active session.
      if (_activeSessionId && _sessions[_activeSessionId]) {
        var buf = _sessions[_activeSessionId].buffer;
        buf.push(bytes);
        // Trim if over limit.
        var total = 0;
        for (var i = 0; i < buf.length; i++) total += buf[i].length;
        while (total > _sessionBufferLimit && buf.length > 0) {
          total -= buf[0].length;
          buf.shift();
        }
      }
      return;
    }
    // Text message — parse JSON.
    var msg;
    try {
      msg = JSON.parse(event.data);
    } catch (e) {
      return;
    }
    switch (msg.type) {
      case "sessions":
        _handleSessionsList(msg.sessions);
        break;
      case "session_created":
        // Tab is added by the sessions list that follows immediately.
        break;
      case "session_switched":
        _handleSessionSwitched(msg.session);
        break;
      case "session_closed":
        _handleSessionClosed(msg.session);
        break;
      case "session_exited":
        _handleSessionExited(msg.session);
        break;
      case "pong":
        break;
    }
  };

  ws.onclose = function (event) {
    _termWs = null;
    _clearSessionState();
    if (event.code !== 1000) {
      _term.write("\r\n\x1b[33mDisconnected. Reconnecting...\x1b[0m\r\n");
      _scheduleReconnect();
    } else {
      // Clean close (all sessions ended) — hide the terminal panel.
      _hideTermPanel();
    }
  };

  ws.onerror = function () {
    // Error details logged by the browser; onclose handles reconnection.
  };
}

/**
 * disconnectTerminal — close the WebSocket cleanly.
 */
function disconnectTerminal() {
  if (_termReconnectTimer) {
    clearTimeout(_termReconnectTimer);
    _termReconnectTimer = null;
  }
  if (_termWs) {
    _termWs.close(1000);
    _termWs = null;
  }
  _clearSessionState();
}

/**
 * isTerminalConnected — true when the WebSocket is open.
 */
function isTerminalConnected() {
  return _termWs !== null && _termWs.readyState === WebSocket.OPEN;
}

function _scheduleReconnect() {
  if (_termReconnectTimer) return;
  _termReconnectTimer = setTimeout(function () {
    _termReconnectTimer = null;
    if (_term) {
      _term.clear();
    }
    connectTerminal();
    // Exponential backoff: 1s → 2s → 4s → ... → 30s max.
    _termReconnectDelay = Math.min(_termReconnectDelay * 2, 30000);
  }, _termReconnectDelay);
}

function _clearTermScreen() {
  if (!_term) return;
  // Clear scrollback, then clear visible screen and reset cursor.
  // _term.clear() only clears scrollback; the ANSI sequence clears
  // the visible viewport so no stale content remains.
  _term.clear();
  _term.write("\x1b[2J\x1b[H");
}

function _deferTermFocus() {
  setTimeout(function () {
    if (_term) _term.focus();
  }, 0);
}

function _hideTermPanel() {
  var panel = document.getElementById("status-bar-panel");
  var handle = document.getElementById("status-bar-panel-resize");
  var btn = document.getElementById("status-bar-terminal-btn");
  var tabBar = document.getElementById("terminal-tab-bar");
  if (panel) panel.classList.add("hidden");
  if (handle) handle.classList.add("hidden");
  if (btn) btn.setAttribute("aria-expanded", "false");
  if (tabBar) tabBar.hidden = true;
}

// --- Session message handlers ---

function _clearSessionState() {
  for (var id in _sessions) {
    removeTerminalTab(id);
  }
  _sessions = {};
  _activeSessionId = null;
  _termTabCounter = 0;
}

function _handleSessionsList(sessions) {
  if (!sessions) return;
  // Build a set of IDs from the server.
  var serverIds = {};
  for (var i = 0; i < sessions.length; i++) {
    serverIds[sessions[i].id] = true;
  }
  // Remove tabs for sessions no longer on the server.
  for (var id in _sessions) {
    if (!serverIds[id]) {
      removeTerminalTab(id);
      delete _sessions[id];
    }
  }
  // Add tabs for new sessions.
  for (var j = 0; j < sessions.length; j++) {
    var s = sessions[j];
    if (!_sessions[s.id]) {
      _sessions[s.id] = { buffer: [] };
      var tabLabel = s.container
        ? s.container.length > 24
          ? s.container.slice(0, 24) + "\u2026"
          : s.container
        : null;
      addTerminalTab(s.id, tabLabel);
    }
    if (s.active && _activeSessionId !== s.id) {
      var prevActive = _activeSessionId;
      _activeSessionId = s.id;
      activateTerminalTab(s.id);
      // Clear xterm and restore the new active session's buffer when switching.
      if (_term && prevActive) {
        _clearTermScreen();
        if (_sessions[s.id]) {
          var buf = _sessions[s.id].buffer;
          for (var k = 0; k < buf.length; k++) {
            _term.write(buf[k]);
          }
        }
      }
    } else if (s.active) {
      _activeSessionId = s.id;
      activateTerminalTab(s.id);
    }
  }
  if (_activeSessionId) _deferTermFocus();
}

function _handleSessionSwitched(id) {
  _activeSessionId = id;
  activateTerminalTab(id);
  // Restore the target session's buffer.
  if (_term) {
    _clearTermScreen();
    if (_sessions[id]) {
      var buf = _sessions[id].buffer;
      for (var i = 0; i < buf.length; i++) {
        _term.write(buf[i]);
      }
    }
    _deferTermFocus();
  }
}

function _handleSessionClosed(id) {
  removeTerminalTab(id);
  delete _sessions[id];
}

function _handleSessionExited(id) {
  if (id === _activeSessionId && _term) {
    _term.write("\r\n\x1b[33mSession ended.\x1b[0m\r\n");
  }
  removeTerminalTab(id);
  delete _sessions[id];
}

// --- Container picker ---

var _containerPickerEl = null;

function _showContainerPicker() {
  // Toggle off if already open.
  if (_containerPickerEl) {
    _dismissContainerPicker();
    return;
  }

  var containerUrl =
    typeof routes !== "undefined" && routes.containers
      ? routes.containers.list()
      : "/api/containers";
  var token =
    typeof getWallfacerToken === "function" ? getWallfacerToken() : "";
  var headers = {};
  if (token) headers["Authorization"] = "Bearer " + token;

  fetch(containerUrl, { headers: headers })
    .then(function (resp) {
      return resp.json();
    })
    .then(function (containers) {
      _renderContainerPicker(containers || []);
    })
    .catch(function () {
      _renderContainerPicker([]);
    });
}

function _renderContainerPicker(containers) {
  _dismissContainerPicker();

  var picker = document.createElement("div");
  picker.className = "terminal-container-picker";

  var running = containers.filter(function (c) {
    return c.state === "running";
  });

  if (running.length === 0) {
    var empty = document.createElement("div");
    empty.className = "terminal-container-picker__empty";
    empty.textContent = "No running containers";
    picker.appendChild(empty);
  } else {
    for (var i = 0; i < running.length; i++) {
      var c = running[i];
      var item = document.createElement("button");
      item.className = "terminal-container-picker__item";
      var label = c.task_title || c.name;
      if (c.id) label += " @ " + c.id.slice(0, 8);
      item.textContent = label;
      item.setAttribute("data-container-name", c.name);
      item.addEventListener("mousedown", function (e) {
        e.preventDefault();
      });
      (function (name) {
        item.addEventListener("click", function () {
          _dismissContainerPicker();
          if (_termWs && _termWs.readyState === WebSocket.OPEN) {
            _termWs.send(
              JSON.stringify({ type: "create_session", container: name }),
            );
          }
          _deferTermFocus();
        });
      })(c.name);
      picker.appendChild(item);
    }
  }

  // Position the picker above the container button using fixed positioning
  // to escape the panel's overflow:hidden clipping.
  var containerBtn = document.getElementById("terminal-container-btn");
  if (containerBtn) {
    var btnRect = containerBtn.getBoundingClientRect();
    picker.style.bottom = window.innerHeight - btnRect.top + 4 + "px";
    picker.style.right = window.innerWidth - btnRect.right + "px";
  }
  document.body.appendChild(picker);
  _containerPickerEl = picker;

  // Dismiss on click outside or Escape.
  setTimeout(function () {
    document.addEventListener("mousedown", _onPickerOutsideClick);
    document.addEventListener("keydown", _onPickerEscape);
  }, 0);
}

function _dismissContainerPicker() {
  if (_containerPickerEl) {
    _containerPickerEl.remove();
    _containerPickerEl = null;
  }
  document.removeEventListener("mousedown", _onPickerOutsideClick);
  document.removeEventListener("keydown", _onPickerEscape);
}

function _onPickerOutsideClick(e) {
  if (_containerPickerEl && !_containerPickerEl.contains(e.target)) {
    _dismissContainerPicker();
  }
}

function _onPickerEscape(e) {
  if (e.key === "Escape") {
    _dismissContainerPicker();
    _deferTermFocus();
  }
}

// --- Terminal tab management ---

function _updateTabBarVisibility() {
  var tabBar = document.getElementById("terminal-tab-bar");
  var tabList = document.getElementById("terminal-tab-list");
  if (!tabBar || !tabList) return;
  tabBar.hidden = tabList.children.length === 0;
}

function addTerminalTab(sessionId, label) {
  var tabList = document.getElementById("terminal-tab-list");
  if (!tabList) return;

  if (!label) {
    _termTabCounter++;
    label = "Shell " + _termTabCounter;
  }

  var tab = document.createElement("div");
  tab.className = "terminal-tab";
  tab.setAttribute("data-session-id", sessionId);
  tab.setAttribute("aria-selected", "false");
  // Prevent tab clicks from stealing focus from xterm.
  tab.addEventListener("mousedown", function (e) {
    e.preventDefault();
  });

  var labelSpan = document.createElement("span");
  labelSpan.className = "terminal-tab__label";
  labelSpan.textContent = label;
  tab.appendChild(labelSpan);

  var closeBtn = document.createElement("button");
  closeBtn.className = "terminal-tab__close";
  closeBtn.setAttribute("aria-label", "Close session");
  closeBtn.setAttribute("tabindex", "-1");
  closeBtn.innerHTML = "\u00d7";
  closeBtn.addEventListener("mousedown", function (e) {
    e.preventDefault();
  });
  closeBtn.addEventListener("click", function (e) {
    e.stopPropagation();
    _onTabClose(sessionId);
  });
  tab.appendChild(closeBtn);

  tab.addEventListener("click", function () {
    _onTabClick(sessionId);
  });

  tabList.appendChild(tab);
  _updateTabBarVisibility();
}

function removeTerminalTab(sessionId) {
  var tabList = document.getElementById("terminal-tab-list");
  if (!tabList) return;
  var tab = tabList.querySelector('[data-session-id="' + sessionId + '"]');
  if (tab) tab.remove();
  _updateTabBarVisibility();
}

function activateTerminalTab(sessionId) {
  var tabList = document.getElementById("terminal-tab-list");
  if (!tabList) return;
  var tabs = tabList.querySelectorAll(".terminal-tab");
  for (var i = 0; i < tabs.length; i++) {
    tabs[i].setAttribute(
      "aria-selected",
      tabs[i].getAttribute("data-session-id") === sessionId ? "true" : "false",
    );
  }
}

function renameTerminalTab(sessionId, label) {
  var tabList = document.getElementById("terminal-tab-list");
  if (!tabList) return;
  var tab = tabList.querySelector('[data-session-id="' + sessionId + '"]');
  if (!tab) return;
  var labelSpan = tab.querySelector(".terminal-tab__label");
  if (labelSpan) labelSpan.textContent = label;
}

function setTabClickHandler(fn) {
  _onTabClick = fn;
}
function setTabCloseHandler(fn) {
  _onTabClose = fn;
}
function setTabAddHandler(fn) {
  _onTabAdd = fn;
}

window.initTerminal = initTerminal;
window.connectTerminal = connectTerminal;
window.disconnectTerminal = disconnectTerminal;
window.isTerminalConnected = isTerminalConnected;
window.addTerminalTab = addTerminalTab;
window.removeTerminalTab = removeTerminalTab;
window.activateTerminalTab = activateTerminalTab;
window.renameTerminalTab = renameTerminalTab;
window.setTabClickHandler = setTabClickHandler;
window.setTabCloseHandler = setTabCloseHandler;
window.setTabAddHandler = setTabAddHandler;
