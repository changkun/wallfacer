// --- Workspace / config controller ---
//
// Owns server-config hydration (fetchConfig), the workspace-browser UI, and
// workspace-group persistence. All functions here depend only on globals
// defined in state.js, helpers from transport.js (api/withAuthHeaders), and
// the generated Routes object.

// ---------------------------------------------------------------------------
// Sandbox state (set by fetchConfig, consumed by tasks UI)
// ---------------------------------------------------------------------------

let availableSandboxes = [];
let defaultSandbox = "";
let defaultSandboxByActivity = {};
let sandboxUsable = {};
let sandboxReasons = {};
let SANDBOX_ACTIVITY_KEYS = [
  "implementation",
  "testing",
  "refinement",
  "title",
  "oversight",
  "commit_message",
  "idea_agent",
];

// ---------------------------------------------------------------------------
// Sandbox helpers
// ---------------------------------------------------------------------------

function sandboxDisplayName(id) {
  if (!id) return "Default";
  if (id === "claude") return "Claude";
  if (id === "codex") return "Codex";
  return id.charAt(0).toUpperCase() + id.slice(1);
}

function populateSandboxSelects() {
  var selects = Array.from(
    document.querySelectorAll("select[data-sandbox-select]"),
  );
  for (var sel of selects) {
    if (!sel) continue;
    var current = sel.value;
    var defaultText = sel.dataset.defaultText || "Default";
    var includeDefault = sel.dataset.defaultOption !== "false";
    sel.innerHTML = "";
    if (includeDefault) {
      var effectiveDefault = sel.dataset.defaultSandbox || "";
      if (!effectiveDefault) {
        var matched = SANDBOX_ACTIVITY_KEYS.find(function (key) {
          return sel.id.endsWith("-" + key);
        });
        if (matched) {
          effectiveDefault =
            defaultSandboxByActivity[matched] || defaultSandbox || "";
        } else {
          effectiveDefault = defaultSandbox || "";
        }
      }
      var suffix = effectiveDefault
        ? " (" + sandboxDisplayName(effectiveDefault) + ")"
        : "";
      sel.innerHTML = '<option value="">' + defaultText + suffix + "</option>";
    }
    for (var s of availableSandboxes) {
      if (!s) continue;
      var opt = document.createElement("option");
      opt.value = s;
      var usable = sandboxUsable[s] !== false;
      opt.textContent =
        sandboxDisplayName(s) + (usable ? "" : " (unavailable)");
      if (!usable) {
        opt.disabled = true;
        if (sandboxReasons[s]) opt.title = sandboxReasons[s];
      }
      sel.appendChild(opt);
    }
    sel.value = current;
    if (sel.selectedIndex === -1 || sel.value !== current) {
      sel.value = "";
    }
  }
}

function collectSandboxByActivity(prefix) {
  var out = {};
  SANDBOX_ACTIVITY_KEYS.forEach(function (key) {
    var el = document.getElementById(prefix + key);
    if (!el) return;
    var value = (el.value || "").trim();
    if (value) out[key] = value;
  });
  return out;
}

function applySandboxByActivity(prefix, values) {
  var data = values || {};
  SANDBOX_ACTIVITY_KEYS.forEach(function (key) {
    var el = document.getElementById(prefix + key);
    if (!el) return;
    el.value = data[key] || "";
  });
}

// ---------------------------------------------------------------------------
// Server config hydration
// ---------------------------------------------------------------------------

