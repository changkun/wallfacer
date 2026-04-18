// --- Spec Explorer ---
// Renders the spec tree from the GET /api/specs/tree API in the explorer
// panel when in spec mode. Shows status badges, recursive progress counts,
// and collapsible subtrees.

var _specTreeData = null;
var _specTreeTimer = null;
var _specStreamSource = null;
var _specStreamRetryDelay = 1000;
var _specExpandedPaths = new Set(
  JSON.parse(localStorage.getItem("wallfacer-spec-expanded") || "[]"),
);
var _explorerRootMode = "workspace"; // "workspace" | "specs"
var _specStatusFilter = localStorage.getItem("wallfacer-spec-filter") || "all";
var _specTextFilter = ""; // text query from the search box
var _selectedSpecPaths = new Set();
var _lastCheckedSpecIndex = -1;
var _showArchived =
  localStorage.getItem("wallfacer-spec-show-archived") === "true";

// getSpecIndex returns the current workspace's roadmap index (the
// specs/README.md metadata surfaced by GET /api/specs/tree) or null
// when no roadmap exists. Consumed by downstream modules (explorer
// pinned entry, layout state machine) that need to know whether to
// show the "Roadmap" pin or include specs/README.md in the focus
// resolution order.
function getSpecIndex() {
  if (!_specTreeData || !_specTreeData.index) return null;
  return _specTreeData.index;
}

// _syncSpecModeState mirrors the freshly fetched tree + index onto the
// shared specModeState object so spec-mode.js's layout state machine
// (and any other consumer) can read authoritative state without a
// second round-trip.
function _syncSpecModeState(data) {
  if (typeof specModeState === "undefined" || !specModeState) return;
  specModeState.tree = (data && data.nodes) || [];
  specModeState.index = (data && data.index) || null;
  // Nudge the dep-graph renderer so newly arrived spec nodes show up
  // without a second click. The renderer's fingerprint guards against
  // redundant re-renders.
  if (typeof window !== "undefined" && window.depGraphEnabled) {
    if (typeof scheduleRender === "function") scheduleRender();
    else if (typeof render === "function") render();
  }
}

// _firstLeafPath returns the path of the first leaf node in `nodes`,
// or the first node's path if no node is explicitly marked is_leaf.
// Used by the bootstrap choreography to auto-focus the first spec
// after a chat-first → three-pane transition.
function _firstLeafPath(nodes) {
  if (!Array.isArray(nodes) || nodes.length === 0) return "";
  for (var i = 0; i < nodes.length; i++) {
    if (nodes[i] && nodes[i].is_leaf && nodes[i].path) return nodes[i].path;
  }
  return nodes[0].path || "";
}

// Status → icon mapping.
var _specStatusIcons = {
  complete: "\u2705",
  validated: "\u2714",
  drafted: "\uD83D\uDCDD",
  vague: "\uD83D\uDCAD",
  stale: "\u26A0\uFE0F",
  archived: "\uD83D\uDCE6",
};

function loadSpecTree() {
  // Show loading indicator while fetching.
  var treeEl = document.getElementById("explorer-tree");
  if (treeEl && !_specTreeData) {
    treeEl.innerHTML = '<div class="spec-loading">Loading specs\u2026</div>';
  }

  fetch(Routes.specs.tree(), { headers: withBearerHeaders() })
    .then(function (r) {
      return r.json();
    })
    .then(function (data) {
      _specTreeData = data;
      _syncSpecModeState(data);
      renderSpecTree();
      // Update pane visibility based on whether specs exist.
      var hasSpecs = data && data.nodes && data.nodes.length > 0;
      if (typeof _updateSpecPaneVisibility === "function") {
        _updateSpecPaneVisibility(hasSpecs);
      }
    })
    .catch(function (err) {
      console.error("spec tree load error:", err);
      if (treeEl) {
        treeEl.innerHTML =
          '<div class="spec-loading">Failed to load specs</div>';
      }
      // On error, fall back to chat-only mode.
      if (typeof _updateSpecPaneVisibility === "function") {
        _updateSpecPaneVisibility(false);
      }
    });
}

