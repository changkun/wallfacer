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
    innerHTML: "",
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
