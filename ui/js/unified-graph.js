// unified-graph.js — Merge spec tree + task DAG into a single node/edge
// set for the Dep Graph tab.
//
// Semantic axes:
//   * Specs form a containment hierarchy (parent → child via the file tree)
//     plus design-level cross-dependencies (spec.depends_on[]).
//   * Tasks form a runtime DAG (task.depends_on[]).
//   * A leaf spec "materializes" into a task via spec.dispatched_task_id,
//     linking the two graphs.
//
// Node and edge schema:
//   node: { id, kind, label, status, extra }
//     kind: "spec" | "task"
//     extra for spec: { path, isLeaf, depth, effort, track }
//     extra for task: { dispatched: bool, archived: bool }
//   edge: { from, to, kind }
//     kind: "containment" | "spec_dep" | "dispatch" | "task_dep"
//
// Edge direction convention: `from` comes before `to` in the visual flow
// (left-to-right layered layout). Concretely:
//   * containment: parent → child (parent is the container)
//   * dispatch:    spec  → task  (spec materializes into task)
//   * spec_dep:    prereq-spec → dependent-spec (B must exist before A, A.depends_on=[B] → B→A)
//   * task_dep:    prereq-task → dependent-task (same shape as spec_dep)
// This matches the existing depgraph.js convention so a single topological
// layout places all edges consistently.
//
// Filtering rules applied here:
//   * Specs with status: "archived" are skipped entirely (nodes and edges)
//     to match the explorer's default behaviour.
//   * Archived tasks are skipped unless explicitly included via opts.
//   * Specs whose path (or any ancestor's path) is in `opts.collapsedSpecs`
//     are treated as hidden for rendering. Their dispatched tasks and
//     their outgoing/incoming edges drop out of the graph. The collapsed
//     spec itself stays visible with a hasChildren=true flag so the
//     renderer can draw a toggle handle.

