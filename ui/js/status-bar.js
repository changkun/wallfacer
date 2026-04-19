// --- Status bar ---
// Thin always-visible footer that shows SSE connection health, active
// workspace, in-progress count, waiting count, and a stub terminal panel.

function initStatusBar() {
  // Keyboard shortcut: Ctrl+` cycles bottom panels:
  //   nothing open → terminal → dep graph → close (all hidden)
  document.addEventListener("keydown", function (e) {
    if (e.key !== "`" || !e.ctrlKey) return;
    if (e.metaKey || e.altKey || e.shiftKey) return;
    e.preventDefault();
    _cycleBottomPanel();
  });

  updateStatusBar();
}

function updateStatusBar() {
  _updateConnDot();
  _updateCounts();
  _updateWorkspace();
  renderPresence();
}

function _updateConnDot() {
  var dot = document.getElementById("status-bar-conn-dot");
  var label = document.getElementById("status-bar-conn-label");
  if (!dot || !label) return;

  // _sseConnState is maintained by api.js for both leader and follower tabs.
  var state = typeof _sseConnState !== "undefined" ? _sseConnState : "closed";

  dot.className = "status-bar-conn-dot status-bar-conn-dot--" + state;

  var labelText =
    state === "ok"
      ? "Connected"
      : state === "reconnecting"
        ? "Reconnecting…"
        : "Disconnected";
  label.textContent = labelText;
  dot.setAttribute("aria-label", labelText);
}

function _updateCounts() {
  var inProgressEl = document.getElementById("status-bar-in-progress");
  var waitingEl = document.getElementById("status-bar-waiting");
  if (!inProgressEl || !waitingEl) return;

  var taskList =
    typeof tasks !== "undefined" && Array.isArray(tasks) ? tasks : [];
  var inProgressCount = 0;
  var waitingCount = 0;
  for (var i = 0; i < taskList.length; i++) {
    var s = taskList[i].status;
    if (s === "in_progress" || s === "committing") inProgressCount++;
    else if (s === "waiting" || s === "failed") waitingCount++;
  }

  inProgressEl.textContent = String(inProgressCount);
  waitingEl.textContent = String(waitingCount);
}

function _updateWorkspace() {
  var el = document.getElementById("status-bar-workspace");
  if (!el) return;

  var workspaces =
    typeof activeWorkspaces !== "undefined" && Array.isArray(activeWorkspaces)
      ? activeWorkspaces
      : [];
  var groups =
    typeof workspaceGroups !== "undefined" && Array.isArray(workspaceGroups)
      ? workspaceGroups
      : [];

  if (workspaces.length === 0) {
    el.textContent = "";
    el.style.display = "none";
    return;
  }

  var label = "";
  // Prefer the active group name if available
  if (groups.length > 0) {
    var activeGroup = groups.find(function (g) {
      return (
        Array.isArray(g.workspaces) &&
        g.workspaces.length === workspaces.length &&
        g.workspaces.every(function (w, i) {
          return w === workspaces[i];
        })
      );
    });
    if (activeGroup && activeGroup.name) {
      label = activeGroup.name;
    }
  }

  // Fall back to basename of the first workspace path
  if (!label && workspaces[0]) {
    var parts = workspaces[0].replace(/\/$/, "").split("/");
    label = parts[parts.length - 1] || workspaces[0];
  }

  el.textContent = label;
  el.style.display = label ? "" : "none";
}

// Toggle the terminal panel via Ctrl+`. Dep Graph graduated to a full Workspace
// tab (see sidebar-nav-depgraph and switchMode('depgraph')).
function _cycleBottomPanel() {
  var termPanel = document.getElementById("status-bar-panel");
  var termOpen = termPanel && !termPanel.classList.contains("hidden");
  var termAvailable = typeof terminalEnabled !== "undefined" && terminalEnabled;

  if (termOpen) {
    _hideTerminalPanel();
  } else if (termAvailable) {
    _showTerminalPanel();
  }
}

function _showTerminalPanel() {
  var panel = document.getElementById("status-bar-panel");
  var handle = document.getElementById("status-bar-panel-resize");
  var btn = document.getElementById("status-bar-terminal-btn");
  var tabBar = document.getElementById("terminal-tab-bar");
  if (panel) panel.classList.remove("hidden");
  if (handle) handle.classList.remove("hidden");
  if (btn) btn.setAttribute("aria-expanded", "true");
  if (tabBar) tabBar.hidden = false;
  if (typeof connectTerminal === "function") connectTerminal();
}

