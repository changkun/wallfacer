// --- Terminal: xterm.js integration with WebSocket PTY relay ---

var _term = null;
var _fitAddon = null;
var _termWs = null;
var _termReconnectTimer = null;
var _termReconnectDelay = 1000;

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
    background: _getCSSVar("--bg-card") || "#272420",
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

  var panel = document.getElementById("status-bar-panel");
  if (panel) {
    _term.open(panel);
    // Fit after open so dimensions are calculated correctly.
    try {
      _fitAddon.fit();
    } catch (_) {
      /* panel may be hidden */
    }
  }

  // Re-fit when the panel resizes (drag handle, window resize).
  if (panel && typeof ResizeObserver !== "undefined") {
    new ResizeObserver(function () {
      if (!panel.classList.contains("hidden")) {
        try {
          _fitAddon.fit();
        } catch (_) {
          /* ignore */
        }
      }
    }).observe(panel);
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
  var proto = location.protocol === "https:" ? "wss:" : "ws:";
  var url = proto + "//" + location.host + "/api/terminal/ws";
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

window.initTerminal = initTerminal;
window.connectTerminal = connectTerminal;
window.disconnectTerminal = disconnectTerminal;
window.isTerminalConnected = isTerminalConnected;
