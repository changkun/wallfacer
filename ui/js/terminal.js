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
function _buildTermTheme() {
  return {
    background: _getCSSVar("--bg") || "#1a1917",
    foreground: _getCSSVar("--text") || "#cccccc",
    cursor: _getCSSVar("--accent") || "#4e8cff",
    selectionBackground: "rgba(78,140,255,0.3)",
  };
}

function initTerminal() {
  if (_term) return;
  if (typeof Terminal === "undefined") return;

  _term = new Terminal({
    cursorBlink: true,
    fontSize: 13,
    fontFamily: '"SF Mono", Menlo, Monaco, "Courier New", monospace',
    theme: _buildTermTheme(),
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

  // Wire the "+" button to the tab-add callback.
  var addBtn = document.getElementById("terminal-tab-add");
  if (addBtn) {
    addBtn.addEventListener("click", function () {
      _onTabAdd();
    });
  }

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
      _term.write(new Uint8Array(event.data));
    }
    // Text messages (pong) are silently ignored.
  };

  ws.onclose = function (event) {
    _termWs = null;
    if (event.code !== 1000) {
      _term.write("\r\n\x1b[33mDisconnected. Reconnecting...\x1b[0m\r\n");
      _scheduleReconnect();
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

  var labelSpan = document.createElement("span");
  labelSpan.className = "terminal-tab__label";
  labelSpan.textContent = label;
  tab.appendChild(labelSpan);

  var closeBtn = document.createElement("button");
  closeBtn.className = "terminal-tab__close";
  closeBtn.setAttribute("aria-label", "Close session");
  closeBtn.innerHTML = "\u00d7";
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