// switchExplorerRoot switches the explorer panel between workspace files
// and spec tree views. Called by switchMode() in spec-mode.js.
function switchExplorerRoot(mode) {
  if (mode === _explorerRootMode) return;
  _explorerRootMode = mode;

  var treeEl = document.getElementById("explorer-tree");
  if (treeEl) treeEl.innerHTML = "";

  _stopSpecTreePoll();

  var filterEl = document.getElementById("spec-status-filter");
  if (filterEl) {
    filterEl.classList.toggle("hidden", mode !== "specs");
    if (mode === "specs") {
      _updateArchivedFilterOption();
      filterEl.value = _specStatusFilter;
    }
  }
  var archivedWrap = document.getElementById("spec-show-archived-wrap");
  if (archivedWrap && archivedWrap.classList) {
    archivedWrap.classList.toggle("hidden", mode !== "specs");
  }
  var archivedToggle = document.getElementById("spec-show-archived");
  if (archivedToggle && mode === "specs") {
    archivedToggle.checked = _showArchived;
  }
  var dispatchBar = document.getElementById("spec-dispatch-bar");
  if (dispatchBar) {
    dispatchBar.classList.toggle("hidden", mode !== "specs");
  }
  if (mode !== "specs") {
    _hideMinimap();
  }

  if (mode === "specs") {
    // Stop the workspace explorer's refresh poll so it doesn't overwrite
    // the spec tree with workspace files.
    if (typeof _stopExplorerRefreshPoll === "function") {
      _stopExplorerRefreshPoll();
    }
    loadSpecTree();
    _startSpecTreePoll();
  } else {
    // Restore workspace file explorer.
    if (typeof _loadExplorerRoots === "function") {
      _loadExplorerRoots();
    }
    if (typeof _startExplorerRefreshPoll === "function") {
      _startExplorerRefreshPoll();
    }
  }
}

function _startSpecTreePoll() {
  _stopSpecTreePoll();
  _startSpecTreeStream();
}

function _stopSpecTreePoll() {
  if (_specTreeTimer) {
    clearInterval(_specTreeTimer);
    _specTreeTimer = null;
  }
  _stopSpecTreeStream();
}

function _startSpecTreeStream() {
  _stopSpecTreeStream();
  if (!Routes || !Routes.specs || !Routes.specs.stream) return;

  var url = withAuthToken(Routes.specs.stream());
  _specStreamSource = new EventSource(url);
  _specStreamRetryDelay = 1000;

  _specStreamSource.addEventListener("snapshot", function (e) {
    _specStreamRetryDelay = 1000;
    try {
      // Capture the pre-update state so we can detect the
      // chat-first → three-pane transition that triggers the
      // first-spec bootstrap choreography.
      var prevEmpty =
        !_specTreeData ||
        (!(_specTreeData.nodes && _specTreeData.nodes.length > 0) &&
          !_specTreeData.index);

      _specTreeData = JSON.parse(e.data);
      _syncSpecModeState(_specTreeData);
      renderSpecTree();
      // Update pane visibility on live updates (specs may have been added/removed).
      var hasSpecs =
        _specTreeData && _specTreeData.nodes && _specTreeData.nodes.length > 0;
      if (typeof _updateSpecPaneVisibility === "function") {
        _updateSpecPaneVisibility(hasSpecs);
      }

      // Chat-first → three-pane transition driven by a newly-created
      // spec: kick the bootstrap choreography (auto-focus + toast).
      // The layout transition itself is already in flight via
      // _updateSpecPaneVisibility / _applyLayout above.
      if (
        prevEmpty &&
        hasSpecs &&
        typeof BootstrapChoreography !== "undefined" &&
        BootstrapChoreography
      ) {
        var firstNode = _firstLeafPath(_specTreeData.nodes);
        if (firstNode) {
          var ws =
            typeof activeWorkspaces !== "undefined" &&
            activeWorkspaces &&
            activeWorkspaces.length > 0
              ? activeWorkspaces[0]
              : "";
          BootstrapChoreography.trigger(firstNode, ws);
        }
      }
    } catch (err) {
      console.error("spec stream parse error:", err);
    }
  });

  _specStreamSource.addEventListener("heartbeat", function () {
    // Connection alive — nothing to do.
  });

  _specStreamSource.onerror = function () {
    if (
      _specStreamSource &&
      _specStreamSource.readyState === EventSource.CLOSED
    ) {
      _specStreamSource = null;
      var jittered = _specStreamRetryDelay * (1 + Math.random());
      _specTreeTimer = setTimeout(_startSpecTreeStream, jittered);
      _specStreamRetryDelay = Math.min(_specStreamRetryDelay * 2, 30000);
    }
  };
}