async function fetchConfig() {
  try {
    var cfg = await api(Routes.config.get());
    activeWorkspaces = Array.isArray(cfg.workspaces)
      ? cfg.workspaces.slice()
      : [];
    workspaceGroups = Array.isArray(cfg.workspace_groups)
      ? cfg.workspace_groups.slice()
      : [];
    activeGroups = Array.isArray(cfg.active_groups) ? cfg.active_groups : [];
    workspaceBrowserPath =
      cfg.workspace_browser_path ||
      activeWorkspaces[0] ||
      workspaceBrowserPath ||
      "";
    workspacePickerRequired = activeWorkspaces.length === 0;
    var toggleMap = {
      autopilot: "autopilot-toggle",
      autorefine: "autorefine-toggle",
      autotest: "autotest-toggle",
      autosubmit: "autosubmit-toggle",
      autosync: "autosync-toggle",
      autopush: "autopush-toggle",
    };
    for (var key in toggleMap) {
      var el = document.getElementById(toggleMap[key]);
      if (el) el.checked = !!cfg[key];
    }
    // Keep globals in sync (they are used elsewhere).
    autopilot = !!cfg.autopilot;
    autorefine = !!cfg.autorefine;
    autotest = !!cfg.autotest;
    autosubmit = !!cfg.autosubmit;
    autosync = !!cfg.autosync;
    autopush = !!cfg.autopush;
    availableSandboxes = Array.isArray(cfg.sandboxes) ? cfg.sandboxes : [];
    defaultSandbox = cfg.default_sandbox || "";
    defaultSandboxByActivity = cfg.activity_sandboxes || {};
    sandboxUsable = cfg.sandbox_usable || {};
    sandboxReasons = cfg.sandbox_reasons || {};
    if (
      Array.isArray(cfg.sandbox_activities) &&
      cfg.sandbox_activities.length > 0
    ) {
      SANDBOX_ACTIVITY_KEYS = cfg.sandbox_activities;
    }
    if (typeof setBrainstormCategories === "function") {
      setBrainstormCategories(cfg.ideation_categories || []);
    }
    populateSandboxSelects();
    renderWorkspaceSelectionSummary();
    renderWorkspaceGroups();
    renderHeaderWorkspaceGroupTabs();
    if (workspacePickerRequired) {
      stopTasksStream();
      stopGitStream();
      resetBoardState();
      showWorkspacePicker(true);
    } else {
      hideWorkspacePicker();
      restartActiveStreams();
    }
    // Sync ideation toggle and spinner state.
    if (typeof updateIdeationConfig === "function") updateIdeationConfig(cfg);
    updateAutomationActiveCount();
    if (typeof updateWatcherHealth === "function")
      updateWatcherHealth(cfg.watcher_health || []);
    if (typeof reloadExplorerTree === "function") reloadExplorerTree();

    // Terminal feature gate: init xterm.js when enabled.
    terminalEnabled = !!cfg.terminal_enabled;
    if (typeof applyTerminalVisibility === "function")
      applyTerminalVisibility();
    if (terminalEnabled && typeof initTerminal === "function") initTerminal();
  } catch (e) {
    console.error("fetchConfig:", e);
  }
}

// ---------------------------------------------------------------------------
// Workspace picker modal
// ---------------------------------------------------------------------------

var _workspacePickerDismiss = null;
function showWorkspacePicker(required) {
  var modal = document.getElementById("workspace-picker");
  var closeBtn = document.getElementById("workspace-picker-close");
  var filterInput = document.getElementById("workspace-browser-filter");
  if (!modal) return;
  workspacePickerRequired = !!required;
  if (closeBtn) closeBtn.style.display = workspacePickerRequired ? "none" : "";
  modal.classList.remove("hidden");
  modal.classList.add("flex");
  workspaceBrowserFilterQuery = "";
  if (filterInput) filterInput.value = "";
  if (!workspaceSelectionDraft.length && activeWorkspaces.length) {
    workspaceSelectionDraft = activeWorkspaces.slice();
  }
  renderWorkspaceSelectionDraft();
  browseWorkspaces(workspaceBrowserPath || "");
  if (_workspacePickerDismiss) _workspacePickerDismiss();
  _workspacePickerDismiss = bindModalDismiss(modal, hideWorkspacePicker);
}

function hideWorkspacePicker() {
  if (workspacePickerRequired) return;
  var modal = document.getElementById("workspace-picker");
  if (!modal) return;
  modal.classList.add("hidden");
  modal.classList.remove("flex");
  if (_workspacePickerDismiss) {
    _workspacePickerDismiss();
    _workspacePickerDismiss = null;
  }
}

// ---------------------------------------------------------------------------
// Workspace rendering helpers
// ---------------------------------------------------------------------------

function renderWorkspaceSelectionSummary() {
  var el = document.getElementById("settings-workspace-list");
  if (!el) return;
  if (!activeWorkspaces.length) {
    el.innerHTML =
      '<div style="color:var(--text-muted);">No workspaces configured.</div>';
    return;
  }
  el.innerHTML = activeWorkspaces
    .map(function (path) {
      return (
        '<div style="font-family:monospace;font-size:11px;padding:6px 8px;border:1px solid var(--border);border-radius:6px;background:var(--bg-elevated);">' +
        escapeHtml(path) +
        "</div>"
      );
    })
    .join("");
}

function workspaceGroupLabel(group) {
  if (!group || !Array.isArray(group.workspaces) || !group.workspaces.length)
    return "Empty group";
  if (group.name) return group.name;
  var names = group.workspaces.map(function (path) {
    var clean = String(path || "").replace(/[\\/]+$/, "");
    var parts = clean.split(/[\\/]/);
    return parts[parts.length - 1] || clean;
  });
  return names.join(" + ");
}

function workspaceGroupsEqual(a, b) {
  if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length)
    return false;
  for (var i = 0; i < a.length; i += 1) {
    if (a[i] !== b[i]) return false;
  }
  return true;
}

