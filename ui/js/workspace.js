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

// Active popover state (set when the sidebar ws-switch is open).
var _wsPopoverOpen = false;

// Retained for back-compat: some call sites (tests, legacy modules) still read
// these. The sidebar popover doesn't need auto-collapse.
var _autoCollapsedGroupIndices = [];

// renderHeaderWorkspaceGroupTabs — legacy name kept because many callers still
// reference it. The group switcher now lives in the sidebar (a single button
// that opens a popover); the header exposes branch tabs instead.
function renderHeaderWorkspaceGroupTabs() {
  renderSidebarWorkspaceSwitch();
}

function renderSidebarWorkspaceSwitch() {
  var nameEl = document.getElementById("sidebar-ws-name");
  var dotEl = document.getElementById("sidebar-ws-dot");
  var switchBtn = document.getElementById("sidebar-ws-switch");
  if (!switchBtn) return;
  // Locate the active group; fall back to basename of first workspace.
  var activeGroup = null;
  workspaceGroups.forEach(function (group) {
    var paths = Array.isArray(group.workspaces) ? group.workspaces : [];
    if (workspaceGroupsEqual(paths, activeWorkspaces)) activeGroup = group;
  });
  var label = activeGroup
    ? workspaceGroupLabel(activeGroup)
    : activeWorkspaces && activeWorkspaces.length
      ? (function () {
          var clean = String(activeWorkspaces[0] || "").replace(/[\\/]+$/, "");
          var parts = clean.split(/[\\/]/);
          return parts[parts.length - 1] || clean;
        })()
      : "workspace";
  if (nameEl) nameEl.textContent = label;
  if (dotEl) dotEl.textContent = (label.charAt(0) || "W").toUpperCase();
  switchBtn.title = label;

  // If the popover is open, rebuild its contents so live badges and the
  // active highlight stay in sync.
  if (_wsPopoverOpen) _renderWorkspaceGroupPopover();
}

function toggleWorkspaceGroupPopover(event) {
  if (event) event.stopPropagation();
  if (_wsPopoverOpen) {
    closeWorkspaceGroupPopover();
    return;
  }
  _openWorkspaceGroupPopover();
}

function _openWorkspaceGroupPopover() {
  var pop = document.getElementById("sidebar-ws-popover");
  var btn = document.getElementById("sidebar-ws-switch");
  if (!pop || !btn) return;
  _wsPopoverOpen = true;
  btn.setAttribute("aria-expanded", "true");
  pop.removeAttribute("hidden");
  _renderWorkspaceGroupPopover();
  _positionWorkspaceGroupPopover();
  // Outside-click dismisses. Defer registration so the triggering click
  // doesn't immediately close it.
  setTimeout(function () {
    document.addEventListener("click", _wsPopoverOutsideClick, true);
    document.addEventListener("keydown", _wsPopoverEscapeKey, true);
    if (typeof window !== "undefined" && window.addEventListener) {
      window.addEventListener("resize", _positionWorkspaceGroupPopover, true);
    }
  }, 0);
}

// _positionWorkspaceGroupPopover anchors the fixed-position popover directly
// below the ws-switch button. Width tracks the button when the sidebar is
// expanded; falls back to 220px when collapsed so it stays readable.
function _positionWorkspaceGroupPopover() {
  var pop = document.getElementById("sidebar-ws-popover");
  var btn = document.getElementById("sidebar-ws-switch");
  if (!pop || !btn) return;
  if (typeof btn.getBoundingClientRect !== "function") return;
  var rect = btn.getBoundingClientRect();
  var collapsed = rect.width < 80;
  var width = collapsed ? 220 : rect.width;
  pop.style.top = Math.round(rect.bottom + 4) + "px";
  pop.style.left = Math.round(rect.left) + "px";
  pop.style.width = width + "px";
}

function closeWorkspaceGroupPopover() {
  var pop = document.getElementById("sidebar-ws-popover");
  var btn = document.getElementById("sidebar-ws-switch");
  _wsPopoverOpen = false;
  if (btn) btn.setAttribute("aria-expanded", "false");
  if (pop) pop.setAttribute("hidden", "");
  document.removeEventListener("click", _wsPopoverOutsideClick, true);
  document.removeEventListener("keydown", _wsPopoverEscapeKey, true);
  if (typeof window !== "undefined" && window.removeEventListener) {
    window.removeEventListener("resize", _positionWorkspaceGroupPopover, true);
  }
}

function _wsPopoverOutsideClick(e) {
  var pop = document.getElementById("sidebar-ws-popover");
  var btn = document.getElementById("sidebar-ws-switch");
  if (!pop) return;
  if (pop.contains(e.target)) return;
  if (btn && btn.contains(e.target)) return;
  closeWorkspaceGroupPopover();
}

