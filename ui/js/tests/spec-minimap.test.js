/**
 * Unit tests for spec-minimap.js — dependency minimap rendering.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-minimap.js"), "utf8");

function makeSvgEl(tag) {
  const _attrs = {};
  const _children = [];
  const _listeners = {};
  return {
    tagName: tag,
    setAttribute(k, v) {
      _attrs[k] = v;
    },
    getAttribute(k) {
      return _attrs[k] || null;
    },
    appendChild(child) {
      _children.push(child);
    },
    addEventListener(type, fn) {
      if (!_listeners[type]) _listeners[type] = [];
      _listeners[type].push(fn);
    },
    get style() {
      return {};
    },
    set innerHTML(v) {
      _children.length = 0;
    },
    get children() {
      return _children;
    },
    textContent: "",
    _attrs,
    _children,
  };
}

function makeContext() {
  const registry = new Map();

  function makeEl(tag, id) {
    const _classList = new Set();
    const el = {
      tagName: tag,
      classList: {
        add(c) {
          _classList.add(c);
        },
        remove(c) {
          _classList.delete(c);
        },
        toggle(c, force) {
          if (force) _classList.add(c);
          else _classList.delete(c);
        },
        contains(c) {
          return _classList.has(c);
        },
      },
      setAttribute(k, v) {},
      set innerHTML(v) {},
    };
    if (id) registry.set(id, el);
    return el;
  }

  // Create minimap container and SVG.
  const container = makeEl("DIV", "spec-minimap");
  const svgEl = makeSvgEl("svg");
  registry.set("spec-minimap-svg", svgEl);

  const ctx = {
    document: {
      getElementById(id) {
        return registry.get(id) || null;
      },
      createElementNS(ns, tag) {
        return makeSvgEl(tag);
      },
    },
    focusSpec: () => {},
    activeWorkspaces: ["/workspace"],
    Math,
    console,
    registry,
    svgEl,
    container,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

const MOCK_NODES = [
  {
    path: "a.md",
    spec: { title: "A", status: "complete", depends_on: [] },
    children: [],
    is_leaf: true,
    depth: 0,
  },
  {
    path: "b.md",
    spec: { title: "B", status: "validated", depends_on: ["a.md"] },
    children: [],
    is_leaf: true,
    depth: 0,
  },
  {
    path: "c.md",
    spec: { title: "C", status: "drafted", depends_on: ["a.md"] },
    children: [],
    is_leaf: true,
    depth: 0,
  },
];

describe("buildReverseDeps", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
  });

  it("builds reverse dependency index", () => {
    const reverse = ctx.buildReverseDeps(MOCK_NODES);
    expect(reverse["a.md"]).toEqual(["b.md", "c.md"]);
    expect(reverse["b.md"]).toBeUndefined();
  });

  it("returns empty object for nodes with no dependencies", () => {
    const reverse = ctx.buildReverseDeps([MOCK_NODES[0]]);
    expect(Object.keys(reverse)).toHaveLength(0);
  });
});

describe("renderMinimap", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
  });

  it("shows upstream nodes for spec with depends_on", () => {
    ctx.renderMinimap("b.md", { nodes: MOCK_NODES, progress: {} });
    // SVG should have children (rect+text for each node + edges).
    expect(ctx.svgEl._children.length).toBeGreaterThan(0);
  });

  it("shows downstream nodes for spec that others depend on", () => {
    ctx.renderMinimap("a.md", { nodes: MOCK_NODES, progress: {} });
    // A has 2 downstream nodes (B and C depend on A).
    // Each node = rect + text, plus edges. So >= 6 SVG children (3 nodes * 2 + 2 edges).
    expect(ctx.svgEl._children.length).toBeGreaterThanOrEqual(6);
  });

  it("hides minimap when spec has no deps and no dependents", () => {
    const isolated = [
      {
        path: "z.md",
        spec: { title: "Z", status: "drafted", depends_on: [] },
        children: [],
        is_leaf: true,
        depth: 0,
      },
    ];
    ctx.renderMinimap("z.md", { nodes: isolated, progress: {} });
    expect(ctx.container.classList.contains("hidden")).toBe(true);
  });

  it("uses correct status colors", () => {
    expect(ctx._minimapStatusColors["complete"]).toBe("#d4edda");
    expect(ctx._minimapStatusColors["validated"]).toBe("#cce5ff");
    expect(ctx._minimapStatusColors["drafted"]).toBe("#fff3cd");
    expect(ctx._minimapStatusColors["stale"]).toBe("#f8d7da");
    expect(ctx._minimapStatusColors["vague"]).toBe("#e2e3e5");
  });

  it("hides minimap when specPath is null", () => {
    ctx.renderMinimap(null, { nodes: MOCK_NODES, progress: {} });
    expect(ctx.container.classList.contains("hidden")).toBe(true);
  });

  it("focused node has highlight border", () => {
    ctx.renderMinimap("b.md", { nodes: MOCK_NODES, progress: {} });
    // The focused node rect should have stroke-width 2.
    const rects = ctx.svgEl._children.filter((c) => c.tagName === "rect");
    const focusedRect = rects.find(
      (r) => r._attrs["data-spec-path"] === "b.md",
    );
    expect(focusedRect).toBeTruthy();
    expect(focusedRect._attrs["stroke-width"]).toBe(2);
  });
});