// activeGroupBadgeHtml returns HTML for task count badges if the given
// workspace group has in-progress or waiting tasks. Returns "" when both are 0.
// For the viewed group, counts are computed from the live SSE-synced tasks
// array so badges update in real time. For background groups, server-side
// activeGroups data is used.
function activeGroupBadgeHtml(group) {
  var key = group.key || "";
  if (!key) return "";

  var inProgress = 0;
  var waiting = 0;

  // Check if this is the currently viewed group.
  var paths = Array.isArray(group.workspaces) ? group.workspaces : [];
  var isViewed = workspaceGroupsEqual(paths, activeWorkspaces);

  if (isViewed && typeof tasks !== "undefined") {
    // Compute from live task list for instant updates.
    for (var j = 0; j < tasks.length; j++) {
      var s = tasks[j].status;
      if (s === "in_progress" || s === "committing") inProgress++;
      else if (s === "waiting") waiting++;
    }
  } else {
    // Use server-side data for background groups.
    for (var i = 0; i < activeGroups.length; i++) {
      if (activeGroups[i].key !== key) continue;
      inProgress = activeGroups[i].in_progress || 0;
      waiting = activeGroups[i].waiting || 0;
      break;
    }
  }

  var parts = [];
  if (inProgress > 0) {
    parts.push(
      '<span class="badge badge-in_progress" style="font-size:9px;padding:1px 5px;" title="' +
        inProgress +
        ' running">' +
        '<span class="spinner" style="width:7px;height:7px;border-width:1.5px;vertical-align:middle;"></span> ' +
        inProgress +
        "</span>",
    );
  }
  if (waiting > 0) {
    parts.push(
      '<span class="badge badge-waiting" style="font-size:9px;padding:1px 5px;" title="' +
        waiting +
        ' waiting">' +
        waiting +
        "</span>",
    );
  }
  return parts.length > 0
    ? ' <span style="font-weight:400;margin-left:4px;display:inline-flex;gap:3px;vertical-align:middle;">' +
        parts.join("") +
        "</span>"
    : "";
}

function workspaceSwitchSpinnerHtml() {
  return '<span class="spinner" style="width:11px;height:11px;border-width:1.5px;vertical-align:middle;"></span>';
}

function setWorkspaceGroupSwitching(index, switching) {
  workspaceGroupSwitchingIndex = switching ? index : -1;
  workspaceGroupSwitching = !!switching;
  renderWorkspaceGroups();
  renderHeaderWorkspaceGroupTabs();
}

function renderWorkspaceGroups() {
  var el = document.getElementById("settings-workspace-groups");
  if (!el) return;
  if (!workspaceGroups.length) {
    el.innerHTML =
      '<div style="color:var(--text-muted);font-size:11px;">Saved workspace groups will appear here after you switch boards.</div>';
    return;
  }
  el.innerHTML = workspaceGroups
    .map(function (group, index) {
      var paths = Array.isArray(group.workspaces) ? group.workspaces : [];
      var active = workspaceGroupsEqual(paths, activeWorkspaces);
      var switching =
        workspaceGroupSwitching && workspaceGroupSwitchingIndex === index;
      return (
        '<div style="border:1px solid var(--border);border-radius:8px;padding:8px;background:var(--bg-elevated);display:flex;flex-direction:column;gap:8px;">' +
        '<div style="display:flex;align-items:center;justify-content:space-between;gap:8px;">' +
        '<div style="font-size:12px;font-weight:600;">' +
        escapeHtml(workspaceGroupLabel(group)) +
        (active
          ? ' <span style="font-size:10px;color:var(--text-muted);font-weight:500;">Current</span>'
          : "") +
        activeGroupBadgeHtml(group) +
        "</div>" +
        '<div style="display:flex;gap:6px;align-items:center;">' +
        '<button type="button" class="btn-icon" style="font-size:11px;padding:3px 8px;" onclick="useWorkspaceGroup(' +
        index +
        ')"' +
        (workspaceGroupSwitching ? " disabled" : "") +
        ">" +
        (switching ? workspaceSwitchSpinnerHtml() + " Switching..." : "Use") +
        "</button>" +
        '<button type="button" class="btn-ghost" style="font-size:11px;padding:3px 8px;" onclick="renameWorkspaceGroup(' +
        index +
        ')"' +
        (workspaceGroupSwitching ? " disabled" : "") +
        ">Rename</button>" +
        '<button type="button" class="btn-ghost" style="font-size:11px;padding:3px 8px;" onclick="editWorkspaceGroup(' +
        index +
        ')"' +
        (workspaceGroupSwitching ? " disabled" : "") +
        ">Edit</button>" +
        '<button type="button" class="btn-ghost" style="font-size:11px;padding:3px 8px;" onclick="deleteWorkspaceGroup(' +
        index +
        ')"' +
        (workspaceGroupSwitching ? " disabled" : "") +
        ">Remove</button>" +
        "</div>" +
        "</div>" +
        '<div style="display:flex;flex-direction:column;gap:4px;">' +
        paths
          .map(function (path) {
            return (
              '<div style="font-family:monospace;font-size:11px;color:var(--text-muted);word-break:break-all;">' +
              escapeHtml(path) +
              "</div>"
            );
          })
          .join("") +
        "</div>" +
        "</div>"
      );
    })
    .join("");
}

