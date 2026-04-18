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

  // Task status → background colour (matches depgraph.js).
  var TASK_STATUS_COLORS = {
    backlog: "#6b6560",
    in_progress: "#2c5f98",
    waiting: "#a07020",
    committing: "#5a3d8a",
    done: "#1a6030",
    failed: "#8c2020",
    cancelled: "#5a3d8a",
  };

  // Spec status → background colour. Specs use a separate palette so they
  // read as a different node kind at a glance.
  var SPEC_STATUS_COLORS = {
    vague: "#5d4f42",
    drafted: "#6f5632",
    validated: "#2f5a4c",
    complete: "#2a4a2a",
    stale: "#5a3030",
    archived: "#3d3a36",
  };

  // Edge styling per kind. containment and dispatch are structural so they
  // read as thin solid lines; spec_dep and task_dep are design/runtime
  // dependencies so they use dashed variants with distinct hues.
  //
  // task_dep has a secondary state — when the prerequisite task is already
  // done, the edge goes solid to signal "this dependency is satisfied."
  // See _edgeStyle() which consults the source node's status.
  //
  // NOTE: these colours are duplicated in ui/css/docs.css for the header
  // legend (`.depgraph-mode__legend-edge--*`). Keep the two in sync when
  // tweaking the palette; a future theme refactor should consolidate them.
  var EDGE_STYLES = {
    containment: { color: "#7a7570", width: 1.2, dash: null },
    dispatch: { color: "#3b82c4", width: 1.5, dash: null },
    spec_dep: { color: "#b07045", width: 1.5, dash: "6 3" },
    task_dep: { color: "#5a9058", width: 1.5, dash: "4 2" },
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
  function layoutForce(graph, opts) {
    opts = opts || {};
    var pinnedPositions =
      opts.pinnedPositions && typeof opts.pinnedPositions.get === "function"
        ? opts.pinnedPositions
        : null;

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
      var totalDisp = _forceStep(
        positions,
        adj,
        temperature,
        isPinned,
      );
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
          dx = (i - j) || 1;
          dy = (j - i) || 1;
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

  // Backwards-compat alias: any caller/test that still reaches for
  // layoutSugiyama gets the force-directed layout now. The returned
  // shape is identical (positions + edgePaths + svgW + svgH + hasCycles).
  var layoutSugiyama = layoutForce;

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
        var h = currentPos ? currentPos.height || _itemHeight(item) : _itemHeight(item);
        var centre =
          count > 0
            ? sum / count
            : (currentPos ? currentPos.y : 0) + h / 2;
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

  function _routeEdges(graph, edgeChains, positions) {
    return edgeChains
      .map(function (entry) {
        var points = [];
        for (var i = 0; i < entry.chain.length; i++) {
          var id = entry.chain[i];
          var p = positions.get(id);
          if (!p) return null;
          var h = p.height || NODE_H;
          if (i === 0) {
            points.push({ x: p.x + NODE_W, y: p.y + h / 2 });
          } else if (i === entry.chain.length - 1) {
            points.push({ x: p.x, y: p.y + h / 2 });
          } else {
            points.push({ x: p.x, y: p.y });
          }
        }
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
    if (!points || points.length === 0) return "";
    var d = "M" + points[0].x + "," + points[0].y;
    for (var i = 1; i < points.length; i++) {
      var a = points[i - 1];
      var b = points[i];
      var dx = b.x - a.x;
      // Half of the horizontal distance → smooth S-curve through waypoints.
      var cp = Math.max(20, dx / 2);
      d +=
        " C" +
        (a.x + cp) +
        "," +
        a.y +
        " " +
        (b.x - cp) +
        "," +
        b.y +
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
  //   pinnedIds (Set<string>)         — nodes currently pinned; used to
  //                                     draw the pin corner marker.
  //   focusedNodeId (string | null)   — when set, non-neighbourhood nodes
  //                                     and edges are dimmed so the user
  //                                     can zero in on one topic.
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

    // --- Edges first so nodes sit on top ---
    layout.edgePaths.forEach(function (routed) {
      var e = routed.edge;
      var pts = routed.points;
      if (!pts || pts.length < 2) return;

      var base = EDGE_STYLES[e.kind] || EDGE_STYLES.task_dep;
      var style = { color: base.color, width: base.width, dash: base.dash };
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
      if (style.dash) path.setAttribute("stroke-dasharray", style.dash);
      path.setAttribute("data-kind", e.kind);
      if (focusSet) {
        var edgeInFocus = focusSet.has(e.from) && focusSet.has(e.to);
        if (!edgeInFocus) path.setAttribute("opacity", "0.18");
      }
      svg.appendChild(path);
    });

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
      if (focusSet && !focusSet.has(n.id)) {
        g.setAttribute("opacity", "0.28");
      }

      var body = svgNs("g");
      body.style.cursor = "grab";

      var rect = svgNs("rect");
      rect.setAttribute("x", String(x));
      rect.setAttribute("y", String(y));
      rect.setAttribute("width", String(NODE_W));
      rect.setAttribute("height", String(h));
      rect.setAttribute("rx", n.kind === "spec" ? "12" : "6");
      rect.setAttribute(
        "fill",
        n.kind === "spec"
          ? SPEC_STATUS_COLORS[n.status] || "#4a4540"
          : TASK_STATUS_COLORS[n.status] || "#4B5563",
      );
      if (n.kind === "task" && n.extra && n.extra.dispatched) {
        rect.setAttribute("stroke", "#3b82c4");
        rect.setAttribute("stroke-width", "1.5");
      }
      if (focusedId === n.id) {
        rect.setAttribute("stroke", "#f7c466");
        rect.setAttribute("stroke-width", "2");
      }
      body.appendChild(rect);

      var chip = svgNs("text");
      chip.setAttribute("x", String(x + 8));
      chip.setAttribute("y", String(y + 14));
      chip.setAttribute("font-size", "10");
      chip.setAttribute("font-family", "system-ui, sans-serif");
      chip.setAttribute("fill", "#ffffffb3");
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
        y + LABEL_TOP_PAD + Math.max(0, (h - LABEL_TOP_PAD - LABEL_BOTTOM_PAD - totalLabelH) / 2);
      var label = svgNs("text");
      label.setAttribute("x", String(labelCenterX));
      label.setAttribute("y", String(labelStartY));
      label.setAttribute("text-anchor", "middle");
      label.setAttribute("font-size", "12");
      label.setAttribute("font-family", "system-ui, sans-serif");
      label.setAttribute("fill", "#ffffff");
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
        pin.setAttribute("cx", String(x + 4));
        pin.setAttribute("cy", String(y + 4));
        pin.setAttribute("r", "3");
        pin.setAttribute("fill", "#f7c466");
        body.appendChild(pin);
      }

      _wireNodeInteractions(g, body, n, x, y, opts);
      g.appendChild(body);

      if (hasToggle) {
        var handle = _makeToggleHandle(n, x, y, h, opts);
        if (handle) g.appendChild(handle);
      }

      svg.appendChild(g);
    });

    return true;
  }

  // _wireNodeInteractions binds drag / click / dblclick / shift+click on
  // a single node group. The drag state machine lives here so it can
  // distinguish drags from clicks via a movement threshold, preventing
  // a normal click from triggering both focus and pin.
  var DRAG_THRESHOLD = 4;

  function _wireNodeInteractions(g, body, n, x, y, opts) {
    var dragState = null;

    body.addEventListener("mousedown", function (e) {
      // Let space+drag (canvas pan) win over node drag so the user can
      // pan over a node. Also ignore right-click / middle-click.
      if (e.button !== undefined && e.button !== 0) return;
      dragState = {
        startX: e.clientX,
        startY: e.clientY,
        originX: x,
        originY: y,
        moved: false,
      };
      body.style.cursor = "grabbing";
      // Stop propagation so the SVG backdrop's click handler doesn't
      // fire on mouseup-after-drag.
      if (typeof e.stopPropagation === "function") e.stopPropagation();
    });

    body.addEventListener("mousemove", function (e) {
      if (!dragState) return;
      var dx = e.clientX - dragState.startX;
      var dy = e.clientY - dragState.startY;
      if (!dragState.moved && Math.hypot(dx, dy) < DRAG_THRESHOLD) {
        return;
      }
      dragState.moved = true;
      g.setAttribute("transform", "translate(" + dx + "," + dy + ")");
    });

    function finishDrag(e) {
      if (!dragState) return;
      var wasMoved = dragState.moved;
      var dx = e.clientX - dragState.startX;
      var dy = e.clientY - dragState.startY;
      dragState = null;
      body.style.cursor = "grab";
      if (!wasMoved) return;
      g.removeAttribute("transform");
      if (typeof opts.onPinNode === "function") {
        opts.onPinNode(n.id, x + dx, y + dy);
      }
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
  function _makeToggleHandle(n, x, y, h, opts) {
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
    var handle = svgNs("g");
    handle.style.cursor = "pointer";
    handle.setAttribute("data-role", "toggle");
    handle.setAttribute("data-collapsed", collapsed ? "1" : "0");

    var circle = svgNs("circle");
    circle.setAttribute("cx", String(cx));
    circle.setAttribute("cy", String(cy));
    circle.setAttribute("r", "9");
    circle.setAttribute("fill", "#ffffff1a");
    circle.setAttribute("stroke", "#ffffff66");
    circle.setAttribute("stroke-width", "1");
    handle.appendChild(circle);

    var glyph = svgNs("text");
    glyph.setAttribute("x", String(cx));
    glyph.setAttribute("y", String(cy + 4));
    glyph.setAttribute("text-anchor", "middle");
    glyph.setAttribute("font-size", "13");
    glyph.setAttribute("font-family", "system-ui, sans-serif");
    glyph.setAttribute("fill", "#ffffff");
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
