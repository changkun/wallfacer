/**
 * Tests for ui/js/dispatch-toast.js — the dispatch-complete toast that
 * bridges users from Plan mode to the Board after a successful dispatch.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "dispatch-toast.js"), "utf8");

function makeEl(tag, opts = {}) {
  const children = [];
  const classes = new Set();
  const attrs = new Map();
  const listeners = new Map();
  const el = {
    tagName: tag.toUpperCase(),
    children,
    classList: {
      add: (c) => classes.add(c),
      remove: (c) => classes.delete(c),
      contains: (c) => classes.has(c),
      toggle: (c, force) => {
        if (force === true) classes.add(c);
        else if (force === false) classes.delete(c);
        else classes.has(c) ? classes.delete(c) : classes.add(c);
      },
    },
    appendChild: (child) => {
      children.push(child);
      child.parentNode = el;
      return child;
    },
    removeChild: (child) => {
      const idx = children.indexOf(child);
      if (idx >= 0) children.splice(idx, 1);
      child.parentNode = null;
      return child;
    },
    setAttribute: (k, v) => attrs.set(k, v),
    getAttribute: (k) => attrs.get(k) ?? null,
    addEventListener: (type, fn) => {
      if (!listeners.has(type)) listeners.set(type, []);
      listeners.get(type).push(fn);
    },
    dispatchEvent: (type) => {
      const fns = listeners.get(type) || [];
      for (const fn of fns) fn();
    },
    scrollIntoView: vi.fn(),
    textContent: "",
    className: opts.className || "",
    dataset: {},
    parentNode: null,
    _listeners: listeners,
    _attrs: attrs,
    _classes: classes,
  };
  return el;
}

function makeContext(overrides = {}) {
  const body = makeEl("body");
  body.querySelector = (sel) => {
    // Walk the tree to find a matching element (very minimal selector
    // support — just `[data-task-id="..."]`).
    const m = /^\[data-task-id="(.+?)"\]$/.exec(sel);
    if (!m) return null;
    const id = m[1];
    function walk(el) {
      if (el.dataset && el.dataset.taskId === id) return el;
      for (const child of el.children || []) {
        const found = walk(child);
        if (found) return found;
      }
      return null;
    }
    return walk(body);
  };
  const elementsById = new Map();

  const timers = [];

  const ctx = {
    console,
    document: {
      body,
      createElement: (tag) => makeEl(tag),
      getElementById: (id) => elementsById.get(id) || null,
      querySelector: body.querySelector,
    },
    switchMode: vi.fn(),
    setTimeout: (fn, ms) => {
      const id = timers.length + 1;
      timers.push({ id, fn, ms, fired: false });
      return id;
    },
    clearTimeout: (id) => {
      for (const t of timers) if (t.id === id) t.fired = true;
    },
    requestAnimationFrame: (fn) => {
      fn();
      return 1;
    },
    _timers: timers,
    _elementsById: elementsById,
    ...overrides,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

function advanceAllTimers(ctx) {
  // Fire every pending timer at most once.
  for (const t of ctx._timers) {
    if (!t.fired) {
      t.fired = true;
      t.fn();
    }
  }
}

describe("showDispatchCompleteToast", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
  });

  it("TestToast_RendersOnDispatchSuccess — toast appears with correct text", () => {
    ctx.showDispatchCompleteToast(["task-a"]);
    const toast = ctx.document.body.children[0];
    expect(toast).toBeTruthy();
    expect(toast.className).toBe("dispatch-toast");
    const text = toast.children[0];
    expect(text.textContent).toBe("Dispatched 1 task to the Board.");
  });

  it("pluralises the task count when > 1", () => {
    ctx.showDispatchCompleteToast(["a", "b", "c"]);
    const toast = ctx.document.body.children[0];
    expect(toast.children[0].textContent).toBe(
      "Dispatched 3 tasks to the Board.",
    );
  });

  it("handles an empty or missing taskIds list without throwing", () => {
    expect(() => ctx.showDispatchCompleteToast()).not.toThrow();
    const toast = ctx.document.body.children[0];
    expect(toast.children[0].textContent).toBe(
      "Dispatched 0 tasks to the Board.",
    );
  });

  it("TestToast_ViewOnBoardSwitchesMode — clicking the action switches to board", () => {
    ctx.showDispatchCompleteToast(["task-a"]);
    const toast = ctx.document.body.children[0];
    // children: [text, view button, close button]
    const viewBtn = toast.children[1];
    expect(viewBtn.textContent).toContain("View on Board");
    viewBtn.dispatchEvent("click");
    expect(ctx.switchMode).toHaveBeenCalledWith("board");
  });

  it("TestToast_DoesNotPersistPreference — action button omits the {persist: true} opt", () => {
    ctx.showDispatchCompleteToast(["task-a"]);
    const toast = ctx.document.body.children[0];
    const viewBtn = toast.children[1];
    viewBtn.dispatchEvent("click");
    // switchMode was called with a single arg — no opts object means no
    // persist, so localStorage.wallfacer-mode stays untouched.
    const call = ctx.switchMode.mock.calls[0];
    expect(call).toEqual(["board"]);
  });

  it("TestToast_PulsesNewTasks — cards get the just-created class on click", () => {
    // Pre-seed the DOM with a card whose data-task-id matches the dispatch.
    const card = makeEl("div");
    card.dataset.taskId = "task-a";
    ctx.document.body.appendChild(card);

    ctx.showDispatchCompleteToast(["task-a"]);
    const toast = ctx.document.body.children[ctx.document.body.children.length - 1];
    const viewBtn = toast.children[1];
    viewBtn.dispatchEvent("click");

    expect(card._classes.has("task-card--just-created")).toBe(true);
    expect(card.scrollIntoView).toHaveBeenCalled();

    // Advance the 1200ms timer so the class is removed.
    advanceAllTimers(ctx);
    expect(card._classes.has("task-card--just-created")).toBe(false);
  });

  it("TestToast_AutoDismissAfter8s — advancing timers removes the toast", () => {
    ctx.showDispatchCompleteToast(["task-a"]);
    expect(ctx.document.body.children.length).toBe(1);
    advanceAllTimers(ctx);
    expect(ctx.document.body.children.length).toBe(0);
  });

  it("replaces an existing toast when a second dispatch completes", () => {
    ctx.showDispatchCompleteToast(["a"]);
    ctx.showDispatchCompleteToast(["b", "c"]);
    // Only one toast in the DOM at a time.
    expect(ctx.document.body.children.length).toBe(1);
    expect(ctx.document.body.children[0].children[0].textContent).toBe(
      "Dispatched 2 tasks to the Board.",
    );
  });

  it("close button dismisses the toast immediately", () => {
    ctx.showDispatchCompleteToast(["a"]);
    const toast = ctx.document.body.children[0];
    const closeBtn = toast.children[2];
    closeBtn.dispatchEvent("click");
    expect(ctx.document.body.children.length).toBe(0);
  });
});