// updateWorkspaceGroupBadges updates only the badge portions of existing
// workspace group tabs without rebuilding the entire tab bar. Called from
// render() on every task state change for live badge updates.
function updateWorkspaceGroupBadges() {
  var el = document.getElementById("workspace-group-tabs");
  if (!el) return;
  var badges = el.querySelectorAll(".wg-badge");
  for (var i = 0; i < badges.length; i++) {
    var key = badges[i].getAttribute("data-wg-key");
    if (!key) continue;
    // Find the matching group to recompute the badge.
    for (var j = 0; j < workspaceGroups.length; j++) {
      if ((workspaceGroups[j].key || "") === key) {
        badges[i].innerHTML = activeGroupBadgeHtml(workspaceGroups[j]);
        break;
      }
    }
  }
}

var _tabOverflowObserver = null;

function renderHeaderWorkspaceGroupTabs() {
  var el = document.getElementById("workspace-group-tabs");
  if (!el) return;
  // Ensure the active group is never hidden.
  workspaceGroups.forEach(function (group, index) {
    var paths = Array.isArray(group.workspaces) ? group.workspaces : [];
    if (workspaceGroupsEqual(paths, activeWorkspaces)) {
      hiddenGroupIndices.delete(index);
    }
  });
  var tabs = "";
  workspaceGroups.forEach(function (group, index) {
    if (hiddenGroupIndices.has(index)) return;
    var paths = Array.isArray(group.workspaces) ? group.workspaces : [];
    var active = workspaceGroupsEqual(paths, activeWorkspaces);
    var switching =
      workspaceGroupSwitching && workspaceGroupSwitchingIndex === index;
    var cls = "workspace-group-tab";
    if (active) cls += " workspace-group-tab--active";
    if (switching) cls += " workspace-group-tab--switching";
    var badgeHtml =
      '<span class="wg-badge" data-wg-key="' +
      escapeHtml(group.key || "") +
      '">' +
      activeGroupBadgeHtml(group) +
      "</span>";
    var label = switching
      ? workspaceSwitchSpinnerHtml() +
        " " +
        escapeHtml(workspaceGroupLabel(group))
      : escapeHtml(workspaceGroupLabel(group)) + badgeHtml;
    var title = paths.join("\n");
    var closeBtn = active
      ? ""
      : '<span class="workspace-group-tab__close" onclick="event.stopPropagation();hideWorkspaceGroupTab(' +
        index +
        ')" title="Hide tab">&times;</span>';
    if (active) {
      tabs +=
        '<div class="' +
        cls +
        '" data-group-index="' +
        index +
        '" title="' +
        escapeHtml(title) +
        '" ondblclick="startInlineTabRename(this,' +
        index +
        ')">' +
        label +
        '<span class="workspace-group-tab__edit" onclick="event.stopPropagation();startInlineTabRename(this.parentElement,' +
        index +
        ')" title="Rename group">&#9998;</span>' +
        "</div>";
    } else {
      tabs +=
        '<button type="button" class="' +
        cls +
        '" data-group-index="' +
        index +
        '" title="' +
        escapeHtml(title) +
        '" onclick="useWorkspaceGroup(' +
        index +
        ')"' +
        (workspaceGroupSwitching ? " disabled" : "") +
        ">" +
        label +
        closeBtn +
        "</button>";
    }
  });
  // "+" button to add a workspace group tab.
  tabs +=
    '<button type="button" class="workspace-group-tab workspace-group-tab--add" onclick="addWorkspaceGroupTab(event)" title="Add workspace group">+</button>';
  el.innerHTML = tabs;
  // Auto-collapse overflowing tabs after layout.
  requestAnimationFrame(function () {
    _collapseOverflowingTabs();
  });
  _setupTabOverflowObserver();
}

// Detect and auto-hide tabs that overflow the container width.
function _collapseOverflowingTabs() {
  var el = document.getElementById("workspace-group-tabs");
  if (!el) return;
  if (!el.children) return;
  var children = Array.from(el.children);
  if (children.length <= 1) return;

  _tabCollapseRunning = true;

  // First, reset all non-manually-hidden tabs to visible.
  children.forEach(function (child) {
    child.style.display = "";
  });

  if (typeof el.getBoundingClientRect !== "function") {
    _tabCollapseRunning = false;
    return;
  }
  var containerRight = el.getBoundingClientRect().right;
  // The "+" button is always the last child; it must remain visible.
  var addBtn = children[children.length - 1];
  var collapsedIndices = [];

  // Walk tabs in reverse (excluding active and "+") and hide those that overflow.
  for (var i = children.length - 2; i >= 0; i--) {
    var tab = children[i];
    // Never hide the active tab.
    if (tab.classList.contains("workspace-group-tab--active")) continue;
    // Check if the "+" button overflows — if so, this tab needs to go.
    if (addBtn.getBoundingClientRect().right <= containerRight + 1) break;
    tab.style.display = "none";
    var idx = tab.dataset.groupIndex;
    if (idx !== undefined) collapsedIndices.push(parseInt(idx, 10));
  }

  // Store auto-collapsed indices so the "+" menu can show them.
  _autoCollapsedGroupIndices = collapsedIndices;
  _tabCollapseRunning = false;
}