function _hideTerminalPanel() {
  var panel = document.getElementById("status-bar-panel");
  var handle = document.getElementById("status-bar-panel-resize");
  var btn = document.getElementById("status-bar-terminal-btn");
  var tabBar = document.getElementById("terminal-tab-bar");
  if (panel) panel.classList.add("hidden");
  if (handle) handle.classList.add("hidden");
  if (btn) btn.setAttribute("aria-expanded", "false");
  if (tabBar) tabBar.hidden = true;
}

function toggleTerminalPanel() {
  var panel = document.getElementById("status-bar-panel");
  if (!panel) return;
  if (typeof terminalEnabled !== "undefined" && !terminalEnabled) {
    // Terminal disabled — show a message if panel is somehow opened.
    if (!panel.classList.contains("hidden")) {
      _hideTerminalPanel();
    }
    return;
  }
  var isHidden = panel.classList.contains("hidden");
  if (isHidden) {
    _showTerminalPanel();
  } else {
    _hideTerminalPanel();
  }
}

// ---------------------------------------------------------------------------
// Resizable terminal panel
// ---------------------------------------------------------------------------
var _panelMinHeight = 80;
var _panelMaxHeight = 600;
var _panelStorageKey = "wallfacer-panel-height";

function _initPanelResize() {
  var handle = document.getElementById("status-bar-panel-resize");
  var panel = document.getElementById("status-bar-panel");
  if (!handle || !panel) return;

  // Restore persisted height
  var stored = localStorage.getItem(_panelStorageKey);
  if (stored) {
    var h = parseInt(stored, 10);
    if (h >= _panelMinHeight && h <= _panelMaxHeight) {
      panel.style.height = h + "px";
    }
  }

  var startY = 0;
  var startH = 0;

  function onMouseMove(e) {
    // Panel grows upward: mouse moving up (smaller clientY) = larger panel
    var delta = startY - e.clientY;
    var newH = Math.min(
      _panelMaxHeight,
      Math.max(_panelMinHeight, startH + delta),
    );
    panel.style.height = newH + "px";
  }

  function onMouseUp() {
    document.removeEventListener("mousemove", onMouseMove);
    document.removeEventListener("mouseup", onMouseUp);
    handle.classList.remove("status-bar-panel-resize--active");
    document.body.style.userSelect = "";
    document.body.style.cursor = "";
    // Persist
    localStorage.setItem(_panelStorageKey, parseInt(panel.style.height, 10));
  }

  handle.addEventListener("mousedown", function (e) {
    e.preventDefault();
    startY = e.clientY;
    startH = panel.offsetHeight;
    handle.classList.add("status-bar-panel-resize--active");
    document.body.style.userSelect = "none";
    document.body.style.cursor = "ns-resize";
    document.addEventListener("mousemove", onMouseMove);
    document.addEventListener("mouseup", onMouseUp);
  });
}

// Apply terminal visibility gate based on terminalEnabled global.
function applyTerminalVisibility() {
  var btn = document.getElementById("status-bar-terminal-btn");
  if (!btn) return;
  if (typeof terminalEnabled !== "undefined" && terminalEnabled) {
    btn.classList.remove("hidden");
  } else {
    btn.classList.add("hidden");
  }
}

// ---------------------------------------------------------------------------
// Latere.ai sign-in badge (cloud mode only)
// ---------------------------------------------------------------------------

// renderSigninBadge populates #sidebar-signin based on the server config
// snapshot. Called by workspace.js after /api/config resolves. Local mode
// (config.cloud !== true) leaves the container empty — no "Sign in" link,
// no avatar, nothing that would suggest a cloud feature exists.
function renderSigninBadge(config) {
  var el = document.getElementById("sidebar-signin");
  if (!el) return;
  if (!config || config.cloud !== true) {
    el.innerHTML = "";
    _presenceSelf = null;
    renderPresence();
    return;
  }
  // Fire-and-forget; any fetch failure leaves the badge empty, which reads
  // correctly as "not signed in" without surfacing a broken affordance.
  fetch("/api/auth/me", { credentials: "same-origin" })
    .then(function (resp) {
      if (resp.status === 204) {
        el.innerHTML = '<a href="/login" class="sb-signin__link">Sign in</a>';
        return null;
      }
      if (resp.status === 200) return resp.json();
      return null;
    })
    .then(function (user) {
      if (!user) {
        _presenceSelf = null;
        renderPresence();
        return;
      }
      _renderSignedIn(el, user, config.auth_url);
      _presenceSelf = user;
      renderPresence();
    })
    .catch(function () {
      // Network error: leave empty; status-bar connection dot already
      // surfaces the real connectivity state.
    });
}

