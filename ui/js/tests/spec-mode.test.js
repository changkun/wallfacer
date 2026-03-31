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
  boardTab.id = "mode-tab-board";
  boardTab.classList.add("active");

  const specTab = makeEl("BUTTON");
  specTab.id = "mode-tab-spec";

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
    console,
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

  it("setCurrentMode persists to localStorage", () => {
    ctx.setCurrentMode("spec");
    expect(ctx.storage.get("wallfacer-mode")).toBe("spec");
    expect(ctx.getCurrentMode()).toBe("spec");
  });

  it("switchMode updates tab active classes", () => {
    const dom = ctx.document;
    const boardTab = dom.getElementById("mode-tab-board");
    const specTab = dom.getElementById("mode-tab-spec");

    ctx.switchMode("spec");

    expect(boardTab.classList.contains("active")).toBe(false);
    expect(specTab.classList.contains("active")).toBe(true);
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

  it("switchMode persists mode", () => {
    ctx.switchMode("spec");
    expect(ctx.storage.get("wallfacer-mode")).toBe("spec");
  });

  it("restores spec mode from localStorage", () => {
    // Create a new context with spec mode pre-set in storage.
    const dom = makeDom();
    const storage = new Map([["wallfacer-mode", "spec"]]);
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
      console,
      storage,
    };
    vm.createContext(ctx2);
    vm.runInContext(code, ctx2);

    expect(ctx2.getCurrentMode()).toBe("spec");
  });
});