var _autoCollapsedGroupIndices = [];
var _tabCollapseRunning = false;

function _setupTabOverflowObserver() {
  var el = document.getElementById("workspace-group-tabs");
  if (!el) return;
  if (_tabOverflowObserver) _tabOverflowObserver.disconnect();
  if (typeof ResizeObserver === "undefined") return;
  _tabOverflowObserver = new ResizeObserver(function () {
    if (_tabCollapseRunning) return;
    _collapseOverflowingTabs();
  });
  _tabOverflowObserver.observe(el);
}

function hideWorkspaceGroupTab(index) {
  hiddenGroupIndices.add(index);
  renderHeaderWorkspaceGroupTabs();
}

function addWorkspaceGroupTab(event) {
  // If there are hidden or auto-collapsed groups, show a picker; otherwise open the workspace picker.
  var hiddenGroups = [];
  workspaceGroups.forEach(function (group, index) {
    if (
      hiddenGroupIndices.has(index) ||
      _autoCollapsedGroupIndices.indexOf(index) !== -1
    ) {
      hiddenGroups.push({
        group: group,
        index: index,
        autoCollapsed: _autoCollapsedGroupIndices.indexOf(index) !== -1,
      });
    }
  });
  if (hiddenGroups.length === 0) {
    showWorkspacePicker(false);
    return;
  }
  // Show a popover positioned below the "+" button.
  var existing = document.getElementById("workspace-group-add-menu");
  if (existing) {
    existing.remove();
    return;
  }

  // Find the "+" button that triggered this.
  var btn = event && event.currentTarget;
  var menu = document.createElement("div");
  menu.id = "workspace-group-add-menu";
  menu.style.cssText =
    "position:fixed;z-index:50;min-width:200px;max-width:320px;padding:6px;border:1px solid var(--border);border-radius:8px;background:var(--bg-card);box-shadow:0 8px 24px rgba(0,0,0,0.18);";
  var html = "";
  hiddenGroups.forEach(function (item) {
    // Auto-collapsed tabs switch workspace; manually hidden tabs restore the tab.
    var action = item.autoCollapsed
      ? "document.getElementById('workspace-group-add-menu').remove();useWorkspaceGroup(" +
        item.index +
        ")"
      : "restoreWorkspaceGroupTab(" + item.index + ")";
    html +=
      '<button type="button" onclick="' +
      action +
      '" style="width:100%;text-align:left;padding:6px 8px;border:none;border-radius:6px;background:transparent;color:inherit;cursor:pointer;font-size:11px;" onmouseover="this.style.background=\'var(--bg-input)\'" onmouseout="this.style.background=\'transparent\'">' +
      escapeHtml(workspaceGroupLabel(item.group)) +
      "</button>";
  });
  html +=
    '<div style="border-top:1px solid var(--border);margin:4px 0;"></div>';
  html +=
    '<button type="button" onclick="document.getElementById(\'workspace-group-add-menu\').remove();showWorkspacePicker(false)" style="width:100%;text-align:left;padding:6px 8px;border:none;border-radius:6px;background:transparent;color:inherit;cursor:pointer;font-size:11px;" onmouseover="this.style.background=\'var(--bg-input)\'" onmouseout="this.style.background=\'transparent\'">New workspace group...</button>';
  menu.innerHTML = html;
  document.body.appendChild(menu);

  // Position below the "+" button.
  if (btn) {
    var rect = btn.getBoundingClientRect();
    menu.style.top = rect.bottom + 4 + "px";
    menu.style.left = rect.left + "px";
  }

  // Close on outside click.
  setTimeout(function () {
    document.addEventListener("click", function closeMenu(e) {
      if (!menu.contains(e.target)) {
        menu.remove();
        document.removeEventListener("click", closeMenu);
      }
    });
  }, 0);
}

function restoreWorkspaceGroupTab(index) {
  hiddenGroupIndices.delete(index);
  var menu = document.getElementById("workspace-group-add-menu");
  if (menu) menu.remove();
  renderHeaderWorkspaceGroupTabs();
}

// Keep these as no-ops for callers that still reference them.
function hideHeaderWorkspaceGroups() {}
function toggleHeaderWorkspaceGroups() {}

