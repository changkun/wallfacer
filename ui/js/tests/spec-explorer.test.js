/**
 * Unit tests for spec-explorer.js — spec tree rendering and explorer mode switching.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
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
    Routes: {
      specs: {
        tree: () => "/api/specs/tree",
        dispatch: () => "/api/specs/dispatch",
      },
    },
    withBearerHeaders: () => ({}),
    withAuthHeaders: (h) => h || {},
    api: () => Promise.resolve({ dispatched: [], errors: [] }),
    confirm: () => true,
    alert: () => {},
    showConfirm: () => Promise.resolve(true),
    showAlert: () => {},
    Promise,
    escapeHtml: (s) => String(s).replace(/</g, "&lt;").replace(/>/g, "&gt;"),
    focusSpec: opts.focusSpec || (() => {}),
    focusRoadmapIndex: opts.focusRoadmapIndex || (() => {}),
    isRoadmapFocused: opts.isRoadmapFocused || (() => false),
    getFocusedSpecPath: () => null,
    activeWorkspaces: ["/workspace/repo"],
    _loadExplorerRoots: opts._loadExplorerRoots || (() => {}),
    _startExplorerRefreshPoll: opts._startExplorerRefreshPoll || (() => {}),
    _stopExplorerRefreshPoll: () => {},
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

  it("checkbox rendered for any validated spec", () => {
    ctx._specExpandedPaths.add("local/foo.md");
    ctx._specTreeData = MOCK_TREE_DATA;
    ctx.renderSpecTree();
    const treeEl = ctx.registry.get("explorer-tree");
    // Foo is validated (non-leaf) — should have checkbox.
    // Bar is complete — no checkbox.
    expect(treeEl.innerHTML).toContain("spec-select-checkbox");
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

  // --- Archived spec visibility ---

  const MOCK_TREE_WITH_ARCHIVED = {
    nodes: [
      {
        path: "local/live.md",
        spec: {
          title: "Live",
          status: "validated",
          depends_on: [],
          track: "local",
        },
        children: [],
        is_leaf: true,
        depth: 0,
      },
      {
        path: "local/arch.md",
        spec: {
          title: "Arch",
          status: "archived",
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

  it("archived status icon is mapped", () => {
    expect(ctx._specStatusIcons["archived"]).toBe("\uD83D\uDCE6");
  });

  it("archived specs hidden by default", () => {
    ctx._specTreeData = MOCK_TREE_WITH_ARCHIVED;
    ctx.renderSpecTree();
    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).toContain("Live");
    expect(treeEl.innerHTML).not.toContain("Arch");
  });

  it("toggleShowArchived on reveals archived specs with archived class", () => {
    ctx._specTreeData = MOCK_TREE_WITH_ARCHIVED;
    ctx.toggleShowArchived(true);
    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).toContain("Arch");
    expect(treeEl.innerHTML).toContain("spec-node--archived");
  });

  it("toggleShowArchived persists the preference to localStorage", () => {
    ctx.toggleShowArchived(true);
    expect(ctx.storage.get("wallfacer-spec-show-archived")).toBe("true");
    ctx.toggleShowArchived(false);
    expect(ctx.storage.get("wallfacer-spec-show-archived")).toBe("false");
  });

  it("incomplete filter excludes archived specs", () => {
    ctx._specTreeData = MOCK_TREE_WITH_ARCHIVED;
    ctx.toggleShowArchived(true);
    ctx.filterSpecTree("incomplete");
    const treeEl = ctx.registry.get("explorer-tree");
    // Live is validated → incomplete; Arch is archived → excluded.
    expect(treeEl.innerHTML).toContain("Live");
    expect(treeEl.innerHTML).not.toContain("Arch");
  });

  it("turning archived off while filtered on archived resets filter", () => {
    ctx._specTreeData = MOCK_TREE_WITH_ARCHIVED;
    ctx.toggleShowArchived(true);
    ctx.filterSpecTree("archived");
    ctx.toggleShowArchived(false);
    expect(ctx._specStatusFilter).toBe("all");
  });

  it("_forceCollapseArchived removes archived paths from expanded set", () => {
    ctx._specTreeData = {
      nodes: [
        {
          path: "local/arch.md",
          spec: { title: "Arch", status: "archived", track: "local" },
          children: ["local/arch/child.md"],
          is_leaf: false,
          depth: 0,
        },
      ],
      progress: {},
    };
    ctx._specExpandedPaths.add("local/arch.md");
    ctx._forceCollapseArchived();
    expect(ctx._specExpandedPaths.has("local/arch.md")).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Pinned Roadmap entry (explorer-roadmap-entry spec).
// ---------------------------------------------------------------------------

const INDEX_META = {
  path: "specs/README.md",
  workspace: "/workspace/repo",
  title: "Custom Repo Roadmap",
  modified: "2026-04-13T10:00:00Z",
};

describe("spec-explorer pinned Roadmap", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
    ctx._specExpandedPaths.add("__track__local");
  });

  it("TestExplorer_PinnedRoadmap_RendersWhenIndexPresent — pinned row at top of DOM", () => {
    ctx._specTreeData = { ...MOCK_TREE_DATA, index: INDEX_META };
    ctx.renderSpecTree();
    const treeEl = ctx.registry.get("explorer-tree");
    // The pinned entry appears before the track headers — a stable
    // visual anchor at the top of the explorer.
    const html = treeEl.innerHTML;
    const pinnedIdx = html.indexOf("spec-explorer-pinned");
    const trackIdx = html.indexOf("spec-track-header");
    expect(pinnedIdx).toBeGreaterThanOrEqual(0);
    expect(trackIdx).toBeGreaterThanOrEqual(0);
    expect(pinnedIdx).toBeLessThan(trackIdx);
    expect(html).toContain("\uD83D\uDCCB Roadmap");
    expect(html).toContain('data-entry="index"');
    // Always renders the literal "Roadmap" label regardless of the
    // backend-provided title — the spec forbids localising this.
    expect(treeEl.innerHTML).not.toContain(INDEX_META.title);
  });

  it("TestExplorer_PinnedRoadmap_HiddenWhenIndexNull — no pinned node", () => {
    ctx._specTreeData = { ...MOCK_TREE_DATA, index: null };
    ctx.renderSpecTree();
    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).not.toContain("spec-explorer-pinned");
    expect(treeEl.innerHTML).not.toContain("\uD83D\uDCCB Roadmap");
  });

  it("TestExplorer_PinnedRoadmap_ClickFocusesIndex — _onSpecIndexClick calls focusRoadmapIndex", () => {
    const focusRoadmapIndex = vi.fn();
    ctx = makeContext({ focusRoadmapIndex });
    // Inject focusRoadmapIndex as a global visible to spec-explorer's
    // runtime lookup (we created it via makeContext's overrides).
    Object.assign(ctx, { focusRoadmapIndex });
    ctx._specTreeData = { ...MOCK_TREE_DATA, index: INDEX_META };
    ctx.renderSpecTree();
    ctx._onSpecIndexClick();
    expect(focusRoadmapIndex).toHaveBeenCalledWith(INDEX_META);
  });

  it("_onSpecIndexClick is a no-op when no index is present", () => {
    const focusRoadmapIndex = vi.fn();
    ctx = makeContext({ focusRoadmapIndex });
    Object.assign(ctx, { focusRoadmapIndex });
    ctx._specTreeData = { ...MOCK_TREE_DATA, index: null };
    ctx._onSpecIndexClick();
    expect(focusRoadmapIndex).not.toHaveBeenCalled();
  });

  it("Enter on the pinned entry triggers focus (keyboard affordance)", () => {
    const focusRoadmapIndex = vi.fn();
    ctx = makeContext({ focusRoadmapIndex });
    Object.assign(ctx, { focusRoadmapIndex });
    ctx._specTreeData = { ...MOCK_TREE_DATA, index: INDEX_META };
    ctx._onSpecIndexKeydown({
      key: "Enter",
      preventDefault: () => {},
    });
    expect(focusRoadmapIndex).toHaveBeenCalledWith(INDEX_META);
  });

  it("pinned row gets the focused class when the index is focused", () => {
    ctx = makeContext({ isRoadmapFocused: () => true });
    ctx._specTreeData = { ...MOCK_TREE_DATA, index: INDEX_META };
    ctx.renderSpecTree();
    const treeEl = ctx.registry.get("explorer-tree");
    expect(treeEl.innerHTML).toContain("spec-explorer-pinned--focused");
  });
});