function _stopSpecTreeStream() {
  if (_specStreamSource) {
    _specStreamSource.close();
    _specStreamSource = null;
  }
}

// filterSpecTree sets the status filter and re-renders.
function filterSpecTree(filter) {
  _specStatusFilter = filter;
  localStorage.setItem("wallfacer-spec-filter", filter);
  renderSpecTree();
}

// toggleShowArchived flips the "Show archived" visibility, persists the choice,
// and force-collapses any archived parents so their subtrees do not flood the
// view when first revealed.
function toggleShowArchived(show) {
  _showArchived = !!show;
  localStorage.setItem("wallfacer-spec-show-archived", String(_showArchived));
  if (_showArchived) {
    _forceCollapseArchived();
  }
  // If the user had the "archived" filter active and now hid archived specs,
  // reset to "all" to avoid rendering an empty tree.
  if (!_showArchived && _specStatusFilter === "archived") {
    _specStatusFilter = "all";
    localStorage.setItem("wallfacer-spec-filter", "all");
    var filterEl = document.getElementById("spec-status-filter");
    if (filterEl) filterEl.value = "all";
  }
  _updateArchivedFilterOption();
  renderSpecTree();
}

// _forceCollapseArchived removes archived node paths from the expanded set so
// their subtrees render collapsed the first time the user toggles them on.
function _forceCollapseArchived() {
  if (!_specTreeData || !_specTreeData.nodes) return;
  var changed = false;
  var nodes = _specTreeData.nodes;
  for (var i = 0; i < nodes.length; i++) {
    var n = nodes[i];
    if (
      n.spec &&
      n.spec.status === "archived" &&
      _specExpandedPaths.has(n.path)
    ) {
      _specExpandedPaths.delete(n.path);
      changed = true;
    }
  }
  if (changed) {
    localStorage.setItem(
      "wallfacer-spec-expanded",
      JSON.stringify(Array.from(_specExpandedPaths)),
    );
  }
}

// _updateArchivedFilterOption adds (or updates) an `archived` option in the
// status filter dropdown. The option is disabled unless `_showArchived` is on,
// matching the rule that archived specs are hidden by default.
function _updateArchivedFilterOption() {
  var filterEl = document.getElementById("spec-status-filter");
  if (!filterEl) return;
  var existing = null;
  if (filterEl.querySelectorAll) {
    var opts = filterEl.querySelectorAll("option");
    for (var i = 0; i < opts.length; i++) {
      if (opts[i].value === "archived") {
        existing = opts[i];
        break;
      }
    }
  }
  if (!existing) {
    existing = document.createElement("option");
    existing.value = "archived";
    existing.textContent = "Archived";
    if (filterEl.appendChild) filterEl.appendChild(existing);
  }
  existing.disabled = !_showArchived;
  existing.title = _showArchived
    ? ""
    : "Enable 'Show archived' to filter by this status";
}