function _renderSignedIn(el, user, authURL) {
  // Prefer name; fall back to email so a user with only an email provider
  // still sees something meaningful.
  var display = user.name || user.email || "";
  var picture = user.picture || "";

  // Manual element construction (not innerHTML) so user-controlled strings
  // land as text nodes / attribute values. Avatar URL is still a URL — an
  // attacker-controlled picture field could only fetch their own image.
  var wrap = document.createElement("button");
  wrap.className = "sb-signin__user";
  wrap.type = "button";
  wrap.setAttribute("aria-haspopup", "menu");
  wrap.setAttribute("aria-expanded", "false");

  // Avatar: picture URL when provided, otherwise an initials circle so
  // the badge always has a visual anchor. Initials from display name
  // (first letter) uppercased.
  if (picture) {
    var img = document.createElement("img");
    img.className = "sb-signin__avatar";
    img.src = picture;
    img.alt = "";
    img.setAttribute("referrerpolicy", "no-referrer");
    wrap.appendChild(img);
  } else {
    var fallback = document.createElement("span");
    fallback.className = "sb-signin__avatar sb-signin__avatar--fallback";
    fallback.textContent = (display.charAt(0) || "?").toUpperCase();
    fallback.setAttribute("aria-hidden", "true");
    wrap.appendChild(fallback);
  }

  var nameEl = document.createElement("span");
  nameEl.className = "sb-signin__name";
  nameEl.textContent = display;
  wrap.appendChild(nameEl);

  // Current-view label sits right after the name so the user can see
  // what scope they're in at a glance. Populated by
  // _fetchAndRenderOrgSwitcher once /api/auth/orgs resolves; shows
  // "Personal" when no active org.
  var viewLabel = document.createElement("span");
  viewLabel.className = "sb-signin__view-label";
  viewLabel.textContent = "Personal";
  wrap.appendChild(viewLabel);

  var chevron = document.createElement("span");
  chevron.className = "sb-signin__chevron";
  chevron.setAttribute("aria-hidden", "true");
  chevron.textContent = "▾";
  wrap.appendChild(chevron);

  // Menu container (hidden by default). Populated with Personal +
  // org entries + Sign out by the fetch. Menu open/close toggles on
  // wrap click; click-outside closes it.
  var menu = document.createElement("div");
  menu.className = "sb-signin__menu";
  menu.setAttribute("role", "menu");
  menu.hidden = true;
  wrap.appendChild(menu);

  // Click toggles the menu. Cross-handler close-on-outside-click runs
  // once globally; add it now.
  wrap.addEventListener("click", function (e) {
    e.stopPropagation();
    var nowOpen = menu.hidden;
    menu.hidden = !nowOpen;
    wrap.setAttribute("aria-expanded", nowOpen ? "true" : "false");
  });
  document.addEventListener("click", function () {
    if (!menu.hidden) {
      menu.hidden = true;
      wrap.setAttribute("aria-expanded", "false");
    }
  });
  menu.addEventListener("click", function (e) {
    e.stopPropagation();
  });

  // Clear and attach. Also install the front-channel logout iframe so a
  // central sign-out at auth.latere.ai clears our cookie via
  // /logout/notify. Gate on authURL presence — if the server didn't
  // publish auth_url the iframe would load a relative "/logout" and
  // re-enter us in a loop.
  el.innerHTML = "";
  el.appendChild(wrap);

  // Fire the org-list fetch after the signed-in badge is already up.
  // The fetch populates the menu with "Personal" + org rows + Sign out.
  // Label updates to current org name when the response includes it.
  _fetchAndRenderOrgSwitcher(menu, viewLabel);
  if (authURL) {
    var frame = document.createElement("iframe");
    frame.name = "latere-logout-iframe";
    frame.src = authURL.replace(/\/$/, "") + "/logout";
    frame.style.display = "none";
    frame.setAttribute("sandbox", "allow-scripts allow-same-origin");
    frame.setAttribute("aria-hidden", "true");
    el.appendChild(frame);
  }
}

