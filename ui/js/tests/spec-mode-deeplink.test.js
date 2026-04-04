/**
 * Unit tests for spec mode deep-linking and keyboard shortcuts.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-mode.js"), "utf8");

function makeContext(opts = {}) {
  const registry = new Map();
  const storage = new Map();
  if (opts.mode) storage.set("wallfacer-mode", opts.mode);

  function makeEl(tag, id) {
    const _classList = new Set();
    const _style = {};
    const el = {
      tagName: tag,
      style: _style,
      textContent: "",
      innerHTML: "",
      value: "",
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
      focus() {
        el._focused = true;
      },
      _focused: false,
    };
    if (id) registry.set(id, el);
    return el;
  }

  const ids = [
    "sidebar-nav-board",
    "sidebar-nav-spec",
    "board",
    "spec-mode-container",
    "spec-focused-title",
    "spec-focused-status",
    "spec-focused-body",
    "spec-dispatch-btn",
    "spec-summarize-btn",
    "spec-chat-input",
  ];
  for (const id of ids) makeEl("DIV", id);
  registry.get("sidebar-nav-board").classList.add("active");
  registry.get("spec-mode-container").style.display = "none";

  let _hash = opts.hash || "";
  let _pathname = "/";
  const replaceStateCalls = [];

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
    location: {
      get hash() {
        return _hash;
      },
      set hash(v) {
        _hash = v;
      },
      get pathname() {
        return _pathname;
      },
    },
    history: {
      replaceState(state, title, url) {
        replaceStateCalls.push(url);
        if (url.indexOf("#") >= 0) {
          _hash = url.substring(url.indexOf("#"));
        } else {
          _hash = "";
        }
      },
    },
    fetch: () => Promise.reject(new Error("stubbed")),
    Routes: { explorer: { readFile: () => "/api/explorer/file" } },
    withBearerHeaders: () => ({}),
    renderMarkdown: (text) => "<p>" + text + "</p>",
    setInterval: () => 42,
    clearInterval: () => {},
    console,
    showConfirm: () => Promise.resolve(true),
    showAlert: () => {},
    Promise,
    registry,
    replaceStateCalls,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

describe("spec deep-linking", () => {
  it("focusSpec updates location hash", () => {
    const ctx = makeContext();
    ctx.focusSpec("specs/local/foo.md", "/workspace");
    expect(ctx.replaceStateCalls).toContain("#spec/specs%2Flocal%2Ffoo.md");
  });

  it("switching to board mode clears spec hash", () => {
    const ctx = makeContext();
    ctx.switchMode("spec");
    ctx.focusSpec("specs/local/foo.md", "/workspace");
    ctx.switchMode("board");
    // Last replaceState should clear the hash.
    const last = ctx.replaceStateCalls[ctx.replaceStateCalls.length - 1];
    expect(last).toBe("/");
  });

  it("switching to board mode without spec hash is a no-op", () => {
    const ctx = makeContext();
    ctx.switchMode("spec");
    const countBefore = ctx.replaceStateCalls.length;
    ctx.switchMode("board");
    // No hash was set, so no replaceState for clearing.
    expect(ctx.replaceStateCalls.length).toBe(countBefore);
  });
});

describe("spec mode keyboard stubs", () => {
  it("breakDownFocusedSpec calls PlanningChat.sendMessage", () => {
    const ctx = makeContext();
    // breakDownFocusedSpec now delegates to PlanningChat.sendMessage if available.
    // Since PlanningChat is not loaded in this sandbox, it should not throw.
    ctx.breakDownFocusedSpec();
    // No assertion beyond "does not throw" — the integration is tested in planning-chat.test.js.
  });

  it("dispatchFocusedSpec is a no-op stub", () => {
    const ctx = makeContext();
    // Should not throw.
    ctx.dispatchFocusedSpec();
  });

  it("openSelectedSpec is a no-op stub", () => {
    const ctx = makeContext();
    ctx.openSelectedSpec();
  });
});