// setSpecTextFilter updates the text filter and re-renders the spec tree.
function setSpecTextFilter(query) {
  _specTextFilter = (query || "").toLowerCase();
  // Only re-render the spec tree when actually in spec mode.
  // Calling renderSpecTree in board mode overwrites the workspace file tree.
  if (_explorerRootMode === "specs") {
    renderSpecTree();
  }
}

// _nodeMatchesFilter checks if a node or any of its descendants match
// the current status filter and text query. Non-leaf nodes are visible
// if any descendant matches. Archived specs are hidden unless the user
// has explicitly opted in via the "Show archived" toggle.
function _nodeMatchesFilter(node, nodesByPath) {
  var spec = node.spec;
  if (!spec) return false;

  // Archived specs are invisible unless opted in.
  if (spec.status === "archived" && !_showArchived) return false;

  // Status filter.
  var statusMatch = true;
  if (_specStatusFilter !== "all") {
    if (_specStatusFilter === "incomplete") {
      statusMatch = spec.status !== "complete" && spec.status !== "archived";
    } else {
      statusMatch = spec.status === _specStatusFilter;
    }
  }

  // Text filter.
  var textMatch = true;
  if (_specTextFilter) {
    var title = (spec.title || "").toLowerCase();
    var path = (node.path || "").toLowerCase();
    textMatch =
      title.includes(_specTextFilter) || path.includes(_specTextFilter);
  }

  var selfMatch = statusMatch && textMatch;

  // Leaf nodes: match directly.
  if (node.is_leaf) return selfMatch;

  // Non-leaf: visible if self matches or any descendant matches.
  if (selfMatch) return true;
  var children = node.children || [];
  for (var i = 0; i < children.length; i++) {
    var child = nodesByPath[children[i]];
    if (child && _nodeMatchesFilter(child, nodesByPath)) return true;
  }
  return false;
}

