/**
 * Comprehensive coverage tests for spec-mode.js.
 * Focuses on functions NOT covered by spec-mode.test.js:
 * parseSpecFrontmatter, focusSpec, toggleSpecChat, toggleSidebar,
 * expandSidebar, _updateSpecPaneVisibility, _restoreSpecChatState,
 * _restoreSidebarState, breakDownFocusedSpec, dispatchFocusedSpec,
 * _applyMode edge cases, _loadAndRenderSpec, _showSpecReadme, etc.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-mode.js"), "utf8");

function makeEl(tag) {
  const _classList = new Set();
  const _style = {};
  let _id = "";
  let _textContent = "";
  let _innerHTML = "";
  let _className = "";
  const _children = [];
  let _onclick = null;

  const el = {
    tagName: tag ? tag.toUpperCase() : "DIV",
    get id() {
      return _id;
    },
    set id(v) {
      _id = v;
    },
    style: _style,
    get textContent() {
      return _textContent;
    },
    set textContent(v) {
      _textContent = v;
    },
    get innerHTML() {
      return _innerHTML;
    },
    set innerHTML(v) {
      _innerHTML = v;
      // Parse simple tags for firstElementChild simulation.
      _children.length = 0;
      const m = v.match(/^<(\w+)/);
      if (m) {
        const child = makeEl(m[1]);
        child._parent = el;
        _children.push(child);
      }
    },
    get firstElementChild() {
      return _children[0] || null;
    },
    get className() {
      return _className;
    },
    set className(v) {
      _className = v;
      _classList.clear();
      v.split(/\s+/)
        .filter(Boolean)
        .forEach((c) => { _classList.add(c); });
    },
    addEventListener: vi.fn(),
    get onclick() {
      return _onclick;
    },
    set onclick(fn) {
      _onclick = fn;
    },
    get offsetWidth() {
      return parseInt(_style.width, 10) || 300;
    },
    classList: {
      add(c) {
        _classList.add(c);
        _className = [..._classList].join(" ");
      },
      remove(c) {
        _classList.delete(c);
        _className = [..._classList].join(" ");
      },
      toggle(c, force) {
        if (force === undefined) {
          if (_classList.has(c)) {
            _classList.delete(c);
            _className = [..._classList].join(" ");
            return false;
          }
          _classList.add(c);
          _className = [..._classList].join(" ");
          return true;
        }
        if (force) _classList.add(c);
        else _classList.delete(c);
        _className = [..._classList].join(" ");
        return !!force;
      },
      contains(c) {
        return _classList.has(c);
      },
    },
    focus: vi.fn(),
    remove() {
      const idx = _children.indexOf(this);
      if (idx >= 0) _children.splice(idx, 1);
      // Remove from parent's children if tracked.
      if (this._parent) {
        const pChildren = this._parent._getChildren
          ? this._parent._getChildren()
          : [];
        const pi = pChildren.indexOf(this);
        if (pi >= 0) pChildren.splice(pi, 1);
      }
    },
    _getChildren() {
      return _children;
    },
    _pushChild(child) {
      child._parent = el;
      _children.push(child);
    },
  };
  return el;
}

function makeDom(extraElements) {
  const registry = new Map();

  function registerEl(id, tag) {
    const el = makeEl(tag || "DIV");
    el.id = id;
    registry.set(id, el);
    return el;
  }

  // Standard elements spec-mode.js expects.
  registerEl("sidebar-nav-board", "BUTTON");
  registerEl("sidebar-nav-spec", "BUTTON");
  registerEl("sidebar-nav-docs", "BUTTON");
  registerEl("board", "MAIN");
  registerEl("spec-mode-container", "DIV");
  registerEl("docs-mode-container", "DIV");
  registerEl("explorer-panel", "DIV");
  registerEl("task-search", "INPUT");
  registerEl("workspace-git-bar", "DIV");
  registerEl("app-sidebar", "DIV");
  registerEl("spec-focused-title", "SPAN");
  registerEl("spec-focused-status", "SPAN");
  registerEl("spec-focused-kind", "SPAN");
  registerEl("spec-focused-effort", "SPAN");
  registerEl("spec-focused-meta", "SPAN");
  registerEl("spec-focused-body-inner", "DIV");
  registerEl("spec-focused-body", "DIV");
  registerEl("spec-focused-view", "DIV");
  registerEl("spec-dispatch-btn", "BUTTON");
  registerEl("spec-summarize-btn", "BUTTON");
  registerEl("spec-chat-stream", "DIV");
  registerEl("spec-chat-resize", "DIV");
  registerEl("spec-chat-input", "INPUT");

  if (extraElements) {
    for (const [id, tag] of extraElements) {
      registerEl(id, tag);
    }
  }

  let _appHeader = makeEl("DIV");

  return {
    registry,
    getElementById(id) {
      return registry.get(id) || null;
    },
    querySelector(sel) {
      if (sel === ".app-header") return _appHeader;
      return null;
    },
    querySelectorAll() {
      return [];
    },
    addEventListener: vi.fn(),
    createElement(tag) {
      return makeEl(tag);
    },
    _appHeader,
  };
}

function makeContext(overrides = {}) {
  const dom = overrides.dom || makeDom(overrides.extraElements);
  const storage = overrides.storage || new Map();

  const ctx = {
    document: dom,
    localStorage: {
      getItem(k) {
        return storage.get(k) ?? null;
      },
      setItem(k, v) {
        storage.set(k, v);
      },
    },
    fetch: overrides.fetch || vi.fn(() => Promise.reject(new Error("stubbed"))),
    Routes: overrides.Routes || {
      explorer: { readFile: () => "/api/explorer/file" },
    },
    withBearerHeaders: overrides.withBearerHeaders || (() => ({})),
    renderMarkdown:
      overrides.renderMarkdown || ((text) => "<p>" + text + "</p>"),
    setInterval: overrides.setInterval || vi.fn(() => 42),
    clearInterval: overrides.clearInterval || vi.fn(),
    location: overrides.location || { hash: "", pathname: "/" },
    history: overrides.history || { replaceState: vi.fn() },
    console,
    window: overrides.window || { innerWidth: 1200, dispatchEvent: vi.fn() },
    Event:
      overrides.Event ||
      function (type) {
        this.type = type;
      },
    storage,
    // Stubs for optional globals that spec-mode.js checks with typeof.
    _mdRender: overrides._mdRender || {
      enhanceMarkdown: vi.fn(() => Promise.resolve()),
    },
    buildFloatingToc: overrides.buildFloatingToc || vi.fn(),
    teardownFloatingToc: overrides.teardownFloatingToc || vi.fn(),
    renderMinimap: overrides.renderMinimap || vi.fn(),
    activeWorkspaces: overrides.activeWorkspaces || ["/home/user/project"],
    _specTreeData: overrides._specTreeData || null,
    switchExplorerRoot: overrides.switchExplorerRoot || vi.fn(),
    setSpecTextFilter: overrides.setSpecTextFilter || vi.fn(),
    _ensureDocsLoaded: overrides._ensureDocsLoaded || vi.fn(),
    PlanningChat: overrides.PlanningChat || {
      init: vi.fn(),
      sendMessage: vi.fn(),
    },
    parseInt,
    Math,
    JSON,
    encodeURIComponent,
    Error,
    showConfirm: overrides.showConfirm || (() => Promise.resolve(true)),
    showAlert: overrides.showAlert || (() => {}),
    Promise,
  };

  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

// ---- Tests ----

describe("parseSpecFrontmatter", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
  });

  it("returns empty frontmatter and empty body for null/empty input", () => {
    expect(ctx.parseSpecFrontmatter(null)).toEqual({
      frontmatter: {},
      body: "",
    });
    expect(ctx.parseSpecFrontmatter("")).toEqual({
      frontmatter: {},
      body: "",
    });
  });

  it("returns raw text as body when no frontmatter delimiters", () => {
    const result = ctx.parseSpecFrontmatter("Just some markdown text.");
    expect(result.frontmatter).toEqual({});
    expect(result.body).toBe("Just some markdown text.");
  });

  it("parses simple key-value frontmatter", () => {
    const text =
      "---\ntitle: My Spec\nstatus: drafted\nauthor: alice\n---\n# Hello\nBody here.";
    const result = ctx.parseSpecFrontmatter(text);
    expect(result.frontmatter.title).toBe("My Spec");
    expect(result.frontmatter.status).toBe("drafted");
    expect(result.frontmatter.author).toBe("alice");
    expect(result.body).toBe("# Hello\nBody here.");
  });

  it("skips lines without colons", () => {
    const text =
      "---\ntitle: Test\nno-colon-here\nstatus: validated\n---\nBody";
    const result = ctx.parseSpecFrontmatter(text);
    expect(result.frontmatter.title).toBe("Test");
    expect(result.frontmatter.status).toBe("validated");
    expect(result.body).toBe("Body");
  });

  it("skips YAML list values (starting with -)", () => {
    const text =
      "---\ntitle: Test\ndepends_on:\n  - specs/a.md\n  - specs/b.md\nstatus: complete\n---\nBody";
    const result = ctx.parseSpecFrontmatter(text);
    expect(result.frontmatter.title).toBe("Test");
    expect(result.frontmatter.status).toBe("complete");
    // depends_on has no simple value, its line value starts with nothing useful
    // and list items start with -, so they're skipped.
  });

  it("skips YAML block scalar indicators (| and >)", () => {
    const text =
      "---\ntitle: Test\ndescription: |\nstatus: drafted\n---\nBody text";
    const result = ctx.parseSpecFrontmatter(text);
    expect(result.frontmatter.title).toBe("Test");
    expect(result.frontmatter.status).toBe("drafted");
    expect(result.frontmatter.description).toBeUndefined();
  });

  it("handles frontmatter with dates and effort", () => {
    const text =
      "---\ntitle: Spec\ncreated: 2026-01-01\nupdated: 2026-02-15\neffort: large\n---\nContent";
    const result = ctx.parseSpecFrontmatter(text);
    expect(result.frontmatter.created).toBe("2026-01-01");
    expect(result.frontmatter.updated).toBe("2026-02-15");
    expect(result.frontmatter.effort).toBe("large");
  });

  it("handles values with colons in them", () => {
    const text = "---\ntitle: My Spec: Extended Edition\n---\nBody";
    const result = ctx.parseSpecFrontmatter(text);
    expect(result.frontmatter.title).toBe("My Spec: Extended Edition");
  });
});

describe("toggleSidebar", () => {
  it("toggles collapsed class on sidebar", () => {
    const ctx = makeContext();
    const sidebar = ctx.document.getElementById("app-sidebar");
    expect(sidebar.classList.contains("collapsed")).toBe(false);

    ctx.toggleSidebar();
    expect(sidebar.classList.contains("collapsed")).toBe(true);
    expect(ctx.storage.get("wallfacer-sidebar-collapsed")).toBe("1");

    ctx.toggleSidebar();
    expect(sidebar.classList.contains("collapsed")).toBe(false);
    expect(ctx.storage.get("wallfacer-sidebar-collapsed")).toBe("");
  });

  it("does nothing if sidebar element is missing", () => {
    const dom = makeDom();
    dom.registry.delete("app-sidebar");
    const ctx = makeContext({ dom });
    // Should not throw.
    ctx.toggleSidebar();
  });
});

describe("expandSidebar", () => {
  it("expands collapsed sidebar", () => {
    const ctx = makeContext();
    const sidebar = ctx.document.getElementById("app-sidebar");
    sidebar.classList.add("collapsed");

    ctx.expandSidebar();
    expect(sidebar.classList.contains("collapsed")).toBe(false);
  });

  it("does nothing if sidebar is already expanded", () => {
    const ctx = makeContext();
    const sidebar = ctx.document.getElementById("app-sidebar");
    expect(sidebar.classList.contains("collapsed")).toBe(false);

    // Should not toggle (which would collapse it).
    ctx.expandSidebar();
    expect(sidebar.classList.contains("collapsed")).toBe(false);
  });

  it("does nothing if sidebar element is missing", () => {
    const dom = makeDom();
    dom.registry.delete("app-sidebar");
    const ctx = makeContext({ dom });
    ctx.expandSidebar();
  });
});

describe("_restoreSidebarState", () => {
  it("restores collapsed state from localStorage", () => {
    const storage = new Map([["wallfacer-sidebar-collapsed", "1"]]);
    const ctx = makeContext({ storage });
    // Call _restoreSidebarState directly since DOMContentLoaded doesn't auto-fire.
    ctx._restoreSidebarState();
    const sidebar = ctx.document.getElementById("app-sidebar");
    expect(sidebar.classList.contains("collapsed")).toBe(true);
  });

  it("does not collapse when localStorage is empty", () => {
    const ctx = makeContext();
    ctx._restoreSidebarState();
    const sidebar = ctx.document.getElementById("app-sidebar");
    expect(sidebar.classList.contains("collapsed")).toBe(false);
  });
});

describe("toggleSpecChat", () => {
  it("hides visible chat pane and stores state", () => {
    const ctx = makeContext();
    const chatStream = ctx.document.getElementById("spec-chat-stream");
    const resizeHandle = ctx.document.getElementById("spec-chat-resize");
    chatStream.style.display = "";

    ctx.toggleSpecChat();
    expect(chatStream.style.display).toBe("none");
    expect(resizeHandle.style.display).toBe("none");
    expect(ctx.storage.get("wallfacer-spec-chat-open")).toBe("0");
  });

  it("shows hidden chat pane and focuses input", () => {
    const ctx = makeContext();
    const chatStream = ctx.document.getElementById("spec-chat-stream");
    const chatInput = ctx.document.getElementById("spec-chat-input");
    chatStream.style.display = "none";

    ctx.toggleSpecChat();
    expect(chatStream.style.display).toBe("");
    expect(ctx.storage.get("wallfacer-spec-chat-open")).toBe("1");
    expect(chatInput.focus).toHaveBeenCalled();
  });

  it("does nothing if chat stream element is missing", () => {
    const dom = makeDom();
    dom.registry.delete("spec-chat-stream");
    const ctx = makeContext({ dom });
    ctx.toggleSpecChat(); // Should not throw.
  });
});

describe("_restoreSpecChatState", () => {
  it("hides chat when saved state is 0", () => {
    const storage = new Map([["wallfacer-spec-chat-open", "0"]]);
    const ctx = makeContext({ storage });
    ctx._restoreSpecChatState();
    const chatStream = ctx.document.getElementById("spec-chat-stream");
    expect(chatStream.style.display).toBe("none");
  });

  it("leaves chat visible when no saved state", () => {
    const ctx = makeContext();
    ctx._restoreSpecChatState();
    const chatStream = ctx.document.getElementById("spec-chat-stream");
    // Default display should not be "none".
    expect(chatStream.style.display).not.toBe("none");
  });
});

describe("_applyMode edge cases", () => {
  it("updates search placeholder for spec mode", () => {
    const ctx = makeContext();
    ctx.switchMode("spec");
    const searchInput = ctx.document.getElementById("task-search");
    expect(searchInput.textContent || searchInput.placeholder).toContain(
      "spec",
    );
  });

  it("updates search placeholder for board mode", () => {
    const ctx = makeContext();
    ctx.switchMode("spec");
    ctx.switchMode("board");
    const searchInput = ctx.document.getElementById("task-search");
    expect(searchInput.placeholder).toContain("task");
  });

  it("calls switchExplorerRoot with specs for spec mode", () => {
    const switchExplorerRoot = vi.fn();
    const ctx = makeContext({ switchExplorerRoot });
    ctx.switchMode("spec");
    expect(switchExplorerRoot).toHaveBeenCalledWith("specs");
  });

  it("calls switchExplorerRoot with workspace for board mode", () => {
    const switchExplorerRoot = vi.fn();
    const ctx = makeContext({ switchExplorerRoot });
    ctx.switchMode("spec");
    switchExplorerRoot.mockClear();
    ctx.switchMode("board");
    expect(switchExplorerRoot).toHaveBeenCalledWith("workspace");
  });

  it("calls _ensureDocsLoaded when switching to docs mode", () => {
    const _ensureDocsLoaded = vi.fn();
    const ctx = makeContext({ _ensureDocsLoaded });
    ctx.switchMode("docs");
    expect(_ensureDocsLoaded).toHaveBeenCalled();
  });

  it("hides workspace bar and header in docs mode", () => {
    const ctx = makeContext();
    ctx.switchMode("docs");
    const gitBar = ctx.document.getElementById("workspace-git-bar");
    const header = ctx.document.querySelector(".app-header");
    expect(gitBar.style.display).toBe("none");
    expect(header.style.display).toBe("none");
  });

  it("shows workspace bar and header when leaving docs mode", () => {
    const ctx = makeContext();
    ctx.switchMode("docs");
    ctx.switchMode("board");
    const gitBar = ctx.document.getElementById("workspace-git-bar");
    const header = ctx.document.querySelector(".app-header");
    expect(gitBar.style.display).toBe("");
    expect(header.style.display).toBe("");
  });

  it("clears spec text filter when leaving spec mode", () => {
    const setSpecTextFilter = vi.fn();
    const ctx = makeContext({ setSpecTextFilter });
    ctx.switchMode("spec");
    setSpecTextFilter.mockClear();
    ctx.switchMode("board");
    expect(setSpecTextFilter).toHaveBeenCalledWith("");
  });

  it("clears spec hash when leaving spec mode with spec hash", () => {
    const replaceState = vi.fn();
    const ctx = makeContext({
      location: { hash: "#spec/local/foo.md", pathname: "/app" },
      history: { replaceState },
    });
    ctx.switchMode("spec");
    replaceState.mockClear();
    ctx.switchMode("board");
    expect(replaceState).toHaveBeenCalledWith(null, "", "/app");
  });

  it("does not clear hash when leaving spec mode with non-spec hash", () => {
    const replaceState = vi.fn();
    const ctx = makeContext({
      location: { hash: "#other", pathname: "/app" },
      history: { replaceState },
    });
    ctx.switchMode("spec");
    replaceState.mockClear();
    ctx.switchMode("board");
    // Should not call replaceState because hash doesn't start with #spec/.
    expect(replaceState).not.toHaveBeenCalled();
  });

  it("hides explorer panel in all modes", () => {
    const ctx = makeContext();
    const explorerPanel = ctx.document.getElementById("explorer-panel");

    ctx.switchMode("spec");
    expect(explorerPanel.style.display).toBe("none");

    ctx.switchMode("board");
    expect(explorerPanel.style.display).toBe("none");
  });
});

describe("_updateSpecPaneVisibility", () => {
  it("shows explorer panel when specs exist and mode is spec", () => {
    const ctx = makeContext();
    ctx.switchMode("spec");
    const explorerPanel = ctx.document.getElementById("explorer-panel");
    const container = ctx.document.getElementById("spec-mode-container");

    ctx._updateSpecPaneVisibility(true);
    expect(explorerPanel.style.display).toBe("");
    expect(container.classList.contains("spec-mode--chat-only")).toBe(false);
  });

  it("hides explorer and adds chat-only class when no specs", () => {
    const ctx = makeContext();
    ctx.switchMode("spec");
    const explorerPanel = ctx.document.getElementById("explorer-panel");
    const container = ctx.document.getElementById("spec-mode-container");

    ctx._updateSpecPaneVisibility(false);
    expect(explorerPanel.style.display).toBe("none");
    expect(container.classList.contains("spec-mode--chat-only")).toBe(true);
  });

  it("hides explorer when not in spec mode even if specs exist", () => {
    const ctx = makeContext();
    // Stay in board mode.
    const explorerPanel = ctx.document.getElementById("explorer-panel");

    ctx._updateSpecPaneVisibility(true);
    expect(explorerPanel.style.display).toBe("none");
  });
});

describe("focusSpec", () => {
  it("sets focused path and updates title element", () => {
    const ctx = makeContext();
    const titleEl = ctx.document.getElementById("spec-focused-title");
    const innerEl = ctx.document.getElementById("spec-focused-body-inner");

    ctx.focusSpec("local/my-spec.md", "/home/user/project");

    expect(ctx.getFocusedSpecPath()).toBe("local/my-spec.md");
    expect(titleEl.textContent).toBe("local/my-spec.md");
    expect(innerEl.innerHTML).toContain("Loading");
  });

  it("updates location hash for deep-linking", () => {
    const replaceState = vi.fn();
    const ctx = makeContext({ history: { replaceState } });

    ctx.focusSpec("local/my-spec.md", "/home/user/project");

    expect(replaceState).toHaveBeenCalledWith(
      null,
      "",
      "#spec/" + encodeURIComponent("local/my-spec.md"),
    );
  });

  it("calls renderMinimap when spec tree data is available", () => {
    const renderMinimap = vi.fn();
    const treeData = { nodes: [{ path: "local/my-spec.md", is_leaf: true }] };
    const ctx = makeContext({ renderMinimap, _specTreeData: treeData });

    ctx.focusSpec("local/my-spec.md", "/home/user/project");
    expect(renderMinimap).toHaveBeenCalledWith("local/my-spec.md", treeData);
  });

  it("starts refresh polling", () => {
    const setIntervalFn = vi.fn(() => 99);
    const ctx = makeContext({ setInterval: setIntervalFn });

    ctx.focusSpec("local/my-spec.md", "/home/user/project");
    expect(setIntervalFn).toHaveBeenCalled();
  });
});

describe("_loadAndRenderSpec", () => {
  // Regression guard: the spec tree returns paths that already include the
  // "specs/" prefix (e.g. "specs/local/foo.md"). Earlier code re-added it
  // when building the explorer/file URL, producing "/specs/specs/..." and
  // 404s for every spec click.
  it("does not double-prefix specs/ in the fetched URL", async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        text: () => Promise.resolve("---\ntitle: x\n---\n"),
      }),
    );
    const ctx = makeContext({ fetch: fetchMock });
    ctx.focusSpec("specs/local/feature.md", "/home/user/project");
    await new Promise((r) => setTimeout(r, 50));

    expect(fetchMock).toHaveBeenCalled();
    const url = fetchMock.mock.calls[0][0];
    // The URL-encoded absolute path must NOT contain "specs%2Fspecs".
    expect(url).not.toMatch(/specs%2Fspecs/);
    // It must contain the single expected absolute path.
    expect(url).toContain(
      encodeURIComponent("/home/user/project/specs/local/feature.md"),
    );
  });

  it("fetches spec and renders frontmatter and body on success", async () => {
    const specText =
      "---\ntitle: My Feature\nstatus: validated\nauthor: bob\ncreated: 2026-01-01\nupdated: 2026-03-01\neffort: medium\n---\n# My Feature\nSome content here.";

    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        text: () => Promise.resolve(specText),
      }),
    );

    const treeData = {
      nodes: [{ path: "local/feature.md", is_leaf: true }],
    };

    const ctx = makeContext({
      fetch: fetchMock,
      _specTreeData: treeData,
    });

    ctx.focusSpec("local/feature.md", "/home/user/project");

    // Wait for async fetch chain to resolve.
    await new Promise((r) => setTimeout(r, 50));

    const titleEl = ctx.document.getElementById("spec-focused-title");
    expect(titleEl.textContent).toBe("My Feature");

    const statusEl = ctx.document.getElementById("spec-focused-status");
    expect(statusEl.textContent).toBe("validated");
    expect(statusEl.classList.contains("spec-status--validated")).toBe(true);

    const kindEl = ctx.document.getElementById("spec-focused-kind");
    expect(kindEl.textContent).toBe("implementation");

    const effortEl = ctx.document.getElementById("spec-focused-effort");
    expect(effortEl.textContent).toBe("medium");

    const metaEl = ctx.document.getElementById("spec-focused-meta");
    expect(metaEl.textContent).toContain("bob");
    expect(metaEl.textContent).toContain("2026-01-01");
    expect(metaEl.textContent).toContain("2026-03-01");

    // Dispatch button visible for validated spec.
    const dispatchBtn = ctx.document.getElementById("spec-dispatch-btn");
    expect(dispatchBtn.classList.contains("hidden")).toBe(false);
  });

  it("hides dispatch button for non-validated specs", async () => {
    const specText = "---\ntitle: Draft\nstatus: drafted\n---\nBody";

    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        text: () => Promise.resolve(specText),
      }),
    );

    const ctx = makeContext({ fetch: fetchMock });
    ctx.focusSpec("local/draft.md", "/home/user/project");

    await new Promise((r) => setTimeout(r, 50));

    const dispatchBtn = ctx.document.getElementById("spec-dispatch-btn");
    expect(dispatchBtn.classList.contains("hidden")).toBe(true);
  });

  it("shows breakdown button for drafted specs", async () => {
    const specText = "---\ntitle: Draft\nstatus: drafted\n---\nBody";
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        text: () => Promise.resolve(specText),
      }),
    );
    const ctx = makeContext({ fetch: fetchMock });
    ctx.focusSpec("local/draft.md", "/home/user/project");
    await new Promise((r) => setTimeout(r, 50));

    const breakdownBtn = ctx.document.getElementById("spec-summarize-btn");
    expect(breakdownBtn.classList.contains("hidden")).toBe(false);
    expect(breakdownBtn.textContent).toBe("Break Down");
  });

  it("hides breakdown button for complete specs", async () => {
    const specText = "---\ntitle: Done\nstatus: complete\n---\nBody";
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        text: () => Promise.resolve(specText),
      }),
    );
    const ctx = makeContext({ fetch: fetchMock });
    ctx.focusSpec("local/done.md", "/home/user/project");
    await new Promise((r) => setTimeout(r, 50));

    const breakdownBtn = ctx.document.getElementById("spec-summarize-btn");
    expect(breakdownBtn.classList.contains("hidden")).toBe(true);
  });

  it("determines design kind for non-leaf nodes", async () => {
    const specText = "---\ntitle: Design Spec\nstatus: validated\n---\nBody";
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        text: () => Promise.resolve(specText),
      }),
    );
    const treeData = {
      nodes: [{ path: "local/design.md", is_leaf: false }],
    };
    const ctx = makeContext({ fetch: fetchMock, _specTreeData: treeData });
    ctx.focusSpec("local/design.md", "/home/user/project");
    await new Promise((r) => setTimeout(r, 50));

    const kindEl = ctx.document.getElementById("spec-focused-kind");
    expect(kindEl.textContent).toBe("design");
    expect(kindEl.className).toContain("spec-kind--design");
  });

  it("clears state on fetch error", async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: false,
        status: 404,
      }),
    );
    const clearIntervalFn = vi.fn();
    const teardownFloatingToc = vi.fn();
    const replaceState = vi.fn();
    const ctx = makeContext({
      fetch: fetchMock,
      clearInterval: clearIntervalFn,
      teardownFloatingToc,
      history: { replaceState },
      location: { hash: "#spec/local/missing.md", pathname: "/app" },
    });

    ctx.focusSpec("local/missing.md", "/home/user/project");
    await new Promise((r) => setTimeout(r, 50));

    // Focused path should be cleared.
    expect(ctx.getFocusedSpecPath()).toBe(null);
    // Hash should be cleared.
    expect(replaceState).toHaveBeenCalledWith(null, "", "/app");
    // Buttons should be hidden.
    const dispatchBtn = ctx.document.getElementById("spec-dispatch-btn");
    expect(dispatchBtn.classList.contains("hidden")).toBe(true);
    const breakdownBtn = ctx.document.getElementById("spec-summarize-btn");
    expect(breakdownBtn.classList.contains("hidden")).toBe(true);
    // teardownFloatingToc should be called.
    expect(teardownFloatingToc).toHaveBeenCalled();
  });

  it("skips re-render when content has not changed", async () => {
    const specText = "---\ntitle: Same\nstatus: drafted\n---\nBody";
    let callCount = 0;
    const fetchMock = vi.fn(() => {
      callCount++;
      return Promise.resolve({
        ok: true,
        text: () => Promise.resolve(specText),
      });
    });

    const renderMarkdown = vi.fn((t) => "<p>" + t + "</p>");
    const ctx = makeContext({ fetch: fetchMock, renderMarkdown });

    ctx.focusSpec("local/same.md", "/home/user/project");
    await new Promise((r) => setTimeout(r, 50));

    expect(renderMarkdown).toHaveBeenCalledTimes(1);

    // Call _loadAndRenderSpec again with same content.
    ctx._loadAndRenderSpec();
    await new Promise((r) => setTimeout(r, 50));

    // renderMarkdown should NOT be called again since content is the same.
    expect(renderMarkdown).toHaveBeenCalledTimes(1);
  });

  it("does nothing when no focused spec path", () => {
    const fetchMock = vi.fn();
    const ctx = makeContext({ fetch: fetchMock });
    ctx._loadAndRenderSpec();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("uses activeWorkspaces[0] over stored workspace", async () => {
    const specText = "---\ntitle: WS\nstatus: drafted\n---\nBody";
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        text: () => Promise.resolve(specText),
      }),
    );
    const ctx = makeContext({
      fetch: fetchMock,
      activeWorkspaces: ["/new/workspace"],
    });

    // Spec tree returns paths with "specs/" prefix; pass the realistic form.
    ctx.focusSpec("specs/local/ws.md", "/old/workspace");
    await new Promise((r) => setTimeout(r, 50));

    const fetchUrl = fetchMock.mock.calls[0][0];
    expect(fetchUrl).toContain(
      encodeURIComponent("/new/workspace/specs/local/ws.md"),
    );
  });
});

describe("breakDownFocusedSpec", () => {
  it("sends /break-down via PlanningChat", () => {
    const sendMessage = vi.fn();
    const ctx = makeContext({
      PlanningChat: { init: vi.fn(), sendMessage },
    });
    ctx.breakDownFocusedSpec();
    expect(sendMessage).toHaveBeenCalledWith("/break-down");
  });
});

describe("dispatchFocusedSpec", () => {
  it("is a no-op stub that does not throw", () => {
    const ctx = makeContext();
    expect(() => ctx.dispatchFocusedSpec()).not.toThrow();
  });
});

describe("openSelectedSpec", () => {
  it("is a no-op stub that does not throw", () => {
    const ctx = makeContext();
    expect(() => ctx.openSelectedSpec()).not.toThrow();
  });
});

describe("getFocusedSpecPath", () => {
  it("returns null initially", () => {
    const ctx = makeContext();
    expect(ctx.getFocusedSpecPath()).toBe(null);
  });

  it("returns path after focusSpec", () => {
    const ctx = makeContext();
    ctx.focusSpec("local/test.md", "/ws");
    expect(ctx.getFocusedSpecPath()).toBe("local/test.md");
  });
});

describe("_startSpecRefreshPoll and _stopSpecRefreshPoll", () => {
  it("starts and stops polling interval", () => {
    const setIntervalFn = vi.fn(() => 123);
    const clearIntervalFn = vi.fn();
    const ctx = makeContext({
      setInterval: setIntervalFn,
      clearInterval: clearIntervalFn,
    });

    ctx._startSpecRefreshPoll();
    expect(setIntervalFn).toHaveBeenCalled();

    ctx._stopSpecRefreshPoll();
    expect(clearIntervalFn).toHaveBeenCalledWith(123);
  });

  it("clears previous timer when starting new poll", () => {
    const clearIntervalFn = vi.fn();
    let timerId = 100;
    const setIntervalFn = vi.fn(() => timerId++);
    const ctx = makeContext({
      setInterval: setIntervalFn,
      clearInterval: clearIntervalFn,
    });

    ctx._startSpecRefreshPoll();
    ctx._startSpecRefreshPoll();
    // First start sets timer 100, second start clears 100 and sets 101.
    expect(clearIntervalFn).toHaveBeenCalledWith(100);
  });

  it("stopSpecRefreshPoll is safe to call multiple times", () => {
    const clearIntervalFn = vi.fn();
    const ctx = makeContext({ clearInterval: clearIntervalFn });
    ctx._stopSpecRefreshPoll();
    ctx._stopSpecRefreshPoll();
    // clearInterval should not be called when no timer is set.
    expect(clearIntervalFn).not.toHaveBeenCalled();
  });
});

describe("DOMContentLoaded handler", () => {
  it("calls PlanningChat.init on load", () => {
    const init = vi.fn();
    const ctx = makeContext({ PlanningChat: { init, sendMessage: vi.fn() } });
    const addEventCalls = ctx.document.addEventListener.mock.calls;
    const domReady = addEventCalls.find((c) => c[0] === "DOMContentLoaded");
    expect(domReady).toBeDefined();
    domReady[1]();
    expect(init).toHaveBeenCalled();
  });

  it("applies non-board mode on DOMContentLoaded", () => {
    const storage = new Map([["wallfacer-mode", "spec"]]);
    const ctx = makeContext({ storage });
    const addEventCalls = ctx.document.addEventListener.mock.calls;
    const domReady = addEventCalls.find((c) => c[0] === "DOMContentLoaded");
    expect(domReady).toBeDefined();
    domReady[1]();

    const board = ctx.document.getElementById("board");
    expect(board.style.display).toBe("none");
  });
});

describe("_showSpecReadme", () => {
  it("fetches and renders specs/README.md", async () => {
    const readmeText = "# Roadmap\nSpec overview here.";
    const fetchMock = vi.fn(() =>
      Promise.resolve({
        ok: true,
        text: () => Promise.resolve(readmeText),
      }),
    );
    const renderMarkdown = vi.fn((t) => "<div>" + t + "</div>");
    const ctx = makeContext({
      fetch: fetchMock,
      renderMarkdown,
      activeWorkspaces: ["/home/user/project"],
    });

    ctx._showSpecReadme();
    await new Promise((r) => setTimeout(r, 50));

    const titleEl = ctx.document.getElementById("spec-focused-title");
    expect(titleEl.textContent).toBe("Specs");

    expect(fetchMock).toHaveBeenCalled();
    const url = fetchMock.mock.calls[0][0];
    expect(url).toContain("README.md");

    expect(renderMarkdown).toHaveBeenCalledWith(readmeText);
  });

  it("shows placeholder when README fetch fails", async () => {
    const fetchMock = vi.fn(() => Promise.resolve({ ok: false, status: 404 }));
    const ctx = makeContext({
      fetch: fetchMock,
      activeWorkspaces: ["/ws"],
    });

    ctx._showSpecReadme();
    await new Promise((r) => setTimeout(r, 50));

    const titleEl = ctx.document.getElementById("spec-focused-title");
    expect(titleEl.textContent).toBe("Select a spec");
  });

  it("does nothing when no active workspaces", () => {
    const fetchMock = vi.fn();
    const ctx = makeContext({
      fetch: fetchMock,
      activeWorkspaces: [],
    });
    ctx._showSpecReadme();
    expect(fetchMock).not.toHaveBeenCalled();
  });

  it("clears header metadata elements", async () => {
    const fetchMock = vi.fn(() =>
      Promise.resolve({ ok: true, text: () => Promise.resolve("content") }),
    );
    const ctx = makeContext({ fetch: fetchMock });

    // Pre-set some values.
    ctx.document.getElementById("spec-focused-status").textContent = "old";
    ctx.document.getElementById("spec-focused-kind").textContent = "old";
    ctx.document.getElementById("spec-focused-effort").textContent = "old";
    ctx.document.getElementById("spec-focused-meta").textContent = "old";

    ctx._showSpecReadme();

    // Synchronous clearing happens immediately.
    expect(ctx.document.getElementById("spec-focused-status").textContent).toBe(
      "",
    );
    expect(ctx.document.getElementById("spec-focused-kind").textContent).toBe(
      "",
    );
    expect(ctx.document.getElementById("spec-focused-effort").textContent).toBe(
      "",
    );
    expect(ctx.document.getElementById("spec-focused-meta").textContent).toBe(
      "",
    );
  });
});

describe("switchMode with docs", () => {
  it("shows docs container and hides board and spec", () => {
    const ctx = makeContext();
    ctx.switchMode("docs");

    const board = ctx.document.getElementById("board");
    const specView = ctx.document.getElementById("spec-mode-container");
    const docsView = ctx.document.getElementById("docs-mode-container");

    expect(board.style.display).toBe("none");
    expect(specView.style.display).toBe("none");
    expect(docsView.style.display).toBe("");
  });
});

describe("mode restoration from localStorage", () => {
  it("ignores invalid mode values in localStorage", () => {
    const storage = new Map([["wallfacer-mode", "invalid"]]);
    const ctx = makeContext({ storage });
    expect(ctx.getCurrentMode()).toBe("board");
  });

  it("restores docs mode from localStorage", () => {
    const storage = new Map([["wallfacer-mode", "docs"]]);
    const ctx = makeContext({ storage });
    expect(ctx.getCurrentMode()).toBe("docs");
  });
});

describe("_initSpecChatResize", () => {
  it("restores persisted chat width from localStorage", () => {
    const storage = new Map([["wallfacer-spec-chat-width", "400"]]);
    const ctx = makeContext({ storage });
    ctx._initSpecChatResize();

    const chatPane = ctx.document.getElementById("spec-chat-stream");
    expect(chatPane.style.width).toBe("400px");
  });

  it("does not restore width below minimum", () => {
    const storage = new Map([["wallfacer-spec-chat-width", "100"]]);
    const ctx = makeContext({ storage });
    ctx._initSpecChatResize();

    const chatPane = ctx.document.getElementById("spec-chat-stream");
    // Width should NOT be set since 100 < _specChatMinWidth (280).
    expect(chatPane.style.width).toBeUndefined();
  });
});