// _fetchAndRenderOrgSwitcher queries /api/auth/orgs and populates
// the badge menu + current-view label. Menu is always populated with
// at least a "Personal" entry and a "Sign out" entry so the user
// always has something to click even with zero org memberships.
//
// Menu layout:
//   ☑ Personal                 (checked when no active org)
//   ─────
//   ☑ Org A
//   ☐ Org B                    (when user has ≥1 org)
//   ─────
//   Sign out
function _fetchAndRenderOrgSwitcher(menu, viewLabel) {
  var addSeparator = function () {
    var hr = document.createElement("div");
    hr.className = "sb-signin__menu-sep";
    hr.setAttribute("role", "separator");
    menu.appendChild(hr);
  };
  var addItem = function (text, opts) {
    var item = document.createElement("button");
    item.type = "button";
    item.className = "sb-signin__menu-item";
    if (opts && opts.active) {
      item.classList.add("sb-signin__menu-item--active");
      item.setAttribute("aria-current", "true");
    }
    item.setAttribute("role", "menuitem");
    item.textContent = text;
    if (opts && opts.onClick) {
      item.addEventListener("click", opts.onClick);
    }
    menu.appendChild(item);
    return item;
  };

  var renderFallback = function () {
    // No data yet / 204 / error: show just Personal + Sign out so
    // the menu is never empty.
    addItem("Personal", { active: true });
    addSeparator();
    addItem("Sign out", {
      onClick: function () {
        window.location.href = "/logout";
      },
    });
  };

  fetch("/api/auth/orgs", { credentials: "same-origin" })
    .then(function (resp) {
      if (resp.status !== 200) return null;
      return resp.json();
    })
    .then(function (data) {
      menu.innerHTML = "";
      if (!data || !Array.isArray(data.orgs) || data.orgs.length === 0) {
        renderFallback();
        return;
      }
      var currentID = data.current_id || "";
      // Personal is always shown as a peer of the orgs. Clicking it
      // when already on personal is a no-op; clicking from an org
      // posts /api/auth/switch-org with org_id="" which clears
      // active_org on the SSO session and re-scopes the token.
      addItem("Personal", {
        active: !currentID,
        onClick: function () {
          if (!currentID) return;
          _switchOrg("");
        },
      });
      addSeparator();
      for (var i = 0; i < data.orgs.length; i += 1) {
        (function (o) {
          if (!o || !o.id) return;
          addItem(o.name || o.id, {
            active: o.id === currentID,
            onClick: function () {
              if (o.id === currentID) return;
              _switchOrg(o.id);
            },
          });
        })(data.orgs[i]);
      }
      addSeparator();
      addItem("Sign out", {
        onClick: function () {
          window.location.href = "/logout";
        },
      });

      // Label updates to the active org name, else "Personal".
      if (currentID) {
        for (var j = 0; j < data.orgs.length; j += 1) {
          if (data.orgs[j].id === currentID) {
            viewLabel.textContent = data.orgs[j].name || data.orgs[j].id;
            break;
          }
        }
      } else {
        viewLabel.textContent = "Personal";
      }
    })
    .catch(function () {
      menu.innerHTML = "";
      renderFallback();
    });
}

// _switchOrg POSTs /api/auth/switch-org and follows the redirect on
// success. Empty target means "go to personal view" (no active org).
function _switchOrg(target) {
  fetch("/api/auth/switch-org", {
    method: "POST",
    credentials: "same-origin",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ org_id: target }),
  })
    .then(function (resp) {
      if (resp.status !== 200) return null;
      return resp.json();
    })
    .then(function (body) {
      if (body && body.redirect_url) {
        window.location.href = body.redirect_url;
      }
    })
    .catch(function () {
      // Silent — the menu stays open for another try.
    });
}

// ---------------------------------------------------------------------------
// Presence (agents + self, bottom of left sidebar)
// ---------------------------------------------------------------------------

// _presenceSelf is set by renderSigninBadge when the user is signed in, so
// renderPresence can surface the signed-in user as a peer without re-fetching
// /api/auth/me on every task-stream tick.
var _presenceSelf = null;

