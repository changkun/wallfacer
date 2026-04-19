// depgraph.js — Panel-based DAG visualization of task dependencies.
//
// Assigns window.renderDependencyGraph(tasks) and window.hideDependencyGraph(),
// overriding the bezier-overlay implementation from dep-graph.js.
//
// The panel is inserted as a block element after the #board container so it
// does not disturb the board column layout.

(function () {
  "use strict";

  var PAD = 24;
  var NODE_W = 180;
  var NODE_H = 48;
  var H_GAP = 120;
  var V_GAP = 16;

  // Matches the tint+stroke palette defined on `.depgraph-mode-container`
  // in docs.css. Legacy renderer pulls the values live so theme changes
  // flow through without rebuilding the subgraph.
  var STATUS_COLORS = {
    backlog: "#8e8a80",
    in_progress: "#3a6db3",
    waiting: "#a56a12",
    committing: "#6a4aa3",
    done: "#3f7a4a",
    failed: "#a32d2d",
    cancelled: "#7a766e",
  };
  var STATUS_TINTS = {
    backlog: "rgba(142,138,128,0.12)",
    in_progress: "rgba(58,109,179,0.14)",
    waiting: "rgba(165,106,18,0.14)",
    committing: "rgba(106,74,163,0.14)",
    done: "rgba(63,122,74,0.14)",
    failed: "rgba(163,45,45,0.14)",
    cancelled: "rgba(122,118,110,0.12)",
  };

  // Module-level fingerprint cache — avoids re-rendering unchanged graphs.
  var _lastFingerprint = null;

  // Collapsed spec paths (persisted in localStorage). By default every
  // non-leaf spec is collapsed so the initial view is a high-level
  // overview; users drill in by clicking +/- handles. Expanded paths are
  // stored explicitly so the on-disk set is small and a new spec added
  // later stays collapsed until the user expands it.
  var _expandedSpecs = _loadExpandedSpecs();

  function _loadExpandedSpecs() {
    try {
      var raw = localStorage.getItem("depgraph-expanded-specs");
      if (!raw) return new Set();
      var arr = JSON.parse(raw);
      return new Set(Array.isArray(arr) ? arr : []);
    } catch (_e) {
      return new Set();
    }
  }

  function _saveExpandedSpecs() {
    try {
      localStorage.setItem(
        "depgraph-expanded-specs",
        JSON.stringify(Array.from(_expandedSpecs)),
      );
    } catch (_e) {
      // localStorage full or disabled — graceful degradation.
    }
  }

  // _collapsedSetFor returns the set of spec paths that are currently
  // *collapsed* (every non-leaf path except those the user explicitly
  // expanded). Passed to buildUnifiedGraph.
  function _collapsedSetFor(specNodes) {
    var collapsed = new Set();
    for (var i = 0; i < specNodes.length; i++) {
      var n = specNodes[i];
      if (!n || !n.spec) continue;
      if (n.is_leaf) continue;
      if (_expandedSpecs.has(n.path)) continue;
      collapsed.add(n.path);
    }
    return collapsed;
  }

  function _toggleSpecExpanded(path) {
    if (!path) return;
    if (_expandedSpecs.has(path)) _expandedSpecs.delete(path);
    else _expandedSpecs.add(path);
    _saveExpandedSpecs();
    // Invalidate fingerprint so the re-render isn't short-circuited.
    _lastFingerprint = null;
    if (typeof scheduleRender === "function") scheduleRender();
    else if (typeof render === "function") render();
  }

  // --- Pinned positions (drag-to-reposition) -------------------------------
  //
  // Each node's pinned (x, y) is persisted so dragged layouts survive a
  // reload. We store the full Map as an array of [id, {x,y}] pairs for
  // JSON compatibility.
  // Version stamp stored next to the pinned-positions blob. The Map
  // layout engine changed from force-directed to layered (Sugiyama), so
  // positions pinned under the old layout reference coords that make no
  // sense under the new layout (they all cluster in the force sim's
  // equilibrium region and visibly clash with the new column grid).
  // Bump this whenever a layout change invalidates the coordinate space.
  var PINNED_LAYOUT_VERSION = "sugiyama-v1";
  var _pinnedPositions = _loadPinnedPositions();

  function _loadPinnedPositions() {
    try {
      var version = localStorage.getItem("depgraph-pinned-version");
      if (version !== PINNED_LAYOUT_VERSION) {
        // Old pins were computed against a different layout — drop
        // them so the fresh layout gets a clean canvas.
        localStorage.removeItem("depgraph-pinned-positions");
        localStorage.setItem("depgraph-pinned-version", PINNED_LAYOUT_VERSION);
        return new Map();
      }
      var raw = localStorage.getItem("depgraph-pinned-positions");
      if (!raw) return new Map();
      var arr = JSON.parse(raw);
      if (!Array.isArray(arr)) return new Map();
      return new Map(
        arr.filter(function (e) {
          return (
            Array.isArray(e) &&
            e.length === 2 &&
            e[1] &&
            typeof e[1].x === "number" &&
            typeof e[1].y === "number"
          );
        }),
      );
    } catch (_e) {
      return new Map();
    }
  }

  function _savePinnedPositions() {
    try {
      localStorage.setItem(
        "depgraph-pinned-positions",
        JSON.stringify(Array.from(_pinnedPositions.entries())),
      );
    } catch (_e) {
      // localStorage full or disabled — pin only lives for this session.
    }
  }

  function _pinNode(id, x, y) {
    if (!id || typeof x !== "number" || typeof y !== "number") return;
    _pinnedPositions.set(id, { x: x, y: y });
    _savePinnedPositions();
    // Invalidate fingerprint so the next data-driven render applies the
    // pin. Don't schedule a render here — drag already committed the
    // new coords to the DOM via _shiftGroupCoords and re-routed every
    // incident edge through liveUpdateNode, so an immediate re-render
    // would just wipe and rebuild the SVG for zero visual change.
    _lastFingerprint = null;
  }

  function _unpinNode(id) {
    if (!id || !_pinnedPositions.has(id)) return;
    _pinnedPositions.delete(id);
    _savePinnedPositions();
    _lastFingerprint = null;
    _scheduleMapRender();
  }

  function _pinnedIds() {
    return new Set(_pinnedPositions.keys());
  }

  // --- Focus state ---------------------------------------------------------
  //
  // Focus is a session-only attention lens. Clicking a node sets it as the
  // focus: the renderer dims everything outside its 1-hop neighbourhood
  // and the viewport recentres on the focused node. Click the empty
  // canvas (or the same node again) to clear.
  var _focusedNodeId = null;

  function _focusNode(id) {
    // Toggle off if clicking the already-focused node (cheap "escape").
    if (id && id === _focusedNodeId) {
      _focusedNodeId = null;
    } else {
      _focusedNodeId = id || null;
    }
    _lastFingerprint = null;
    _scheduleMapRender();
    if (_focusedNodeId) _scrollFocusedIntoView(_focusedNodeId);
  }

  // Delegate navigation to the legacy click target (spec → Plan mode,
  // task → modal). Called by the renderer on shift+click.
  function _navigateNode(id, node) {
    if (!id) return;
    if (node && node.kind === "task") {
      var taskId = id.replace(/^task:/, "");
      if (typeof window.openTaskModal === "function")
        window.openTaskModal(taskId);
      else if (typeof window.openModal === "function") window.openModal(taskId);
      return;
    }
    if (node && node.kind === "spec") {
      var path = (node.extra && node.extra.path) || id.replace(/^spec:/, "");
      if (!path) return;
      if (typeof window.focusSpec === "function") {
        if (typeof window.switchMode === "function")
          window.switchMode("spec", { persist: true });
        window.focusSpec(path);
      }
    }
  }

  function _scheduleMapRender() {
    if (typeof scheduleRender === "function") scheduleRender();
    else if (typeof render === "function") render();
  }

  // _scrollFocusedIntoView brings the focused node's node group into the
  // visible viewport of #depgraph-mount on the next paint. The SVG has
  // been (re)rendered by the time this runs because scheduleRender also
  // goes through the RAF scheduler.
  function _scrollFocusedIntoView(id) {
    if (typeof requestAnimationFrame !== "function") return;
    requestAnimationFrame(function () {
      var mount = document.getElementById("depgraph-mount");
      if (!mount) return;
      var group = mount.querySelector('g[data-id="' + _escapeAttr(id) + '"]');
      if (!group) return;
      var rect = typeof group.getBBox === "function" ? group.getBBox() : null;
      if (!rect) return;
      // Centre the focused node in the mount's viewport by scrolling to
      // its top-left minus half the viewport size.
      mount.scrollTo({
        left: Math.max(0, rect.x + rect.width / 2 - mount.clientWidth / 2),
        top: Math.max(0, rect.y + rect.height / 2 - mount.clientHeight / 2),
        behavior: "smooth",
      });
    });
  }

  function _escapeAttr(s) {
    return String(s).replace(/"/g, '\\"');
  }

  // resetMapLayout clears every user-pinned position and the focus state
  // so the Map falls back to the auto-computed Sugiyama layout. Exposed
  // globally so the "Reset layout" button in the Map header can call it
  // directly.
  function resetMapLayout() {
    var hasState =
      _pinnedPositions.size !== 0 ||
      !!_focusedNodeId ||
      Math.abs(_zoomLevel - 1) > 1e-4 ||
      _searchQuery !== "";
    if (!hasState) return;
    _pinnedPositions = new Map();
    _focusedNodeId = null;
    _searchQuery = "";
    _savePinnedPositions();
    resetMapZoom();
    _lastFingerprint = null;
    _scheduleMapRender();
  }
  if (typeof window !== "undefined") {
    window.resetMapLayout = resetMapLayout;
  }

  // --- Show archived toggle -------------------------------------------------
  //
  // Archived specs and tasks are hidden by default (matches the explorer's
  // default). Flip the #depgraph-show-archived checkbox in the Map header
  // to include them; the state persists in localStorage so the next reload
  // honours the user's preference.
  var _showArchived = _loadShowArchived();

  function _loadShowArchived() {
    try {
      return localStorage.getItem("depgraph-show-archived") === "1";
    } catch (_e) {
      return false;
    }
  }

  function setMapShowArchived(checked) {
    _showArchived = !!checked;
    try {
      localStorage.setItem("depgraph-show-archived", _showArchived ? "1" : "0");
    } catch (_e) {
      // localStorage full/disabled — the toggle only lives for this session.
    }
    _lastFingerprint = null;
    _scheduleMapRender();
  }

  function _syncShowArchivedCheckbox() {
    var cb = document.getElementById("depgraph-show-archived");
    if (cb && cb.checked !== _showArchived) cb.checked = _showArchived;
  }

  // --- First-open centering -------------------------------------------------
  //
  // The Map's SVG can extend far past the visible viewport, and a fresh
  // page load starts the scroll at (0, 0). If node coords happen to sit
  // away from the top-left corner (common after layout sweeps push
  // barycentres into the middle of the canvas) the user sees blank space
  // and has to hunt for the graph.
  //
  // On the first render of a given Map-open session, and only while the
  // user hasn't scrolled yet, bring the content's bounding box into
  // view by scrolling to the content's centroid minus half the viewport.
  // Subsequent renders keep whatever scroll the user has set.
  var _centeredThisSession = false;

  function _centerIntoViewIfUntouched() {
    var mount = document.getElementById("depgraph-mount");
    if (!mount) return;
    if (_centeredThisSession) return;
    if (mount.scrollLeft !== 0 || mount.scrollTop !== 0) {
      _centeredThisSession = true;
      return;
    }
    if (typeof requestAnimationFrame !== "function") return;
    requestAnimationFrame(function () {
      var svg = document.getElementById("depgraph-svg");
      if (!svg || typeof svg.getBBox !== "function") return;
      var bbox;
      try {
        bbox = svg.getBBox();
      } catch (_e) {
        return;
      }
      if (!bbox || bbox.width <= 0) return;
      var centerX = bbox.x + bbox.width / 2;
      var centerY = bbox.y + bbox.height / 2;
      var targetX = Math.max(0, centerX - mount.clientWidth / 2);
      var targetY = Math.max(0, centerY - mount.clientHeight / 2);
      mount.scrollTo({ left: targetX, top: targetY, behavior: "instant" });
      _centeredThisSession = true;
    });
  }

  // Reset the centering flag whenever the user leaves Map mode so the
  // next open re-anchors the content. Called from _applyMode transitions
  // via window, since those live in a different module.
  function _resetMapCentering() {
    _centeredThisSession = false;
  }
  if (typeof window !== "undefined") {
    window._resetMapCentering = _resetMapCentering;
  }

  if (typeof window !== "undefined") {
    window.setMapShowArchived = setMapShowArchived;
  }

  // --- Zoom ----------------------------------------------------------------
  //
  // Zoom is a multiplier applied to the rendered SVG's width/height
  // attributes. The internal viewBox stays at the graph's layout coords
  // so node positions, drag math, and pinned coordinates all remain in
  // graph space. Scroll-based pan (overflow:auto + space-drag) keeps
  // working because the SVG simply becomes physically larger inside the
  // mount. The drag handler in unified-graph reads the scale via
  // opts.getScale() so a screen-pixel drag maps to dx/scale graph units.
  var ZOOM_MIN = 0.25;
  var ZOOM_MAX = 3.0;
  var _zoomLevel = _loadZoom();
  var _zoomInstalled = false;

  function _loadZoom() {
    try {
      var raw = localStorage.getItem("depgraph-zoom");
      var v = raw ? parseFloat(raw) : 1;
      if (!Number.isFinite(v) || v <= 0) return 1;
      return Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, v));
    } catch (_e) {
      return 1;
    }
  }

  function _saveZoom() {
    try {
      localStorage.setItem("depgraph-zoom", String(_zoomLevel));
    } catch (_e) {
      // disabled — zoom only lives for this session.
    }
  }

  function _getZoom() {
    return _zoomLevel;
  }

  function _setZoom(next, anchorX, anchorY) {
    var clamped = Math.max(ZOOM_MIN, Math.min(ZOOM_MAX, next));
    if (Math.abs(clamped - _zoomLevel) < 1e-4) return;
    var prev = _zoomLevel;
    var mount = document.getElementById("depgraph-mount");
    var svg = document.getElementById("depgraph-svg");
    // Anchor the zoom so the point under the cursor (or viewport centre
    // when anchors are missing) stays fixed on screen.
    var localX = 0;
    var localY = 0;
    if (mount && typeof mount.getBoundingClientRect === "function") {
      var r = mount.getBoundingClientRect();
      if (
        typeof anchorX === "number" &&
        typeof anchorY === "number" &&
        anchorX >= r.left &&
        anchorX <= r.right &&
        anchorY >= r.top &&
        anchorY <= r.bottom
      ) {
        localX = anchorX - r.left;
        localY = anchorY - r.top;
      } else {
        localX = mount.clientWidth / 2;
        localY = mount.clientHeight / 2;
      }
    }
    var graphX = mount ? (mount.scrollLeft + localX) / prev : 0;
    var graphY = mount ? (mount.scrollTop + localY) / prev : 0;

    _zoomLevel = clamped;
    _saveZoom();
    _applyZoomToSvg(svg);
    if (mount) {
      mount.scrollLeft = Math.max(0, graphX * clamped - localX);
      mount.scrollTop = Math.max(0, graphY * clamped - localY);
    }
  }

  function _applyZoomToSvg(svg) {
    if (!svg) return;
    var vb = svg.getAttribute("viewBox");
    if (!vb) return;
    var parts = vb.split(/\s+/);
    if (parts.length !== 4) return;
    var w = parseFloat(parts[2]);
    var h = parseFloat(parts[3]);
    if (!Number.isFinite(w) || !Number.isFinite(h)) return;
    svg.setAttribute("width", String(Math.round(w * _zoomLevel)));
    svg.setAttribute("height", String(Math.round(h * _zoomLevel)));
  }

  function _installZoom() {
    if (_zoomInstalled) return;
    if (typeof document === "undefined" || !document.addEventListener) return;
    _zoomInstalled = true;

    document.addEventListener(
      "wheel",
      function (e) {
        if (!_panActive()) return;
        var mount = _panMount();
        if (!mount || !mount.contains(e.target)) return;
        // Zoom on plain wheel + Cmd/Ctrl (standard trackpad pinch). A
        // plain wheel scrolls as usual so mousewheel navigation still
        // works for users who prefer it.
        if (!(e.ctrlKey || e.metaKey)) return;
        e.preventDefault();
        // deltaY-scaled factor so trackpad pinch (many small events) and
        // mouse wheel (fewer large events) both feel smooth. The exponent
        // keeps multiplications commutative so repeated events compose
        // cleanly.
        var factor = Math.exp(-e.deltaY * 0.01);
        _setZoom(_zoomLevel * factor, e.clientX, e.clientY);
      },
      { passive: false },
    );
  }

  function resetMapZoom() {
    if (Math.abs(_zoomLevel - 1) < 1e-4) return;
    _zoomLevel = 1;
    _saveZoom();
    _applyZoomToSvg(document.getElementById("depgraph-svg"));
  }

  // --- Search filter -------------------------------------------------------
  //
  // The search box in the Map header filters nodes by label substring.
  // State is kept in-memory for the session (no localStorage — users
  // want to clear on reload). Queue a re-render so the dim opacity is
  // reapplied across the SVG.
  var _searchQuery = "";

  function setMapSearch(q) {
    var v = (q == null ? "" : String(q)).trim();
    if (v === _searchQuery) return;
    _searchQuery = v;
    _lastFingerprint = null;
    _scheduleMapRender();
  }
  if (typeof window !== "undefined") {
    window.setMapSearch = setMapSearch;
  }

  // --- Hover highlight -----------------------------------------------------
  //
  // Hover is transient: enter raises the opacity of neighbour nodes and
  // edges while dimming the rest, leave restores. We mutate the DOM
  // in-place so hovering doesn't trigger a full re-render. Focus state
  // takes precedence — if a node is focused, the hover overlay yields.
  function _applyHoverHighlight(id) {
    var svg = document.getElementById("depgraph-svg");
    if (!svg) return;
    // Skip hover highlight while focus is active — focus is sticky.
    if (_focusedNodeId) return;
    var nodes = svg.querySelectorAll("g[data-id]");
    var edges = svg.querySelectorAll("path[data-kind]");
    if (!id) {
      nodes.forEach(function (n) {
        if (n.dataset.hoverDimmed === "1") {
          n.removeAttribute("opacity");
          delete n.dataset.hoverDimmed;
        }
      });
      edges.forEach(function (e) {
        if (e.dataset.hoverDimmed === "1") {
          e.removeAttribute("opacity");
          delete e.dataset.hoverDimmed;
        }
      });
      return;
    }
    // Derive neighbour set from the rendered DOM so we don't need the
    // graph object here.
    var neighbourSet = new Set([id]);
    edges.forEach(function (e) {
      var from = e.getAttribute("data-from");
      var to = e.getAttribute("data-to");
      if (from === id) neighbourSet.add(to);
      if (to === id) neighbourSet.add(from);
    });
    nodes.forEach(function (n) {
      var nid = n.getAttribute("data-id");
      if (neighbourSet.has(nid)) {
        if (n.dataset.hoverDimmed === "1") {
          n.removeAttribute("opacity");
          delete n.dataset.hoverDimmed;
        }
      } else if (!n.getAttribute("opacity")) {
        n.setAttribute("opacity", "0.35");
        n.dataset.hoverDimmed = "1";
      }
    });
    edges.forEach(function (e) {
      var from = e.getAttribute("data-from");
      var to = e.getAttribute("data-to");
      var inNeigh = neighbourSet.has(from) && neighbourSet.has(to);
      if (inNeigh) {
        if (e.dataset.hoverDimmed === "1") {
          e.removeAttribute("opacity");
          delete e.dataset.hoverDimmed;
        }
      } else if (!e.getAttribute("opacity")) {
        e.setAttribute("opacity", "0.2");
        e.dataset.hoverDimmed = "1";
      }
    });
  }

  // --- Canvas pan (hold Space + drag) --------------------------------------
  //
  // The Dep Graph canvas is an overflow:auto container with a potentially
  // wide SVG inside. By default the user can only scroll. Holding Space
  // turns the cursor into a grab handle; pressing the mouse button then
  // drives the container's scrollLeft/scrollTop directly so the user can
  // drag the canvas the way they would in Figma or Miro.
  //
  // Listeners are installed once (lazy, via _installPan) and gated on the
  // Dep Graph mode being active — outside Dep Graph mode every handler is
  // a cheap no-op.
  var _panInstalled = false;
  var _panKeyDown = false;
  var _panDragging = false;
  var _panStartMouseX = 0;
  var _panStartMouseY = 0;
  var _panStartScrollX = 0;
  var _panStartScrollY = 0;

  function _panActive() {
    return (
      typeof window !== "undefined" &&
      !!window.depGraphEnabled &&
      !!document.getElementById("depgraph-mount")
    );
  }

  function _panMount() {
    return document.getElementById("depgraph-mount");
  }

  function _installPan() {
    if (_panInstalled) return;
    if (typeof document === "undefined" || !document.addEventListener) return;
    _panInstalled = true;

    document.addEventListener("keydown", function (e) {
      if (!_panActive()) return;
      if (e.key !== " " && e.code !== "Space") return;
      // Don't hijack Space while typing in an input/textarea.
      var active = document.activeElement;
      if (
        active &&
        (active.tagName === "INPUT" ||
          active.tagName === "TEXTAREA" ||
          active.isContentEditable)
      ) {
        return;
      }
      if (_panKeyDown) {
        // Suppress the browser's page-scroll default while held.
        e.preventDefault();
        return;
      }
      _panKeyDown = true;
      var mount = _panMount();
      if (mount) mount.style.cursor = "grab";
      e.preventDefault();
    });

    document.addEventListener("keyup", function (e) {
      if (e.key !== " " && e.code !== "Space") return;
      _panKeyDown = false;
      _panDragging = false;
      var mount = _panMount();
      if (mount) mount.style.cursor = "";
    });

    // Blur/visibility changes can leave the "Space held" flag stuck if the
    // keyup never fires (e.g. tab switch). Reset defensively.
    window.addEventListener("blur", function () {
      _panKeyDown = false;
      _panDragging = false;
      var mount = _panMount();
      if (mount) mount.style.cursor = "";
    });

    document.addEventListener("mousedown", function (e) {
      if (!_panActive() || !_panKeyDown) return;
      var mount = _panMount();
      if (!mount || !mount.contains(e.target)) return;
      _panDragging = true;
      _panStartMouseX = e.clientX;
      _panStartMouseY = e.clientY;
      _panStartScrollX = mount.scrollLeft;
      _panStartScrollY = mount.scrollTop;
      mount.style.cursor = "grabbing";
      e.preventDefault();
    });

    document.addEventListener("mousemove", function (e) {
      if (!_panDragging) return;
      var mount = _panMount();
      if (!mount) return;
      var dx = e.clientX - _panStartMouseX;
      var dy = e.clientY - _panStartMouseY;
      mount.scrollLeft = _panStartScrollX - dx;
      mount.scrollTop = _panStartScrollY - dy;
    });

    document.addEventListener("mouseup", function () {
      if (!_panDragging) return;
      _panDragging = false;
      var mount = _panMount();
      if (mount) mount.style.cursor = _panKeyDown ? "grab" : "";
    });
  }

  // ---------------------------------------------------------------------------
  // Helpers
  // ---------------------------------------------------------------------------

  function hasDepEdge(t) {
    return !!(t.depends_on && t.depends_on.length > 0);
  }

  /** Return the subset of tasks that form the dependency subgraph. */
  function getSubgraph(tasks) {
    var involved = new Set();
    for (var i = 0; i < tasks.length; i++) {
      var t = tasks[i];
      if (t.depends_on && t.depends_on.length > 0) {
        involved.add(t.id);
        for (var j = 0; j < t.depends_on.length; j++)
          involved.add(t.depends_on[j]);
      }
    }
    return tasks.filter(function (t) {
      return involved.has(t.id);
    });
  }

  /**
   * Kahn's topological sort.
   *
   * Edge direction: if B.depends_on contains A, draw A→B (A is a prerequisite).
   * Level 0 = tasks with no prerequisites (inDegree 0 in the prerequisite graph).
   *
   * Returns { levels: Array<Array<task>>, cycleNodes: Array<task> }.
   * cycleNodes contains tasks that could not be placed due to cycles.
   */
  function kahnLevels(subgraph) {
    var idToTask = new Map();
    subgraph.forEach(function (t) {
      idToTask.set(t.id, t);
    });

    var inDegree = new Map();
    var adj = new Map();
    subgraph.forEach(function (t) {
      if (!inDegree.has(t.id)) inDegree.set(t.id, 0);
      if (!adj.has(t.id)) adj.set(t.id, []);
    });

    subgraph.forEach(function (t) {
      if (!t.depends_on) return;
      t.depends_on.forEach(function (depId) {
        if (!idToTask.has(depId)) return;
        // depId → t.id edge (depId must be done before t)
        inDegree.set(t.id, (inDegree.get(t.id) || 0) + 1);
        if (!adj.has(depId)) adj.set(depId, []);
        adj.get(depId).push(t.id);
      });
    });

    var levels = [];
    var processed = new Set();
    var queue = [];
    inDegree.forEach(function (deg, id) {
      if (deg === 0) queue.push(id);
    });

    while (queue.length > 0) {
      // Stable sort within each level by position field.
      queue.sort(function (a, b) {
        var ta = idToTask.get(a);
        var tb = idToTask.get(b);
        return ((ta && ta.position) || 0) - ((tb && tb.position) || 0);
      });

      levels.push(
        queue
          .map(function (id) {
            return idToTask.get(id);
          })
          .filter(Boolean),
      );

      var next = [];
      queue.forEach(function (id) {
        processed.add(id);
        (adj.get(id) || []).forEach(function (neighborId) {
          var newDeg = (inDegree.get(neighborId) || 0) - 1;
          inDegree.set(neighborId, newDeg);
          if (newDeg === 0) next.push(neighborId);
        });
      });
      queue = next;
    }

    var cycleNodes = subgraph.filter(function (t) {
      return !processed.has(t.id);
    });
    return { levels: levels, cycleNodes: cycleNodes };
  }

  /**
   * Compute pixel coordinates for each node.
   * Returns { positions: Map<id, {x,y,task}>, svgW, svgH, hasCycles }.
   */
  function computeLayout(levels, cycleNodes) {
    var positions = new Map();
    var maxY = 0;

    for (var col = 0; col < levels.length; col++) {
      var sorted = levels[col].slice().sort(function (a, b) {
        return (a.position || 0) - (b.position || 0);
      });
      var x = PAD + col * (NODE_W + H_GAP);
      var y = PAD;
      sorted.forEach(function (t) {
        positions.set(t.id, { x: x, y: y, task: t });
        y += NODE_H + V_GAP;
        if (y > maxY) maxY = y;
      });
    }

    if (cycleNodes.length > 0) {
      var cycleCol = levels.length;
      var cx = PAD + cycleCol * (NODE_W + H_GAP);
      var cy = PAD;
      cycleNodes.forEach(function (t) {
        positions.set(t.id, { x: cx, y: cy, task: t });
        cy += NODE_H + V_GAP;
        if (cy > maxY) maxY = cy;
      });
    }

    var totalCols = levels.length + (cycleNodes.length > 0 ? 1 : 0);
    var svgW =
      totalCols > 0
        ? PAD + totalCols * NODE_W + Math.max(0, totalCols - 1) * H_GAP + PAD
        : PAD * 2;
    var svgH = maxY + PAD;

    return {
      positions: positions,
      svgW: svgW,
      svgH: svgH,
      hasCycles: cycleNodes.length > 0,
    };
  }

  function svgNs(tag) {
    return document.createElementNS("http://www.w3.org/2000/svg", tag);
  }

  function clearChildren(el) {
    while (el.firstChild) el.removeChild(el.firstChild);
  }

  /**
   * Populate the SVG element with edges (paths) and node groups.
   * Edges are rendered first so nodes appear on top.
   */
  function renderSvg(svg, subgraph, positions, hasCycles) {
    clearChildren(svg);

    var idToTask = new Map();
    subgraph.forEach(function (t) {
      idToTask.set(t.id, t);
    });

    // Read theme colour for edges from CSS custom properties.
    var computedStyle = getComputedStyle(document.documentElement);
    var edgeColor = (
      computedStyle.getPropertyValue("--text-muted") || "#908c86"
    ).trim();

    // --- Edges ---
    subgraph.forEach(function (t) {
      if (!t.depends_on || t.depends_on.length === 0) return;
      t.depends_on.forEach(function (depId) {
        var srcPos = positions.get(depId); // prerequisite is the source
        var dstPos = positions.get(t.id); // dependent is the destination
        if (!srcPos || !dstPos) return;

        // Right-centre of source → left-centre of destination.
        var x1 = srcPos.x + NODE_W;
        var y1 = srcPos.y + NODE_H / 2;
        var x2 = dstPos.x;
        var y2 = dstPos.y + NODE_H / 2;
        var cp = H_GAP / 2;

        var depTask = idToTask.get(depId);
        var satisfied = depTask && depTask.status === "done";

        var path = svgNs("path");
        path.setAttribute(
          "d",
          "M" +
            x1 +
            "," +
            y1 +
            " C" +
            (x1 + cp) +
            "," +
            y1 +
            " " +
            (x2 - cp) +
            "," +
            y2 +
            " " +
            x2 +
            "," +
            y2,
        );
        path.setAttribute("stroke", edgeColor);
        path.setAttribute("stroke-width", "1.5");
        path.setAttribute("fill", "none");
        if (!satisfied) path.setAttribute("stroke-dasharray", "4 2");

        svg.appendChild(path);
      });
    });

    // --- Nodes ---
    positions.forEach(function (pos) {
      var t = pos.task;
      var x = pos.x;
      var y = pos.y;

      var g = svgNs("g");
      g.style.cursor = "pointer";
      // Closure to capture task id for click handler.
      (function (tid) {
        g.addEventListener("click", function () {
          if (typeof window.openTaskModal === "function")
            window.openTaskModal(tid);
        });
      })(t.id);

      var rect = svgNs("rect");
      rect.setAttribute("x", x);
      rect.setAttribute("y", y);
      rect.setAttribute("width", NODE_W);
      rect.setAttribute("height", NODE_H);
      rect.setAttribute("rx", "6");
      var statusStroke = STATUS_COLORS[t.status] || "#7a766e";
      var statusFill = STATUS_TINTS[t.status] || "rgba(0,0,0,0.05)";
      rect.setAttribute("fill", statusFill);
      rect.setAttribute("stroke", statusStroke);
      rect.setAttribute("stroke-width", "1.25");

      var rawTitle = t.title || t.prompt || t.id || "";
      var truncated =
        rawTitle.length > 22 ? rawTitle.slice(0, 22) + "\u2026" : rawTitle;

      var text = svgNs("text");
      text.setAttribute("x", x + NODE_W / 2);
      text.setAttribute("y", y + NODE_H / 2);
      text.setAttribute("dominant-baseline", "middle");
      text.setAttribute("text-anchor", "middle");
      var inkColor = (
        computedStyle.getPropertyValue("--text") || "#1b1916"
      ).trim();
      text.setAttribute("fill", inkColor);
      text.setAttribute("font-size", "12");
      text.textContent = truncated;

      var tooltip = svgNs("title");
      tooltip.textContent = rawTitle;

      g.appendChild(rect);
      g.appendChild(text);
      g.appendChild(tooltip);
      svg.appendChild(g);
    });

    // --- Cycle warning label ---
    if (hasCycles) {
      var maxX = PAD;
      positions.forEach(function (pos) {
        if (pos.x > maxX) maxX = pos.x;
      });
      var label = svgNs("text");
      label.setAttribute("x", maxX + NODE_W / 2);
      label.setAttribute("y", PAD - 8);
      label.setAttribute("text-anchor", "middle");
      label.setAttribute("fill", "#8c2020");
      label.setAttribute("font-size", "11");
      label.textContent = "\u26a0 cycle";
      svg.appendChild(label);
    }
  }

  // ---------------------------------------------------------------------------
  // Panel DOM management
  // ---------------------------------------------------------------------------

  /**
   * Return the existing panel element, or create and insert a new one.
   * Idempotent: calling twice returns the same element.
   */
  function getOrCreatePanel() {
    var panel = document.getElementById("depgraph-panel");
    if (panel) return panel;

    // Preferred mount: the full-pane Dep Graph mode container. It contains
    // a pre-rendered shell with header + empty-state message, plus a
    // #depgraph-mount slot for the SVG canvas. When that shell exists, we
    // only add the SVG (the header comes from HTML).
    var fullPaneMount = document.getElementById("depgraph-mount");
    if (fullPaneMount) {
      panel = document.createElement("div");
      panel.id = "depgraph-panel";
      panel.style.cssText = "display:block;width:100%;";

      var svgInPane = document.createElementNS(
        "http://www.w3.org/2000/svg",
        "svg",
      );
      svgInPane.id = "depgraph-svg";
      svgInPane.style.display = "block";
      panel.appendChild(svgInPane);

      fullPaneMount.appendChild(panel);
      return panel;
    }

    // Legacy overlay layout (below the board) — retained so any caller
    // that still reaches for the old insertion point keeps working.
    panel = document.createElement("div");
    panel.id = "depgraph-panel";
    panel.style.cssText = [
      "display:block",
      "margin:16px 24px",
      "border:1px solid var(--border)",
      "border-radius:12px",
      "overflow:hidden",
      "background:var(--bg-raised)",
    ].join(";");

    var header = document.createElement("div");
    header.style.cssText = [
      "display:flex",
      "align-items:center",
      "justify-content:space-between",
      "padding:8px 16px",
      "border-bottom:1px solid var(--border)",
    ].join(";");

    var titleEl = document.createElement("span");
    titleEl.style.cssText = "font-size:13px;font-weight:600;color:var(--text);";
    titleEl.textContent = "Map";

    var collapsed = localStorage.getItem("depgraph-collapsed") === "true";
    var collapseBtn = document.createElement("button");
    collapseBtn.style.cssText =
      "background:none;border:none;cursor:pointer;font-size:12px;" +
      "color:var(--text-muted);padding:2px 6px;";
    collapseBtn.textContent = collapsed ? "\u25bc" : "\u25b2";

    collapseBtn.addEventListener("click", function () {
      var nowCollapsed = localStorage.getItem("depgraph-collapsed") === "true";
      var next = !nowCollapsed;
      localStorage.setItem("depgraph-collapsed", String(next));
      collapseBtn.textContent = next ? "\u25bc" : "\u25b2";
      var svgEl = document.getElementById("depgraph-svg");
      if (svgEl) svgEl.style.display = next ? "none" : "block";
    });

    header.appendChild(titleEl);
    header.appendChild(collapseBtn);
    panel.appendChild(header);

    var svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.id = "depgraph-svg";
    svg.style.display = collapsed ? "none" : "block";
    panel.appendChild(svg);

    var wrapper =
      document.querySelector(".board-with-explorer") ||
      document.getElementById("board");
    if (wrapper) {
      wrapper.insertAdjacentElement("afterend", panel);
    } else {
      document.body.appendChild(panel);
    }

    return panel;
  }

  // ---------------------------------------------------------------------------
  // Inspector (Legend / Selection / Critical path)
  //
  // The inspector lives in `ui/partials/depgraph-mode.html` as a right-hand
  // aside. depgraph.js owns the dynamic sections — Selection reflects the
  // currently-focused node, Critical path shows the longest dependency
  // chain through the unified graph.
  // ---------------------------------------------------------------------------

  // _longestChain returns up to one longest path through (nodes, edges),
  // considering only dependency edges (spec_dep, task_dep). Containment
  // and dispatch edges are structural and would inflate the chain with
  // non-dependency hops. Falls back to an empty array when the graph is
  // empty, acyclic check is implicit via topo-order traversal — if the
  // graph has a cycle the function still terminates because we iterate
  // each edge once.
  function _longestChain(graph) {
    if (!graph || !graph.nodes || graph.nodes.length === 0) return [];
    var depKinds = { spec_dep: true, task_dep: true };
    var outs = new Map();
    var ins = new Map();
    graph.nodes.forEach(function (n) {
      outs.set(n.id, []);
      ins.set(n.id, 0);
    });
    graph.edges.forEach(function (e) {
      if (!depKinds[e.kind]) return;
      var arr = outs.get(e.from);
      if (!arr) return;
      arr.push(e.to);
      ins.set(e.to, (ins.get(e.to) || 0) + 1);
    });

    // Kahn topo order over the dep subgraph.
    var q = [];
    ins.forEach(function (v, k) {
      if (v === 0) q.push(k);
    });
    var order = [];
    while (q.length > 0) {
      var u = q.shift();
      order.push(u);
      var next = outs.get(u) || [];
      for (var i = 0; i < next.length; i++) {
        var v2 = next[i];
        var nv = (ins.get(v2) || 0) - 1;
        ins.set(v2, nv);
        if (nv === 0) q.push(v2);
      }
    }
    if (order.length < graph.nodes.length) {
      // Cycle present; fall back to an empty path rather than a partial.
      return [];
    }

    var dist = new Map();
    var prev = new Map();
    order.forEach(function (id) {
      dist.set(id, 0);
    });
    for (var j = 0; j < order.length; j++) {
      var a = order[j];
      var outList = outs.get(a) || [];
      for (var k2 = 0; k2 < outList.length; k2++) {
        var b = outList[k2];
        var cand = (dist.get(a) || 0) + 1;
        if (cand > (dist.get(b) || 0)) {
          dist.set(b, cand);
          prev.set(b, a);
        }
      }
    }

    // Pick the node with the largest dist; walk back via prev.
    var endId = null;
    var endDist = -1;
    dist.forEach(function (d, id) {
      if (d > endDist) {
        endDist = d;
        endId = id;
      }
    });
    if (endDist <= 0 || !endId) return [];
    var chain = [];
    var cur = endId;
    while (cur) {
      chain.unshift(cur);
      cur = prev.get(cur);
    }
    return chain;
  }

  function _inspectorSlot(id) {
    if (typeof document === "undefined") return null;
    return document.getElementById(id);
  }

  function _clear(el) {
    if (!el) return;
    while (el.firstChild) el.removeChild(el.firstChild);
  }

  function _el(tag, attrs, text) {
    var e = document.createElement(tag);
    if (attrs) {
      for (var k in attrs) {
        if (k === "className") e.className = attrs[k];
        else if (k === "onclick") e.onclick = attrs[k];
        else e.setAttribute(k, attrs[k]);
      }
    }
    if (text !== undefined && text !== null) e.textContent = String(text);
    return e;
  }

  function _renderSelection(graph, focusedId) {
    var slot = _inspectorSlot("depgraph-inspector-selection");
    if (!slot) return;
    _clear(slot);
    var node = null;
    if (focusedId && graph && graph.nodes) {
      for (var i = 0; i < graph.nodes.length; i++) {
        if (graph.nodes[i].id === focusedId) {
          node = graph.nodes[i];
          break;
        }
      }
    }
    if (!node) {
      slot.appendChild(
        _el(
          "p",
          { className: "depgraph-inspector__muted" },
          "Click a node to focus its neighbourhood. Shift+click opens the task or spec.",
        ),
      );
      return;
    }
    var card = _el("div", { className: "depgraph-inspector__selection-card" });
    card.appendChild(
      _el(
        "div",
        { className: "depgraph-inspector__selection-kind" },
        node.kind === "spec" ? "Spec" : "Task",
      ),
    );
    card.appendChild(
      _el(
        "div",
        { className: "depgraph-inspector__selection-label" },
        node.label || node.id,
      ),
    );
    var meta = _el("div", { className: "depgraph-inspector__selection-meta" });
    var pill = _el(
      "span",
      { className: "depgraph-inspector__status-pill" },
      node.status || "—",
    );
    if (node.kind === "spec") {
      pill.setAttribute("data-spec-status", node.status || "");
    } else {
      pill.setAttribute("data-task-status", node.status || "");
    }
    meta.appendChild(pill);
    if (node.kind === "spec" && node.extra && node.extra.path) {
      meta.appendChild(_el("span", null, node.extra.path));
    }
    card.appendChild(meta);
    slot.appendChild(card);
  }

  function _renderCriticalPath(graph, focusedId) {
    var slot = _inspectorSlot("depgraph-inspector-critical");
    if (!slot) return;
    _clear(slot);
    var chain = _longestChain(graph);
    if (!chain.length) {
      slot.appendChild(
        _el(
          "p",
          { className: "depgraph-inspector__muted" },
          "Longest dependency chain appears here once the graph has edges.",
        ),
      );
      return;
    }
    var byId = new Map();
    graph.nodes.forEach(function (n) {
      byId.set(n.id, n);
    });
    var list = _el("ol", { className: "depgraph-inspector__critical-list" });
    chain.forEach(function (id) {
      var n = byId.get(id);
      if (!n) return;
      var li = _el("li", null);
      if (focusedId === id) li.className = "is-focused";
      li.appendChild(
        _el(
          "span",
          { className: "depgraph-inspector__critical-kind" },
          n.kind === "spec" ? "SPEC" : "TASK",
        ),
      );
      li.appendChild(
        _el(
          "span",
          { className: "depgraph-inspector__critical-label" },
          n.label || n.id,
        ),
      );
      li.onclick = function () {
        _focusNode(id);
      };
      list.appendChild(li);
    });
    slot.appendChild(list);
  }

  function _renderInspector(graph, focusedId) {
    if (typeof document === "undefined") return;
    _renderSelection(graph, focusedId);
    _renderCriticalPath(graph, focusedId);
  }

  // ---------------------------------------------------------------------------
  // Internal hide (does NOT reset fingerprint — for empty-subgraph path).
  // ---------------------------------------------------------------------------
  function _hidePanel() {
    var panel = document.getElementById("depgraph-panel");
    if (panel) panel.style.display = "none";
  }

  // ---------------------------------------------------------------------------
  // Public API
  // ---------------------------------------------------------------------------

  /**
   * Render the dependency-graph panel for the given task list.
   *
   * Skips re-rendering when the dependency-relevant subset of tasks has not
   * changed since the last call (fingerprint comparison).
   */
  function renderDependencyGraph(tasks) {
    // When spec-mode state is populated and unified-graph is loaded, prefer
    // the unified spec+task renderer so the Dep Graph tab shows the full
    // coordination picture. Fall back to task-only when no specs exist.
    var specNodes =
      typeof specModeState !== "undefined" && specModeState
        ? specModeState.tree || []
        : [];
    var useUnified =
      typeof buildUnifiedGraph === "function" &&
      typeof renderUnifiedGraph === "function" &&
      Array.isArray(specNodes) &&
      specNodes.length > 0;

    if (useUnified) {
      _syncShowArchivedCheckbox();
      var graph = buildUnifiedGraph(tasks, specNodes, {
        collapsedSpecs: _collapsedSetFor(specNodes),
        includeArchivedSpecs: _showArchived,
        includeArchivedTasks: _showArchived,
      });
      // Always make the panel visible — returning early without doing so
      // leaves it hidden after Dep Graph → Board → Dep Graph round-trips.
      var panel = getOrCreatePanel();
      panel.style.display = "block";

      var unifiedFp = _graphFingerprint(graph) + "|f=" + (_focusedNodeId || "");
      if (unifiedFp === _lastFingerprint) {
        // Fingerprint unchanged but inspector may still need to reflect
        // the focused selection if the caller toggled focus.
        _renderInspector(graph, _focusedNodeId);
        return;
      }
      _lastFingerprint = unifiedFp;

      var svg = document.getElementById("depgraph-svg");
      if (!svg) return;
      var paneEmpty = _paneEmpty();
      if (graph.nodes.length === 0) {
        svg.style.display = "none";
        if (paneEmpty) paneEmpty.style.display = "";
        return;
      }
      if (paneEmpty) paneEmpty.style.display = "none";
      svg.style.display = "block";
      renderUnifiedGraph(graph, svg, {
        onToggleSpec: _toggleSpecExpanded,
        onPinNode: _pinNode,
        onUnpinNode: _unpinNode,
        onFocusNode: _focusNode,
        onNavigateNode: _navigateNode,
        onHoverNode: _applyHoverHighlight,
        pinnedIds: _pinnedIds(),
        pinnedPositions: _pinnedPositions,
        focusedNodeId: _focusedNodeId,
        searchQuery: _searchQuery,
        getScale: _getZoom,
      });
      _applyZoomToSvg(svg);
      _installPan();
      _installZoom();
      _centerIntoViewIfUntouched();
      _renderInspector(graph, _focusedNodeId);
      return;
    }

    // --- Legacy task-only path (no specs in the workspace) ---
    var targetIds = new Set();
    tasks.forEach(function (t) {
      if (t.depends_on)
        t.depends_on.forEach(function (d) {
          targetIds.add(d);
        });
    });
    var relevant = tasks.filter(function (t) {
      return hasDepEdge(t) || targetIds.has(t.id);
    });
    var fingerprint = JSON.stringify(
      relevant.map(function (t) {
        return {
          id: t.id,
          status: t.status,
          depends_on: t.depends_on,
          position: t.position,
          title: t.title,
        };
      }),
    );

    if (fingerprint === _lastFingerprint) return;
    _lastFingerprint = fingerprint;

    var subgraph = getSubgraph(tasks);
    var legacyPanel = getOrCreatePanel();
    legacyPanel.style.display = "block";

    var legacySvg = document.getElementById("depgraph-svg");
    if (!legacySvg) return;

    var paneEmptyLegacy = _paneEmpty();
    var inlineEmpty = legacyPanel.querySelector(".depgraph-empty");

    if (subgraph.length === 0) {
      legacySvg.style.display = "none";
      if (paneEmptyLegacy) {
        paneEmptyLegacy.style.display = "";
      } else {
        if (!inlineEmpty) {
          inlineEmpty = document.createElement("div");
          inlineEmpty.className = "depgraph-empty";
          inlineEmpty.style.cssText =
            "padding:24px;text-align:center;color:var(--text-muted);font-size:12px;";
          inlineEmpty.textContent =
            "No dependency edges. Add depends-on links between tasks to see the graph.";
          legacyPanel.appendChild(inlineEmpty);
        }
        inlineEmpty.style.display = "";
      }
      return;
    }

    if (paneEmptyLegacy) paneEmptyLegacy.style.display = "none";
    if (inlineEmpty) inlineEmpty.style.display = "none";

    var kr = kahnLevels(subgraph);
    var kl = computeLayout(kr.levels, kr.cycleNodes);

    var isCollapsed = localStorage.getItem("depgraph-collapsed") === "true";
    if (!isCollapsed) {
      legacySvg.setAttribute("viewBox", "0 0 " + kl.svgW + " " + kl.svgH);
      legacySvg.setAttribute("width", kl.svgW);
      legacySvg.setAttribute("height", kl.svgH);
      legacySvg.style.display = "block";
    }

    renderSvg(legacySvg, subgraph, kl.positions, kl.hasCycles);

    // Feed the inspector with a minimal graph-shaped payload so the
    // Selection and Critical path sections stay populated on the
    // legacy (tasks-only) path.
    var legacyGraph = {
      nodes: subgraph.map(function (t) {
        return {
          id: t.id,
          kind: "task",
          status: t.status || "backlog",
          label: t.title || t.prompt || t.id,
        };
      }),
      edges: [],
    };
    subgraph.forEach(function (t) {
      if (!t.depends_on) return;
      t.depends_on.forEach(function (depId) {
        legacyGraph.edges.push({ from: depId, to: t.id, kind: "task_dep" });
      });
    });
    _renderInspector(legacyGraph, _focusedNodeId);
  }

  function _paneEmpty() {
    return document.getElementById("depgraph-mount")
      ? document.querySelector("#depgraph-mount .depgraph-mode__empty")
      : null;
  }

  // Fingerprint of the unified graph covers both tasks and specs so the
  // graph re-renders when either side changes.
  function _graphFingerprint(graph) {
    return JSON.stringify({
      n: graph.nodes.map(function (n) {
        return [n.id, n.status, n.label, n.extra && n.extra.dispatched];
      }),
      e: graph.edges.map(function (e) {
        return [e.from, e.to, e.kind];
      }),
    });
  }

  /**
   * Hide the dependency-graph panel.
   * Resets the fingerprint so the graph is fully re-rendered next time it is shown.
   */
  function hideDependencyGraph() {
    _hidePanel();
    _lastFingerprint = null;
  }

  window.renderDependencyGraph = renderDependencyGraph;
  window.hideDependencyGraph = hideDependencyGraph;
})();
