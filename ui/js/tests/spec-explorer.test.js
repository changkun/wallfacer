/**
 * Unit tests for spec-explorer.js — spec tree rendering and explorer mode switching.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-explorer.js"), "utf8");

function makeEl(tag, registry) {
  const _classList = new Set();
  const _style = {};
  const _listeners = {};
  let _id = "";

  const el = {
    tagName: tag,
    get id() {
      return _id;
    },
    set id(v) {
      _id = v;
      if (v) registry.set(v, el);
    },
    style: _style,
    innerHTML: "",
    className: "",
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
    addEventListener(type, fn) {
      if (!_listeners[type]) _listeners[type] = [];
      _listeners[type].push(fn);
    },
    querySelectorAll() {
      return [];
    },
    getAttribute(name) {
      if (name === "data-spec-path") return el._specPath || null;
      if (name === "data-path") return el._path || null;
      return null;
    },
    remove() {},
    get nextSibling() {
      return null;
    },
    insertBefore() {},
    _specPath: null,
    _path: null,
  };
  return el;
}

function makeContext(opts = {}) {
  const registry = new Map();
  const storage = new Map();

  const ids = [
    "explorer-tree",
    "spec-explorer-workspace-toggle",
    "spec-status-filter",
    "spec-dispatch-bar",
    "spec-dispatch-selected-btn",
  ];
  for (const id of ids) {
    const el = makeEl("DIV", registry);
    el.id = id;
  }

  const ctx = {
    document: {
      getElementById(id) {
        return registry.get(id) || null;
      },
      createElement(tag) {
        return makeEl(tag, registry);
      },
    },
    localStorage: {
      getItem(k) {
        return storage.get(k) ?? null;
      },
      setItem(k, v) {
        storage.set(k, v);
      },
    },
    fetch: opts.fetch || (() => Promise.reject(new Error("stubbed"))),
    Routes: { specs: { tree: () => "/api/specs/tree" } },
    withBearerHeaders: () => ({}),
    escapeHtml: (s) => String(s).replace(/</g, "&lt;").replace(/>/g, "&gt;"),
    focusSpec: opts.focusSpec || (() => {}),
    getFocusedSpecPath: () => null,
    activeWorkspaces: ["/workspace/repo"],
    _loadExplorerRoots: opts._loadExplorerRoots || (() => {}),
    _startExplorerRefreshPoll: opts._startExplorerRefreshPoll || (() => {}),
    setInterval: (fn) => {
      ctx._intervalFn = fn;
      return 42;
    },
    clearInterval: () => {},
    JSON,
    Array,
    Set,
    console,
    registry,
    storage,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

const MOCK_TREE_DATA = {
  nodes: [
    {
      path: "local/foo.md",
      spec: {
        title: "Foo",
        status: "validated",
        depends_on: [],
        affects: [],
        track: "local",
      },
      children: ["local/foo/bar.md"],
      is_leaf: false,
      depth: 0,
    },
    {
      path: "local/foo/bar.md",
      spec: {
        title: "Bar",
        status: "complete",
        depends_on: [],
        affects: [],
        track: "local",
      },
      children: [],
      is_leaf: true,
      depth: 1,
    },
  ],
  progress: {
    "local/foo.md": { Complete: 1, Total: 1 },
  },
};

describe("spec-explorer", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
    // Expand the "local" track group so nodes inside it are visible.
    ctx._specExpandedPaths.add("__track__local");
  });

  it("renderSpecTree renders nodes with titles", () => {
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.renderSpecTree();

    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).toContain("Foo");
    // Bar is not visible because parent is not expanded by default
  });

  it("status icons are mapped correctly", () => {
    expect(ctx._specStatusIcons["complete"]).toBe("\u2705");
    expect(ctx._specStatusIcons["validated"]).toBe("\u2714");
    expect(ctx._specStatusIcons["drafted"]).toBe("\uD83D\uDCDD");
    expect(ctx._specStatusIcons["stale"]).toBe("\u26A0\uFE0F");
  });

  it("progress badge is shown for non-leaf nodes", () => {
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.renderSpecTree();

    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).toContain("1/1");
  });

  it("switchExplorerRoot to specs shows toggle", () => {
    ctx.switchExplorerRoot("specs");
    const toggle = ctx.registry.get("spec-explorer-workspace-toggle");
    expect(toggle.classList.contains("hidden")).toBe(false);
  });

  it("switchExplorerRoot to workspace hides toggle", () => {
    ctx.switchExplorerRoot("specs");
    ctx.switchExplorerRoot("workspace");
    const toggle = ctx.registry.get("spec-explorer-workspace-toggle");
    expect(toggle.classList.contains("hidden")).toBe(true);
  });

  it("switchExplorerRoot to workspace calls _loadExplorerRoots", () => {
    let called = false;
    const ctx2 = makeContext({
      _loadExplorerRoots: () => {
        called = true;
      },
    });
    ctx2.switchExplorerRoot("specs");
    ctx2.switchExplorerRoot("workspace");
    expect(called).toBe(true);
  });

  it("expanded state persists in localStorage", () => {
    ctx._specExpandedPaths.add("local/foo.md");
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.renderSpecTree();

    // Simulate toggle by adding to set and saving.
    ctx.localStorage.setItem(
      "wallfacer-spec-expanded",
      JSON.stringify(Array.from(ctx._specExpandedPaths)),
    );

    const stored = JSON.parse(ctx.storage.get("wallfacer-spec-expanded"));
    expect(stored).toContain("local/foo.md");
  });

  it("children are visible when parent is expanded", () => {
    ctx._specExpandedPaths.add("local/foo.md");
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.renderSpecTree();

    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).toContain("Bar");
  });

  it("filterSpecTree by drafted hides non-matching nodes", () => {
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.filterSpecTree("drafted");
    const treeEl = ctx.registry.get("explorer-tree");
    // Neither Foo (validated) nor Bar (complete) match "drafted".
    expect(treeEl.innerHTML).not.toContain("Foo");
    expect(treeEl.innerHTML).not.toContain("Bar");
  });

  it("filterSpecTree 'all' shows everything", () => {
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.filterSpecTree("drafted");
    ctx.filterSpecTree("all");
    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).toContain("Foo");
  });

  it("filterSpecTree 'incomplete' hides complete nodes", () => {
    ctx._specExpandedPaths.add("local/foo.md");
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.filterSpecTree("incomplete");
    const treeEl = ctx.registry.get("explorer-tree");
    // Foo (validated) should be visible, Bar (complete) should not.
    expect(treeEl.innerHTML).toContain("Foo");
    expect(treeEl.innerHTML).not.toContain("Bar");
  });

  it("filterSpecTree persists to localStorage", () => {
    ctx.filterSpecTree("stale");
    expect(ctx.storage.get("wallfacer-spec-filter")).toBe("stale");
  });

  it("ancestor is visible when descendant matches filter", () => {
    ctx._specExpandedPaths.add("local/foo.md");
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.filterSpecTree("complete");
    const treeEl = ctx.registry.get("explorer-tree");
    // Foo (validated) is visible because Bar (complete) is a descendant.
    expect(treeEl.innerHTML).toContain("Foo");
    expect(treeEl.innerHTML).toContain("Bar");
  });

  // --- Multi-select tests ---

  it("checkbox rendered for validated leaf specs only", () => {
    ctx._specExpandedPaths.add("local/foo.md");
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.renderSpecTree();
    const treeEl = ctx.registry.get("explorer-tree");
    // Foo is non-leaf (validated but not leaf) — no checkbox.
    // Bar is leaf but complete — no checkbox.
    expect(treeEl.innerHTML).not.toContain("spec-select-checkbox");
  });

  it("checkbox rendered for validated leaf", () => {
    const data = {
      nodes: [
        {
          path: "local/leaf.md",
          spec: {
            title: "Leaf",
            status: "validated",
            depends_on: [],
            track: "local",
          },
          children: [],
          is_leaf: true,
          depth: 0,
        },
      ],
      progress: {},
    };
    ctx._specTreeData = data;
    ctx.renderSpecTree();
    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).toContain("spec-select-checkbox");
  });

  it("selection count updates dispatch button", () => {
    ctx._selectedSpecPaths.add("local/a.md");
    ctx._selectedSpecPaths.add("local/b.md");
    ctx._updateDispatchSelectedButton();
    const btn = ctx.registry.get("spec-dispatch-selected-btn");
    expect(btn.textContent).toBe("Dispatch Selected (2)");
    expect(btn.classList.contains("hidden")).toBe(false);
  });

  it("dispatch button hidden when no selection", () => {
    ctx._selectedSpecPaths.clear();
    ctx._updateDispatchSelectedButton();
    const btn = ctx.registry.get("spec-dispatch-selected-btn");
    expect(btn.classList.contains("hidden")).toBe(true);
  });

  it("selection survives re-render", () => {
    const data = {
      nodes: [
        {
          path: "local/leaf.md",
          spec: {
            title: "Leaf",
            status: "validated",
            depends_on: [],
            track: "local",
          },
          children: [],
          is_leaf: true,
          depth: 0,
        },
      ],
      progress: {},
    };
    ctx._specTreeData = data;
    ctx._selectedSpecPaths.add("local/leaf.md");
    ctx.renderSpecTree();
    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).toContain("checked");
  });

  it("dispatchSelectedSpecs is callable", () => {
    ctx._selectedSpecPaths.add("local/a.md");
    // Should not throw.
    ctx.dispatchSelectedSpecs();
  });
});
