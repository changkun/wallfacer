/**
 * Unit tests for unified-graph.js — the spec+task graph data merge.
 *
 * The module is loaded into an isolated vm context with a minimal `window`
 * stub so its IIFE can attach the public function.
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "unified-graph.js"), "utf8");

function loadModule() {
  const windowObj = {};
  const ctx = { window: windowObj, module: { exports: {} } };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return windowObj.buildUnifiedGraph;
}

// loadRenderer loads the IIFE with a real-enough DOM stub so
// renderUnifiedGraph can be exercised. Returns the renderer plus the
// registry used for assertions.
function loadRenderer() {
  const windowObj = {};

  function makeEl(tag) {
    const _children = [];
    const _attrs = {};
    const _style = {};
    const _listeners = {};
    let _text = "";
    const el = {
      tagName: tag,
      get children() {
        return _children;
      },
      style: _style,
      get firstChild() {
        return _children[0] || null;
      },
      setAttribute(k, v) {
        _attrs[k] = String(v);
      },
      getAttribute(k) {
        return Object.hasOwn(_attrs, k) ? _attrs[k] : null;
      },
      removeAttribute(k) {
        delete _attrs[k];
      },
      appendChild(child) {
        _children.push(child);
        return child;
      },
      removeChild(child) {
        const i = _children.indexOf(child);
        if (i !== -1) _children.splice(i, 1);
      },
      addEventListener(ev, fn) {
        if (!_listeners[ev]) _listeners[ev] = [];
        _listeners[ev].push(fn);
      },
      get textContent() {
        return _text;
      },
      set textContent(v) {
        _text = String(v);
      },
      _attrs,
      _listeners,
    };
    return el;
  }

  const document = {
    createElementNS(_ns, tag) {
      return makeEl(tag);
    },
    createElement(tag) {
      return makeEl(tag);
    },
  };

  const ctx = {
    window: windowObj,
    module: { exports: {} },
    document,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return {
    buildUnifiedGraph: windowObj.buildUnifiedGraph,
    renderUnifiedGraph: windowObj.renderUnifiedGraph,
    makeEl,
  };
}

function specNode(path, spec, children, depth) {
  return {
    path,
    spec,
    children: children || [],
    is_leaf: !children || children.length === 0,
    depth: depth || 0,
  };
}

function spec(opts) {
  return Object.assign(
    {
      title: "",
      status: "drafted",
      depends_on: [],
      affects: [],
      effort: "medium",
      created: "",
      updated: "",
      author: "",
      dispatched_task_id: null,
    },
    opts || {},
  );
}

describe("buildUnifiedGraph", () => {
  it("returns empty graph when no tasks and no specs", () => {
    const buildUnifiedGraph = loadModule();
    const { nodes, edges } = buildUnifiedGraph([], []);
    expect(nodes).toEqual([]);
    expect(edges).toEqual([]);
  });

  it("handles undefined/null inputs gracefully", () => {
    const buildUnifiedGraph = loadModule();
    const result = buildUnifiedGraph(undefined, null);
    expect(result.nodes).toEqual([]);
    expect(result.edges).toEqual([]);
  });

  it("emits task nodes and task_dep edges for a task-only DAG", () => {
    const buildUnifiedGraph = loadModule();
    const tasks = [
      { id: "a", title: "Task A", status: "done", depends_on: [] },
      { id: "b", title: "Task B", status: "backlog", depends_on: ["a"] },
    ];
    const { nodes, edges } = buildUnifiedGraph(tasks, []);
    expect(nodes).toHaveLength(2);
    expect(nodes.every((n) => n.kind === "task")).toBe(true);
    // b.depends_on = [a] → a must come first, edge is a → b.
    expect(edges).toEqual([{ from: "task:a", to: "task:b", kind: "task_dep" }]);
  });

  it("skips archived tasks by default", () => {
    const buildUnifiedGraph = loadModule();
    const tasks = [
      { id: "a", title: "A", archived: true, depends_on: [] },
      { id: "b", title: "B", archived: false, depends_on: [] },
    ];
    const { nodes } = buildUnifiedGraph(tasks, []);
    expect(nodes.map((n) => n.id)).toEqual(["task:b"]);
  });

  it("includes archived tasks when opts.includeArchivedTasks is true", () => {
    const buildUnifiedGraph = loadModule();
    const tasks = [{ id: "a", title: "A", archived: true, depends_on: [] }];
    const { nodes } = buildUnifiedGraph(tasks, [], {
      includeArchivedTasks: true,
    });
    expect(nodes).toHaveLength(1);
    expect(nodes[0].extra.archived).toBe(true);
  });

  it("emits spec nodes and containment edges from the spec tree", () => {
    const buildUnifiedGraph = loadModule();
    const nodes = [
      specNode("specs/foo.md", spec({ title: "Foo" }), ["specs/foo/bar.md"], 0),
      specNode("specs/foo/bar.md", spec({ title: "Bar" }), [], 1),
    ];
    const result = buildUnifiedGraph([], nodes);
    expect(result.nodes).toHaveLength(2);
    expect(result.edges).toEqual([
      {
        from: "spec:specs/foo.md",
        to: "spec:specs/foo/bar.md",
        kind: "containment",
      },
    ]);
    const fooNode = result.nodes.find((n) => n.id === "spec:specs/foo.md");
    expect(fooNode.kind).toBe("spec");
    expect(fooNode.extra.isLeaf).toBe(false);
    expect(fooNode.extra.depth).toBe(0);
  });

  it("filters archived specs by default", () => {
    const buildUnifiedGraph = loadModule();
    const nodes = [
      specNode("specs/a.md", spec({ title: "A", status: "archived" }), [], 0),
      specNode("specs/b.md", spec({ title: "B", status: "drafted" }), [], 0),
    ];
    const result = buildUnifiedGraph([], nodes);
    expect(result.nodes.map((n) => n.id)).toEqual(["spec:specs/b.md"]);
  });

  it("includes archived specs when opts.includeArchivedSpecs is true", () => {
    const buildUnifiedGraph = loadModule();
    const nodes = [
      specNode("specs/a.md", spec({ title: "A", status: "archived" }), [], 0),
    ];
    const result = buildUnifiedGraph([], nodes, {
      includeArchivedSpecs: true,
    });
    expect(result.nodes).toHaveLength(1);
  });

  it("drops containment edges to filtered (archived) children", () => {
    const buildUnifiedGraph = loadModule();
    const nodes = [
      specNode("specs/p.md", spec({ title: "Parent" }), ["specs/c.md"], 0),
      specNode(
        "specs/c.md",
        spec({ title: "Child", status: "archived" }),
        [],
        1,
      ),
    ];
    const result = buildUnifiedGraph([], nodes);
    expect(result.nodes.map((n) => n.id)).toEqual(["spec:specs/p.md"]);
    expect(result.edges).toEqual([]);
  });

  it("emits spec_dep edges when a spec depends on another spec", () => {
    const buildUnifiedGraph = loadModule();
    const nodes = [
      specNode(
        "specs/a.md",
        spec({ title: "A", depends_on: ["specs/b.md"] }),
        [],
        0,
      ),
      specNode("specs/b.md", spec({ title: "B" }), [], 0),
    ];
    const result = buildUnifiedGraph([], nodes);
    const depEdge = result.edges.find((e) => e.kind === "spec_dep");
    // A.depends_on=[B] → B must come before A, edge is B → A.
    expect(depEdge).toEqual({
      from: "spec:specs/b.md",
      to: "spec:specs/a.md",
      kind: "spec_dep",
    });
  });

  it("drops spec_dep edges to missing/filtered targets", () => {
    const buildUnifiedGraph = loadModule();
    const nodes = [
      specNode(
        "specs/a.md",
        spec({ title: "A", depends_on: ["specs/missing.md"] }),
        [],
        0,
      ),
    ];
    const result = buildUnifiedGraph([], nodes);
    expect(result.edges).toEqual([]);
  });

  it("emits dispatch edge and flags task as dispatched when a leaf spec points to a task", () => {
    const buildUnifiedGraph = loadModule();
    const specNodes = [
      specNode(
        "specs/leaf.md",
        spec({ title: "Leaf", dispatched_task_id: "task-123" }),
        [],
        1,
      ),
    ];
    const tasks = [
      { id: "task-123", title: "Dispatched task", depends_on: [] },
    ];
    const result = buildUnifiedGraph(tasks, specNodes);

    const dispatchEdge = result.edges.find((e) => e.kind === "dispatch");
    expect(dispatchEdge).toEqual({
      from: "spec:specs/leaf.md",
      to: "task:task-123",
      kind: "dispatch",
    });
    const taskNode = result.nodes.find((n) => n.id === "task:task-123");
    expect(taskNode.extra.dispatched).toBe(true);
  });

  it('treats dispatched_task_id === "null" (literal string) as unset', () => {
    const buildUnifiedGraph = loadModule();
    const specNodes = [
      specNode(
        "specs/leaf.md",
        spec({ title: "Leaf", dispatched_task_id: "null" }),
        [],
        1,
      ),
    ];
    const result = buildUnifiedGraph([], specNodes);
    expect(result.edges).toEqual([]);
  });

  it("drops dispatch edge when the target task is missing (e.g. archived task)", () => {
    const buildUnifiedGraph = loadModule();
    const specNodes = [
      specNode(
        "specs/leaf.md",
        spec({ title: "Leaf", dispatched_task_id: "ghost" }),
        [],
        1,
      ),
    ];
    const result = buildUnifiedGraph([], specNodes);
    const dispatchEdges = result.edges.filter((e) => e.kind === "dispatch");
    expect(dispatchEdges).toEqual([]);
  });

  it("keeps standalone tasks (no spec dispatches to them) in the graph", () => {
    const buildUnifiedGraph = loadModule();
    const tasks = [{ id: "orphan", title: "Orphan task", depends_on: [] }];
    const result = buildUnifiedGraph(tasks, []);
    const taskNode = result.nodes.find((n) => n.id === "task:orphan");
    expect(taskNode).toBeDefined();
    expect(taskNode.extra.dispatched).toBe(false);
  });

  it("builds a mixed graph with containment, spec_dep, dispatch, and task_dep edges", () => {
    const buildUnifiedGraph = loadModule();
    const specNodes = [
      specNode(
        "specs/parent.md",
        spec({ title: "Parent" }),
        ["specs/parent/child.md"],
        0,
      ),
      specNode(
        "specs/parent/child.md",
        spec({
          title: "Child",
          dispatched_task_id: "task-a",
          depends_on: ["specs/other.md"],
        }),
        [],
        1,
      ),
      specNode("specs/other.md", spec({ title: "Other" }), [], 0),
    ];
    const tasks = [
      { id: "task-a", title: "A", depends_on: [] },
      { id: "task-b", title: "B", depends_on: ["task-a"] },
    ];
    const result = buildUnifiedGraph(tasks, specNodes);

    const kinds = result.edges.map((e) => e.kind).sort();
    expect(kinds).toEqual(
      ["containment", "dispatch", "spec_dep", "task_dep"].sort(),
    );
    // task-a is dispatched from the leaf spec → its node is flagged.
    const taskA = result.nodes.find((n) => n.id === "task:task-a");
    expect(taskA.extra.dispatched).toBe(true);
    // task-b is standalone (no spec dispatched to it) — not flagged.
    const taskB = result.nodes.find((n) => n.id === "task:task-b");
    expect(taskB.extra.dispatched).toBe(false);
  });

  it("uses the basename as a fallback label when spec.title is empty", () => {
    const buildUnifiedGraph = loadModule();
    const nodes = [specNode("specs/foo/bar.md", spec({ title: "" }), [], 1)];
    const result = buildUnifiedGraph([], nodes);
    expect(result.nodes[0].label).toBe("bar.md");
  });

  it("uses a short id as a fallback label when task.title is empty", () => {
    const buildUnifiedGraph = loadModule();
    const tasks = [{ id: "abcdef1234", title: "", depends_on: [] }];
    const { nodes } = buildUnifiedGraph(tasks, []);
    expect(nodes[0].label).toBe("abcdef12");
  });

  describe("collapse", () => {
    it("hides descendants of collapsed specs but keeps the collapsed spec visible", () => {
      const buildUnifiedGraph = loadModule();
      const nodes = [
        specNode("p.md", spec({ title: "Parent" }), ["p/c.md"], 0),
        specNode("p/c.md", spec({ title: "Child" }), ["p/c/g.md"], 1),
        specNode("p/c/g.md", spec({ title: "Grand" }), [], 2),
      ];
      const result = buildUnifiedGraph([], nodes, {
        collapsedSpecs: new Set(["p.md"]),
      });
      expect(result.nodes.map((n) => n.id)).toEqual(["spec:p.md"]);
      expect(result.edges).toEqual([]);
    });

    it("sets hasChildren=true and collapsed=true on a collapsed non-leaf spec", () => {
      const buildUnifiedGraph = loadModule();
      const nodes = [
        specNode("p.md", spec({ title: "P" }), ["p/c.md"], 0),
        specNode("p/c.md", spec({ title: "C" }), [], 1),
      ];
      const result = buildUnifiedGraph([], nodes, {
        collapsedSpecs: new Set(["p.md"]),
      });
      const p = result.nodes.find((n) => n.id === "spec:p.md");
      expect(p.extra.hasChildren).toBe(true);
      expect(p.extra.collapsed).toBe(true);
    });

    it("sets hasChildren=true and collapsed=false on an expanded non-leaf spec", () => {
      const buildUnifiedGraph = loadModule();
      const nodes = [
        specNode("p.md", spec({ title: "P" }), ["p/c.md"], 0),
        specNode("p/c.md", spec({ title: "C" }), [], 1),
      ];
      const result = buildUnifiedGraph([], nodes); // no collapsedSpecs
      const p = result.nodes.find((n) => n.id === "spec:p.md");
      expect(p.extra.hasChildren).toBe(true);
      expect(p.extra.collapsed).toBe(false);
    });

    it("drops dispatch and spec_dep edges whose endpoints are hidden by collapse", () => {
      const buildUnifiedGraph = loadModule();
      const nodes = [
        specNode("p.md", spec({ title: "P" }), ["p/c.md"], 0),
        specNode(
          "p/c.md",
          spec({
            title: "C",
            dispatched_task_id: "t1",
            depends_on: ["other.md"],
          }),
          [],
          1,
        ),
        specNode("other.md", spec({ title: "Other" }), [], 0),
      ];
      const tasks = [{ id: "t1", title: "T1", depends_on: [] }];
      const result = buildUnifiedGraph(tasks, nodes, {
        collapsedSpecs: new Set(["p.md"]),
      });
      const edgeKinds = result.edges.map((e) => e.kind).sort();
      // No dispatch edge (child spec hidden), no spec_dep to child, no
      // containment out of the collapsed parent.
      expect(edgeKinds).toEqual([]);
      // Task t1 is still in the graph as a standalone node (it exists on
      // the board even if its dispatching spec is hidden).
      expect(result.nodes.map((n) => n.id)).toContain("task:t1");
    });
  });
});

describe("renderUnifiedGraph", () => {
  it("returns false for an empty graph and does not populate the SVG", () => {
    const { renderUnifiedGraph, makeEl } = loadRenderer();
    const svg = makeEl("svg");
    const ok = renderUnifiedGraph({ nodes: [], edges: [] }, svg);
    expect(ok).toBe(false);
    expect(svg.children).toHaveLength(0);
  });

  it("returns false when svg argument is missing", () => {
    const { renderUnifiedGraph } = loadRenderer();
    const ok = renderUnifiedGraph({ nodes: [{ id: "a" }], edges: [] }, null);
    expect(ok).toBe(false);
  });

  it("renders one <g> group per node and an SVG sized to the layout", () => {
    const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
    const tasks = [
      { id: "a", title: "A", status: "done", depends_on: [] },
      { id: "b", title: "B", status: "backlog", depends_on: ["a"] },
    ];
    const graph = buildUnifiedGraph(tasks, []);
    const svg = makeEl("svg");
    const ok = renderUnifiedGraph(graph, svg);
    expect(ok).toBe(true);

    // One <path> for the edge and one <g> per node; <g> also wraps the <text>
    // and <rect> children, so top-level SVG children are path + 2 groups.
    const groups = svg.children.filter((c) => c.tagName === "g");
    expect(groups).toHaveLength(2);
    const paths = svg.children.filter((c) => c.tagName === "path");
    expect(paths).toHaveLength(1);
    expect(paths[0].getAttribute("data-kind")).toBe("task_dep");

    // viewBox / width / height were set.
    expect(svg.getAttribute("viewBox")).toMatch(/^0 0 \d+ \d+$/);
    expect(Number(svg.getAttribute("width"))).toBeGreaterThan(0);
    expect(Number(svg.getAttribute("height"))).toBeGreaterThan(0);
  });

  it("distinguishes spec and task nodes via data-kind attributes", () => {
    const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
    const specNodes = [
      {
        path: "specs/leaf.md",
        spec: {
          title: "Leaf",
          status: "validated",
          depends_on: [],
          dispatched_task_id: "t1",
        },
        children: [],
        is_leaf: true,
        depth: 0,
      },
    ];
    const tasks = [{ id: "t1", title: "Impl", status: "done", depends_on: [] }];
    const graph = buildUnifiedGraph(tasks, specNodes);
    const svg = makeEl("svg");
    renderUnifiedGraph(graph, svg);

    const groups = svg.children.filter((c) => c.tagName === "g");
    const kinds = groups.map((g) => g.getAttribute("data-kind")).sort();
    expect(kinds).toEqual(["spec", "task"]);

    // dispatch edge should be present.
    const paths = svg.children.filter((c) => c.tagName === "path");
    const edgeKinds = paths.map((p) => p.getAttribute("data-kind"));
    expect(edgeKinds).toContain("dispatch");
  });

  it("drops the dash on task_dep edges whose prerequisite is done (satisfied cue)", () => {
    const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
    const tasks = [
      { id: "a", title: "A", status: "done", depends_on: [] },
      { id: "b", title: "B", status: "in_progress", depends_on: ["a"] },
      { id: "c", title: "C", status: "backlog", depends_on: [] },
      { id: "d", title: "D", status: "backlog", depends_on: ["c"] },
    ];
    const graph = buildUnifiedGraph(tasks, []);
    const svg = makeEl("svg");
    renderUnifiedGraph(graph, svg);

    const paths = svg.children.filter((c) => c.tagName === "path");
    expect(paths).toHaveLength(2);
    // The a→b edge should be solid (a is done); the c→d edge should be dashed.
    const byAttrs = paths.map((p) => ({
      dash: p.getAttribute("stroke-dasharray"),
      d: p.getAttribute("d"),
    }));
    const solid = byAttrs.filter((e) => !e.dash);
    const dashed = byAttrs.filter((e) => e.dash);
    expect(solid).toHaveLength(1);
    expect(dashed).toHaveLength(1);
  });

  it("draws a toggle handle on spec nodes with children and invokes onToggleSpec on click", () => {
    const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
    const specNodes = [
      {
        path: "p.md",
        spec: spec({ title: "P" }),
        children: ["p/c.md"],
        is_leaf: false,
        depth: 0,
      },
      {
        path: "p/c.md",
        spec: spec({ title: "C" }),
        children: [],
        is_leaf: true,
        depth: 1,
      },
    ];
    const graph = buildUnifiedGraph([], specNodes);
    const svg = makeEl("svg");
    let toggled = null;
    renderUnifiedGraph(graph, svg, {
      onToggleSpec: (path) => {
        toggled = path;
      },
    });
    // Find the toggle handle (g with data-role="toggle").
    const specGroup = svg.children.find(
      (c) => c.tagName === "g" && c.getAttribute("data-id") === "spec:p.md",
    );
    expect(specGroup).toBeDefined();
    const toggle = specGroup.children.find(
      (c) => c.getAttribute("data-role") === "toggle",
    );
    expect(toggle).toBeDefined();
    // Simulate the click.
    const listeners = toggle._listeners.click || [];
    expect(listeners.length).toBeGreaterThan(0);
    listeners[0]({ stopPropagation: () => {} });
    expect(toggled).toBe("p.md");
  });

  it("omits the toggle handle for leaf specs (no children)", () => {
    const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
    const specNodes = [
      {
        path: "leaf.md",
        spec: spec({ title: "L" }),
        children: [],
        is_leaf: true,
        depth: 0,
      },
    ];
    const graph = buildUnifiedGraph([], specNodes);
    const svg = makeEl("svg");
    renderUnifiedGraph(graph, svg);
    const specGroup = svg.children.find(
      (c) => c.tagName === "g" && c.getAttribute("data-id") === "spec:leaf.md",
    );
    const toggle = specGroup.children.find(
      (c) => c.getAttribute("data-role") === "toggle",
    );
    expect(toggle).toBeUndefined();
  });

  function spec(opts) {
    return Object.assign(
      {
        title: "",
        status: "drafted",
        depends_on: [],
        affects: [],
        effort: "medium",
        created: "",
        updated: "",
        author: "",
        dispatched_task_id: null,
      },
      opts || {},
    );
  }

  it("places connected task nodes within a bounded spring distance", () => {
    // Force-directed layout doesn't guarantee a specific axis ordering,
    // but it does keep connected nodes closer than the initial circular
    // seed radius. A two-node component should converge to roughly an
    // edge-rest-length apart.
    const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
    const tasks = [
      { id: "a", title: "A", depends_on: [] },
      { id: "b", title: "B", depends_on: ["a"] },
    ];
    const graph = buildUnifiedGraph(tasks, []);
    const svg = makeEl("svg");
    renderUnifiedGraph(graph, svg);

    const groups = svg.children.filter((c) => c.tagName === "g");
    const byId = new Map();
    for (const g of groups) {
      byId.set(g.getAttribute("data-id"), g);
    }
    const getRect = (g) => {
      const body = g.children.find((c) => c.tagName === "g");
      const host = body || g;
      return host.children.find((c) => c.tagName === "rect");
    };
    const rectA = getRect(byId.get("task:a"));
    const rectB = getRect(byId.get("task:b"));
    const xA = Number(rectA.getAttribute("x"));
    const yA = Number(rectA.getAttribute("y"));
    const xB = Number(rectB.getAttribute("x"));
    const yB = Number(rectB.getAttribute("y"));
    const dist = Math.hypot(xA - xB, yA - yB);
    // Nodes should separate (minimum-anti-overlap kicks in) but stay
    // within a reasonable cluster.
    expect(dist).toBeGreaterThan(80);
    expect(dist).toBeLessThan(1200);
  });

  describe("interactions", () => {
    // Helper to grab the node <g> and its inner body <g> for a given id.
    function pickNode(svg, id) {
      const group = svg.children.find(
        (c) => c.tagName === "g" && c.getAttribute("data-id") === id,
      );
      const body = group && group.children.find((c) => c.tagName === "g");
      return { group, body };
    }

    function fire(el, ev, payload) {
      const ls = el._listeners[ev] || [];
      for (const fn of ls) fn(payload || {});
    }

    it("invokes onFocusNode(id) on a plain click", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [{ id: "a", title: "A", depends_on: [] }],
        [],
      );
      const svg = makeEl("svg");
      let focused = "unset";
      renderUnifiedGraph(graph, svg, {
        onFocusNode: (id) => {
          focused = id;
        },
      });
      const { body, group } = pickNode(svg, "task:a");
      expect(body).toBeDefined();
      // No transform set → treated as a click, not a drag.
      expect(group.getAttribute("transform")).toBeNull();
      fire(body, "click", { shiftKey: false });
      expect(focused).toBe("task:a");
    });

    it("invokes onNavigateNode on shift+click", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [{ id: "a", title: "A", depends_on: [] }],
        [],
      );
      const svg = makeEl("svg");
      let navigated = null;
      let focused = "unset";
      renderUnifiedGraph(graph, svg, {
        onFocusNode: (id) => {
          focused = id;
        },
        onNavigateNode: (id) => {
          navigated = id;
        },
      });
      const { body } = pickNode(svg, "task:a");
      fire(body, "click", { shiftKey: true });
      expect(navigated).toBe("task:a");
      // Shift+click skips focus.
      expect(focused).toBe("unset");
    });

    it("invokes onFocusNode(null) when the backdrop is clicked", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [{ id: "a", title: "A", depends_on: [] }],
        [],
      );
      const svg = makeEl("svg");
      let focused = "unset";
      renderUnifiedGraph(graph, svg, {
        onFocusNode: (id) => {
          focused = id;
        },
      });
      const backdrop = svg.children.find(
        (c) =>
          c.tagName === "rect" &&
          c.getAttribute("data-role") === "canvas-backdrop",
      );
      expect(backdrop).toBeDefined();
      fire(backdrop, "click", {});
      expect(focused).toBeNull();
    });

    it("invokes onPinNode(id, x, y) after a drag past the threshold", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [{ id: "a", title: "A", depends_on: [] }],
        [],
      );
      const svg = makeEl("svg");
      let pinned = null;
      renderUnifiedGraph(graph, svg, {
        onPinNode: (id, x, y) => {
          pinned = { id, x, y };
        },
      });
      const { body } = pickNode(svg, "task:a");
      fire(body, "mousedown", {
        button: 0,
        clientX: 100,
        clientY: 100,
        stopPropagation: () => {},
      });
      fire(body, "mousemove", { clientX: 180, clientY: 140 });
      fire(body, "mouseup", { clientX: 180, clientY: 140 });
      expect(pinned).not.toBeNull();
      expect(pinned.id).toBe("task:a");
      // Drag delta (80, 40) applied to the node's original coords.
      // Original x was set by layout (>0); the pin call adds the delta.
      expect(pinned.x).toBeGreaterThan(0);
      expect(pinned.y).toBeGreaterThan(0);
    });

    it("does not invoke onPinNode when movement stays under the drag threshold", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [{ id: "a", title: "A", depends_on: [] }],
        [],
      );
      const svg = makeEl("svg");
      let pinned = null;
      let focused = null;
      renderUnifiedGraph(graph, svg, {
        onPinNode: (id, x, y) => {
          pinned = { id, x, y };
        },
        onFocusNode: (id) => {
          focused = id;
        },
      });
      const { body } = pickNode(svg, "task:a");
      fire(body, "mousedown", {
        button: 0,
        clientX: 100,
        clientY: 100,
        stopPropagation: () => {},
      });
      fire(body, "mousemove", { clientX: 101, clientY: 101 });
      fire(body, "mouseup", { clientX: 101, clientY: 101 });
      fire(body, "click", { shiftKey: false });
      expect(pinned).toBeNull();
      // A tap registered as a click, not a drag → focus fires.
      expect(focused).toBe("task:a");
    });

    it("invokes onUnpinNode on dblclick", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [{ id: "a", title: "A", depends_on: [] }],
        [],
      );
      const svg = makeEl("svg");
      let unpinned = null;
      renderUnifiedGraph(graph, svg, {
        onUnpinNode: (id) => {
          unpinned = id;
        },
      });
      const { body } = pickNode(svg, "task:a");
      fire(body, "dblclick", { stopPropagation: () => {} });
      expect(unpinned).toBe("task:a");
    });

    it("dims nodes outside the focused node's 1-hop neighbourhood", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const tasks = [
        { id: "a", title: "A", depends_on: [] },
        { id: "b", title: "B", depends_on: ["a"] },
        { id: "c", title: "C", depends_on: [] },
      ];
      const graph = buildUnifiedGraph(tasks, []);
      const svg = makeEl("svg");
      renderUnifiedGraph(graph, svg, { focusedNodeId: "task:a" });
      const { group: groupA } = pickNode(svg, "task:a");
      const { group: groupB } = pickNode(svg, "task:b");
      const { group: groupC } = pickNode(svg, "task:c");
      // Focused node + neighbour retain full opacity.
      expect(groupA.getAttribute("opacity")).toBeNull();
      expect(groupB.getAttribute("opacity")).toBeNull();
      // Unrelated node is dimmed.
      expect(groupC.getAttribute("opacity")).toBe("0.28");
    });

    it("draws a pin marker on nodes whose id is in pinnedIds", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [
          { id: "a", title: "A", depends_on: [] },
          { id: "b", title: "B", depends_on: [] },
        ],
        [],
      );
      const svg = makeEl("svg");
      renderUnifiedGraph(graph, svg, { pinnedIds: new Set(["task:a"]) });
      const { body: bodyA } = pickNode(svg, "task:a");
      const { body: bodyB } = pickNode(svg, "task:b");
      const pinA = bodyA.children.find(
        (c) => c.tagName === "circle" && c.getAttribute("fill") === "#f7c466",
      );
      const pinB = bodyB.children.find(
        (c) => c.tagName === "circle" && c.getAttribute("fill") === "#f7c466",
      );
      expect(pinA).toBeDefined();
      expect(pinB).toBeUndefined();
    });

    it("dims nodes that don't match searchQuery (case-insensitive label/path)", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const tasks = [
        { id: "a", title: "Alpha", depends_on: [] },
        { id: "b", title: "Beta", depends_on: [] },
      ];
      const graph = buildUnifiedGraph(tasks, []);
      const svg = makeEl("svg");
      renderUnifiedGraph(graph, svg, { searchQuery: "alph" });
      const { group: groupA } = pickNode(svg, "task:a");
      const { group: groupB } = pickNode(svg, "task:b");
      expect(groupA.getAttribute("opacity")).toBeNull();
      expect(groupB.getAttribute("opacity")).toBe("0.28");
    });

    it("drag delta is divided by scale so pins land at correct graph coords", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [{ id: "a", title: "A", depends_on: [] }],
        [],
      );
      const svg = makeEl("svg");
      let pinned = null;
      renderUnifiedGraph(graph, svg, {
        onPinNode: (id, x, y) => {
          pinned = { id, x, y };
        },
        getScale: () => 2,
      });
      const { body } = pickNode(svg, "task:a");
      const { body: _ignore } = { body };
      void _ignore;
      // Capture pre-drag origin to verify the delta-in-graph-coords math.
      const rect = body.children.find((c) => c.tagName === "rect");
      const originX = Number(rect.getAttribute("x"));
      const originY = Number(rect.getAttribute("y"));
      fire(body, "mousedown", {
        button: 0,
        clientX: 0,
        clientY: 0,
        stopPropagation: () => {},
      });
      // 200 screen pixels at scale 2 → 100 graph units.
      fire(body, "mousemove", { clientX: 200, clientY: 80 });
      fire(body, "mouseup", { clientX: 200, clientY: 80 });
      expect(pinned).not.toBeNull();
      expect(pinned.x).toBeCloseTo(originX + 100, 5);
      expect(pinned.y).toBeCloseTo(originY + 40, 5);
    });

    it("invokes onHoverNode on mouseenter and null on mouseleave", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [{ id: "a", title: "A", depends_on: [] }],
        [],
      );
      const svg = makeEl("svg");
      const calls = [];
      renderUnifiedGraph(graph, svg, {
        onHoverNode: (id) => calls.push(id),
      });
      const { body } = pickNode(svg, "task:a");
      fire(body, "mouseenter", {});
      fire(body, "mouseleave", {});
      expect(calls).toEqual(["task:a", null]);
    });

    it("annotates edge paths with data-from / data-to for hover lookup", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [
          { id: "a", title: "A", depends_on: [] },
          { id: "b", title: "B", depends_on: ["a"] },
        ],
        [],
      );
      const svg = makeEl("svg");
      renderUnifiedGraph(graph, svg);
      const path = svg.children.find(
        (c) =>
          c.tagName === "path" && c.getAttribute("data-kind") === "task_dep",
      );
      expect(path).toBeDefined();
      expect(path.getAttribute("data-from")).toBe("task:a");
      expect(path.getAttribute("data-to")).toBe("task:b");
    });

    it("renders edges as straight line segments (polyline)", () => {
      // With the straight-line routing, the path must use only M (move)
      // and L (lineto) commands — no Q (quadratic) or C (cubic).
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [
          { id: "a", title: "A", depends_on: [] },
          { id: "b", title: "B", depends_on: ["a"] },
        ],
        [],
      );
      const svg = makeEl("svg");
      renderUnifiedGraph(graph, svg);
      const edge = svg.children.find(
        (el) =>
          el.tagName === "path" && el.getAttribute("data-kind") === "task_dep",
      );
      expect(edge).toBeDefined();
      const d = edge.getAttribute("d") || "";
      expect(d).toMatch(/^M[\d.,-]+ L[\d.,-]+$/);
      expect(d).not.toMatch(/[QC]/);
    });

    it("live-updates incident edges during drag (no lag behind the node)", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [
          { id: "a", title: "A", depends_on: [] },
          { id: "b", title: "B", depends_on: ["a"] },
        ],
        [],
      );
      const svg = makeEl("svg");
      renderUnifiedGraph(graph, svg, {
        pinnedPositions: new Map([
          ["task:a", { x: 0, y: 0 }],
          ["task:b", { x: 600, y: 0 }],
        ]),
      });
      const path = svg.children.find(
        (c) =>
          c.tagName === "path" && c.getAttribute("data-kind") === "task_dep",
      );
      const initialD = path.getAttribute("d");
      const { body } = pickNode(svg, "task:a");
      fire(body, "mousedown", {
        button: 0,
        clientX: 0,
        clientY: 0,
        stopPropagation: () => {},
      });
      fire(body, "mousemove", { clientX: 40, clientY: 120 });
      // The path d must have changed during the drag — if it's still the
      // initial shape, edges are lagging (the old behaviour).
      const liveD = path.getAttribute("d");
      expect(liveD).not.toBe(initialD);
      fire(body, "mouseup", { clientX: 40, clientY: 120 });
    });

    it("committed drag persists in DOM so a second drag starts from the new baseline", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [{ id: "a", title: "A", depends_on: [] }],
        [],
      );
      const svg = makeEl("svg");
      const pins = [];
      renderUnifiedGraph(graph, svg, {
        pinnedPositions: new Map([["task:a", { x: 100, y: 100 }]]),
        onPinNode: (id, newX, newY) => pins.push({ id, x: newX, y: newY }),
      });
      const { body, group } = pickNode(svg, "task:a");
      // First drag: +50 x
      fire(body, "mousedown", {
        button: 0,
        clientX: 0,
        clientY: 0,
        stopPropagation: () => {},
      });
      fire(body, "mousemove", { clientX: 50, clientY: 0 });
      fire(body, "mouseup", { clientX: 50, clientY: 0 });
      expect(pins[0].x).toBe(150);
      // Transform must be cleared after mouseup (delta committed to attrs).
      expect(group.getAttribute("transform")).toBeNull();
      // Second drag: +50 x — must land at 200, not 150 (no snap-back).
      fire(body, "mousedown", {
        button: 0,
        clientX: 0,
        clientY: 0,
        stopPropagation: () => {},
      });
      fire(body, "mousemove", { clientX: 50, clientY: 0 });
      fire(body, "mouseup", { clientX: 50, clientY: 0 });
      expect(pins[1].x).toBe(200);
    });

    it("attaches edges to the node rectangle perimeter, not the centre", () => {
      // With two nodes pinned at known positions, the edge must begin at
      // the border of the source rectangle (facing the destination) and
      // end at the border of the destination rectangle (facing the
      // source). This is what prevents multiple edges from collapsing to
      // one point when many parallel edges converge on the same node.
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [
          { id: "a", title: "A", depends_on: [] },
          { id: "b", title: "B", depends_on: ["a"] },
        ],
        [],
      );
      const svg = makeEl("svg");
      // Pin A at (0, 0) and B at (600, 0) so the edge goes L→R horizontally.
      const pinnedPositions = new Map([
        ["task:a", { x: 0, y: 0 }],
        ["task:b", { x: 600, y: 0 }],
      ]);
      renderUnifiedGraph(graph, svg, { pinnedPositions });
      const path = svg.children.find(
        (c) =>
          c.tagName === "path" && c.getAttribute("data-kind") === "task_dep",
      );
      expect(path).toBeDefined();
      const d = path.getAttribute("d") || "";
      const m = d.match(/^M(-?\d+(?:\.\d+)?),(-?\d+(?:\.\d+)?)/);
      const tail = d.match(/(-?\d+(?:\.\d+)?),(-?\d+(?:\.\d+)?)$/);
      expect(m).not.toBeNull();
      expect(tail).not.toBeNull();
      // NODE_W = 220 in unified-graph.js; the source at x=0 → right
      // border at 220. The destination at x=600 → left border at 600.
      // Edges must attach at the border, not at the centres (110 / 710).
      expect(parseFloat(m[1])).toBe(220);
      expect(parseFloat(tail[1])).toBe(600);
    });

    it("honours pinnedPositions — a pinned node renders at exact (x, y)", () => {
      const { buildUnifiedGraph, renderUnifiedGraph, makeEl } = loadRenderer();
      const graph = buildUnifiedGraph(
        [
          { id: "a", title: "A", depends_on: [] },
          { id: "b", title: "B", depends_on: ["a"] },
        ],
        [],
      );
      const svg = makeEl("svg");
      const pinnedPositions = new Map([["task:a", { x: 999, y: 555 }]]);
      renderUnifiedGraph(graph, svg, { pinnedPositions });
      const { body } = pickNode(svg, "task:a");
      const rect = body.children.find((c) => c.tagName === "rect");
      expect(Number(rect.getAttribute("x"))).toBe(999);
      expect(Number(rect.getAttribute("y"))).toBe(555);
    });
  });
});
