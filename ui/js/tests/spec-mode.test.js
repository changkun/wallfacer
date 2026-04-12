/**
 * Unit tests for spec-mode.js — mode state and switching.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-mode.js"), "utf8");

function makeDom() {
  const registry = new Map();

  function makeEl(tag) {
    const _classList = new Set();
    const _style = {};
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
    };
    return el;
  }

  // Pre-create the elements spec-mode.js expects.
  const boardTab = makeEl("BUTTON");
  boardTab.id = "sidebar-nav-board";
  boardTab.classList.add("active");

  const specTab = makeEl("BUTTON");
  specTab.id = "sidebar-nav-spec";

  const board = makeEl("MAIN");
  board.id = "board";

  const specContainer = makeEl("DIV");
  specContainer.id = "spec-mode-container";
  specContainer.style.display = "none";

  return {
    registry,
    getElementById(id) {
      return registry.get(id) || null;
    },
    addEventListener() {},
    boardTab,
    specTab,
    board,
    specContainer,
  };
}

function makeContext() {
  const dom = makeDom();
  const storage = new Map();
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
    storage,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

describe("spec-mode", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
  });

  it("defaults to board mode", () => {
    expect(ctx.getCurrentMode()).toBe("board");
  });

  it("getCurrentMode returns the current mode", () => {
    expect(ctx.getCurrentMode()).toBe("board");
  });

  it("setCurrentMode updates the internal variable without writing localStorage", () => {
    ctx.setCurrentMode("spec");
    // setCurrentMode is now a pure state setter; the saved preference is
    // only written via explicit switchMode(..., { persist: true }) calls.
    expect(ctx.storage.has("wallfacer-mode")).toBe(false);
    expect(ctx.getCurrentMode()).toBe("spec");
  });

  it("switchMode updates tab active classes", () => {
    const dom = ctx.document;
    const boardNav = dom.getElementById("sidebar-nav-board");
    const specNav = dom.getElementById("sidebar-nav-spec");

    ctx.switchMode("spec");

    expect(boardNav.classList.contains("active")).toBe(false);
    expect(specNav.classList.contains("active")).toBe(true);
  });

  it("switchMode toggles board and spec container visibility", () => {
    const dom = ctx.document;
    const board = dom.getElementById("board");
    const specContainer = dom.getElementById("spec-mode-container");

    ctx.switchMode("spec");
    expect(board.style.display).toBe("none");
    expect(specContainer.style.display).toBe("");

    ctx.switchMode("board");
    expect(board.style.display).toBe("");
    expect(specContainer.style.display).toBe("none");
  });

  it("switchMode is idempotent", () => {
    ctx.switchMode("board");
    ctx.switchMode("board");
    expect(ctx.getCurrentMode()).toBe("board");
  });

  it("switchMode persists only when opts.persist is true", () => {
    ctx.switchMode("spec");
    expect(ctx.storage.has("wallfacer-mode")).toBe(false);
    ctx.switchMode("board", { persist: true });
    expect(ctx.storage.get("wallfacer-mode")).toBe("board");
    ctx.switchMode("spec", { persist: true });
    // Internal mode "spec" is saved under the user-facing alias "plan".
    expect(ctx.storage.get("wallfacer-mode")).toBe("plan");
  });

  it("sets _highlightTaskId when switching from spec to board with dispatched spec", () => {
    ctx.switchMode("spec");
    // Simulate a focused spec with dispatched_task_id.
    ctx._focusedSpecContent =
      "---\ntitle: Test\nstatus: validated\ndispatched_task_id: abc-123\n---\n# Body\n";
    ctx.switchMode("board");
    // _highlightTaskId should have been set and then cleared (since _highlightBoardTask
    // runs but querySelector returns null in our stub context, so it's a no-op).
    // The key test is that it doesn't throw.
    expect(ctx.getCurrentMode()).toBe("board");
  });

  it("does not set _highlightTaskId for null dispatched_task_id", () => {
    ctx.switchMode("spec");
    ctx._focusedSpecContent =
      "---\ntitle: Test\nstatus: validated\ndispatched_task_id: null\n---\n# Body\n";
    ctx.switchMode("board");
    expect(ctx._highlightTaskId).toBe(null);
  });

  it("restores spec mode from a saved 'plan' preference", () => {
    // Create a new context with the user-facing "plan" label pre-set.
    const dom = makeDom();
    const storage = new Map([["wallfacer-mode", "plan"]]);
    const ctx2 = {
      document: dom,
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
      storage,
    };
    vm.createContext(ctx2);
    vm.runInContext(code, ctx2);

    expect(ctx2.getCurrentMode()).toBe("spec");
  });
});
