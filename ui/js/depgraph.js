// depgraph.js — Panel-based DAG visualization of task dependencies.
//
// Assigns window.renderDependencyGraph(tasks) and window.hideDependencyGraph(),
// overriding the bezier-overlay implementation from dep-graph.js.
//
// The panel is inserted as a block element after the #board container so it
// does not disturb the Kanban column layout.

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

    // Header row
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
    titleEl.textContent = "Dependency Graph";

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

    // SVG canvas
    var svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.id = "depgraph-svg";
    svg.style.display = collapsed ? "none" : "block";
    panel.appendChild(svg);

    // Insert after the #board container.
    var board = document.getElementById("board");
    if (board) {
      board.insertAdjacentElement("afterend", panel);
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
    // Build the fingerprint over dependency-relevant tasks only.
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
    var panel = getOrCreatePanel();
    panel.style.display = "block";

    var svg = document.getElementById("depgraph-svg");
    if (!svg) return;

    // Show empty-state message when no dependency edges exist.
    var emptyMsg = panel.querySelector(".depgraph-empty");
    if (subgraph.length === 0) {
      svg.style.display = "none";
      if (!emptyMsg) {
        emptyMsg = document.createElement("div");
        emptyMsg.className = "depgraph-empty";
        emptyMsg.style.cssText =
          "padding:24px;text-align:center;color:var(--text-muted);font-size:12px;";
        emptyMsg.textContent = "No dependency edges. Add depends-on links between tasks to see the graph.";
        panel.appendChild(emptyMsg);
      }
      emptyMsg.style.display = "";
      return;
    }

    if (emptyMsg) emptyMsg.style.display = "none";

    var result = kahnLevels(subgraph);
    var layout = computeLayout(result.levels, result.cycleNodes);

    var isCollapsed = localStorage.getItem("depgraph-collapsed") === "true";
    if (!isCollapsed) {
      svg.setAttribute("viewBox", "0 0 " + layout.svgW + " " + layout.svgH);
      svg.setAttribute("width", layout.svgW);
      svg.setAttribute("height", layout.svgH);
      svg.style.display = "block";
    }

    renderSvg(svg, subgraph, layout.positions, layout.hasCycles);
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