// renderPresence rebuilds #sidebar-peers from the current `tasks` global and
// the cached signed-in user. Called from updateStatusBar on every task
// update. Empty in local mode with no active tasks — the `:empty` CSS rule
// hides the container, preserving the no-cloud-affordance invariant.
function renderPresence() {
  var el = document.getElementById("sidebar-peers");
  if (!el) return;

  var peers = [];

  // Self first (if signed in via cloud mode).
  if (_presenceSelf) {
    peers.push({
      kind: "self",
      status: "on",
      name: _presenceSelf.name || _presenceSelf.email || "you",
      workspace: _currentWorkspaceLabel(),
    });
  }

  // Active agents: one row per task that is running or awaiting feedback.
  // Done/failed/cancelled tasks are not shown — they aren't actively holding
  // a session in the way Presence conveys.
  var taskList =
    typeof tasks !== "undefined" && Array.isArray(tasks) ? tasks : [];
  for (var i = 0; i < taskList.length && peers.length < 8; i++) {
    var t = taskList[i];
    var s = t.status;
    var dot =
      s === "in_progress" || s === "committing"
        ? "on"
        : s === "waiting"
          ? "idle"
          : null;
    if (!dot) continue;
    peers.push({
      kind: "agent",
      status: dot,
      name: _peerAgentName(t),
      workspace: _peerWorkspaceLabel(t),
    });
  }

  // Always replace in one pass — no orphan nodes, no inline DOM patching
  // that could drift from the tasks list.
  el.innerHTML = "";
  if (peers.length === 0) return;

  var header = document.createElement("div");
  header.className = "sb-section";
  header.style.padding = "0 0 4px";
  header.textContent = "Presence";
  el.appendChild(header);

  for (var j = 0; j < peers.length; j++) {
    el.appendChild(_buildPeerRow(peers[j]));
  }
}

function _buildPeerRow(peer) {
  var row = document.createElement("div");
  row.className = "sb-peer";

  var dot = document.createElement("span");
  dot.className = "pd " + peer.status;
  row.appendChild(dot);

  var name = document.createElement("span");
  name.className = "pn";
  name.textContent = peer.name;
  row.appendChild(name);

  if (peer.workspace) {
    var ws = document.createElement("span");
    ws.className = "pw";
    ws.textContent = peer.workspace;
    row.appendChild(ws);
  }
  return row;
}

// _peerAgentName returns a short identifier for a task's agent. Prefer the
// sandbox kind so the user can tell claude/codex sessions apart, then fall
// back to a shortened task id.
function _peerAgentName(task) {
  var sb = task.sandbox || task.default_sandbox;
  var prefix = sb || "agent";
  var id = (task.id || "").split("-")[0] || "";
  return id ? prefix + "-" + id.slice(0, 4) : prefix;
}

// _peerWorkspaceLabel picks the workspace basename for a task, or falls back
// to a short branch hint if that's all we have. Trimmed to the CSS max-width.
function _peerWorkspaceLabel(task) {
  var ws = task.workspace_label || task.workspace_name || "";
  if (!ws && Array.isArray(task.workspaces) && task.workspaces[0]) {
    var parts = task.workspaces[0].replace(/\/$/, "").split("/");
    ws = parts[parts.length - 1] || task.workspaces[0];
  }
  return ws || "";
}

// _currentWorkspaceLabel reads the active workspace label for the self row,
// mirroring the logic _updateWorkspace uses for the bottom status bar.
function _currentWorkspaceLabel() {
  var list =
    typeof activeWorkspaces !== "undefined" && Array.isArray(activeWorkspaces)
      ? activeWorkspaces
      : [];
  if (list.length === 0) return "";
  var groups =
    typeof workspaceGroups !== "undefined" && Array.isArray(workspaceGroups)
      ? workspaceGroups
      : [];
  if (groups.length > 0) {
    var active = groups.find(function (g) {
      return (
        Array.isArray(g.workspaces) &&
        g.workspaces.length === list.length &&
        g.workspaces.every(function (w, i) {
          return w === list[i];
        })
      );
    });
    if (active && active.name) return active.name;
  }
  var parts = list[0].replace(/\/$/, "").split("/");
  return parts[parts.length - 1] || list[0];
}

