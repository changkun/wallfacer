/**
 * Unit tests for depgraph.js — the panel-based DAG dependency graph.
 *
 * depgraph.js is loaded into an isolated vm context.  DOM APIs are fully
 * stubbed so no real browser or jsdom is required.
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "depgraph.js"), "utf8");

// ---------------------------------------------------------------------------
// Minimal DOM mock
// ---------------------------------------------------------------------------

/**
 * Build a minimal document + element factory that shares an id registry.
 * Provides just enough fidelity for depgraph.js to run and for assertions
 * to succeed.
 */
function makeDom() {
  const registry = new Map();

  function makeEl(tag) {
    const _attrs = {};
    const _style = {};
    const _children = [];
    const _listeners = {};
    let _text = "";
    let _id = "";
    let _className = "";

    const el = {
      tagName: tag,
      get id() {
        return _id;
      },
      set id(v) {
        _id = v;
        if (v) registry.set(v, el);
      },

      get style() {
        return _style;
      },
      get children() {
        return _children;
      },
      get textContent() {
        return _text;
      },
      set textContent(v) {
        _text = String(v || "");
      },
      get className() {
        return _className;
      },
      set className(v) {
        _className = String(v || "");
      },

      setAttribute(k, v) {
        _attrs[k] = String(v);
        if (k === "id") el.id = v;
      },
      getAttribute(k) {
        return  Object.hasOwn(_attrs, k)
          ? _attrs[k]
          : null;
      },

      appendChild(child) {
        _children.push(child);
        if (child.id) registry.set(child.id, child);
        return child;
      },
      removeChild(child) {
        const i = _children.indexOf(child);
        if (i !== -1) _children.splice(i, 1);
      },
      get firstChild() {
        return _children[0] || null;
      },

      // 'afterend' — register the new element so getElementById can find it.
      insertAdjacentElement(_pos, newEl) {
        if (newEl.id) registry.set(newEl.id, newEl);
        else registry.set("__last_inserted", newEl);
      },

      // Simple selector: #id or .class, backed by registry and children.
      querySelector(sel) {
        const idMatch = sel.match(/^#(.+)$/);
        if (idMatch) return registry.get(idMatch[1]) || null;
        const classMatch = sel.match(/^\.(.+)$/);
        if (classMatch) {
          const cls = classMatch[1];
          for (const child of _children) {
            if (child.className === cls) return child;
          }
        }
        return null;
      },

      addEventListener(ev, fn) {
        if (!_listeners[ev]) _listeners[ev] = [];
        _listeners[ev].push(fn);
      },

      _attrs,
      _listeners,
    };
    return el;
  }

  const body = makeEl("body");
  const board = makeEl("main");
  board.id = "board"; // pre-registered so getElementById('board') works

  const document = {
    getElementById(id) {
      return registry.get(id) || null;
    },
    createElement(tag) {
      return makeEl(tag);
    },
    createElementNS(_ns, tag) {
      return makeEl(tag);
    },
    querySelector(sel) {
      // Support class selectors by scanning the registry
      const classMatch = sel.match(/^\.(.+)$/);
      if (classMatch) {
        for (const el of registry.values()) {
          if (el.className && el.className.includes(classMatch[1])) return el;
        }
      }
      const idMatch = sel.match(/^#(.+)$/);
      if (idMatch) return registry.get(idMatch[1]) || null;
      return null;
    },
    body,
  };

  return { document, registry };
}

// ---------------------------------------------------------------------------
// vm context factory
// ---------------------------------------------------------------------------

/**
 * Load depgraph.js into an isolated vm context.
 *
 * depgraph.js is an IIFE that assigns renderDependencyGraph and
 * hideDependencyGraph onto the `window` object (not onto the global context).
 * We therefore expose them via the returned object so tests can call them
 * directly without needing ctx.window.*
 */
function makeContext() {
  const store = {};
  const { document, registry } = makeDom();
  const windowObj = {};

  const ctx = vm.createContext({
    document,
    window: windowObj,
    localStorage: {
      getItem(k) {
        return  Object.hasOwn(store, k) ? store[k] : null;
      },
      setItem(k, v) {
        store[k] = String(v);
      },
    },
    // Stub browser API used to read CSS custom properties for edge colours.
    getComputedStyle: () => ({ getPropertyValue: () => "" }),
    console,
  });

  vm.runInContext(code, ctx, { filename: join(jsDir, "depgraph.js") });

  return {
    ctx,
    registry,
    store,
    renderDependencyGraph: windowObj.renderDependencyGraph,
    hideDependencyGraph: windowObj.hideDependencyGraph,
  };
}

// ---------------------------------------------------------------------------
// Tree-walk helper
// ---------------------------------------------------------------------------

/** Return all descendants (depth-first) whose tagName matches. */
function findAll(root, tag) {
  const results = [];
  const t = tag.toLowerCase();
  function walk(el) {
    if (el && el.tagName && el.tagName.toLowerCase() === t) results.push(el);
    const kids = (el && el.children) || [];
    for (let i = 0; i < kids.length; i++) walk(kids[i]);
  }
  walk(root);
  return results;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("renderDependencyGraph", () => {
  // -------------------------------------------------------------------------
  // Test 1 — linear chain  C → B → A
  // -------------------------------------------------------------------------
  describe("Test 1 — linear chain", () => {
    it("panel exists, SVG has 3 rects and 2 paths; C leftmost, A rightmost", () => {
      const { registry, renderDependencyGraph } = makeContext();

      const tasks = [
        {
          id: "A",
          title: "Task A",
          status: "backlog",
          depends_on: ["B"],
          position: 2,
        },
        {
          id: "B",
          title: "Task B",
          status: "in_progress",
          depends_on: ["C"],
          position: 1,
        },
        {
          id: "C",
          title: "Task C",
          status: "done",
          depends_on: [],
          position: 0,
        },
      ];

      renderDependencyGraph(tasks);

      // Panel must have been created and registered.
      const panel = registry.get("depgraph-panel");
      expect(panel).toBeTruthy();

      // SVG element must exist inside the panel.
      const svg = registry.get("depgraph-svg");
      expect(svg).toBeTruthy();

      // Exactly 3 node <rect>s and 2 edge <path>s.
      expect(findAll(svg, "rect")).toHaveLength(3);
      expect(findAll(svg, "path")).toHaveLength(2);

      // Find the <g> group for each task by matching its <text> textContent.
      const gs = findAll(svg, "g");
      function gForTitle(title) {
        return gs.find((g) =>
          g.children.some(
            (c) => c.tagName === "text" && c.textContent === title,
          ),
        );
      }

      const gC = gForTitle("Task C");
      const gA = gForTitle("Task A");
      expect(gC).toBeTruthy();
      expect(gA).toBeTruthy();

      const rectC = gC.children.find((c) => c.tagName === "rect");
      const rectA = gA.children.find((c) => c.tagName === "rect");
      expect(rectC).toBeTruthy();
      expect(rectA).toBeTruthy();

      // Layout constants (must match depgraph.js).
      const PAD = 24;
      const NODE_W = 180;
      const H_GAP = 120;

      // C has no prerequisites → level 0 → x = PAD.
      expect(Number(rectC.getAttribute("x"))).toBe(PAD);

      // A depends on B which depends on C → level 2 → x = PAD + 2*(NODE_W+H_GAP).
      expect(Number(rectA.getAttribute("x"))).toBe(PAD + 2 * (NODE_W + H_GAP));

      // C must be to the left of A.
      expect(Number(rectC.getAttribute("x"))).toBeLessThan(
        Number(rectA.getAttribute("x")),
      );
    });
  });

  // -------------------------------------------------------------------------
  // Test 2 — no dependency edges
  // -------------------------------------------------------------------------
  describe("Test 2 — empty deps", () => {
    it("panel is hidden or absent when no task has depends_on", () => {
      const { registry, renderDependencyGraph } = makeContext();

      const tasks = [
        {
          id: "X",
          title: "Task X",
          status: "backlog",
          depends_on: [],
          position: 0,
        },
        {
          id: "Y",
          title: "Task Y",
          status: "done",
          depends_on: [],
          position: 1,
        },
        {
          id: "Z",
          title: "Task Z",
          status: "failed",
          depends_on: [],
          position: 2,
        },
      ];

      renderDependencyGraph(tasks);

      const panel = registry.get("depgraph-panel");
      // Panel is shown with an empty-state message when no dep edges exist.
      if (panel) {
        expect(panel.style.display).toBe("block");
        const emptyMsg = panel.querySelector(".depgraph-empty");
        expect(emptyMsg).toBeTruthy();
      }
    });
  });

  // -------------------------------------------------------------------------
  // Test 3 — cycle detection
  // -------------------------------------------------------------------------
  describe("Test 3 — cycle detection", () => {
    it("renders a cycle-warning label and does not throw", () => {
      const { registry, renderDependencyGraph } = makeContext();

      const tasks = [
        {
          id: "A",
          title: "Task A",
          status: "backlog",
          depends_on: ["B"],
          position: 0,
        },
        {
          id: "B",
          title: "Task B",
          status: "backlog",
          depends_on: ["A"],
          position: 1,
        },
      ];

      expect(() => renderDependencyGraph(tasks)).not.toThrow();

      const svg = registry.get("depgraph-svg");
      expect(svg).toBeTruthy();

      // At least one <text> element must contain the word 'cycle'.
      const hasCycleLabel = findAll(svg, "text").some((t) =>
        t.textContent.includes("cycle"),
      );
      expect(hasCycleLabel).toBe(true);
    });
  });

  // -------------------------------------------------------------------------
  // Test 4 — fingerprint caching
  // -------------------------------------------------------------------------
  describe("Test 4 — fingerprint caching", () => {
    it("returns the same SVG DOM node on a second call with identical tasks", () => {
      const { registry, renderDependencyGraph } = makeContext();

      const tasks = [
        {
          id: "A",
          title: "Task A",
          status: "backlog",
          depends_on: ["B"],
          position: 1,
        },
        {
          id: "B",
          title: "Task B",
          status: "done",
          depends_on: [],
          position: 0,
        },
      ];

      renderDependencyGraph(tasks);
      const svgFirst = registry.get("depgraph-svg");
      expect(svgFirst).toBeTruthy();

      // Second identical call must return early without replacing the SVG element.
      renderDependencyGraph(tasks);
      const svgSecond = registry.get("depgraph-svg");

      expect(svgSecond).toBe(svgFirst);
    });
  });
});

// ---------------------------------------------------------------------------
// hideDependencyGraph
// ---------------------------------------------------------------------------

describe("hideDependencyGraph", () => {
  it("sets panel display to none when the panel exists", () => {
    const { registry, renderDependencyGraph, hideDependencyGraph } =
      makeContext();

    const tasks = [
      {
        id: "A",
        title: "Task A",
        status: "backlog",
        depends_on: ["B"],
        position: 1,
      },
      { id: "B", title: "Task B", status: "done", depends_on: [], position: 0 },
    ];

    renderDependencyGraph(tasks);
    const panel = registry.get("depgraph-panel");
    expect(panel).toBeTruthy();

    hideDependencyGraph();
    expect(panel.style.display).toBe("none");
  });

  it("allows the graph to re-render after being hidden (fingerprint reset)", () => {
    const { registry, renderDependencyGraph, hideDependencyGraph } =
      makeContext();

    const tasks = [
      {
        id: "A",
        title: "Task A",
        status: "backlog",
        depends_on: ["B"],
        position: 1,
      },
      { id: "B", title: "Task B", status: "done", depends_on: [], position: 0 },
    ];

    renderDependencyGraph(tasks);
    hideDependencyGraph();

    // Panel must be hidden after the call.
    const panel = registry.get("depgraph-panel");
    expect(panel.style.display).toBe("none");

    // renderDependencyGraph with the same tasks must show the panel again.
    renderDependencyGraph(tasks);
    expect(panel.style.display).toBe("block");
  });
});