(function () {
  "use strict";

  // buildUnifiedGraph returns {nodes, edges} given the board's task list
  // and the /api/specs/tree response's `nodes` array (NodeResponse[]).
  //
  // `specNodes` may be empty (workspace with no specs) or undefined; in that
  // case the result is just the task DAG.
  //
  // Options:
  //   includeArchivedSpecs (default false)
  //   includeArchivedTasks (default false) — archived tasks filtered out
  //   collapsedSpecs      (Set<string>|null) — spec paths whose descendants
  //                        should be hidden. Each collapsed spec stays in
  //                        the graph as a handle; its children + edges
  //                        are filtered out.
  function buildUnifiedGraph(tasks, specNodes, opts) {
    opts = opts || {};
    var includeArchivedSpecs = !!opts.includeArchivedSpecs;
    var includeArchivedTasks = !!opts.includeArchivedTasks;
    var collapsedSpecs =
      opts.collapsedSpecs && typeof opts.collapsedSpecs.has === "function"
        ? opts.collapsedSpecs
        : null;
    tasks = Array.isArray(tasks) ? tasks : [];
    specNodes = Array.isArray(specNodes) ? specNodes : [];

    var nodes = [];
    var edges = [];

    // Index specs by path so dependency edges can look up whether a target
    // is present (skipped specs drop their in/out edges).
    var specByPath = Object.create(null);
    for (var i = 0; i < specNodes.length; i++) {
      var n = specNodes[i];
      if (!n || !n.spec) continue;
      if (!includeArchivedSpecs && n.spec.status === "archived") continue;
      specByPath[n.path] = n;
    }

    // Compute the set of spec paths hidden by a collapsed ancestor. We
    // perform a BFS from each collapsed spec through its children.
    var hiddenSpecs = Object.create(null);
    if (collapsedSpecs) {
      var queue = [];
      collapsedSpecs.forEach(function (p) {
        // Collapsed spec itself is NOT hidden — only its descendants.
        var root = specByPath[p];
        if (!root) return;
        var rootChildren = Array.isArray(root.children) ? root.children : [];
        for (var qi = 0; qi < rootChildren.length; qi++) {
          queue.push(rootChildren[qi]);
        }
      });
      while (queue.length > 0) {
        var cur = queue.shift();
        if (hiddenSpecs[cur]) continue;
        hiddenSpecs[cur] = true;
        var curNode = specByPath[cur];
        if (!curNode) continue;
        var curChildren = Array.isArray(curNode.children)
          ? curNode.children
          : [];
        for (var qj = 0; qj < curChildren.length; qj++) {
          queue.push(curChildren[qj]);
        }
      }
    }

    // Spec nodes + containment edges (parent spec → child spec).
    // We draw containment using the NodeResponse.children array rather than
    // inferring from depth so the edge set mirrors the authoritative tree.
    for (var path in specByPath) {
      if (hiddenSpecs[path]) continue;
      var node = specByPath[path];
      var spec = node.spec;
      var childrenAll = Array.isArray(node.children) ? node.children : [];
      var hasVisibleChildren = false;
      for (var ci = 0; ci < childrenAll.length; ci++) {
        if (specByPath[childrenAll[ci]]) {
          hasVisibleChildren = true;
          break;
        }
      }
      var isCollapsed = !!(
        collapsedSpecs &&
        collapsedSpecs.has(path) &&
        hasVisibleChildren
      );
      nodes.push({
        id: "spec:" + path,
        kind: "spec",
        label: spec.title || _basename(path),
        status: spec.status || "",
        extra: {
          path: path,
          isLeaf: !!node.is_leaf,
          depth: typeof node.depth === "number" ? node.depth : 0,
          effort: spec.effort || "",
          track: spec.track || "",
          // hasChildren reflects the *tree*, not the filtered graph. Even
          // a collapsed spec has hasChildren=true so the renderer draws
          // its toggle.
          hasChildren: hasVisibleChildren,
          collapsed: isCollapsed,
        },
      });
      // Skip containment edges out of a collapsed spec — children are
      // hidden so the edges would dangle.
      if (collapsedSpecs && collapsedSpecs.has(path)) continue;
      for (var c = 0; c < childrenAll.length; c++) {
        var childPath = childrenAll[c];
        if (!specByPath[childPath]) continue;
        if (hiddenSpecs[childPath]) continue;
        edges.push({
          from: "spec:" + path,
          to: "spec:" + childPath,
          kind: "containment",
        });
      }
    }

    // Spec → spec design-level dependencies (depends_on).
    // A.depends_on = [B] means B must come before A: emit edge B → A.
    for (var depPath in specByPath) {
      if (hiddenSpecs[depPath]) continue;
      var depSpec = specByPath[depPath].spec;
      var dependsOn = Array.isArray(depSpec.depends_on)
        ? depSpec.depends_on
        : [];
      for (var d = 0; d < dependsOn.length; d++) {
        var target = dependsOn[d];
        if (!specByPath[target]) continue;
        if (hiddenSpecs[target]) continue;
        edges.push({
          from: "spec:" + target,
          to: "spec:" + depPath,
          kind: "spec_dep",
        });
      }
    }

    // Task nodes + task → task runtime deps.
    var taskById = Object.create(null);
    for (var t = 0; t < tasks.length; t++) {
      var task = tasks[t];
      if (!task || !task.id) continue;
      if (!includeArchivedTasks && task.archived) continue;
      taskById[task.id] = task;
    }
    for (var tid in taskById) {
      var tk = taskById[tid];
      nodes.push({
        id: "task:" + tid,
        kind: "task",
        label: tk.title || _shortId(tid),
        status: tk.status || "",
        extra: {
          dispatched: false,
          archived: !!tk.archived,
        },
      });
      // A.depends_on = [B] means B must come before A: emit edge B → A.
      var tdeps = Array.isArray(tk.depends_on) ? tk.depends_on : [];
      for (var td = 0; td < tdeps.length; td++) {
        var targetId = tdeps[td];
        if (!taskById[targetId]) continue;
        edges.push({
          from: "task:" + targetId,
          to: "task:" + tid,
          kind: "task_dep",
        });
      }
    }

    // Leaf spec → task dispatch edges. Flip the `dispatched` flag on the
    // corresponding task node so the renderer can style it distinctly.
    for (var ldPath in specByPath) {
      if (hiddenSpecs[ldPath]) continue;
      var leafSpec = specByPath[ldPath].spec;
      var dtid =
        leafSpec.dispatched_task_id && leafSpec.dispatched_task_id !== "null"
          ? leafSpec.dispatched_task_id
          : null;
      if (!dtid) continue;
      if (!taskById[dtid]) continue;
      edges.push({
        from: "spec:" + ldPath,
        to: "task:" + dtid,
        kind: "dispatch",
      });
      // Mark the task as dispatched-from-a-spec.
      for (var ni = 0; ni < nodes.length; ni++) {
        if (nodes[ni].id === "task:" + dtid) {
          nodes[ni].extra.dispatched = true;
          break;
        }
      }
    }

    return { nodes: nodes, edges: edges };
  }

  function _basename(path) {
    if (!path) return "";
    var idx = path.lastIndexOf("/");
    return idx === -1 ? path : path.substr(idx + 1);
  }

  function _shortId(id) {
    return id && id.length > 8 ? id.substr(0, 8) : id || "";
  }

  // ---------------------------------------------------------------------------
  // Layout + SVG rendering
  // ---------------------------------------------------------------------------

  var PAD = 24;
  var NODE_W = 220;
  // NODE_H is a floor only; labels that wrap to multiple lines grow the
  // node vertically via _computeNodeHeight.
  var NODE_H = 52;
  var H_GAP = 80;
  var V_GAP = 18;

  // Label wrap tuning. Characters per line at 12px system fonts inside a
  // 220px-wide node leaves room for the 8px inset and the right-side
  // toggle chip on specs with children. Each line past the first adds
  // LABEL_LINE_H to the node height.
  var LABEL_CHARS_PER_LINE = 26;
  var LABEL_CHARS_PER_LINE_WITH_TOGGLE = 22;
  var LABEL_LINE_H = 15;
  var LABEL_TOP_PAD = 24; // vertical space reserved above the label
  var LABEL_BOTTOM_PAD = 12;

  // Dummy-node rail: intermediate waypoints for long edges take up far less
  // vertical space than full nodes so the resulting polyline reads as a
  // smooth curve rather than as discrete stepped segments.
  var DUMMY_H = 0;

  // _wrapLabel splits a label into lines of at most `maxChars` characters,
  // breaking on whitespace where possible and hard-wrapping runs that
  // exceed the limit (e.g. a long spec path). Returns an array of strings.
  function _wrapLabel(label, maxChars) {
    if (!label) return [""];
    var lines = [];
    var words = String(label).split(/\s+/);
    var current = "";
    for (var i = 0; i < words.length; i++) {
      var w = words[i];
      if (w.length === 0) continue;
      // Hard-break runs longer than the line length (URLs, long paths).
      while (w.length > maxChars) {
        if (current) {
          lines.push(current);
          current = "";
        }
        lines.push(w.slice(0, maxChars));
        w = w.slice(maxChars);
      }
      if (!current) {
        current = w;
      } else if (current.length + 1 + w.length <= maxChars) {
        current += " " + w;
      } else {
        lines.push(current);
        current = w;
      }
    }
    if (current) lines.push(current);
    if (lines.length === 0) lines.push("");
    return lines;
  }

  function _nodeLabelLines(node) {
    var hasToggle = node && node.extra && node.extra.hasChildren;
    var maxChars = hasToggle
      ? LABEL_CHARS_PER_LINE_WITH_TOGGLE
      : LABEL_CHARS_PER_LINE;
    return _wrapLabel(node.label || "", maxChars);
  }

  // _computeNodeHeight returns the total pixel height for a node, driven
  // by how many lines its label wraps to.
  function _computeNodeHeight(node) {
    var lines = _nodeLabelLines(node);
    var contentH =
      LABEL_TOP_PAD + lines.length * LABEL_LINE_H + LABEL_BOTTOM_PAD;
    return Math.max(NODE_H, contentH);
  }

  // Status → visual style. Nodes render as tinted-fill rounded rectangles
  // with a saturated stroke in the same hue; the colour values come from
  // CSS custom properties on `.depgraph-mode-container` so light/dark
  // themes can rebalance the palette without touching this file.
  //
  // The fallback hex values mirror the light-theme CSS so tests running in
  // a stub DOM (no getComputedStyle) still produce readable output.
  var TASK_STATUS_FALLBACK = {
    backlog: { stroke: "#8e8a80", fill: "rgba(142,138,128,0.12)" },
    in_progress: { stroke: "#3a6db3", fill: "rgba(58,109,179,0.14)" },
    waiting: { stroke: "#a56a12", fill: "rgba(165,106,18,0.14)" },
    committing: { stroke: "#6a4aa3", fill: "rgba(106,74,163,0.14)" },
    done: { stroke: "#3f7a4a", fill: "rgba(63,122,74,0.14)" },
    failed: { stroke: "#a32d2d", fill: "rgba(163,45,45,0.14)" },
    cancelled: { stroke: "#7a766e", fill: "rgba(122,118,110,0.12)" },
  };
  var SPEC_STATUS_FALLBACK = {
    vague: { stroke: "#7a5418", fill: "rgba(122,84,24,0.10)" },
    drafted: { stroke: "#a56a12", fill: "rgba(165,106,18,0.12)" },
    validated: { stroke: "#2d6d5a", fill: "rgba(45,109,90,0.12)" },
    complete: { stroke: "#3f7a4a", fill: "rgba(63,122,74,0.12)" },
    stale: { stroke: "#a32d2d", fill: "rgba(163,45,45,0.12)" },
    archived: { stroke: "#7a766e", fill: "rgba(122,118,110,0.10)" },
  };
  var EDGE_FALLBACK = {
    containment: "#9a948a",
    dispatch: "#3a6db3",
    spec_dep: "#b07045",
    task_dep: "#4a7a4f",
  };

  // _cssVarReader reads `--*` custom properties off the host element
  // (defaulting to the `.depgraph-mode-container` ancestor) once per
  // render so we only pay the getComputedStyle cost a bounded number of
  // times per frame.
  function _cssVarReader(svg) {
    var host = null;
    if (svg && typeof svg.closest === "function") {
      host = svg.closest(".depgraph-mode-container");
    }
    if (!host && typeof document !== "undefined" && document.documentElement) {
      host = document.documentElement;
    }
    if (!host || typeof getComputedStyle !== "function") {
      return function () {
        return "";
      };
    }
    var cs;
    try {
      cs = getComputedStyle(host);
    } catch (_e) {
      return function () {
        return "";
      };
    }
    var cache = {};
    return function (name) {
      if (cache[name] !== undefined) return cache[name];
      var v = "";
      try {
        v = (cs.getPropertyValue(name) || "").trim();
      } catch (_e) {
        v = "";
      }
      cache[name] = v;
      return v;
    };
  }

  function _nodeStyle(readVar, kind, status) {
    var table = kind === "spec" ? SPEC_STATUS_FALLBACK : TASK_STATUS_FALLBACK;
    var fb = table[status] || { stroke: "#7a766e", fill: "rgba(0,0,0,0.05)" };
    var prefix = kind === "spec" ? "--dg-spec-" : "--dg-task-";
    var key = status === "in_progress" ? "progress" : status;
    var stroke = readVar(prefix + key) || fb.stroke;
    var fill = readVar(prefix + key + "-tint") || fb.fill;
    return { stroke: stroke, fill: fill };
  }

  function _edgeColor(readVar, kind) {
    var name =
      kind === "containment"
        ? "--dg-edge-containment"
        : kind === "dispatch"
          ? "--dg-edge-dispatch"
          : kind === "spec_dep"
            ? "--dg-edge-spec-dep"
            : "--dg-edge-task-dep";
    return readVar(name) || EDGE_FALLBACK[kind] || "#7a7570";
  }

  // Edge styling per kind. containment and dispatch are structural so they
  // read as thin solid lines; spec_dep and task_dep are design/runtime
  // dependencies so they use dashed variants with distinct hues.
  //
  // task_dep has a secondary state — when the prerequisite task is already
  // done, the edge goes solid to signal "this dependency is satisfied."
  var EDGE_STYLES = {
    containment: { width: 1.2, dash: null },
    dispatch: { width: 1.5, dash: null },
    spec_dep: { width: 1.5, dash: "6 3" },
    task_dep: { width: 1.5, dash: "4 2" },
  };

  function svgNs(tag) {
    return document.createElementNS("http://www.w3.org/2000/svg", tag);
  }

  function clearChildren(el) {
    while (el.firstChild) el.removeChild(el.firstChild);
  }

  // ---------------------------------------------------------------------------
  // Sugiyama-style layout
  //
  // The layout engine follows the classic Sugiyama framework:
  //   1. Assign a layer (column) to every node via longest-path Kahn so
  //      every predecessor sits strictly to the left.
  //   2. Insert dummy waypoints so every edge spans exactly one layer;
  //      long edges become chains of dummies that sweep-based crossing
  //      minimisation can reorder alongside real nodes.
  //   3. Minimise crossings by alternating top-down and bottom-up sweeps
  //      that reorder nodes within each layer by the barycenter (mean
  //      index) of their neighbours in the adjacent layer.
  //   4. Assign coordinates from the final per-layer ordering, centring
  //      short columns inside taller ones so the graph reads balanced.
  //   5. Route each real edge through its dummy waypoints so long edges
  //      render as smooth curves that thread around intermediate layers
  //      instead of cutting through unrelated clusters.
  // ---------------------------------------------------------------------------

  var BARYCENTER_ITERATIONS = 24;

  // opts.pinnedPositions — Map<nodeId, {x, y}> of positions the user has
  // dragged. Pinned nodes are fed back into the barycenter sweeps as
  // fixed anchors so unpinned nodes flow around them (incremental
  // relayout), then have their exact coords re-applied at the end. This
  // gives drag-then-relayout the feel of "only what I didn't touch moves."
  // Vertical gap between stacked connected components.
  var COMPONENT_GAP = 56;

  // Force-directed layout tunables. K_IDEAL is the rest length of an
  // edge spring; REPULSION is scaled so node bubbles of radius ~NODE_W/2
  // don't overlap in equilibrium. ITERATIONS is a cap — the simulation
  // exits early when total displacement falls below EPSILON.
  var FORCE_K_IDEAL = 200;
  var FORCE_REPULSION = 42000;
  var FORCE_ITERATIONS = 260;
  var FORCE_EPSILON = 0.35;
  var FORCE_NODE_RADIUS = 140; // bubble radius used for anti-overlap

  // layoutForce is the primary Map layout. It uses Fruchterman-Reingold
  // with a hard minimum-separation repulsion so nodes don't overlap
  // their rectangles. Each connected component is simulated in
  // isolation then stacked on the canvas so disconnected clusters don't
  // drift arbitrarily far from each other.
  //
  // We keep the `layoutSugiyama` name as an alias for callers/tests that
  // already reach for it — the new algorithm is a drop-in replacement
  // with the same return shape.
  // Module-level cache of the last computed layout, keyed by graph
  // structure fingerprint. Re-render with the same structure (the common
  // case: a pin was added, hover fired, search query changed) skips the
  // 260-iteration Fruchterman-Reingold simulation entirely. Only the
  // pinned node moves; every other node keeps its previous position so
  // the user sees zero unrelated jitter.
  var _layoutCache = { fingerprint: null, positions: null, maxCompW: 0 };

  function _structureFingerprint(graph) {
    var ns = [];
    for (var i = 0; i < graph.nodes.length; i++) {
      var n = graph.nodes[i];
      // Node heights drive layout too (label wrapping), so include them.
      ns.push(n.id + "@" + (n.label || "").length);
    }
    ns.sort();
    var es = [];
    for (var j = 0; j < graph.edges.length; j++) {
      var e = graph.edges[j];
      es.push(e.from + ">" + e.to + ":" + e.kind);
    }
    es.sort();
    return ns.join("|") + "##" + es.join("|");
  }

  function layoutForce(graph, opts) {
    opts = opts || {};
    var pinnedPositions =
      opts.pinnedPositions && typeof opts.pinnedPositions.get === "function"
        ? opts.pinnedPositions
        : null;

    // Fast path: structure unchanged → reuse cached positions + apply pins.
    // This is what makes a pin drag not shuffle the rest of the graph.
    var fp = _structureFingerprint(graph);
    if (_layoutCache.fingerprint === fp && _layoutCache.positions) {
      var cached = new Map();
      _layoutCache.positions.forEach(function (p, id) {
        cached.set(id, {
          x: p.x,
          y: p.y,
          node: p.node,
          kind: p.kind || "real",
          height: p.height,
        });
      });
      if (pinnedPositions) {
        pinnedPositions.forEach(function (pos, id) {
          var c = cached.get(id);
          if (!c) return;
          c.x = pos.x;
          c.y = pos.y;
        });
      }
      var edgeChainsFast = graph.edges.map(function (e) {
        return { edge: e, chain: [e.from, e.to] };
      });
      var edgePathsFast = _routeEdges(graph, edgeChainsFast, cached);
      var maxXF = 0;
      var maxYF = 0;
      cached.forEach(function (p) {
        var h = p.height || NODE_H;
        if (p.x + NODE_W > maxXF) maxXF = p.x + NODE_W;
        if (p.y + h > maxYF) maxYF = p.y + h;
      });
      var svgWF = Math.max(PAD + _layoutCache.maxCompW + PAD, maxXF + PAD);
      var svgHF = maxYF + PAD;
      edgePathsFast.forEach(function (routed) {
        if (!routed || !routed.points) return;
        routed.points.forEach(function (pt) {
          pt.x = Math.round(pt.x);
          pt.y = Math.round(pt.y);
        });
      });
      return {
        positions: cached,
        edgePaths: edgePathsFast,
        svgW: Math.round(svgWF),
        svgH: Math.round(svgHF),
        hasCycles: false,
      };
    }

    var components = _connectedComponents(graph);
    if (components.length === 0) {
      return {
        positions: new Map(),
        edgePaths: [],
        svgW: PAD * 2,
        svgH: PAD * 2,
        hasCycles: false,
      };
    }

    var allPositions = new Map();
    var yCursor = PAD;
    var maxCompW = 0;
    var layouts = [];
    components.forEach(function (comp) {
      var L = _forceLayoutComponent(comp, pinnedPositions);
      layouts.push(L);
      if (L.width > maxCompW) maxCompW = L.width;
    });
    layouts.forEach(function (L) {
      var xOffset = Math.max(0, (maxCompW - L.width) / 2);
      L.positions.forEach(function (p, id) {
        p.x += xOffset;
        p.y += yCursor;
        allPositions.set(id, p);
      });
      yCursor += L.height + COMPONENT_GAP;
    });
    yCursor -= COMPONENT_GAP;

    // Pinned positions: re-apply absolute coords after stacking so user
    // drags land exactly where they were dropped. Pinned nodes were
    // already locked during the simulation; this pass handles pins the
    // simulation couldn't see (e.g. across components).
    if (pinnedPositions) {
      pinnedPositions.forEach(function (pos, id) {
        var current = allPositions.get(id);
        if (!current) return;
        current.x = pos.x;
        current.y = pos.y;
      });
    }

    // Build a trivial edge-chain per edge (force-directed renders use
    // straight-ish bezier curves; no dummy waypoints required).
    var edgeChains = graph.edges.map(function (e) {
      return { edge: e, chain: [e.from, e.to] };
    });
    var edgePaths = _routeEdges(graph, edgeChains, allPositions);

    var maxX = 0;
    var maxY = 0;
    allPositions.forEach(function (p) {
      var h = p.height || NODE_H;
      if (p.x + NODE_W > maxX) maxX = p.x + NODE_W;
      if (p.y + h > maxY) maxY = p.y + h;
    });
    var svgW = Math.max(PAD + maxCompW + PAD, maxX + PAD);
    var svgH = Math.max(yCursor + PAD, maxY + PAD);

    // Round to integer pixel coords — keeps the DOM diff small on
    // re-renders and makes the SVG attributes tidy.
    allPositions.forEach(function (p) {
      p.x = Math.round(p.x);
      p.y = Math.round(p.y);
    });
    edgePaths.forEach(function (routed) {
      if (!routed || !routed.points) return;
      routed.points.forEach(function (pt) {
        pt.x = Math.round(pt.x);
        pt.y = Math.round(pt.y);
      });
    });

    // Save for the fast path — subsequent calls with the same structure
    // reuse these positions. We snapshot, not alias, so pin-overrides on
    // future calls don't leak back into the cache.
    var snap = new Map();
    allPositions.forEach(function (p, id) {
      snap.set(id, {
        x: p.x,
        y: p.y,
        node: p.node,
        kind: p.kind || "real",
        height: p.height,
      });
    });
    _layoutCache = { fingerprint: fp, positions: snap, maxCompW: maxCompW };

    return {
      positions: allPositions,
      edgePaths: edgePaths,
      svgW: Math.round(svgW),
      svgH: Math.round(svgH),
      hasCycles: false,
    };
  }

  // _forceLayoutComponent runs Fruchterman-Reingold on one connected
  // component, origin-anchored. Positions carry node heights (labels
  // drive variable vertical extent) so rendering math stays consistent.
  function _forceLayoutComponent(graph, pinnedPositions) {
    var nodes = graph.nodes;
    var N = nodes.length;

    // Deterministic initial layout: arrange nodes on a circle so every
    // render starts from the same seed. For N=1 we keep the single node
    // at origin.
    var positions = new Map();
    var radius = Math.max(120, Math.sqrt(N) * 120);
    nodes.forEach(function (n, i) {
      var angle = (i / Math.max(1, N)) * Math.PI * 2;
      var h = _computeNodeHeight(n);
      var cx = N === 1 ? 0 : Math.cos(angle) * radius;
      var cy = N === 1 ? 0 : Math.sin(angle) * radius;
      positions.set(n.id, {
        x: cx - NODE_W / 2,
        y: cy - h / 2,
        node: n,
        kind: "real",
        height: h,
      });
    });

    // Edge adjacency: every kind counts once (symmetric for the sim).
    var adj = new Map();
    nodes.forEach(function (n) {
      adj.set(n.id, new Set());
    });
    graph.edges.forEach(function (e) {
      if (!positions.has(e.from) || !positions.has(e.to)) return;
      adj.get(e.from).add(e.to);
      adj.get(e.to).add(e.from);
    });

    // Pinned nodes stay put during the simulation so user-placed nodes
    // act as constraints the rest of the graph flows around.
    function isPinned(id) {
      return !!(pinnedPositions && pinnedPositions.has(id));
    }
    if (pinnedPositions) {
      pinnedPositions.forEach(function (pos, id) {
        var p = positions.get(id);
        if (!p) return;
        p.x = pos.x - NODE_W / 2; // caller stores top-left; we want centres
        p.y = pos.y - (p.height || NODE_H) / 2;
      });
    }

    if (N === 1) {
      return _normaliseComponentLayout(positions);
    }

    // Main simulation loop. Temperature decays linearly so late
    // iterations just fine-tune while early ones make big moves.
    var temperature = Math.max(radius * 0.4, 80);
    var coolRate = temperature / FORCE_ITERATIONS;
    for (var iter = 0; iter < FORCE_ITERATIONS; iter++) {
      var totalDisp = _forceStep(positions, adj, temperature, isPinned);
      temperature = Math.max(0, temperature - coolRate);
      if (totalDisp < FORCE_EPSILON * N) break;
    }

    return _normaliseComponentLayout(positions);
  }

  function _forceStep(positions, adj, temperature, isPinned) {
    var ids = Array.from(positions.keys());
    var disp = new Map();
    ids.forEach(function (id) {
      disp.set(id, { dx: 0, dy: 0 });
    });

    // Repulsion: every node pushes every other node. The bubble
    // separation kick-in ensures nodes never overlap their rectangles.
    for (var i = 0; i < ids.length; i++) {
      var pa = positions.get(ids[i]);
      var cxA = pa.x + NODE_W / 2;
      var cyA = pa.y + (pa.height || NODE_H) / 2;
      for (var j = i + 1; j < ids.length; j++) {
        var pb = positions.get(ids[j]);
        var cxB = pb.x + NODE_W / 2;
        var cyB = pb.y + (pb.height || NODE_H) / 2;
        var dx = cxA - cxB;
        var dy = cyA - cyB;
        var d2 = dx * dx + dy * dy;
        if (d2 < 1) {
          // Random kick to break exact overlaps.
          dx = i - j || 1;
          dy = j - i || 1;
          d2 = dx * dx + dy * dy;
        }
        var d = Math.sqrt(d2);
        var f = FORCE_REPULSION / d2;
        // Hard anti-overlap: if bubbles intersect, add a strong extra push.
        var minSep = FORCE_NODE_RADIUS * 2;
        if (d < minSep) {
          f += (minSep - d) * 3.5;
        }
        var fx = (dx / d) * f;
        var fy = (dy / d) * f;
        var da = disp.get(ids[i]);
        var db = disp.get(ids[j]);
        da.dx += fx;
        da.dy += fy;
        db.dx -= fx;
        db.dy -= fy;
      }
    }

    // Attraction: spring along every edge.
    adj.forEach(function (neighbours, id) {
      neighbours.forEach(function (nid) {
        if (nid <= id) return; // each edge once
        var pa = positions.get(id);
        var pb = positions.get(nid);
        if (!pa || !pb) return;
        var cxA = pa.x + NODE_W / 2;
        var cyA = pa.y + (pa.height || NODE_H) / 2;
        var cxB = pb.x + NODE_W / 2;
        var cyB = pb.y + (pb.height || NODE_H) / 2;
        var dx = cxA - cxB;
        var dy = cyA - cyB;
        var d = Math.sqrt(dx * dx + dy * dy) || 1;
        var f = (d * d) / FORCE_K_IDEAL;
        var fx = (dx / d) * f;
        var fy = (dy / d) * f;
        var da = disp.get(id);
        var db = disp.get(nid);
        da.dx -= fx;
        da.dy -= fy;
        db.dx += fx;
        db.dy += fy;
      });
    });

    // Node-edge repulsion: when a non-endpoint node sits close to an
    // edge's straight line, push it perpendicular to the line so the
    // final layout doesn't have edges slicing through unrelated nodes.
    // The clearance threshold matches EDGE_CLEARANCE_PAD in the router
    // so the force nudges nodes *just* far enough for the straight line
    // to clear them — detour routing picks up the rest.
    var NODE_EDGE_CLEARANCE = FORCE_NODE_RADIUS + 30;
    var NODE_EDGE_STRENGTH = 420;
    adj.forEach(function (neighbours, fromId) {
      neighbours.forEach(function (toId) {
        if (toId <= fromId) return;
        var pa = positions.get(fromId);
        var pb = positions.get(toId);
        if (!pa || !pb) return;
        var aCx = pa.x + NODE_W / 2;
        var aCy = pa.y + (pa.height || NODE_H) / 2;
        var bCx = pb.x + NODE_W / 2;
        var bCy = pb.y + (pb.height || NODE_H) / 2;
        var ex = bCx - aCx;
        var ey = bCy - aCy;
        var elen = Math.sqrt(ex * ex + ey * ey);
        if (elen < 1) return;
        var eux = ex / elen;
        var euy = ey / elen;
        var enx = -euy;
        var eny = eux;
        for (var k = 0; k < ids.length; k++) {
          var nid = ids[k];
          if (nid === fromId || nid === toId) continue;
          var pn = positions.get(nid);
          var ncx = pn.x + NODE_W / 2;
          var ncy = pn.y + (pn.height || NODE_H) / 2;
          var rx = ncx - aCx;
          var ry = ncy - aCy;
          var t = (rx * eux + ry * euy) / elen;
          if (t < 0.05 || t > 0.95) continue;
          var d = rx * enx + ry * eny; // signed perp distance
          var absD = Math.abs(d);
          if (absD > NODE_EDGE_CLEARANCE) continue;
          var push = (NODE_EDGE_CLEARANCE - absD) / NODE_EDGE_CLEARANCE;
          // Scale with the squared deficit so the force drops off
          // quickly once the node has moved out of the corridor.
          var mag = NODE_EDGE_STRENGTH * push * push;
          var sign = d >= 0 ? 1 : -1;
          var pfx = enx * sign * mag;
          var pfy = eny * sign * mag;
          var dn = disp.get(nid);
          dn.dx += pfx;
          dn.dy += pfy;
          // Equal-and-opposite split over the two endpoints so the
          // edge itself also shifts out of the node's way.
          var da2 = disp.get(fromId);
          var db2 = disp.get(toId);
          da2.dx -= pfx * 0.5;
          da2.dy -= pfy * 0.5;
          db2.dx -= pfx * 0.5;
          db2.dy -= pfy * 0.5;
        }
      });
    });

    // Apply, clamped by temperature.
    var total = 0;
    disp.forEach(function (d, id) {
      if (isPinned(id)) return;
      var mag = Math.sqrt(d.dx * d.dx + d.dy * d.dy);
      if (mag < 1e-6) return;
      var step = Math.min(mag, temperature);
      var p = positions.get(id);
      p.x += (d.dx / mag) * step;
      p.y += (d.dy / mag) * step;
      total += step;
    });
    return total;
  }

  function _normaliseComponentLayout(positions) {
    var minX = Infinity;
    var minY = Infinity;
    positions.forEach(function (p) {
      if (p.x < minX) minX = p.x;
      if (p.y < minY) minY = p.y;
    });
    if (minX === Infinity) minX = 0;
    if (minY === Infinity) minY = 0;
    var maxX = 0;
    var maxY = 0;
    positions.forEach(function (p) {
      p.x -= minX;
      p.y -= minY;
      var h = p.height || NODE_H;
      if (p.x + NODE_W > maxX) maxX = p.x + NODE_W;
      if (p.y + h > maxY) maxY = p.y + h;
    });
    return { positions: positions, width: maxX, height: maxY };
  }

  // layoutSugiyama — real layered (Sugiyama) layout, the primary Map
  // layout. For DAG-shaped graphs (spec containment + task deps) a
  // layered layout produces clean columns where edges flow left→right
  // without slicing through unrelated nodes. Force-directed got used
  // for a while but visibly tangles on DAGs.
  //
  // Pipeline:
  //   1. Split the graph into connected components.
  //   2. Run `_layoutComponent` on each (layers → dummies → crossing
  //      minimisation → coordinate assignment).
  //   3. Stack components vertically.
  //   4. Apply any user-pinned positions as absolute overrides.
  //   5. Route edges (including detours around any non-endpoint nodes
  //      that end up in a direct-line's path — rare under Sugiyama,
  //      common when pins displace nodes off the layer grid).
  //
  // Cache structure fingerprint so pin-only changes skip the
  // recomputation — matches the fast path the renderer expects.
  function layoutSugiyama(graph, opts) {
    opts = opts || {};
    var pinnedPositions =
      opts.pinnedPositions && typeof opts.pinnedPositions.get === "function"
        ? opts.pinnedPositions
        : null;

    var fp = _structureFingerprint(graph);
    if (_layoutCache.fingerprint === fp && _layoutCache.positions) {
      var cached = new Map();
      _layoutCache.positions.forEach(function (p, id) {
        cached.set(id, {
          x: p.x,
          y: p.y,
          node: p.node,
          kind: p.kind || "real",
          height: p.height,
        });
      });
      if (pinnedPositions) {
        pinnedPositions.forEach(function (pos, id) {
          var c = cached.get(id);
          if (!c) return;
          c.x = pos.x;
          c.y = pos.y;
        });
      }
      var chainsFast = _layoutCache.edgeChains || [];
      var edgePathsFast = _routeEdges(graph, chainsFast, cached);
      var maxXF = 0;
      var maxYF = 0;
      cached.forEach(function (p) {
        var h = p.height || NODE_H;
        if (p.x + NODE_W > maxXF) maxXF = p.x + NODE_W;
        if (p.y + h > maxYF) maxYF = p.y + h;
      });
      edgePathsFast.forEach(function (routed) {
        if (!routed || !routed.points) return;
        routed.points.forEach(function (pt) {
          pt.x = Math.round(pt.x);
          pt.y = Math.round(pt.y);
        });
      });
      return {
        positions: cached,
        edgePaths: edgePathsFast,
        svgW: Math.round(maxXF + PAD),
        svgH: Math.round(maxYF + PAD),
        hasCycles: false,
      };
    }

    var components = _connectedComponents(graph);
    if (components.length === 0) {
      return {
        positions: new Map(),
        edgePaths: [],
        svgW: PAD * 2,
        svgH: PAD * 2,
        hasCycles: false,
      };
    }

    var allPositions = new Map();
    var allEdgeChains = [];
    var yCursor = PAD;
    var maxCompW = 0;
    var layouts = [];
    components.forEach(function (comp) {
      var L = _layoutComponent(comp);
      layouts.push(L);
      if (L.width > maxCompW) maxCompW = L.width;
    });
    layouts.forEach(function (L) {
      var xOffset = Math.max(PAD, PAD + (maxCompW - L.width) / 2);
      L.positions.forEach(function (p, id) {
        p.x += xOffset;
        p.y += yCursor;
        allPositions.set(id, p);
      });
      // Component edge chains use node ids which are already globally
      // unique, so they can be merged directly once positions are
      // translated into the global canvas.
      for (var i = 0; i < L.edgeChains.length; i++) {
        allEdgeChains.push(L.edgeChains[i]);
      }
      yCursor += L.height + COMPONENT_GAP;
    });
    yCursor -= COMPONENT_GAP;

    if (pinnedPositions) {
      pinnedPositions.forEach(function (pos, id) {
        var current = allPositions.get(id);
        if (!current) return;
        current.x = pos.x;
        current.y = pos.y;
      });
    }

    var edgePaths = _routeEdges(graph, allEdgeChains, allPositions);

    var maxX = 0;
    var maxY = 0;
    allPositions.forEach(function (p) {
      var h = p.height || NODE_H;
      if (p.x + NODE_W > maxX) maxX = p.x + NODE_W;
      if (p.y + h > maxY) maxY = p.y + h;
    });
    var svgW = Math.max(PAD + maxCompW + PAD, maxX + PAD);
    var svgH = Math.max(yCursor + PAD, maxY + PAD);

    allPositions.forEach(function (p) {
      p.x = Math.round(p.x);
      p.y = Math.round(p.y);
    });
    edgePaths.forEach(function (routed) {
      if (!routed || !routed.points) return;
      routed.points.forEach(function (pt) {
        pt.x = Math.round(pt.x);
        pt.y = Math.round(pt.y);
      });
    });

    // Snapshot for the fast path on subsequent calls with the same
    // structure (pin-only changes, hover, search).
    var snap = new Map();
    allPositions.forEach(function (p, id) {
      snap.set(id, {
        x: p.x,
        y: p.y,
        node: p.node,
        kind: p.kind || "real",
        height: p.height,
      });
    });
    _layoutCache = {
      fingerprint: fp,
      positions: snap,
      maxCompW: maxCompW,
      edgeChains: allEdgeChains,
    };

    return {
      positions: allPositions,
      edgePaths: edgePaths,
      svgW: Math.round(svgW),
      svgH: Math.round(svgH),
      hasCycles: layouts.some(function (L) {
        return L.hasCycles;
      }),
    };
  }

  // _connectedComponents returns an Array<{nodes, edges}> where each
  // component is reachable under undirected edges. Preserves the node/
  // edge kind so each sub-layout renders identically to a stand-alone
  // graph.
  function _connectedComponents(graph) {
    var nodeById = new Map();
    graph.nodes.forEach(function (n) {
      nodeById.set(n.id, n);
    });
    var adj = new Map();
    graph.nodes.forEach(function (n) {
      adj.set(n.id, []);
    });
    graph.edges.forEach(function (e) {
      if (!nodeById.has(e.from) || !nodeById.has(e.to)) return;
      adj.get(e.from).push(e.to);
      adj.get(e.to).push(e.from);
    });

    var componentOf = new Map();
    var components = [];
    graph.nodes.forEach(function (n) {
      if (componentOf.has(n.id)) return;
      var compIdx = components.length;
      var comp = { nodes: [], edges: [] };
      components.push(comp);
      // Iterative BFS to avoid call-stack limits on deep chains.
      var stack = [n.id];
      while (stack.length > 0) {
        var id = stack.pop();
        if (componentOf.has(id)) continue;
        componentOf.set(id, compIdx);
        comp.nodes.push(nodeById.get(id));
        var neigh = adj.get(id) || [];
        for (var i = 0; i < neigh.length; i++) {
          if (!componentOf.has(neigh[i])) stack.push(neigh[i]);
        }
      }
    });

    graph.edges.forEach(function (e) {
      var ci = componentOf.get(e.from);
      if (ci === undefined) return;
      if (componentOf.get(e.to) !== ci) return; // cross-component; drop
      components[ci].edges.push(e);
    });

    // Sort: larger components first so the visual centre of gravity is
    // at the top of the canvas.
    components.sort(function (a, b) {
      return b.nodes.length - a.nodes.length;
    });
    return components;
  }

  // _layoutComponent runs Sugiyama on a single connected component and
  // returns normalised positions anchored at origin (0, 0). Caller is
  // responsible for translating (x, y) when composing multiple
  // components onto the canvas.
  function _layoutComponent(graph) {
    var nodeById = new Map();
    graph.nodes.forEach(function (n) {
      nodeById.set(n.id, n);
    });

    var layerOf = _assignLayers(graph, nodeById);
    var cycleNodes = graph.nodes.filter(function (n) {
      return !layerOf.has(n.id);
    });
    var maxLayer = -1;
    layerOf.forEach(function (L) {
      if (L > maxLayer) maxLayer = L;
    });
    cycleNodes.forEach(function (n) {
      layerOf.set(n.id, maxLayer + 1);
    });

    var layers = _groupByLayer(graph.nodes, layerOf);
    var expanded = _insertDummies(graph, layerOf, layers);
    _minimizeCrossings(expanded.layers, expanded.adjDown, expanded.adjUp);
    var positions = _assignCoordinates(
      expanded.layers,
      expanded.adjDown,
      expanded.adjUp,
    );

    // Normalise to origin (0, 0) so the caller can freely translate.
    var minX = Infinity;
    var minY = Infinity;
    positions.forEach(function (p) {
      if (p.x < minX) minX = p.x;
      if (p.y < minY) minY = p.y;
    });
    if (minX === Infinity) minX = 0;
    if (minY === Infinity) minY = 0;
    var maxX = 0;
    var maxY = 0;
    positions.forEach(function (p) {
      p.x -= minX;
      p.y -= minY;
      var h = p.height || NODE_H;
      if (p.x + NODE_W > maxX) maxX = p.x + NODE_W;
      if (p.y + h > maxY) maxY = p.y + h;
    });

    return {
      positions: positions,
      edgeChains: expanded.edgeChains,
      width: maxX,
      height: maxY,
      hasCycles: cycleNodes.length > 0,
    };
  }

  // _assignLayers returns a Map<nodeId, layerIndex>. We use *median*
  // layering: each node's layer is the midpoint of its ASAP (earliest
  // layer it could legally sit in) and ALAP (latest layer it could
  // legally sit in). The midpoint spreads nodes evenly across columns
  // instead of piling every zero-in-degree node at layer 0 — critical
  // when a workspace has many standalone tasks that share "layer 0"
  // with every root spec under longest-path-from-source layering.
  //
  // Edge invariant (for all edges a→b, layer[a] < layer[b]) is preserved
  // because ASAP/ALAP respect the invariant independently, so their
  // midpoints do too (proof: asap[b] ≥ asap[a]+1 and alap[b] ≥ alap[a]+1
  // imply asap[b]+alap[b] ≥ asap[a]+alap[a]+2).
  function _assignLayers(graph, nodeById) {
    var adjDown = new Map();
    var adjUp = new Map();
    graph.nodes.forEach(function (n) {
      adjDown.set(n.id, []);
      adjUp.set(n.id, []);
    });
    graph.edges.forEach(function (e) {
      if (!nodeById.has(e.from) || !nodeById.has(e.to)) return;
      adjDown.get(e.from).push(e.to);
      adjUp.get(e.to).push(e.from);
    });

    // Topological order via DFS. Nodes in cycles get skipped; the caller
    // places them in a trailing cycle column.
    var topo = _topologicalOrder(graph.nodes, adjDown);
    if (!topo) return new Map(); // cycle-only graph; caller handles

    // ASAP: layer[a] = max(layer[pred]+1), default 0.
    var asap = new Map();
    topo.forEach(function (id) {
      var preds = adjUp.get(id) || [];
      var m = 0;
      for (var i = 0; i < preds.length; i++) {
        if (!asap.has(preds[i])) continue;
        if (asap.get(preds[i]) + 1 > m) m = asap.get(preds[i]) + 1;
      }
      asap.set(id, m);
    });
    var maxLayer = 0;
    asap.forEach(function (v) {
      if (v > maxLayer) maxLayer = v;
    });

    // ALAP: layer[a] = min(layer[succ]-1), default maxLayer.
    var alap = new Map();
    for (var i = topo.length - 1; i >= 0; i--) {
      var id = topo[i];
      var succs = adjDown.get(id) || [];
      var m = maxLayer;
      var hasSucc = false;
      for (var j = 0; j < succs.length; j++) {
        if (!alap.has(succs[j])) continue;
        hasSucc = true;
        if (alap.get(succs[j]) - 1 < m) m = alap.get(succs[j]) - 1;
      }
      alap.set(id, hasSucc ? m : maxLayer);
    }

    // Median (round toward source so shorter edges are preferred).
    var layer = new Map();
    asap.forEach(function (a, id) {
      var b = alap.has(id) ? alap.get(id) : a;
      layer.set(id, Math.floor((a + b) / 2));
    });
    return layer;
  }

  // _topologicalOrder returns an Array<nodeId> in topological order, or
  // null if the graph has a cycle reachable from this traversal.
  function _topologicalOrder(nodes, adjDown) {
    var visited = new Map(); // 0 = unseen, 1 = on stack, 2 = done
    var order = [];
    var hadCycle = false;
    function visit(id) {
      if (hadCycle) return;
      var state = visited.get(id);
      if (state === 2) return;
      if (state === 1) {
        hadCycle = true;
        return;
      }
      visited.set(id, 1);
      var nexts = adjDown.get(id) || [];
      for (var i = 0; i < nexts.length; i++) visit(nexts[i]);
      visited.set(id, 2);
      order.push(id);
    }
    for (var k = 0; k < nodes.length; k++) visit(nodes[k].id);
    if (hadCycle) return null;
    order.reverse();
    return order;
  }

  function _groupByLayer(nodes, layerOf) {
    var layers = [];
    nodes.forEach(function (n) {
      var L = layerOf.has(n.id) ? layerOf.get(n.id) : 0;
      while (layers.length <= L) layers.push([]);
      layers[L].push({ id: n.id, node: n, kind: "real" });
    });
    return layers;
  }

  function _insertDummies(graph, layerOf, layers) {
    var dummyCounter = 0;
    var expandedLayers = layers.map(function (L) {
      return L.slice();
    });
    var adjDown = new Map();
    var adjUp = new Map();
    var edgeChains = [];

    function ensure(id, bucket) {
      if (!bucket.has(id)) bucket.set(id, []);
    }
    graph.nodes.forEach(function (n) {
      ensure(n.id, adjDown);
      ensure(n.id, adjUp);
    });

    graph.edges.forEach(function (edge) {
      var fromL = layerOf.get(edge.from);
      var toL = layerOf.get(edge.to);
      if (fromL === undefined || toL === undefined || toL <= fromL) {
        edgeChains.push({ edge: edge, chain: [edge.from, edge.to] });
        return;
      }
      var chain = [edge.from];
      var prev = edge.from;
      for (var L = fromL + 1; L < toL; L++) {
        var did = "__dummy_" + dummyCounter++;
        chain.push(did);
        expandedLayers[L].push({ id: did, node: null, kind: "dummy" });
        ensure(did, adjDown);
        ensure(did, adjUp);
        adjDown.get(prev).push(did);
        adjUp.get(did).push(prev);
        prev = did;
      }
      chain.push(edge.to);
      adjDown.get(prev).push(edge.to);
      adjUp.get(edge.to).push(prev);
      edgeChains.push({ edge: edge, chain: chain });
    });

    return {
      layers: expandedLayers,
      adjDown: adjDown,
      adjUp: adjUp,
      edgeChains: edgeChains,
    };
  }

  function _minimizeCrossings(layers, adjDown, adjUp) {
    // Seed with a deterministic order so re-renders of the same data
    // produce identical layouts.
    layers.forEach(function (layer) {
      layer.sort(function (a, b) {
        if (a.kind !== b.kind) return a.kind === "dummy" ? 1 : -1;
        var an = a.node;
        var bn = b.node;
        if (!an || !bn) return a.id < b.id ? -1 : 1;
        if (an.kind !== bn.kind) return an.kind === "spec" ? -1 : 1;
        return a.id < b.id ? -1 : 1;
      });
    });

    for (var iter = 0; iter < BARYCENTER_ITERATIONS; iter++) {
      for (var l = 1; l < layers.length; l++) {
        _reorderByBarycenter(layers[l], layers[l - 1], adjUp);
      }
      for (var m = layers.length - 2; m >= 0; m--) {
        _reorderByBarycenter(layers[m], layers[m + 1], adjDown);
      }
    }
  }

  function _reorderByBarycenter(target, reference, neighborMap) {
    var refIndex = new Map();
    reference.forEach(function (item, idx) {
      refIndex.set(item.id, idx);
    });
    target.forEach(function (item, origIdx) {
      var neighbors = neighborMap.get(item.id) || [];
      var sum = 0;
      var count = 0;
      neighbors.forEach(function (nId) {
        if (refIndex.has(nId)) {
          sum += refIndex.get(nId);
          count++;
        }
      });
      item._bc = count > 0 ? sum / count : origIdx;
      item._origIdx = origIdx;
    });
    target.sort(function (a, b) {
      if (a._bc !== b._bc) return a._bc - b._bc;
      return a._origIdx - b._origIdx;
    });
    target.forEach(function (item) {
      delete item._bc;
      delete item._origIdx;
    });
  }

  // _assignCoordinates turns the per-layer ordering into (x, y) coords.
  // We run a two-pass coordinate assignment so edges read straight:
  //   1. Seed y by the node's position in its layer (naïve top-align).
  //   2. Alternate down/up sweeps that set each node's desired y to the
  //      mean y of its neighbours in the adjacent layer, then resolve
  //      overlaps by packing the column top-to-bottom with at least
  //      NODE_H + V_GAP between successive nodes.
  //
  // The result is that nodes with a shared predecessor cluster at
  // similar heights across columns — edges read as mostly-straight
  // lines instead of zig-zagging between arbitrary slots.
  // _itemHeight returns the pixel height of a layer item. Real nodes can
  // grow beyond NODE_H when their label wraps to multiple lines; dummies
  // are zero-height rails between real nodes.
  function _itemHeight(item) {
    if (item.kind === "dummy") return DUMMY_H;
    return item.node ? _computeNodeHeight(item.node) : NODE_H;
  }

  function _assignCoordinates(layers, adjDown, adjUp) {
    var positions = new Map();

    // Seed: top-align each column, letting taller (wrapped) nodes push
    // subsequent nodes further down.
    layers.forEach(function (layer, L) {
      var x = PAD + L * (NODE_W + H_GAP);
      var y = PAD;
      layer.forEach(function (item) {
        var h = _itemHeight(item);
        positions.set(item.id, {
          x: item.kind === "dummy" ? x + NODE_W / 2 : x,
          y: y,
          node: item.node || null,
          kind: item.kind,
          height: h,
        });
        y += h + V_GAP;
      });
    });

    if (!adjDown || !adjUp) return positions;

    // Sweep helper: push each node toward the mean y of its neighbours'
    // centres in the reference layer, preserving order and packing to
    // prevent overlaps. Desired y for the current node is the mean
    // reference centre minus the current node's half-height, so centres
    // line up across columns regardless of node height.
    function sweep(layerIdx, refLayerIdx, neighborMap) {
      var layer = layers[layerIdx];
      if (!layer || layer.length === 0) return;
      var refLayer = layers[refLayerIdx];
      if (!refLayer) return;

      var desired = layer.map(function (item, idx) {
        var ns = neighborMap.get(item.id) || [];
        var sum = 0;
        var count = 0;
        for (var i = 0; i < ns.length; i++) {
          var p = positions.get(ns[i]);
          if (!p) continue;
          sum += p.y + (p.height || 0) / 2;
          count++;
        }
        var currentPos = positions.get(item.id);
        var h = currentPos
          ? currentPos.height || _itemHeight(item)
          : _itemHeight(item);
        var centre =
          count > 0 ? sum / count : (currentPos ? currentPos.y : 0) + h / 2;
        return {
          item: item,
          idx: idx,
          height: h,
          desired: centre - h / 2,
        };
      });

      var cursor = PAD;
      for (var i = 0; i < desired.length; i++) {
        var d = desired[i];
        var y = Math.max(d.desired, cursor);
        var pos = positions.get(d.item.id);
        pos.y = y;
        cursor = y + d.height + V_GAP;
      }
    }

    for (var iter = 0; iter < 6; iter++) {
      for (var L = 1; L < layers.length; L++) sweep(L, L - 1, adjUp);
      for (var M = layers.length - 2; M >= 0; M--) sweep(M, M + 1, adjDown);
    }

    return positions;
  }

  function _layoutHeight(layers) {
    var max = 0;
    layers.forEach(function (layer) {
      var h = layer.reduce(function (sum, item) {
        return sum + _itemHeight(item) + V_GAP;
      }, 0);
      if (h > max) max = h;
    });
    return max;
  }

  // _rectPerimeterPoint returns the point where the ray from the node
  // centre toward (targetX, targetY) intersects the node's rectangle
  // border. This is what attaches edges to the *edge* of a node
  // rectangle instead of its centre, so multiple edges fan out around
  // the perimeter rather than converging at one point.
  //
  // Geometry: clamp the parameter t so that |dx|*t ≤ halfW and
  // |dy|*t ≤ halfH, pick the smaller t. A tiny inset avoids the stroke
  // of the rectangle overlapping the edge line at the join.
  function _rectPerimeterPoint(pos, targetX, targetY) {
    var halfW = NODE_W / 2;
    var halfH = (pos.height || NODE_H) / 2;
    var cx = pos.x + halfW;
    var cy = pos.y + halfH;
    var dx = targetX - cx;
    var dy = targetY - cy;
    if (dx === 0 && dy === 0) return { x: cx + halfW, y: cy };
    var absDx = Math.abs(dx);
    var absDy = Math.abs(dy);
    // Ray scale: how far we travel along (dx, dy) before hitting the
    // rectangle border. Either side or top/bottom, whichever we reach
    // first.
    var tx = absDx > 0 ? halfW / absDx : Infinity;
    var ty = absDy > 0 ? halfH / absDy : Infinity;
    var t = Math.min(tx, ty);
    return { x: cx + dx * t, y: cy + dy * t };
  }

  // _edgeDetourWaypoints inspects the straight line between startPt and
  // endPt and returns an array of detour waypoints needed to route the
  // edge around any non-endpoint nodes that sit in the direct path.
  //
  // Return shapes:
  //   []              → the straight line is clear; draw a plain cubic.
  //   [w]             → one side has blockers; draw a quadratic bowing
  //                     to the opposite side through w.
  //   [w1, w2]        → blockers on both sides; draw a cubic S-curve
  //                     with control points at w1 (early t) and w2 (late t).
  //
  // The apex for each side covers the *worst* blocker on that side, so
  // a single waypoint pushes the curve far enough to dodge every node
  // in that half-plane.
  var EDGE_CLEARANCE_PAD = 28;
  function _edgeDetourWaypoints(startPt, endPt, positions, excludeIds) {
    var dx = endPt.x - startPt.x;
    var dy = endPt.y - startPt.y;
    var len = Math.sqrt(dx * dx + dy * dy);
    if (len < 60) return []; // very short edges can't route around anything
    var ux = dx / len;
    var uy = dy / len;
    var nx = -uy;
    var ny = ux;
    var posSide = null; // worst blocker with d > 0
    var negSide = null; // worst blocker with d < 0
    positions.forEach(function (pos, id) {
      if (excludeIds.has(id)) return;
      if (pos.kind === "dummy" || !pos.node) return;
      var halfW = NODE_W / 2;
      var halfH = (pos.height || NODE_H) / 2;
      var cx = pos.x + halfW;
      var cy = pos.y + halfH;
      var t = ((cx - startPt.x) * ux + (cy - startPt.y) * uy) / len;
      if (t < 0.1 || t > 0.9) return;
      var d = (cx - startPt.x) * nx + (cy - startPt.y) * ny;
      var rPerp = Math.abs(nx) * halfW + Math.abs(ny) * halfH;
      var clearance = rPerp + EDGE_CLEARANCE_PAD;
      if (Math.abs(d) > clearance) return;
      var deficit = clearance - Math.abs(d);
      var rec = { d: d, deficit: deficit, clearance: clearance, t: t };
      if (d >= 0) {
        if (!posSide || deficit > posSide.deficit) posSide = rec;
      } else {
        if (!negSide || deficit > negSide.deficit) negSide = rec;
      }
    });
    if (!posSide && !negSide) return [];

    var mx = (startPt.x + endPt.x) / 2;
    var my = (startPt.y + endPt.y) / 2;

    // Helper: for a blocker at (t, d) on one side, produce a waypoint on
    // the opposite side sized to clear it.
    function wpForBlocker(b, signForWaypoint) {
      // Quadratic: curve peak = waypoint_offset / 2. Cubic S-curve with
      // two CPs: peak ≈ waypoint_offset / 2.6 on each side. We size for
      // the more conservative case so single-side curves bow a bit more
      // than strictly required; slightly roomier is more legible.
      var apex = 2.2 * (b.clearance + Math.abs(b.d));
      var baseX = startPt.x + (endPt.x - startPt.x) * b.t;
      var baseY = startPt.y + (endPt.y - startPt.y) * b.t;
      return {
        x: baseX + signForWaypoint * nx * apex,
        y: baseY + signForWaypoint * ny * apex,
        t: b.t,
      };
    }

    if (posSide && !negSide) {
      // Bend to -n side (opposite of the +n blocker). Waypoint at
      // midpoint rather than blocker's t so a quadratic centres its
      // apex over the line's middle.
      var w = {
        x: mx - nx * 2 * (posSide.clearance + Math.abs(posSide.d)),
        y: my - ny * 2 * (posSide.clearance + Math.abs(posSide.d)),
      };
      return [w];
    }
    if (negSide && !posSide) {
      var w2 = {
        x: mx + nx * 2 * (negSide.clearance + Math.abs(negSide.d)),
        y: my + ny * 2 * (negSide.clearance + Math.abs(negSide.d)),
      };
      return [w2];
    }
    // Both sides. Build an S-curve: dodge +n blocker by bending toward
    // -n, then dodge -n blocker by bending toward +n. Order the two
    // waypoints along the chord so the S forms naturally.
    var wpPos = wpForBlocker(posSide, -1); // bend to -n
    var wpNeg = wpForBlocker(negSide, +1); // bend to +n
    return wpPos.t <= wpNeg.t ? [wpPos, wpNeg] : [wpNeg, wpPos];
  }

  function _routeEdges(graph, edgeChains, positions) {
    return edgeChains
      .map(function (entry) {
        var chain = entry.chain;
        if (!chain || chain.length < 2) return null;
        var pts = [];
        // Collect intermediate (dummy) waypoints as their stored coords.
        // The source and destination points are computed against the
        // rectangle perimeter so parallel edges fan around the node.
        for (var i = 0; i < chain.length; i++) {
          var p = positions.get(chain[i]);
          if (!p) return null;
          pts.push(p);
        }
        var first = pts[0];
        var last = pts[pts.length - 1];
        // Aim-from point for the source: the next waypoint after it.
        var aimFromNext = pts[1];
        var nextX =
          aimFromNext.kind === "dummy"
            ? aimFromNext.x
            : aimFromNext.x + NODE_W / 2;
        var nextY =
          aimFromNext.kind === "dummy"
            ? aimFromNext.y
            : aimFromNext.y + (aimFromNext.height || NODE_H) / 2;
        // Aim-from point for the destination: the waypoint before it.
        var aimToPrev = pts[pts.length - 2];
        var prevX =
          aimToPrev.kind === "dummy" ? aimToPrev.x : aimToPrev.x + NODE_W / 2;
        var prevY =
          aimToPrev.kind === "dummy"
            ? aimToPrev.y
            : aimToPrev.y + (aimToPrev.height || NODE_H) / 2;

        var startPt = _rectPerimeterPoint(first, nextX, nextY);
        var endPt = _rectPerimeterPoint(last, prevX, prevY);
        var points = [startPt];
        for (var j = 1; j < pts.length - 1; j++) {
          points.push({ x: pts[j].x, y: pts[j].y });
        }
        // Straight-line routing: no detour waypoints. Edges go in a
        // direct polyline from source perimeter to destination
        // perimeter (with any Sugiyama dummy waypoints along the way).
        points.push(endPt);
        return { edge: entry.edge, points: points };
      })
      .filter(Boolean);
  }

  // _smoothPath returns an SVG path `d` attribute that draws a smooth
  // curve through the given waypoints. For each pair of consecutive
  // points, we use a cubic Bezier whose control points sit one gap-unit
  // horizontally from the endpoints so the curve eases into each node
  // and dummy waypoint. With just two points this reduces to the classic
  // horizontally-symmetric "C-shape" bezier the flat layout used before.
  function _smoothPath(points) {
    if (!points || points.length < 2) return "";
    // Straight-line routing: polyline through every waypoint. Simple
    // and readable — what the user explicitly asked for after trying
    // curved detour routing.
    var d = "M" + points[0].x + "," + points[0].y;
    for (var si = 1; si < points.length; si++) {
      d += " L" + points[si].x + "," + points[si].y;
    }
    return d;
  }

  // Retained for reference but no longer reached — kept inside this
  // fallback-name wrapper in case a future iteration wants the curved
  // routing back without re-deriving the maths.
  function _smoothPathCurved(points) {
    if (!points || points.length === 0) return "";
    // Detour routing — the interior points are *control points*, not
    // waypoints the curve passes through:
    //
    //   3 points: quadratic bezier; curve bows toward the control.
    //   4 points: cubic bezier with two controls; produces a smooth
    //             S-curve, used when blockers exist on both sides of
    //             the direct chord.
    //
    // In both cases the curve stays inside the convex hull of the
    // control polygon, so a waypoint placed well clear of a blocker
    // keeps the curve clear too — no V-kinks, no passes-through.
    if (points.length === 3) {
      var s = points[0];
      var w = points[1];
      var e = points[2];
      return (
        "M" + s.x + "," + s.y + " Q" + w.x + "," + w.y + " " + e.x + "," + e.y
      );
    }
    if (points.length === 4) {
      var s2 = points[0];
      var c1 = points[1];
      var c2 = points[2];
      var e2 = points[3];
      return (
        "M" +
        s2.x +
        "," +
        s2.y +
        " C" +
        c1.x +
        "," +
        c1.y +
        " " +
        c2.x +
        "," +
        c2.y +
        " " +
        e2.x +
        "," +
        e2.y
      );
    }
    var d = "M" + points[0].x + "," + points[0].y;
    for (var i = 1; i < points.length; i++) {
      var a = points[i - 1];
      var b = points[i];
      var dx = b.x - a.x;
      var dy = b.y - a.y;
      // Control-point offset is a fraction of the full segment length,
      // so edges that travel vertically or diagonally get a smooth
      // bow-out instead of a horizontal-only C-curve that would cut
      // through the node at steep angles.
      var dist = Math.sqrt(dx * dx + dy * dy);
      var cpLen = Math.max(20, dist * 0.35);
      // Direction of the segment for the tangent at each endpoint.
      var nx = dist > 0 ? dx / dist : 1;
      var ny = dist > 0 ? dy / dist : 0;
      d +=
        " C" +
        (a.x + nx * cpLen) +
        "," +
        (a.y + ny * cpLen) +
        " " +
        (b.x - nx * cpLen) +
        "," +
        (b.y - ny * cpLen) +
        " " +
        b.x +
        "," +
        b.y;
    }
    return d;
  }

  // renderUnifiedGraph draws the {nodes, edges} graph into the given SVG
  // element. Returns true when anything was rendered, false on empty graph.
  //
  // Options:
  //   onToggleSpec(path)              — user clicked the +/- handle.
  //   onPinNode(id, x, y)             — user dragged a node; commit the new
  //                                     position to the pin store.
  //   onUnpinNode(id)                 — user double-clicked a pinned node.
  //   onFocusNode(id | null)          — user clicked a node (id) or empty
  //                                     canvas (null) to focus/unfocus.
  //   onNavigateNode(id)              — shift+click navigation.
  //   onHoverNode(id | null)          — hover enter/leave; transient highlight.
  //   pinnedIds (Set<string>)         — nodes currently pinned; used to
  //                                     draw the pin corner marker.
  //   focusedNodeId (string | null)   — when set, non-neighbourhood nodes
  //                                     and edges are dimmed so the user
  //                                     can zero in on one topic.
  //   searchQuery (string)            — when non-empty, nodes whose label
  //                                     does not contain it (case-insensitive)
  //                                     are dimmed alongside their edges.
  //   getScale()                      — returns current zoom scale; drag
  //                                     deltas are divided by it so pins
  //                                     land at the intended graph coords
  //                                     regardless of zoom level.
  function renderUnifiedGraph(graph, svg, opts) {
    opts = opts || {};
    if (!graph || !Array.isArray(graph.nodes) || graph.nodes.length === 0) {
      clearChildren(svg);
      return false;
    }
    if (!svg) return false;

    var layout = layoutSugiyama(graph, {
      pinnedPositions: opts.pinnedPositions,
    });

    svg.setAttribute("viewBox", "0 0 " + layout.svgW + " " + layout.svgH);
    svg.setAttribute("width", layout.svgW);
    svg.setAttribute("height", layout.svgH);

    clearChildren(svg);

    var nodeById = new Map();
    graph.nodes.forEach(function (n) {
      nodeById.set(n.id, n);
    });

    // Build a focus neighbourhood so the renderer can dim everything
    // outside of it. Includes the focused node itself plus every direct
    // neighbour reachable along any edge kind (up or down). 1-hop is
    // intentional: 2-hop tends to re-include most of the graph and
    // defeats the "zero in on one topic" goal.
    var focusedId = opts.focusedNodeId || null;
    var focusSet = null;
    if (focusedId && nodeById.has(focusedId)) {
      focusSet = new Set([focusedId]);
      graph.edges.forEach(function (e) {
        if (e.from === focusedId) focusSet.add(e.to);
        if (e.to === focusedId) focusSet.add(e.from);
      });
    }

    // Search filter: dim nodes whose label doesn't match the query
    // (case-insensitive). Spec paths count too so users can find by path
    // fragment. Matching is the intersection with focusSet when both are
    // active — focus narrows, search further narrows.
    var rawQuery = typeof opts.searchQuery === "string" ? opts.searchQuery : "";
    var query = rawQuery.trim().toLowerCase();
    var searchSet = null;
    if (query) {
      searchSet = new Set();
      graph.nodes.forEach(function (n) {
        var label = (n.label || "").toLowerCase();
        var path = (n.extra && n.extra.path ? n.extra.path : "").toLowerCase();
        if (
          label.indexOf(query) !== -1 ||
          (path && path.indexOf(query) !== -1)
        ) {
          searchSet.add(n.id);
        }
      });
    }

    // inFocus combines focus and search: a node is visible (full opacity)
    // only when it satisfies *both* filters. null means no filter.
    function inFocus(id) {
      if (focusSet && !focusSet.has(id)) return false;
      if (searchSet && !searchSet.has(id)) return false;
      return true;
    }
    var anyDim = !!(focusSet || searchSet);

    var getScale =
      typeof opts.getScale === "function"
        ? opts.getScale
        : function () {
            return 1;
          };

    var pinnedIds =
      opts.pinnedIds && typeof opts.pinnedIds.has === "function"
        ? opts.pinnedIds
        : null;

    // Let the canvas swallow clicks on empty space → clear focus. Placed
    // as the first child (behind everything) so a click that hits a node
    // still fires the node's handler first.
    var backdrop = svgNs("rect");
    backdrop.setAttribute("x", "0");
    backdrop.setAttribute("y", "0");
    backdrop.setAttribute("width", String(layout.svgW));
    backdrop.setAttribute("height", String(layout.svgH));
    backdrop.setAttribute("fill", "transparent");
    backdrop.setAttribute("data-role", "canvas-backdrop");
    backdrop.addEventListener("click", function () {
      if (typeof opts.onFocusNode === "function") opts.onFocusNode(null);
    });
    svg.appendChild(backdrop);

    // Read theme tokens once per render. Node/edge styles call this
    // closure so a single getComputedStyle() fans out into dozens of
    // var lookups with no additional reflow cost.
    var readVar = _cssVarReader(svg);

    // Arrow markers — one per edge kind so every edge points from its
    // prerequisite to its dependant without us having to compute arrow
    // geometry per segment. `auto-start-reverse` + `refX=9` positions the
    // tip flush with the destination node's perimeter.
    var defs = svgNs("defs");
    ["containment", "dispatch", "spec_dep", "task_dep"].forEach(function (k) {
      var m = svgNs("marker");
      m.setAttribute("id", "dg-arr-" + k.replace("_", "-"));
      m.setAttribute("viewBox", "0 0 10 10");
      m.setAttribute("refX", "9");
      m.setAttribute("refY", "5");
      m.setAttribute("markerWidth", "6");
      m.setAttribute("markerHeight", "6");
      m.setAttribute("orient", "auto-start-reverse");
      m.setAttribute("markerUnits", "userSpaceOnUse");
      var p = svgNs("path");
      p.setAttribute("d", "M0,0 L10,5 L0,10 z");
      p.setAttribute("fill", _edgeColor(readVar, k));
      m.appendChild(p);
      defs.appendChild(m);
    });
    svg.appendChild(defs);

    // Per-node mutable position index — live-updated during drag so
    // incident edges can be re-routed without touching the layout cache.
    // Each value points to the SAME object stored on the DOM-backed
    // render so a live update is a single field write.
    var nodePosRef = new Map();
    layout.positions.forEach(function (pos) {
      if (pos.kind === "dummy" || !pos.node) return;
      nodePosRef.set(pos.node.id, {
        x: pos.x,
        y: pos.y,
        height: pos.height || NODE_H,
      });
    });

    // Index of edges incident on each node. Populated as we create the
    // <path> elements so the drag handler can iterate them cheaply.
    var edgesByNode = new Map();
    function _addIncident(nodeId, record) {
      var arr = edgesByNode.get(nodeId);
      if (!arr) {
        arr = [];
        edgesByNode.set(nodeId, arr);
      }
      arr.push(record);
    }

    // --- Edges first so nodes sit on top ---
    layout.edgePaths.forEach(function (routed) {
      var e = routed.edge;
      var pts = routed.points;
      if (!pts || pts.length < 2) return;

      var base = EDGE_STYLES[e.kind] || EDGE_STYLES.task_dep;
      var style = {
        color: _edgeColor(readVar, e.kind),
        width: base.width,
        dash: base.dash,
      };
      if (e.kind === "task_dep") {
        var src = nodeById.get(e.from);
        if (src && src.kind === "task" && src.status === "done") {
          style.dash = null;
        }
      }

      var path = svgNs("path");
      path.setAttribute("d", _smoothPath(pts));
      path.setAttribute("stroke", style.color);
      path.setAttribute("stroke-width", String(style.width));
      path.setAttribute("fill", "none");
      path.setAttribute("stroke-linecap", "round");
      path.setAttribute(
        "marker-end",
        "url(#dg-arr-" + e.kind.replace("_", "-") + ")",
      );
      if (style.dash) path.setAttribute("stroke-dasharray", style.dash);
      path.setAttribute("data-kind", e.kind);
      path.setAttribute("data-from", e.from);
      path.setAttribute("data-to", e.to);
      if (anyDim) {
        var edgeInFocus = inFocus(e.from) && inFocus(e.to);
        if (!edgeInFocus) path.setAttribute("opacity", "0.18");
      }
      svg.appendChild(path);

      // Store incident records for both endpoints. Only direct (2-point)
      // routed edges get live-updates; dummy-chained edges stay static
      // since recomputing their waypoints requires the layout engine.
      if (pts.length === 2) {
        _addIncident(e.from, { pathEl: path, otherId: e.to });
        _addIncident(e.to, { pathEl: path, otherId: e.from });
      }
    });

    // livePositions: a positions-like Map that _edgeDetourWaypoint can
    // scan while the drag is in progress. Stored separately from
    // nodePosRef because the detour function reads `pos.kind` and
    // `pos.node` and returns positions keyed by id.
    var livePositions = new Map();
    nodePosRef.forEach(function (p, id) {
      livePositions.set(id, {
        x: p.x,
        y: p.y,
        height: p.height,
        kind: "real",
        node: nodeById.get(id),
      });
    });

    // liveUpdateNode re-routes every edge incident on `nodeId` using the
    // node's current nodePosRef entry (updated by the drag handler) and
    // the other endpoint's stored position. Detours around other nodes
    // are re-evaluated each call so the curve dodges obstacles as the
    // dragged node passes over them.
    function liveUpdateNode(nodeId) {
      var incident = edgesByNode.get(nodeId);
      if (!incident) return;
      var self = nodePosRef.get(nodeId);
      if (!self) return;
      // Mirror the drag into livePositions so detour detection uses the
      // current visual position, not the stale render-time one.
      var live = livePositions.get(nodeId);
      if (live) {
        live.x = self.x;
        live.y = self.y;
      }
      for (var i = 0; i < incident.length; i++) {
        var rec = incident[i];
        var other = nodePosRef.get(rec.otherId);
        if (!other) continue;
        var selfCx = self.x + NODE_W / 2;
        var selfCy = self.y + self.height / 2;
        var otherCx = other.x + NODE_W / 2;
        var otherCy = other.y + other.height / 2;
        // Determine source vs dest from the path's data-from.
        var fromId = rec.pathEl.getAttribute("data-from");
        var srcPos = fromId === nodeId ? self : other;
        var dstPos = fromId === nodeId ? other : self;
        var srcCx = srcPos === self ? selfCx : otherCx;
        var srcCy = srcPos === self ? selfCy : otherCy;
        var dstCx = dstPos === self ? selfCx : otherCx;
        var dstCy = dstPos === self ? selfCy : otherCy;
        var startPt = _rectPerimeterPoint(srcPos, dstCx, dstCy);
        var endPt = _rectPerimeterPoint(dstPos, srcCx, srcCy);
        rec.pathEl.setAttribute("d", _smoothPath([startPt, endPt]));
      }
    }

    // --- Nodes ---
    layout.positions.forEach(function (pos) {
      if (pos.kind === "dummy" || !pos.node) return;
      var n = pos.node;
      var x = pos.x;
      var y = pos.y;
      var h = pos.height || NODE_H;

      var g = svgNs("g");
      g.setAttribute("data-kind", n.kind);
      g.setAttribute("data-id", n.id);
      if (anyDim && !inFocus(n.id)) {
        g.setAttribute("opacity", "0.28");
      }

      var body = svgNs("g");
      body.style.cursor = "grab";

      var ns = _nodeStyle(readVar, n.kind, n.status);
      var inkColor = readVar("--text") || "#1b1916";
      var mutedColor = readVar("--text-muted") || "#7a766e";

      var rect = svgNs("rect");
      rect.setAttribute("x", String(x));
      rect.setAttribute("y", String(y));
      rect.setAttribute("width", String(NODE_W));
      rect.setAttribute("height", String(h));
      rect.setAttribute("rx", n.kind === "spec" ? "10" : "6");
      rect.setAttribute("fill", ns.fill);
      rect.setAttribute("stroke", ns.stroke);
      rect.setAttribute("stroke-width", "1.25");
      if (n.kind === "task" && n.extra && n.extra.dispatched) {
        rect.setAttribute("stroke-width", "1.75");
      }
      if (focusedId === n.id) {
        rect.setAttribute("stroke", readVar("--accent") || "#c45a33");
        rect.setAttribute("stroke-width", "2");
      }
      body.appendChild(rect);

      var chip = svgNs("text");
      chip.setAttribute("x", String(x + 10));
      chip.setAttribute("y", String(y + 15));
      chip.setAttribute("font-size", "9");
      chip.setAttribute("font-family", "system-ui, sans-serif");
      chip.setAttribute("font-weight", "600");
      chip.setAttribute("letter-spacing", "0.08em");
      chip.setAttribute("fill", ns.stroke);
      chip.textContent = n.kind === "spec" ? "SPEC" : "TASK";
      body.appendChild(chip);

      // Label wraps to multiple lines — no truncation. Each line is a
      // <tspan> with dy stepping by LABEL_LINE_H. The label block is
      // vertically centred within the node's content area (below the
      // type chip).
      var hasToggle = n.kind === "spec" && n.extra && n.extra.hasChildren;
      var lines = _nodeLabelLines(n);
      var labelCenterX = hasToggle ? x + (NODE_W - 28) / 2 : x + NODE_W / 2;
      var totalLabelH = lines.length * LABEL_LINE_H;
      var labelStartY =
        y +
        LABEL_TOP_PAD +
        Math.max(0, (h - LABEL_TOP_PAD - LABEL_BOTTOM_PAD - totalLabelH) / 2);
      var label = svgNs("text");
      label.setAttribute("x", String(labelCenterX));
      label.setAttribute("y", String(labelStartY));
      label.setAttribute("text-anchor", "middle");
      label.setAttribute("font-size", "12");
      label.setAttribute("font-family", "system-ui, sans-serif");
      label.setAttribute("fill", inkColor);
      for (var li = 0; li < lines.length; li++) {
        var tspan = svgNs("tspan");
        tspan.setAttribute("x", String(labelCenterX));
        tspan.setAttribute("dy", li === 0 ? "0" : String(LABEL_LINE_H));
        tspan.textContent = lines[li];
        label.appendChild(tspan);
      }
      body.appendChild(label);

      // Pin marker on the top-left corner for user-pinned nodes.
      if (pinnedIds && pinnedIds.has(n.id)) {
        var pin = svgNs("circle");
        pin.setAttribute("cx", String(x + 6));
        pin.setAttribute("cy", String(y + 6));
        pin.setAttribute("r", "3");
        pin.setAttribute("fill", readVar("--accent") || "#c45a33");
        body.appendChild(pin);
      }

      _wireNodeInteractions(g, body, n, x, y, opts, getScale, {
        nodePosRef: nodePosRef,
        liveUpdateNode: liveUpdateNode,
      });
      g.appendChild(body);

      if (hasToggle) {
        var handle = _makeToggleHandle(n, x, y, h, opts, {
          stroke: ns.stroke,
          ink: inkColor,
          bg: readVar("--bg-raised") || readVar("--bg") || "#ffffff",
        });
        if (handle) g.appendChild(handle);
      }

      svg.appendChild(g);
    });

    return true;
  }

  // _shiftGroupCoords walks every descendant of `g` and adds (dx, dy) to
  // their positional attributes (x/y for rect/text/tspan, cx/cy for
  // circle). Used on drag-end to commit the transform delta to the
  // nodes' coordinate space so the next drag starts from the new
  // baseline instead of snapping back.
  function _shiftGroupCoords(g, dx, dy) {
    if (!g || (!dx && !dy)) return;
    var queue = [];
    // Seed with direct children — `g.children` may be a live NodeList or
    // a plain array depending on the DOM stub. Support both.
    var kids = g.children || [];
    for (var i = 0; i < kids.length; i++) queue.push(kids[i]);
    while (queue.length > 0) {
      var el = queue.shift();
      var tag = el.tagName;
      if (tag === "circle") {
        var cx = el.getAttribute("cx");
        var cy = el.getAttribute("cy");
        if (cx !== null) el.setAttribute("cx", String(parseFloat(cx) + dx));
        if (cy !== null) el.setAttribute("cy", String(parseFloat(cy) + dy));
      } else {
        var ax = el.getAttribute("x");
        var ay = el.getAttribute("y");
        if (ax !== null) el.setAttribute("x", String(parseFloat(ax) + dx));
        if (ay !== null) el.setAttribute("y", String(parseFloat(ay) + dy));
      }
      var sub = el.children || [];
      for (var j = 0; j < sub.length; j++) queue.push(sub[j]);
    }
  }

  // _wireNodeInteractions binds drag / click / dblclick / shift+click on
  // a single node group. The drag state machine lives here so it can
  // distinguish drags from clicks via a movement threshold, preventing
  // a normal click from triggering both focus and pin.
  var DRAG_THRESHOLD = 4;

  function _wireNodeInteractions(g, body, n, x, y, opts, getScale, drag) {
    var dragState = null;
    drag = drag || {};
    var nodePosRef = drag.nodePosRef || null;
    var liveUpdateNode =
      typeof drag.liveUpdateNode === "function" ? drag.liveUpdateNode : null;
    // Scale divider: when the mount is zoomed, a screen-pixel drag
    // corresponds to a smaller graph-coord move. Without this division,
    // pinning at 2× zoom would land at double the intended position.
    function currentScale() {
      if (typeof getScale !== "function") return 1;
      var s = getScale();
      return typeof s === "number" && s > 0 ? s : 1;
    }

    body.addEventListener("mousedown", function (e) {
      // Let space+drag (canvas pan) win over node drag so the user can
      // pan over a node. Also ignore right-click / middle-click.
      if (e.button !== undefined && e.button !== 0) return;
      // Baseline x/y come from the nodePosRef so a second consecutive
      // drag (without any intervening render) starts from the latest
      // committed position, not the node's original render coords.
      var baselineX = x;
      var baselineY = y;
      if (nodePosRef) {
        var ref = nodePosRef.get(n.id);
        if (ref) {
          baselineX = ref.x;
          baselineY = ref.y;
        }
      }
      dragState = {
        startX: e.clientX,
        startY: e.clientY,
        baselineX: baselineX,
        baselineY: baselineY,
        moved: false,
      };
      body.style.cursor = "grabbing";
      // Stop propagation so the SVG backdrop's click handler doesn't
      // fire on mouseup-after-drag.
      if (typeof e.stopPropagation === "function") e.stopPropagation();
    });

    body.addEventListener("mousemove", function (e) {
      if (!dragState) return;
      var s = currentScale();
      var dx = (e.clientX - dragState.startX) / s;
      var dy = (e.clientY - dragState.startY) / s;
      if (!dragState.moved && Math.hypot(dx, dy) < DRAG_THRESHOLD / s) {
        return;
      }
      dragState.moved = true;
      // Visual: move the node's group via transform relative to the
      // node's render origin (x, y). We use (targetX - x) as the
      // translate so second-drag cases work even when the DOM children
      // have already been shifted from their initial render positions.
      var targetX = dragState.baselineX + dx;
      var targetY = dragState.baselineY + dy;
      g.setAttribute(
        "transform",
        "translate(" + (targetX - x) + "," + (targetY - y) + ")",
      );
      // Live edges: mutate the nodePosRef so _smoothPath traces the
      // current visual position, then redraw incident edges. This is
      // what stops the "edges lag behind the node while dragging" feel.
      if (nodePosRef && liveUpdateNode) {
        var ref = nodePosRef.get(n.id);
        if (ref) {
          ref.x = targetX;
          ref.y = targetY;
          liveUpdateNode(n.id);
        }
      }
    });

    function finishDrag(e) {
      if (!dragState) return;
      var wasMoved = dragState.moved;
      var s = currentScale();
      var dx = (e.clientX - dragState.startX) / s;
      var dy = (e.clientY - dragState.startY) / s;
      var targetX = dragState.baselineX + dx;
      var targetY = dragState.baselineY + dy;
      dragState = null;
      body.style.cursor = "grab";
      if (!wasMoved) return;
      // Commit the translation into each child's own coordinate attrs
      // and clear the transform. Without this, a second drag would
      // overwrite the transform (snapping the node back to its
      // rendered-baseline-plus-new-delta instead of stacking).
      var totalDx = targetX - x;
      var totalDy = targetY - y;
      _shiftGroupCoords(g, totalDx, totalDy);
      g.removeAttribute("transform");
      if (nodePosRef) {
        var ref2 = nodePosRef.get(n.id);
        if (ref2) {
          ref2.x = targetX;
          ref2.y = targetY;
        }
      }
      if (typeof opts.onPinNode === "function") {
        opts.onPinNode(n.id, targetX, targetY);
      }
    }

    // Hover dispatch — transient, no state change. The parent module can
    // translate these into in-place highlight without re-rendering.
    if (typeof opts.onHoverNode === "function") {
      body.addEventListener("mouseenter", function () {
        if (dragState) return;
        opts.onHoverNode(n.id);
      });
      body.addEventListener("mouseleave", function () {
        opts.onHoverNode(null);
      });
    }

    body.addEventListener("mouseup", finishDrag);
    // If the mouse leaves the body mid-drag, fall back to a document
    // mouseup so we don't lose the drag release.
    body.addEventListener("mouseleave", function () {
      if (!dragState) return;
      var releasedOnce = false;
      var onDocUp = function (e2) {
        if (releasedOnce) return;
        releasedOnce = true;
        document.removeEventListener("mouseup", onDocUp);
        finishDrag(e2);
      };
      document.addEventListener("mouseup", onDocUp);
    });

    body.addEventListener("click", function (e) {
      // A click fires after mouseup; suppress it if the mouseup was the
      // tail end of a drag.
      if (g.getAttribute("transform")) return;
      if (e.shiftKey) {
        if (typeof opts.onNavigateNode === "function") {
          opts.onNavigateNode(n.id, n);
          return;
        }
      }
      if (typeof opts.onFocusNode === "function") opts.onFocusNode(n.id);
    });

    body.addEventListener("dblclick", function (e) {
      if (typeof e.stopPropagation === "function") e.stopPropagation();
      if (typeof opts.onUnpinNode === "function") opts.onUnpinNode(n.id);
    });
  }

  // _makeToggleHandle returns an <g> containing the +/- chip that
  // expands/collapses a spec's children. Click handler stops propagation
  // so the main body's spec-focus click doesn't also fire.
  function _makeToggleHandle(n, x, y, h, opts, colors) {
    // Keep the toggle near the top of tall wrapped nodes so it stays
    // within a comfortable click reach regardless of how many label
    // lines the spec's title occupies.
    if (typeof opts === "undefined" && typeof h === "object") {
      opts = h;
      h = NODE_H;
    }
    var nodeH = typeof h === "number" ? h : NODE_H;
    var collapsed = !!(n.extra && n.extra.collapsed);
    var cx = x + NODE_W - 16;
    var cy = y + Math.min(26, nodeH / 2);
    var c = colors || {};
    var stroke = c.stroke || "#7a766e";
    var ink = c.ink || "#1b1916";
    var bg = c.bg || "#ffffff";
    var handle = svgNs("g");
    handle.style.cursor = "pointer";
    handle.setAttribute("data-role", "toggle");
    handle.setAttribute("data-collapsed", collapsed ? "1" : "0");

    var circle = svgNs("circle");
    circle.setAttribute("cx", String(cx));
    circle.setAttribute("cy", String(cy));
    circle.setAttribute("r", "9");
    circle.setAttribute("fill", bg);
    circle.setAttribute("stroke", stroke);
    circle.setAttribute("stroke-width", "1");
    handle.appendChild(circle);

    var glyph = svgNs("text");
    glyph.setAttribute("x", String(cx));
    glyph.setAttribute("y", String(cy + 4));
    glyph.setAttribute("text-anchor", "middle");
    glyph.setAttribute("font-size", "13");
    glyph.setAttribute("font-family", "system-ui, sans-serif");
    glyph.setAttribute("fill", ink);
    glyph.textContent = collapsed ? "+" : "\u2212"; // − (minus)
    handle.appendChild(glyph);

    handle.addEventListener("click", function (e) {
      if (e && typeof e.stopPropagation === "function") e.stopPropagation();
      if (typeof opts.onToggleSpec === "function") {
        opts.onToggleSpec(n.extra && n.extra.path);
      }
    });
    return handle;
  }

  // Export for browser (window) and CommonJS-style test harness.
  if (typeof window !== "undefined") {
    window.buildUnifiedGraph = buildUnifiedGraph;
    window.renderUnifiedGraph = renderUnifiedGraph;
  }
  if (typeof module !== "undefined" && module.exports) {
    module.exports = {
      buildUnifiedGraph: buildUnifiedGraph,
      renderUnifiedGraph: renderUnifiedGraph,
    };
  }
})();