// Expose globally to fit the existing vanilla-JS pattern
window.initStatusBar = initStatusBar;
window.updateStatusBar = updateStatusBar;
window.toggleTerminalPanel = toggleTerminalPanel;
window.applyTerminalVisibility = applyTerminalVisibility;
window.renderSigninBadge = renderSigninBadge;
window.renderPresence = renderPresence;

if (document.readyState === "loading") {
  document.addEventListener("DOMContentLoaded", function () {
    initStatusBar();
    _initPanelResize();
  });
} else {
  initStatusBar();
  _initPanelResize();
}

// --- System status (About tab) ---

// loadSystemStatus fetches runtime debug info and renders it in the
// About tab's system status section.
function loadSystemStatus() {
  var container = document.getElementById("about-system-status");
  var content = document.getElementById("about-system-status-content");
  if (!container || !content) return;

  api(Routes.debug.runtime())
    .then(function (data) {
      var lines = [];

      // Goroutines and memory.
      lines.push(
        "<div>Goroutines: <strong>" +
          (data.go_goroutine_count || 0) +
          "</strong> &middot; Heap: <strong>" +
          formatBytes(data.go_heap_alloc_bytes || 0) +
          "</strong></div>",
      );

      // Active containers.
      lines.push(
        "<div>Active containers: <strong>" +
          (data.active_containers || 0) +
          "</strong></div>",
      );

      // Container circuit breaker.
      if (data.container_circuit) {
        var cc = data.container_circuit;
        var ccColor =
          cc.state === "closed" ? "var(--text-muted)" : "var(--accent)";
        lines.push(
          '<div>Circuit breaker: <strong style="color:' +
            ccColor +
            '">' +
            cc.state +
            "</strong>" +
            (cc.failures > 0 ? " (" + cc.failures + " failures)" : "") +
            "</div>",
        );
      }

      // Worker stats.
      if (data.worker_stats) {
        var ws = data.worker_stats;
        var workerLine =
          "<div>Task workers: <strong>" +
          (ws.enabled ? "enabled" : "disabled") +
          "</strong> &middot; Active: <strong>" +
          (ws.active_workers || 0) +
          "</strong>";
        if (ws.creates > 0 || ws.execs > 0) {
          var total = (ws.execs || 0) + (ws.fallbacks || 0);
          var ratio =
            total > 0 ? Math.round(((ws.execs || 0) / total) * 100) : 0;
          workerLine +=
            " &middot; Creates: " +
            (ws.creates || 0) +
            " &middot; Execs: " +
            (ws.execs || 0) +
            (ws.fallbacks > 0 ? " &middot; Fallbacks: " + ws.fallbacks : "") +
            " &middot; Reuse: <strong>" +
            ratio +
            "%</strong>";
        }
        workerLine += "</div>";
        lines.push(workerLine);

        // Per-activity breakdown: show exec counts and which activity
        // triggered the worker creation (first-to-run for that task).
        if (ws.by_activity && Object.keys(ws.by_activity).length > 0) {
          var actParts = [];
          for (var act in ws.by_activity) {
            var a = ws.by_activity[act];
            var label = act + ": " + (a.execs || 0) + " exec";
            if (a.creates > 0) {
              label += " (" + a.creates + " triggered worker)";
            }
            actParts.push(label);
          }
          lines.push(
            '<div style="padding-left:12px;">' +
              actParts.join(" &middot; ") +
              "</div>",
          );
        }
      }

      // Task states.
      if (data.task_states) {
        var ts = data.task_states;
        var parts = [];
        if (ts.in_progress) parts.push(ts.in_progress + " running");
        if (ts.waiting) parts.push(ts.waiting + " waiting");
        if (ts.backlog) parts.push(ts.backlog + " backlog");
        if (ts.done) parts.push(ts.done + " done");
        if (ts.failed) parts.push(ts.failed + " failed");
        if (parts.length > 0) {
          lines.push("<div>Tasks: " + parts.join(" &middot; ") + "</div>");
        }
      }

      content.innerHTML = lines.join("");
      container.style.display = "";
    })
    .catch(function () {
      container.style.display = "none";
    });
}

function formatBytes(bytes) {
  if (bytes < 1024) return bytes + " B";
  if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + " KB";
  return (bytes / (1024 * 1024)).toFixed(1) + " MB";
}
