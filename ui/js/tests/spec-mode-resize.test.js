/**
 * Unit tests for spec mode chat pane resize.
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
  if (opts.chatWidth) storage.set("wallfacer-spec-chat-width", opts.chatWidth);

  function makeEl(tag, id) {
    const _classList = new Set();
    const _style = {};
    const _listeners = {};
    const el = {
      tagName: tag,
      style: _style,
      textContent: "",
      innerHTML: "",
      value: "",
      offsetWidth: opts.chatInitialWidth || 360,
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
      _fire(type, event) {
        for (const fn of _listeners[type] || []) fn(event);
      },
      focus() {},
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
    "spec-chat-resize",
    "spec-chat-stream",
  ];
  for (const id of ids) makeEl("DIV", id);
  registry.get("sidebar-nav-board").classList.add("active");
  registry.get("spec-mode-container").style.display = "none";

  // Track document-level listeners for mousemove/mouseup.
  const docListeners = {};

  const ctx = {
    document: {
      getElementById(id) {
        return registry.get(id) || null;
      },
      addEventListener(type, fn) {
        if (!docListeners[type]) docListeners[type] = [];
        docListeners[type].push(fn);
      },
      removeEventListener(type, fn) {
        if (docListeners[type]) {
          docListeners[type] = docListeners[type].filter((f) => f !== fn);
        }
      },
      body: {
        style: {},
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
    window: { innerWidth: 1200 },
    fetch: () => Promise.reject(new Error("stubbed")),
    Routes: { explorer: { readFile: () => "/api/explorer/file" } },
    withBearerHeaders: () => ({}),
    renderMarkdown: (text) => "<p>" + text + "</p>",
    setInterval: () => 42,
    clearInterval: () => {},
    location: { hash: "", pathname: "/" },
    history: { replaceState: () => {} },
    Math,
    parseInt,
    console,
    registry,
    storage,
    docListeners,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

describe("chat pane resize", () => {
  it("restores persisted width from localStorage", () => {
    const ctx = makeContext({ chatWidth: "400" });
    // Trigger DOMContentLoaded to init resize.
    for (const fn of ctx.docListeners["DOMContentLoaded"] || []) fn();

    const chatPane = ctx.registry.get("spec-chat-stream");
    expect(chatPane.style.width).toBe("400px");
  });

  it("does not restore width below minimum", () => {
    const ctx = makeContext({ chatWidth: "100" });
    for (const fn of ctx.docListeners["DOMContentLoaded"] || []) fn();

    const chatPane = ctx.registry.get("spec-chat-stream");
    // Width should NOT be set to 100px since it's below min (280).
    expect(chatPane.style.width).not.toBe("100px");
  });

  it("does not set width when no stored value", () => {
    const ctx = makeContext();
    for (const fn of ctx.docListeners["DOMContentLoaded"] || []) fn();

    const chatPane = ctx.registry.get("spec-chat-stream");
    // No width should be set — the CSS default (360px) applies.
    expect(chatPane.style.width).toBeUndefined();
  });
});