function _shortenPath(path) {
  // Detect home directory from the workspace browser starting path.
  // Matches /Users/x, /home/x, or C:\Users\x patterns.
  var m = path.match(/^(\/(?:Users|home)\/[^/]+|[A-Z]:\\Users\\[^\\]+)/);
  if (m) return "~" + path.substring(m[1].length);
  return path;
}

function renderWorkspaceSelectionDraft() {
  var el = document.getElementById("workspace-selection-list");
  if (!el) return;
  if (!workspaceSelectionDraft.length) {
    el.innerHTML =
      '<div style="font-size:11px;color:var(--text-muted);">No folders selected.</div>';
    return;
  }
  el.innerHTML = workspaceSelectionDraft
    .map(function (path) {
      return (
        '<div class="ws-selected-item" title="' +
        escapeHtml(path) +
        '">' +
        '<span class="ws-selected-item__path">' +
        escapeHtml(_shortenPath(path)) +
        "</span>" +
        '<button type="button" class="btn-ghost ws-selected-item__remove" data-workspace-path="' +
        escapeHtml(path) +
        '" onclick="removeWorkspaceSelection(this.dataset.workspacePath)">&times;</button>' +
        "</div>"
      );
    })
    .join("");
}

// ---------------------------------------------------------------------------
// Workspace browser
// ---------------------------------------------------------------------------

function renderWorkspaceBrowser() {
  var crumb = document.getElementById("workspace-browser-breadcrumb");
  var list = document.getElementById("workspace-browser-list");
  var entriesEl = document.getElementById("workspace-browser-entries");
  var visibleEntries = getVisibleWorkspaceBrowserEntries();
  // Render clickable breadcrumb path.
  if (crumb) {
    if (!workspaceBrowserPath) {
      crumb.innerHTML = "";
    } else {
      var sep = workspaceBrowserPath.includes("\\") ? "\\" : "/";
      var segments = workspaceBrowserPath.split(sep).filter(Boolean);
      var html =
        '<span style="color:var(--text-muted);">' + escapeHtml(sep) + "</span>";
      for (var s = 0; s < segments.length; s++) {
        var partial = sep + segments.slice(0, s + 1).join(sep);
        if (s > 0)
          html +=
            '<span style="color:var(--text-muted);">' +
            escapeHtml(sep) +
            "</span>";
        var isLast = s === segments.length - 1;
        html +=
          '<button type="button" onclick="browseWorkspaces(\'' +
          escapeHtml(partial) +
          '\')" style="border:none;background:none;color:' +
          (isLast ? "var(--text)" : "var(--accent)") +
          ";cursor:pointer;font-size:12px;padding:0;font-weight:" +
          (isLast ? "600" : "400") +
          ';">' +
          escapeHtml(segments[s]) +
          "</button>";
      }
      crumb.innerHTML = html;
    }
  }
  if (!list || !entriesEl) return;
  // Build entries with parent (..) at top.
  var rows = "";
  if (workspaceBrowserPath && workspaceBrowserPath !== "/") {
    var parentPath =
      workspaceBrowserPath.replace(/[\\/][^\\/]+[\\/]?$/, "") || "/";
    rows +=
      '<button type="button" class="ws-entry--parent" onclick="browseWorkspaces(\'' +
      escapeHtml(parentPath) +
      "')\"><span>..</span></button>";
  }
  if (!visibleEntries.length && !rows) {
    entriesEl.innerHTML =
      '<div style="font-size:11px;color:var(--text-muted);padding:8px;">' +
      (workspaceBrowserFilterQuery ? "No matches." : "Empty.") +
      "</div>";
    return;
  }
  rows += visibleEntries
    .map(function (entry) {
      var alreadySelected = workspaceSelectionDraft.includes(entry.path);
      var badge = entry.is_git_repo
        ? '<span class="ws-entry__badge">git</span>'
        : "";
      var addBtn = alreadySelected
        ? '<span class="ws-entry__added">added</span>'
        : '<button type="button" class="btn-ghost ws-entry__add" onclick="event.stopPropagation();addWorkspaceSelection(\'' +
          escapeHtml(entry.path) +
          "')\">+ Add</button>";
      var renameBtn =
        '<button type="button" class="btn-ghost ws-entry__rename" onclick="event.stopPropagation();renameWorkspaceBrowserEntry(\'' +
        escapeHtml(entry.path) +
        "','" +
        escapeHtml(entry.name) +
        '\')" title="Rename">&#9998;</button>';
      return (
        '<div class="ws-entry">' +
        '<button type="button" class="ws-entry__name" onclick="openWorkspaceBrowserEntry2(\'' +
        escapeHtml(entry.path) +
        "')\">" +
        "<span>" +
        escapeHtml(entry.name) +
        "</span>" +
        badge +
        "</button>" +
        renameBtn +
        addBtn +
        "</div>"
      );
    })
    .join("");
  entriesEl.innerHTML = rows;
}

