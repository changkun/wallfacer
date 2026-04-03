/**
 * Additional coverage tests for spec-explorer.js — functions not covered
 * by the existing spec-explorer.test.js.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-explorer.js"), "utf8");

function makeEl(tag, registry) {
  const _classList = new Set();
  const _attrs = {};
  const _listeners = {};
  let _textContent = "";

  const el = {
    tagName: tag,
    get id() {
      return _attrs.id || "";
    },
    set id(v) {
      _attrs.id = v;
      if (v && registry) registry.set(v, el);
    },
    get textContent() {
      return _textContent;
    },
    set textContent(v) {
      _textContent = v;
    },
    value: "",
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
    setAttribute(name, val) {
      _attrs[name] = val;
    },
    getAttribute(name) {
      return _attrs[name] ?? null;
    },
    addEventListener(type, fn) {
      if (!_listeners[type]) _listeners[type] = [];
      _listeners[type].push(fn);
    },
    removeEventListener(type, fn) {
      if (_listeners[type]) {
        _listeners[type] = _listeners[type].filter((f) => f !== fn);
      }
    },
    querySelectorAll() {
      return [];
    },
    remove() {
      el._removed = true;
    },
    close() {
      el._closed = true;
    },
    _classList,
    _listeners,
    _attrs,
    _removed: false,
    _closed: false,
  };
  return el;
}

function makeContext(opts = {}) {
  const registry = new Map();
  const storage = new Map();

  const ids = [
    "explorer-tree",
    "spec-status-filter",
    "spec-dispatch-bar",
    "spec-dispatch-selected-btn",
  ];
  for (const id of ids) {
    const el = makeEl("DIV", registry);
    el.id = id;
  }

  const ctx = {
    window: null,
    document: {
      getElementById(id) {
        return registry.get(id) || null;
      },
      createElement(tag) {
        return makeEl(tag, registry);
      },
      querySelectorAll(sel) {
        return [];
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
    EventSource:
      opts.EventSource ||
      function () {
        this.addEventListener = vi.fn();
        this.removeEventListener = vi.fn();
        this.close = vi.fn();
        this.onerror = null;
        this.readyState = 1;
      },
    Routes: {
      specs: {
        tree: () => "/api/specs/tree",
        stream: () => "/api/specs/stream",
      },
    },
    withBearerHeaders: () => ({}),
    withAuthToken: (url) => url,
    escapeHtml: (s) => String(s).replace(/</g, "&lt;").replace(/>/g, "&gt;"),
    focusSpec: opts.focusSpec || (() => {}),
    getFocusedSpecPath: opts.getFocusedSpecPath || (() => null),
    activeWorkspaces: ["/workspace/repo"],
    _loadExplorerRoots: opts._loadExplorerRoots || (() => {}),
    _startExplorerRefreshPoll: opts._startExplorerRefreshPoll || (() => {}),
    _stopExplorerRefreshPoll: opts._stopExplorerRefreshPoll || (() => {}),
    _hideMinimap: opts._hideMinimap || (() => {}),
    _updateSpecPaneVisibility: opts._updateSpecPaneVisibility || (() => {}),
    renderMinimap: opts.renderMinimap || (() => {}),
    setInterval: (fn, ms) => {
      ctx._lastIntervalFn = fn;
      return 99;
    },
    clearInterval: vi.fn(),
    setTimeout: (fn, ms) => {
      ctx._lastTimeoutFn = fn;
      return 88;
    },
    clearTimeout: vi.fn(),
    JSON,
    Array,
    Set,
    Math,
    console,
    parseInt,
    String,
    registry,
    storage,
  };
  ctx.window = ctx;
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

const TREE_DATA = {
  nodes: [
    {
      path: "specs/local/alpha.md",
      spec: {
        title: "Alpha Feature",
        status: "validated",
        depends_on: [],
        affects: [],
        track: "local",
      },
      children: ["specs/local/alpha/child.md"],
      is_leaf: false,
      depth: 0,
    },
    {
      path: "specs/local/alpha/child.md",
      spec: {
        title: "Child Task",
        status: "drafted",
        depends_on: [],
        affects: [],
        track: "local",
      },
      children: [],
      is_leaf: true,
      depth: 1,
    },
    {
      path: "specs/cloud/beta.md",
      spec: {
        title: "Beta Cloud",
        status: "complete",
        depends_on: [],
        affects: [],
        track: "cloud",
      },
      children: [],
      is_leaf: true,
      depth: 0,
    },
  ],
  progress: {
    "specs/local/alpha.md": { Complete: 0, Total: 1 },
  },
};

describe("spec-explorer coverage", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
    ctx._specExpandedPaths.add("__track__local");
    ctx._specExpandedPaths.add("__track__cloud");
  });

  // --- setSpecTextFilter ---

  describe("setSpecTextFilter", () => {
    it("filters tree nodes by title text", () => {
      ctx._specTreeData = TREE_DATA;
      ctx._explorerRootMode = "specs";
      ctx._specExpandedPaths.add("specs/local/alpha.md");

      ctx.setSpecTextFilter("Child");
      const treeEl = ctx.registry.get("explorer-tree");
      expect(treeEl.innerHTML).toContain("Child Task");
      // Alpha should be visible as ancestor of matching child.
      expect(treeEl.innerHTML).toContain("Alpha Feature");
      // Beta (no match) should not appear.
      expect(treeEl.innerHTML).not.toContain("Beta Cloud");
    });

    it("filters by path fragment", () => {
      ctx._specTreeData = TREE_DATA;
      ctx._explorerRootMode = "specs";

      ctx.setSpecTextFilter("cloud/beta");
      const treeEl = ctx.registry.get("explorer-tree");
      expect(treeEl.innerHTML).toContain("Beta Cloud");
      expect(treeEl.innerHTML).not.toContain("Alpha Feature");
    });

    it("clears filter when empty string", () => {
      ctx._specTreeData = TREE_DATA;
      ctx._explorerRootMode = "specs";

      ctx.setSpecTextFilter("nonexistent");
      let treeEl = ctx.registry.get("explorer-tree");
      expect(treeEl.innerHTML).not.toContain("Alpha");

      ctx.setSpecTextFilter("");
      treeEl = ctx.registry.get("explorer-tree");
      expect(treeEl.innerHTML).toContain("Alpha");
    });

    it("does not re-render when not in spec mode", () => {
      ctx._specTreeData = TREE_DATA;
      ctx._explorerRootMode = "workspace";
      const treeEl = ctx.registry.get("explorer-tree");
      treeEl.innerHTML = "workspace-content";

      ctx.setSpecTextFilter("alpha");
      // Should not overwrite workspace content.
      expect(treeEl.innerHTML).toBe("workspace-content");
    });
  });

  // --- _nodeMatchesFilter direct tests ---

  describe("_nodeMatchesFilter", () => {
    it("returns false for node without spec", () => {
      const node = { path: "no-spec.md", is_leaf: true };
      const result = ctx._nodeMatchesFilter(node, {});
      expect(result).toBe(false);
    });

    it("leaf node matches when status and text match", () => {
      ctx._specStatusFilter = "drafted";
      ctx._specTextFilter = "child";
      const nodesByPath = {};
      for (const n of TREE_DATA.nodes) nodesByPath[n.path] = n;

      const childNode = nodesByPath["specs/local/alpha/child.md"];
      expect(ctx._nodeMatchesFilter(childNode, nodesByPath)).toBe(true);
    });

    it("leaf node does not match when status mismatches", () => {
      ctx._specStatusFilter = "complete";
      ctx._specTextFilter = "";
      const nodesByPath = {};
      for (const n of TREE_DATA.nodes) nodesByPath[n.path] = n;

      const childNode = nodesByPath["specs/local/alpha/child.md"];
      expect(ctx._nodeMatchesFilter(childNode, nodesByPath)).toBe(false);
    });

    it("non-leaf matches when descendant matches", () => {
      ctx._specStatusFilter = "drafted";
      ctx._specTextFilter = "";
      const nodesByPath = {};
      for (const n of TREE_DATA.nodes) nodesByPath[n.path] = n;

      const alphaNode = nodesByPath["specs/local/alpha.md"];
      // Alpha is validated, not drafted — but child is drafted.
      expect(ctx._nodeMatchesFilter(alphaNode, nodesByPath)).toBe(true);
    });
  });

  // --- loadSpecTree ---

  describe("loadSpecTree", () => {
    it("renders tree data on successful fetch", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        json: () => Promise.resolve(TREE_DATA),
      });
      const ctx2 = makeContext({ fetch: fetchMock });
      ctx2._specExpandedPaths.add("__track__local");
      ctx2._specExpandedPaths.add("__track__cloud");

      ctx2.loadSpecTree();
      // Wait for promise chain.
      await new Promise((r) => setTimeout(r, 10));

      expect(fetchMock).toHaveBeenCalled();
      const treeEl = ctx2.registry.get("explorer-tree");
      expect(treeEl.innerHTML).toContain("Alpha Feature");
    });

    it("shows loading indicator before data arrives", () => {
      const ctx2 = makeContext({
        fetch: () => new Promise(() => {}), // never resolves
      });
      ctx2._specTreeData = null;
      const treeEl = ctx2.registry.get("explorer-tree");
      treeEl.innerHTML = "";

      ctx2.loadSpecTree();
      expect(treeEl.innerHTML).toContain("Loading specs");
    });

    it("shows error message on fetch failure", async () => {
      const ctx2 = makeContext({
        fetch: () => Promise.reject(new Error("network error")),
      });
      ctx2.loadSpecTree();
      await new Promise((r) => setTimeout(r, 10));

      const treeEl = ctx2.registry.get("explorer-tree");
      expect(treeEl.innerHTML).toContain("Failed to load specs");
    });

    it("calls _updateSpecPaneVisibility with true when specs exist", async () => {
      const visibilitySpy = vi.fn();
      const ctx2 = makeContext({
        fetch: vi.fn().mockResolvedValue({
          json: () => Promise.resolve(TREE_DATA),
        }),
        _updateSpecPaneVisibility: visibilitySpy,
      });

      ctx2.loadSpecTree();
      await new Promise((r) => setTimeout(r, 10));

      expect(visibilitySpy).toHaveBeenCalledWith(true);
    });

    it("calls _updateSpecPaneVisibility with false on error", async () => {
      const visibilitySpy = vi.fn();
      const ctx2 = makeContext({
        fetch: () => Promise.reject(new Error("fail")),
        _updateSpecPaneVisibility: visibilitySpy,
      });

      ctx2.loadSpecTree();
      await new Promise((r) => setTimeout(r, 10));

      expect(visibilitySpy).toHaveBeenCalledWith(false);
    });
  });

  // --- renderSpecTree: track grouping ---

  describe("renderSpecTree track grouping", () => {
    it("groups nodes by track and renders track headers", () => {
      ctx._specTreeData = TREE_DATA;
      ctx.renderSpecTree();

      const treeEl = ctx.registry.get("explorer-tree");
      expect(treeEl.innerHTML).toContain("local");
      expect(treeEl.innerHTML).toContain("cloud");
    });

    it("collapsed track hides its nodes", () => {
      ctx._specExpandedPaths.delete("__track__cloud");
      ctx._specTreeData = TREE_DATA;
      ctx.renderSpecTree();

      const treeEl = ctx.registry.get("explorer-tree");
      expect(treeEl.innerHTML).not.toContain("Beta Cloud");
      // local track is expanded.
      expect(treeEl.innerHTML).toContain("Alpha Feature");
    });
  });

  // --- renderSpecTree: focused spec highlight ---

  describe("renderSpecTree focused spec", () => {
    it("adds focused class to the focused spec node", () => {
      const ctx2 = makeContext({
        getFocusedSpecPath: () => "specs/local/alpha.md",
      });
      ctx2._specExpandedPaths.add("__track__local");
      ctx2._specTreeData = TREE_DATA;
      ctx2.renderSpecTree();

      const treeEl = ctx2.registry.get("explorer-tree");
      expect(treeEl.innerHTML).toContain("spec-node--focused");
    });
  });

  // --- switchExplorerRoot ---

  describe("switchExplorerRoot", () => {
    it("no-op when switching to same mode", () => {
      const loadRoots = vi.fn();
      const ctx2 = makeContext({ _loadExplorerRoots: loadRoots });
      // Default mode is "workspace".
      ctx2.switchExplorerRoot("workspace");
      expect(loadRoots).not.toHaveBeenCalled();
    });

    it("hides minimap when switching away from specs", () => {
      const hideMinimap = vi.fn();
      const ctx2 = makeContext({ _hideMinimap: hideMinimap });
      ctx2.switchExplorerRoot("specs");
      ctx2.switchExplorerRoot("workspace");
      expect(hideMinimap).toHaveBeenCalled();
    });

    it("shows spec-status-filter when switching to specs", () => {
      ctx.switchExplorerRoot("specs");
      const filterEl = ctx.registry.get("spec-status-filter");
      expect(filterEl.classList.contains("hidden")).toBe(false);
    });

    it("hides spec-status-filter when switching to workspace", () => {
      ctx.switchExplorerRoot("specs");
      ctx.switchExplorerRoot("workspace");
      const filterEl = ctx.registry.get("spec-status-filter");
      expect(filterEl.classList.contains("hidden")).toBe(true);
    });

    it("shows dispatch bar when switching to specs", () => {
      ctx.switchExplorerRoot("specs");
      const bar = ctx.registry.get("spec-dispatch-bar");
      expect(bar.classList.contains("hidden")).toBe(false);
    });

    it("stops workspace explorer poll when switching to specs", () => {
      const stopPoll = vi.fn();
      const ctx2 = makeContext({ _stopExplorerRefreshPoll: stopPoll });
      ctx2.switchExplorerRoot("specs");
      expect(stopPoll).toHaveBeenCalled();
    });

    it("starts workspace explorer poll when switching to workspace", () => {
      const startPoll = vi.fn();
      const ctx2 = makeContext({ _startExplorerRefreshPoll: startPoll });
      ctx2.switchExplorerRoot("specs");
      ctx2.switchExplorerRoot("workspace");
      expect(startPoll).toHaveBeenCalled();
    });
  });

  // --- _stopSpecTreePoll ---

  describe("_stopSpecTreePoll", () => {
    it("clears interval timer", () => {
      ctx._specTreeTimer = 42;
      ctx._stopSpecTreePoll();
      expect(ctx.clearInterval).toHaveBeenCalled();
      expect(ctx._specTreeTimer).toBe(null);
    });
  });

  // --- renderSpecTree with empty data ---

  describe("renderSpecTree edge cases", () => {
    it("handles null _specTreeData gracefully", () => {
      ctx._specTreeData = null;
      ctx.renderSpecTree();
      // Should not throw.
      const treeEl = ctx.registry.get("explorer-tree");
      expect(treeEl.innerHTML).toBe("");
    });

    it("handles empty nodes array", () => {
      ctx._specTreeData = { nodes: [], progress: {} };
      ctx.renderSpecTree();
      const treeEl = ctx.registry.get("explorer-tree");
      // No tracks, no nodes — just empty.
      expect(treeEl.innerHTML).toBe("");
    });

    it("renders node with missing title using path", () => {
      const data = {
        nodes: [
          {
            path: "specs/local/notitle.md",
            spec: {
              title: "",
              status: "vague",
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
      expect(treeEl.innerHTML).toContain("specs/local/notitle.md");
    });
  });

  // --- Multi-select dispatch button ---

  describe("_updateDispatchSelectedButton", () => {
    it("shows count when selections exist", () => {
      ctx._selectedSpecPaths.add("a.md");
      ctx._selectedSpecPaths.add("b.md");
      ctx._selectedSpecPaths.add("c.md");
      ctx._updateDispatchSelectedButton();

      const btn = ctx.registry.get("spec-dispatch-selected-btn");
      expect(btn.textContent).toBe("Dispatch Selected (3)");
      expect(btn.classList.contains("hidden")).toBe(false);
    });

    it("hides button when selections cleared", () => {
      ctx._selectedSpecPaths.add("a.md");
      ctx._updateDispatchSelectedButton();
      ctx._selectedSpecPaths.clear();
      ctx._updateDispatchSelectedButton();

      const btn = ctx.registry.get("spec-dispatch-selected-btn");
      expect(btn.classList.contains("hidden")).toBe(true);
    });
  });

  // --- Text filter auto-expands nodes ---

  describe("text filter auto-expansion", () => {
    it("text filter forces nodes visible even in collapsed tracks", () => {
      ctx._specExpandedPaths.delete("__track__cloud");
      ctx._specTreeData = TREE_DATA;
      ctx._explorerRootMode = "specs";

      ctx.setSpecTextFilter("Beta");
      const treeEl = ctx.registry.get("explorer-tree");
      // Text filter should force expansion of track headers.
      expect(treeEl.innerHTML).toContain("Beta Cloud");
    });
  });
});
