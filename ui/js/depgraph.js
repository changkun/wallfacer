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

  var STATUS_COLORS = {
    backlog: "#6b6560",
    in_progress: "#2c5f98",
    waiting: "#a07020",
    committing: "#5a3d8a",
    done: "#1a6030",
    failed: "#8c2020",
    cancelled: "#5a3d8a",
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
  var _pinnedPositions = _loadPinnedPositions();

  function _loadPinnedPositions() {
    try {
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
    _lastFingerprint = null;
    _scheduleMapRender();
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
      var rect =
        typeof group.getBBox === "function" ? group.getBBox() : null;
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
    if (_pinnedPositions.size === 0 && !_focusedNodeId) return;
    _pinnedPositions = new Map();
    _focusedNodeId = null;
    _savePinnedPositions();
    _lastFingerprint = null;
    _scheduleMapRender();
  }
  if (typeof window !== "undefined") {
    window.resetMapLayout = resetMapLayout;
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
      rect.setAttribute("fill", STATUS_COLORS[t.status] || "#4B5563");

      var rawTitle = t.title || t.prompt || t.id || "";
      var truncated =
        rawTitle.length > 22 ? rawTitle.slice(0, 22) + "\u2026" : rawTitle;

      var text = svgNs("text");
      text.setAttribute("x", x + NODE_W / 2);
      text.setAttribute("y", y + NODE_H / 2);
      text.setAttribute("dominant-baseline", "middle");
      text.setAttribute("text-anchor", "middle");
      text.setAttribute("fill", "#f0ede6");
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
      var graph = buildUnifiedGraph(tasks, specNodes, {
        collapsedSpecs: _collapsedSetFor(specNodes),
      });
      // Always make the panel visible — returning early without doing so
      // leaves it hidden after Dep Graph → Board → Dep Graph round-trips.
      var panel = getOrCreatePanel();
      panel.style.display = "block";

      var unifiedFp = _graphFingerprint(graph);
      if (unifiedFp === _lastFingerprint) return;
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
        pinnedIds: _pinnedIds(),
        pinnedPositions: _pinnedPositions,
        focusedNodeId: _focusedNodeId,
      });
      _installPan();
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