function getVisibleWorkspaceBrowserEntries() {
  var query = (workspaceBrowserFilterQuery || "").trim().toLowerCase();
  if (!query) return workspaceBrowserEntries.slice();
  return workspaceBrowserEntries.filter(function (entry) {
    return (
      entry &&
      ((entry.name || "").toLowerCase().includes(query) ||
        (entry.path || "").toLowerCase().includes(query))
    );
  });
}

function setWorkspaceBrowserFilter(query) {
  workspaceBrowserFilterQuery = (query || "").trim();
  var visibleEntries = getVisibleWorkspaceBrowserEntries();
  workspaceBrowserFocusIndex = visibleEntries.length ? 0 : -1;
  renderWorkspaceBrowser();
}

function workspaceBrowserIncludeHidden() {
  var toggle = document.getElementById("workspace-browser-include-hidden");
  return !!(toggle && toggle.checked);
}

async function browseWorkspaces(path) {
  var pathInput = document.getElementById("workspace-browser-path");
  var status = document.getElementById("workspace-browser-status");
  var nextPath =
    typeof path === "string" ? path : pathInput ? pathInput.value.trim() : "";
  try {
    if (status) status.textContent = "Loading...";
    var url = Routes.workspaces.browse();
    var query = [];
    if (nextPath) {
      query.push("path=" + encodeURIComponent(nextPath));
    }
    if (workspaceBrowserIncludeHidden()) {
      query.push("include_hidden=true");
    }
    if (query.length > 0) {
      url += "?" + query.join("&");
    }
    var resp = await api(url);
    workspaceBrowserPath = resp.path || nextPath || "";
    workspaceBrowserEntries = Array.isArray(resp.entries) ? resp.entries : [];
    workspaceBrowserFocusIndex = getVisibleWorkspaceBrowserEntries().length
      ? 0
      : -1;
    if (pathInput) pathInput.value = workspaceBrowserPath;
    if (status)
      status.textContent = workspaceBrowserEntries.length
        ? ""
        : "No subdirectories found.";
    renderWorkspaceBrowser();
  } catch (e) {
    if (status) status.textContent = e.message;
    workspaceBrowserEntries = [];
    workspaceBrowserFocusIndex = -1;
    renderWorkspaceBrowser();
  }
}

function toggleWorkspaceBrowserHidden() {
  browseWorkspaces(workspaceBrowserPath || "");
}

function workspaceBrowserPathKeydown(event) {
  if (event.key === "Enter") {
    event.preventDefault();
    browseWorkspaces();
  }
}

function workspaceBrowserListKeydown(event) {
  var visibleEntries = getVisibleWorkspaceBrowserEntries();
  if (!visibleEntries.length) return;
  if (event.key === "ArrowDown") {
    event.preventDefault();
    workspaceBrowserFocusIndex = Math.min(
      visibleEntries.length - 1,
      workspaceBrowserFocusIndex + 1,
    );
    renderWorkspaceBrowser();
  } else if (event.key === "ArrowUp") {
    event.preventDefault();
    workspaceBrowserFocusIndex = Math.max(0, workspaceBrowserFocusIndex - 1);
    renderWorkspaceBrowser();
  } else if (event.key === "Enter") {
    event.preventDefault();
    if (event.metaKey || event.ctrlKey) {
      openWorkspaceBrowserEntry(workspaceBrowserFocusIndex);
      return;
    }
    addWorkspaceSelection(visibleEntries[workspaceBrowserFocusIndex].path);
  }
}

function selectWorkspaceBrowserEntry(index) {
  workspaceBrowserFocusIndex = index;
  renderWorkspaceBrowser();
}

function openWorkspaceBrowserEntry(index) {
  var entry = getVisibleWorkspaceBrowserEntries()[index];
  if (!entry) return;
  browseWorkspaces(entry.path);
}

function openWorkspaceBrowserEntry2(path) {
  browseWorkspaces(path);
}

function addCurrentWorkspaceFolder() {
  if (!workspaceBrowserPath) return;
  addWorkspaceSelection(workspaceBrowserPath);
}

async function createWorkspaceFolder() {
  if (!workspaceBrowserPath) return;
  var name = await showPrompt("New folder name:", "");
  if (name === null) return;
  name = name.trim();
  if (!name) return;
  try {
    await api(Routes.workspaces.mkdir(), {
      method: "POST",
      body: JSON.stringify({ path: workspaceBrowserPath, name: name }),
    });
    browseWorkspaces(workspaceBrowserPath);
  } catch (e) {
    showAlert("Failed to create folder: " + e.message);
  }
}