// renderSpecTree renders the spec tree into the explorer-tree container.
function renderSpecTree() {
  var treeEl = document.getElementById("explorer-tree");
  if (!treeEl || !_specTreeData) return;

  // Build a lookup and group root nodes by track.
  var nodesByPath = {};
  var trackGroups = {}; // track name -> [nodes]
  var trackOrder = [];
  var nodes = _specTreeData.nodes || [];

  for (var i = 0; i < nodes.length; i++) {
    nodesByPath[nodes[i].path] = nodes[i];
  }

  for (var j = 0; j < nodes.length; j++) {
    if (nodes[j].depth === 0) {
      var trackName = (nodes[j].spec && nodes[j].spec.track) || "other";
      if (!trackGroups[trackName]) {
        trackGroups[trackName] = [];
        trackOrder.push(trackName);
      }
      trackGroups[trackName].push(nodes[j]);
    }
  }

  var html = "";

  // Pinned Roadmap entry: rendered above the track list when the backend
  // surfaced a specs/README.md index. The label is always "\uD83D\uDCCB Roadmap"
  // regardless of the file's H1 — a stable visual anchor per the
  // explorer-roadmap-entry spec.
  var indexMeta = _specTreeData && _specTreeData.index;
  if (indexMeta) {
    var focusedIsIndex =
      typeof isRoadmapFocused === "function" && isRoadmapFocused();
    html +=
      '<div class="spec-explorer-pinned spec-explorer-item--roadmap' +
      (focusedIsIndex ? " spec-explorer-pinned--focused" : "") +
      '" data-entry="index" role="button" tabindex="0">' +
      "\uD83D\uDCCB Roadmap" +
      "</div>";
  }

  for (var ti = 0; ti < trackOrder.length; ti++) {
    var track = trackOrder[ti];
    var trackExpanded =
      _specExpandedPaths.has("__track__" + track) || !!_specTextFilter;
    html +=
      '<div class="spec-track-header" data-track="' +
      escapeHtml(track) +
      '">' +
      '<span class="spec-node-toggle" data-path="__track__' +
      escapeHtml(track) +
      '">' +
      (trackExpanded ? "\u25BE" : "\u25B8") +
      "</span> " +
      '<span class="spec-track-name">' +
      escapeHtml(track) +
      "</span></div>";
    if (trackExpanded) {
      var trackRoots = trackGroups[track];
      for (var k = 0; k < trackRoots.length; k++) {
        html += _renderSpecNode(trackRoots[k], nodesByPath);
      }
    }
  }

  treeEl.innerHTML = html;

  // Attach click handlers.
  var nodeEls = treeEl.querySelectorAll("[data-spec-path]");
  for (var n = 0; n < nodeEls.length; n++) {
    nodeEls[n].addEventListener("click", _onSpecNodeClick);
  }

  // Pinned Roadmap entry: click or Enter/Space focuses the index.
  var pinnedEls = treeEl.querySelectorAll('[data-entry="index"]');
  for (var pi = 0; pi < pinnedEls.length; pi++) {
    pinnedEls[pi].addEventListener("click", _onSpecIndexClick);
    pinnedEls[pi].addEventListener("keydown", _onSpecIndexKeydown);
  }

  // Attach toggle handlers.
  var toggleEls = treeEl.querySelectorAll(".spec-node-toggle");
  for (var t = 0; t < toggleEls.length; t++) {
    toggleEls[t].addEventListener("click", _onSpecToggleClick);
  }

  // Track headers: clicking the header text also toggles.
  var trackHeaders = treeEl.querySelectorAll(".spec-track-header");
  for (var th = 0; th < trackHeaders.length; th++) {
    trackHeaders[th].addEventListener("click", function (e) {
      var track = e.currentTarget.getAttribute("data-track");
      if (!track) return;
      var key = "__track__" + track;
      if (_specExpandedPaths.has(key)) {
        _specExpandedPaths.delete(key);
      } else {
        _specExpandedPaths.add(key);
      }
      localStorage.setItem(
        "wallfacer-spec-expanded",
        JSON.stringify(Array.from(_specExpandedPaths)),
      );
      renderSpecTree();
    });
  }

  // Attach checkbox handlers.
  var checkboxEls = treeEl.querySelectorAll(".spec-select-checkbox");
  for (var c = 0; c < checkboxEls.length; c++) {
    checkboxEls[c].addEventListener("change", _onSpecCheckboxChange);
    checkboxEls[c].addEventListener("click", function (e) {
      e.stopPropagation();
    });
  }

  _updateDispatchSelectedButton();
}

function _renderSpecNode(node, nodesByPath) {
  if (!_nodeMatchesFilter(node, nodesByPath)) return "";

  var spec = node.spec;
  if (!spec) return "";

  var icon = _specStatusIcons[spec.status] || "";
  var title = spec.title || node.path;
  var isExpanded = _specExpandedPaths.has(node.path) || !!_specTextFilter;
  var hasChildren = node.children && node.children.length > 0;

  var progress = "";
  if (
    !node.is_leaf &&
    _specTreeData.progress &&
    _specTreeData.progress[node.path]
  ) {
    var p = _specTreeData.progress[node.path];
    progress =
      ' <span class="spec-node-progress">' +
      p.Complete +
      "/" +
      p.Total +
      "</span>";
  }

  var indent = node.depth * 16;
  var classes = "spec-node";
  if (node.is_leaf) classes += " spec-node--leaf";
  if (spec.status === "archived") classes += " spec-node--archived";
  if (
    typeof getFocusedSpecPath === "function" &&
    getFocusedSpecPath() === node.path
  ) {
    classes += " spec-node--focused";
  }

  var html =
    '<div class="' +
    classes +
    '" data-spec-path="' +
    escapeHtml(node.path) +
    '" style="padding-left: ' +
    indent +
    'px">';

  if (hasChildren) {
    html +=
      '<span class="spec-node-toggle" data-path="' +
      escapeHtml(node.path) +
      '">' +
      (isExpanded ? "\u25BE" : "\u25B8") +
      "</span> ";
  } else {
    html += '<span class="spec-node-toggle-placeholder"></span> ';
  }

  // Checkbox for validated leaf specs (multi-select dispatch).
  if (spec.status === "validated") {
    var checked = _selectedSpecPaths.has(node.path) ? " checked" : "";
    var unmetDeps = _unmetDependencies(spec);
    var disabled = unmetDeps.length > 0 ? " disabled" : "";
    var tooltip =
      unmetDeps.length > 0
        ? ' title="Blocked by: ' + escapeHtml(unmetDeps.join(", ")) + '"'
        : "";
    html +=
      '<input type="checkbox" class="spec-select-checkbox" data-spec-select="' +
      escapeHtml(node.path) +
      '"' +
      checked +
      disabled +
      tooltip +
      "> ";
  }

  html +=
    '<span class="spec-node-icon">' +
    icon +
    "</span> " +
    '<span class="spec-node-title">' +
    escapeHtml(title) +
    "</span>" +
    progress +
    "</div>";

  // Render children if expanded.
  if (hasChildren && isExpanded) {
    for (var i = 0; i < node.children.length; i++) {
      var child = nodesByPath[node.children[i]];
      if (child) {
        html += _renderSpecNode(child, nodesByPath);
      }
    }
  }

  return html;
}