function _wsPopoverEscapeKey(e) {
  if (e.key === "Escape") closeWorkspaceGroupPopover();
}

function _renderWorkspaceGroupPopover() {
  var pop = document.getElementById("sidebar-ws-popover");
  if (!pop) return;
  var rows = "";
  workspaceGroups.forEach(function (group, index) {
    var paths = Array.isArray(group.workspaces) ? group.workspaces : [];
    var active = workspaceGroupsEqual(paths, activeWorkspaces);
    var switching =
      workspaceGroupSwitching && workspaceGroupSwitchingIndex === index;
    var cls = "sb-ws-popover__item" + (active ? " active" : "");
    var badgeHtml =
      '<span class="wg-badge" data-wg-key="' +
      escapeHtml(group.key || "") +
      '">' +
      activeGroupBadgeHtml(group) +
      "</span>";
    var icon = switching
      ? workspaceSwitchSpinnerHtml()
      : active
        ? '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><polyline points="20 6 9 17 4 12"></polyline></svg>'
        : '<span class="sb-ws-popover__spacer"></span>';
    var title = paths.join("\n");
    rows +=
      '<button type="button" class="' +
      cls +
      '" data-group-index="' +
      index +
      '" title="' +
      escapeHtml(title) +
      '" onclick="' +
      (active
        ? "closeWorkspaceGroupPopover()"
        : "closeWorkspaceGroupPopover();useWorkspaceGroup(" + index + ")") +
      '"' +
      (workspaceGroupSwitching && !active ? " disabled" : "") +
      ">" +
      '<span class="sb-ws-popover__check">' +
      icon +
      "</span>" +
      '<span class="sb-ws-popover__label">' +
      escapeHtml(workspaceGroupLabel(group)) +
      badgeHtml +
      "</span>" +
      "</button>";
  });
  rows +=
    '<div class="sb-ws-popover__divider"></div>' +
    '<button type="button" class="sb-ws-popover__item sb-ws-popover__add" onclick="closeWorkspaceGroupPopover();showWorkspacePicker(false)">' +
    '<span class="sb-ws-popover__check"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><line x1="12" y1="5" x2="12" y2="19"></line><line x1="5" y1="12" x2="19" y2="12"></line></svg></span>' +
    '<span class="sb-ws-popover__label">Add workspace group…</span>' +
    "</button>";
  pop.innerHTML = rows;
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
    body: JSON.stringify({
      workspace_groups: workspaceGroups.map(function (g) {
        return { name: g.name, workspaces: g.workspaces };
      }),
    }),
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

// _workspaceGroupKey returns a deterministic string identifier for a
// workspace path-set: the absolute paths sorted and joined with "\u0000"
// (NUL is illegal in POSIX paths, so it can't appear inside any path).
function _workspaceGroupKey(paths) {
  if (!paths || paths.length === 0) return "";
  return paths.slice().sort().join("\u0000");
}

// _seenWorkspaceGroups returns the set of group keys that have ever been
// activated on this device, persisted in localStorage so the markers
// survive reloads.
function _seenWorkspaceGroups() {
  if (typeof localStorage === "undefined") return {};
  try {
    var raw = localStorage.getItem("wallfacer-seen-workspace-groups");
    if (!raw) return {};
    var parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch (_) {
    return {};
  }
}

function _isUnseenWorkspaceGroup(paths) {
  var key = _workspaceGroupKey(paths);
  if (!key) return false;
  var seen = _seenWorkspaceGroups();
  return !seen[key];
}

function _rememberWorkspaceGroup(paths) {
  if (typeof localStorage === "undefined") return;
  var key = _workspaceGroupKey(paths);
  if (!key) return;
  var seen = _seenWorkspaceGroups();
  seen[key] = 1;
  try {
    localStorage.setItem(
      "wallfacer-seen-workspace-groups",
      JSON.stringify(seen),
    );
  } catch (_) {
    // Quota exceeded or storage disabled — best-effort only; the worst
    // case is that the next switch is treated as new again.
  }
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
    // Mark the newly activated workspace group as "fresh" only when its
    // path-set fingerprint hasn't been activated before. Switching back
    // to a long-used group (with its own saved Plan/Board preference)
    // must not re-force Plan and override the user's saved choice.
    if (
      typeof markWorkspaceIsNew === "function" &&
      _isUnseenWorkspaceGroup(activeWorkspaces)
    ) {
      markWorkspaceIsNew();
      _rememberWorkspaceGroup(activeWorkspaces);
    }
    // Tear down planning chat state — the server's planner now points at
    // a different workspace group's threads, but the cached UI threads,
    // tabs, and message bubbles all belong to the prior group and would
    // bleed into the new one until something forced a reload.
    if (typeof PlanningChat !== "undefined" && PlanningChat.reload) {
      PlanningChat.reload();
    }
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