async function renameWorkspaceBrowserEntry(path, currentName) {
  var newName = await showPrompt("Rename folder:", currentName);
  if (newName === null) return;
  newName = newName.trim();
  if (!newName || newName === currentName) return;
  try {
    await api(Routes.workspaces.rename(), {
      method: "POST",
      body: JSON.stringify({ path: path, name: newName }),
    });
    browseWorkspaces(workspaceBrowserPath);
  } catch (e) {
    showAlert("Failed to rename folder: " + e.message);
  }
}

function addWorkspaceSelection(path) {
  if (!path) return;
  if (!workspaceSelectionDraft.includes(path)) {
    workspaceSelectionDraft.push(path);
  }
  renderWorkspaceSelectionDraft();
  renderWorkspaceBrowser();
}

function removeWorkspaceSelection(path) {
  workspaceSelectionDraft = workspaceSelectionDraft.filter(function (item) {
    return item !== path;
  });
  renderWorkspaceSelectionDraft();
  renderWorkspaceBrowser();
}

function clearWorkspaceSelection() {
  workspaceSelectionDraft = [];
  renderWorkspaceSelectionDraft();
}

// ---------------------------------------------------------------------------
// Workspace-group persistence and switching
// ---------------------------------------------------------------------------

async function saveWorkspaceGroups() {
  await api(Routes.config.update(), {
    method: "PUT",
    body: JSON.stringify({ workspace_groups: workspaceGroups.map(function (g) { return { name: g.name, workspaces: g.workspaces }; }) }),
  });
}

async function useWorkspaceGroup(index) {
  var group = workspaceGroups[index];
  if (!group || !Array.isArray(group.workspaces)) return;
  setWorkspaceGroupSwitching(index, true);
  workspaceSelectionDraft = group.workspaces.slice();
  renderWorkspaceSelectionDraft();
  try {
    await applyWorkspaceSelection();
  } finally {
    setWorkspaceGroupSwitching(-1, false);
  }
}

function editWorkspaceGroup(index) {
  var group = workspaceGroups[index];
  if (!group || !Array.isArray(group.workspaces)) return;
  workspaceSelectionDraft = group.workspaces.slice();
  showWorkspacePicker(false);
}

async function deleteWorkspaceGroup(index) {
  workspaceGroups = workspaceGroups.filter(function (_, i) {
    return i !== index;
  });
  renderWorkspaceGroups();
  renderHeaderWorkspaceGroupTabs();
  try {
    await saveWorkspaceGroups();
  } catch (e) {
    showAlert("Failed to update workspace groups: " + e.message);
    await fetchConfig();
  }
}

async function renameWorkspaceGroup(index) {
  var group = workspaceGroups[index];
  if (!group) return;
  var current = group.name || workspaceGroupLabel(group);
  var newName = await showPrompt("Rename workspace group:", current);
  if (newName === null) return;
  newName = newName.trim();
  group.name = newName; // empty string clears the custom name
  renderWorkspaceGroups();
  renderHeaderWorkspaceGroupTabs();
  try {
    await saveWorkspaceGroups();
  } catch (e) {
    showAlert("Failed to rename workspace group: " + e.message);
    await fetchConfig();
  }
}

function startInlineTabRename(tabEl, index) {
  var group = workspaceGroups[index];
  if (!group) return;
  var current = group.name || workspaceGroupLabel(group);
  var input = document.createElement("input");
  input.type = "text";
  input.value = current;
  input.className = "workspace-group-tab__rename-input";
  // Replace tab content with input.
  var origHtml = tabEl.innerHTML;
  tabEl.innerHTML = "";
  tabEl.appendChild(input);
  input.focus();
  input.select();

  var committed = false;
  function commit() {
    if (committed) return;
    committed = true;
    var newName = input.value.trim();
    group.name = newName;
    renderWorkspaceGroups();
    renderHeaderWorkspaceGroupTabs();
    saveWorkspaceGroups();
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
  input.addEventListener("blur", function () {
    commit();
  });
}

async function applyWorkspaceSelection() {
  var status = document.getElementById("workspace-apply-status");
  var settingsStatus = document.getElementById("settings-workspace-status");
  try {
    if (status) status.textContent = "Switching...";
    if (settingsStatus) settingsStatus.textContent = "Switching...";
    stopTasksStream();
    stopGitStream();
    resetBoardState();
    await api(Routes.workspaces.update(), {
      method: "PUT",
      body: JSON.stringify({ workspaces: workspaceSelectionDraft.slice() }),
    });
    activeWorkspaces = workspaceSelectionDraft.slice();
    workspacePickerRequired = activeWorkspaces.length === 0;
    await fetchConfig();
    hideHeaderWorkspaceGroups();
    if (status) status.textContent = "Saved.";
    if (settingsStatus) settingsStatus.textContent = "Updated.";
  } catch (e) {
    if (status) status.textContent = e.message;
    if (settingsStatus) settingsStatus.textContent = e.message;
    showAlert("Failed to switch workspaces: " + e.message);
  }
}