function _onSpecNodeClick(e) {
  // Don't handle if the toggle arrow or checkbox was clicked.
  if (e.target.classList && e.target.classList.contains("spec-node-toggle")) {
    return;
  }
  if (
    e.target.classList &&
    e.target.classList.contains("spec-select-checkbox")
  ) {
    return;
  }
  var el = e.currentTarget;
  var path = el.getAttribute("data-spec-path");
  if (!path) return;

  // Determine workspace from activeWorkspaces.
  var ws =
    typeof activeWorkspaces !== "undefined" && activeWorkspaces.length > 0
      ? activeWorkspaces[0]
      : "";

  if (typeof focusSpec === "function") {
    focusSpec(path, ws);
    renderSpecTree(); // re-render to update focused highlight
  }
}

// _onSpecIndexClick focuses the pinned Roadmap entry — delegates to
// focusRoadmapIndex in spec-mode.js. The tree re-renders so the
// pinned row picks up the focused highlight.
function _onSpecIndexClick() {
  var idx = getSpecIndex();
  if (!idx) return;
  if (typeof focusRoadmapIndex === "function") {
    focusRoadmapIndex(idx);
    renderSpecTree();
  }
}

function _onSpecIndexKeydown(e) {
  if (e.key === "Enter" || e.key === " ") {
    e.preventDefault();
    _onSpecIndexClick();
  }
}

function _onSpecToggleClick(e) {
  e.stopPropagation();
  var path = e.currentTarget.getAttribute("data-path");
  if (!path) return;

  if (_specExpandedPaths.has(path)) {
    _specExpandedPaths.delete(path);
  } else {
    _specExpandedPaths.add(path);
  }

  localStorage.setItem(
    "wallfacer-spec-expanded",
    JSON.stringify(Array.from(_specExpandedPaths)),
  );
  renderSpecTree();
}

// _unmetDependencies returns a list of dependency paths from spec.depends_on
// that are not yet complete. Returns an empty array if all deps are met.
function _unmetDependencies(spec) {
  if (
    !spec.depends_on ||
    spec.depends_on.length === 0 ||
    !_specTreeData ||
    !_specTreeData.nodes
  )
    return [];
  var nodesByPath = {};
  for (var i = 0; i < _specTreeData.nodes.length; i++) {
    nodesByPath[_specTreeData.nodes[i].path] = _specTreeData.nodes[i];
  }
  var unmet = [];
  for (var j = 0; j < spec.depends_on.length; j++) {
    var depPath = spec.depends_on[j];
    var depNode = nodesByPath[depPath];
    if (!depNode || !depNode.spec || depNode.spec.status !== "complete") {
      // Show the title if available, otherwise the path.
      var label = depNode && depNode.spec ? depNode.spec.title : depPath;
      unmet.push(label);
    }
  }
  return unmet;
}

