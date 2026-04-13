/**
 * Unit tests for spec mode layout — verifies the three-pane structure
 * and visibility toggling via switchMode.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const specModeCode = readFileSync(join(jsDir, "spec-mode.js"), "utf8");

function makeEl(tag, registry) {
  const _classList = new Set();
  const _style = {};
  const _attrs = new Map();
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
    style: _style,
    textContent: "",
    get className() {
      return _className;
    },
    set className(v) {
      _className = v || "";
    },
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
    setAttribute(k, v) {
      _attrs.set(k, v);
    },
    getAttribute(k) {
      return _attrs.has(k) ? _attrs.get(k) : null;
    },
    hasAttribute(k) {
      return _attrs.has(k);
    },
    removeAttribute(k) {
      _attrs.delete(k);
    },
    innerHTML: "",
    // Stubbed for _syncChatFirstEmptyHint's has-messages probe. Tests that
    // need a truthy result set `__childMatches` on the element directly.
    querySelector(_sel) {
      return el.__childMatches ? {} : null;
    },
  };
  return el;
}

function makeContext() {
  const registry = new Map();
  const storage = new Map();

  // Create all elements the layout expects.
  const ids = [
    "sidebar-nav-board",
    "sidebar-nav-spec",
    "board",
    "spec-mode-container",
    "explorer-panel",
    "spec-focused-view",
    "spec-focused-title",
    "spec-focused-status",
    "spec-dispatch-btn",
    "spec-summarize-btn",
    "spec-focused-body",
    "spec-chat-resize",
    "spec-chat-stream",
    "spec-chat-messages",
    "spec-chat-input",
    "spec-chat-send",
    "spec-chat-empty-hint",
  ];
  for (const id of ids) {
    const el = makeEl("DIV", registry);
    el.id = id;
  }

  // Board tab starts active, spec container starts hidden.
  registry.get("sidebar-nav-board").classList.add("active");
  registry.get("spec-mode-container").style.display = "none";

  const ctx = {
    document: {
      getElementById(id) {
        return registry.get(id) || null;
      },
      addEventListener() {},
    },
    localStorage: {
      getItem(k) {
        return storage.get(k) ?? null;
      },
      setItem(k, v) {
        storage.set(k, v);
      },
    },
    fetch: () => Promise.reject(new Error("stubbed")),
    Routes: { explorer: { readFile: () => "/api/explorer/file" } },
    withBearerHeaders: () => ({}),
    renderMarkdown: (text) => "<p>" + text + "</p>",
    setInterval: () => 42,
    clearInterval: () => {},
    location: { hash: "", pathname: "/" },
    history: { replaceState: () => {} },
    console,
    showConfirm: () => Promise.resolve(true),
    showAlert: () => {},
    Promise,
    registry,
  };
  vm.createContext(ctx);
  vm.runInContext(specModeCode, ctx);
  return ctx;
}

describe("spec-mode-layout", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
  });

  it("spec mode container is hidden by default", () => {
    const specContainer = ctx.registry.get("spec-mode-container");
    expect(specContainer.style.display).toBe("none");
  });

  it("switching to spec mode shows spec container and hides board", () => {
    ctx.switchMode("spec");
    expect(ctx.registry.get("board").style.display).toBe("none");
    expect(ctx.registry.get("spec-mode-container").style.display).toBe("");
  });

  it("switching back to board mode restores board visibility", () => {
    ctx.switchMode("spec");
    ctx.switchMode("board");
    expect(ctx.registry.get("board").style.display).toBe("");
    expect(ctx.registry.get("spec-mode-container").style.display).toBe("none");
  });

  it("spec focused view elements exist", () => {
    expect(ctx.registry.get("spec-focused-title")).toBeTruthy();
    expect(ctx.registry.get("spec-focused-body")).toBeTruthy();
    expect(ctx.registry.get("spec-focused-status")).toBeTruthy();
  });

  it("chat stream elements exist", () => {
    expect(ctx.registry.get("spec-chat-messages")).toBeTruthy();
    expect(ctx.registry.get("spec-chat-input")).toBeTruthy();
    expect(ctx.registry.get("spec-chat-send")).toBeTruthy();
  });

  it("dispatch and summarize buttons exist", () => {
    expect(ctx.registry.get("spec-dispatch-btn")).toBeTruthy();
    expect(ctx.registry.get("spec-summarize-btn")).toBeTruthy();
  });

  it("resize handle exists", () => {
    expect(ctx.registry.get("spec-chat-resize")).toBeTruthy();
  });
});

// ---------------------------------------------------------------------------
// Layout state machine (layout-state-machine spec).
// ---------------------------------------------------------------------------

describe("spec-mode layout state machine", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
    // Enter spec mode so the explorer-panel show/hide logic exercises the
    // `getCurrentMode() === "spec"` branch.
    ctx.switchMode("spec");
  });

  function container() {
    return ctx.registry.get("spec-mode-container");
  }

  function explorer() {
    return ctx.registry.get("explorer-panel");
  }

  function setTree(nodes) {
    ctx.specModeState.tree = nodes || [];
  }

  function setIndex(idx) {
    ctx.specModeState.index = idx || null;
  }

  it("TestLayout_EmptyTreeNullIndex_RendersChatFirst", () => {
    setTree([]);
    setIndex(null);
    ctx._applyLayout();
    expect(container().getAttribute("data-layout")).toBe("chat-first");
    expect(explorer().style.display).toBe("none");
  });

  it("TestLayout_NonEmptyTree_RendersThreePane", () => {
    setTree([{ path: "local/a.md" }]);
    setIndex(null);
    ctx._applyLayout();
    expect(container().getAttribute("data-layout")).toBe("three-pane");
    expect(explorer().style.display).toBe("");
  });

  it("TestLayout_IndexOnlyNoSpecs_RendersThreePane", () => {
    setTree([]);
    setIndex({ path: "specs/README.md", workspace: "/ws" });
    ctx._applyLayout();
    expect(container().getAttribute("data-layout")).toBe("three-pane");
  });

  it("TestLayout_TransitionOnSSEUpdate — chat-first → three-pane when index arrives", () => {
    setTree([]);
    setIndex(null);
    ctx._applyLayout();
    expect(container().getAttribute("data-layout")).toBe("chat-first");
    // Simulate an SSE snapshot that adds the Roadmap index.
    setIndex({ path: "specs/README.md", workspace: "/ws" });
    ctx._applyLayout();
    expect(container().getAttribute("data-layout")).toBe("three-pane");
  });

  it("TestLayout_TransitionReverseOnEmpty — three-pane → chat-first when everything clears", () => {
    setTree([{ path: "local/a.md" }]);
    setIndex(null);
    ctx._applyLayout();
    expect(container().getAttribute("data-layout")).toBe("three-pane");
    setTree([]);
    setIndex(null);
    ctx._applyLayout();
    expect(container().getAttribute("data-layout")).toBe("chat-first");
  });

  it("_updateSpecPaneVisibility delegates to _applyLayout and sets data-layout", () => {
    ctx._updateSpecPaneVisibility(false);
    expect(container().getAttribute("data-layout")).toBe("chat-first");
    expect(container().classList.contains("spec-mode--chat-only")).toBe(true);
    ctx._updateSpecPaneVisibility(true);
    expect(container().getAttribute("data-layout")).toBe("three-pane");
    expect(container().classList.contains("spec-mode--chat-only")).toBe(false);
  });

  it("getLayoutState returns the currently applied layout", () => {
    setTree([]);
    setIndex(null);
    ctx._applyLayout();
    expect(ctx.getLayoutState()).toBe("chat-first");
    setTree([{ path: "x" }]);
    ctx._applyLayout();
    expect(ctx.getLayoutState()).toBe("three-pane");
  });

  it("focusRoadmapIndex updates specModeState.focusedSpecPath", () => {
    // focusRoadmapIndex calls fetch(); stub it to resolve without
    // triggering the markdown path (not exercised here).
    ctx.fetch = () =>
      Promise.resolve({ ok: true, text: () => Promise.resolve("") });
    ctx.focusRoadmapIndex({
      path: "specs/README.md",
      workspace: "/ws",
    });
    expect(ctx.specModeState.focusedSpecPath).toBe("specs/README.md");
  });

  it("focusSpec updates specModeState.focusedSpecPath", () => {
    ctx.focusSpec("specs/local/bar.md", "/ws");
    expect(ctx.specModeState.focusedSpecPath).toBe("specs/local/bar.md");
  });

  // ---------------------------------------------------------------------
  // Chat-first empty-state hint (parent chat-first-mode spec, lines 540+).
  // The composer placeholder must mention /create and a visible hint must
  // sit above the composer whenever chat-first layout has no messages.
  // ---------------------------------------------------------------------

  function hint() {
    return ctx.registry.get("spec-chat-empty-hint");
  }
  function input() {
    return ctx.registry.get("spec-chat-input");
  }
  function messages() {
    return ctx.registry.get("spec-chat-messages");
  }

  it("chat-first + no messages → hint visible and placeholder mentions /create", () => {
    messages().__childMatches = false;
    setTree([]);
    setIndex(null);
    ctx._applyLayout();
    expect(hint().classList.contains("spec-chat-empty-hint--visible")).toBe(
      true,
    );
    expect(input().placeholder).toContain("/create");
  });

  it("chat-first + messages present → hint hidden and placeholder resets", () => {
    messages().__childMatches = true;
    setTree([]);
    setIndex(null);
    ctx._applyLayout();
    expect(hint().classList.contains("spec-chat-empty-hint--visible")).toBe(
      false,
    );
    expect(input().placeholder).toBe("Message...");
  });

  it("three-pane layout never shows the empty-state hint", () => {
    messages().__childMatches = false;
    setTree([{ path: "a.md" }]);
    setIndex(null);
    ctx._applyLayout();
    expect(hint().classList.contains("spec-chat-empty-hint--visible")).toBe(
      false,
    );
    expect(input().placeholder).toBe("Message...");
  });
});