// --- Multi-select dispatch ---

function _onSpecCheckboxChange(e) {
  var path = e.target.getAttribute("data-spec-select");
  if (!path) return;

  // Shift-click range selection (shiftKey is available on change events
  // in most browsers; fall back to click listener if not).
  if (e.shiftKey) {
    var checkboxes = document.querySelectorAll
      ? Array.from(document.querySelectorAll(".spec-select-checkbox"))
      : [];
    var currentIndex = checkboxes.indexOf(e.target);
    if (_lastCheckedSpecIndex >= 0 && currentIndex >= 0) {
      var start = Math.min(_lastCheckedSpecIndex, currentIndex);
      var end = Math.max(_lastCheckedSpecIndex, currentIndex);
      for (var i = start; i <= end; i++) {
        var cbPath = checkboxes[i].getAttribute("data-spec-select");
        if (cbPath) {
          if (e.target.checked) {
            _selectedSpecPaths.add(cbPath);
          } else {
            _selectedSpecPaths.delete(cbPath);
          }
          checkboxes[i].checked = e.target.checked;
        }
      }
    }
  }

  if (e.target.checked) {
    _selectedSpecPaths.add(path);
  } else {
    _selectedSpecPaths.delete(path);
  }

  // Track last checked index for shift-click.
  var allCheckboxes = document.querySelectorAll
    ? Array.from(document.querySelectorAll(".spec-select-checkbox"))
    : [];
  _lastCheckedSpecIndex = allCheckboxes.indexOf(e.target);

  _updateDispatchSelectedButton();
}

function _updateDispatchSelectedButton() {
  var btn = document.getElementById("spec-dispatch-selected-btn");
  if (!btn) return;
  var count = _selectedSpecPaths.size;
  if (count > 0) {
    btn.classList.remove("hidden");
    btn.textContent = "Dispatch Selected (" + count + ")";
  } else {
    btn.classList.add("hidden");
  }
}

// dispatchSelectedSpecs dispatches all selected specs to the task board
// via the batch dispatch API. Clears selection on success.
function dispatchSelectedSpecs() {
  var paths = Array.from(_selectedSpecPaths);
  if (paths.length === 0) return;

  showConfirm("Dispatch " + paths.length + " specs to the task board?").then(
    function (confirmed) {
      if (!confirmed) return;

      var btn = document.getElementById("spec-dispatch-selected-btn");
      if (btn) {
        btn.disabled = true;
        btn.textContent = "Dispatching\u2026";
      }

      api(Routes.specs.dispatch(), {
        method: "POST",
        body: JSON.stringify({ paths: paths, run: false }),
      })
        .then(function (resp) {
          var dispatched = (resp && resp.dispatched) || [];
          var errors = (resp && resp.errors) || [];

          // Clear selection and uncheck all checkboxes.
          _selectedSpecPaths.clear();
          var checkboxes = document.querySelectorAll
            ? document.querySelectorAll(".spec-select-checkbox")
            : [];
          for (var i = 0; i < checkboxes.length; i++) {
            checkboxes[i].checked = false;
          }
          _updateDispatchSelectedButton();

          if (errors.length > 0 && dispatched.length > 0) {
            showAlert(
              "Dispatched " +
                dispatched.length +
                " specs. " +
                errors.length +
                " failed:\n" +
                errors
                  .map(function (e) {
                    return e.spec_path + ": " + e.error;
                  })
                  .join("\n"),
            );
          } else if (errors.length > 0) {
            showAlert(
              "Dispatch failed:\n" +
                errors
                  .map(function (e) {
                    return e.spec_path + ": " + e.error;
                  })
                  .join("\n"),
            );
          }
        })
        .catch(function (err) {
          showAlert("Dispatch failed: " + err.message);
        })
        .finally(function () {
          if (btn) {
            btn.disabled = false;
            btn.textContent = "Dispatch Selected (0)";
          }
        });
    },
  );
}
