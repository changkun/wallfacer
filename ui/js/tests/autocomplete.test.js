/**
 * Tests for the shared trigger-driven autocomplete widget.
 *
 * The widget lives in ui/js/lib/autocomplete.ts and is consumed by both
 * mention.js (@) and planning-chat.js (/). These tests exercise the
 * trigger-agnostic behaviors — dropdown open/close, keyboard nav,
 * auto-select, and stale-load guarding — via a vm sandbox that mirrors
 * the script-tag global-scope model the widget ships under.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const widgetCode = readFileSync(
  join(jsDir, "build/lib/autocomplete.js"),
  "utf8",
);

function makeEl(tag = "DIV") {
  const classes = new Set();
  const children = [];
  const listeners = {};
  const el = {
    tagName: tag,
    style: {},
    dataset: {},
    parentElement: null,
    get children() {
      return children;
    },
    _listeners: listeners,
    classList: {
      add: (c) => classes.add(c),
      remove: (c) => classes.delete(c),
      toggle: (c, force) => {
        if (force) classes.add(c);
        else classes.delete(c);
      },
      contains: (c) => classes.has(c),
    },
    get className() {
      return [...classes].join(" ");
    },
    set className(v) {
      classes.clear();
      for (const c of String(v).split(/\s+/).filter(Boolean)) classes.add(c);
    },
    textContent: "",
    innerHTML: "",
    value: "",
    selectionStart: 0,
    appendChild: (child) => {
      child.parentElement = el;
      children.push(child);
      return child;
    },
    remove: () => {
      const p = el.parentElement;
      if (!p) return;
      const idx = p.children.indexOf(el);
      if (idx >= 0) p.children.splice(idx, 1);
      el.parentElement = null;
    },
    addEventListener: (type, fn) => {
      (listeners[type] ||= []).push(fn);
    },
    dispatchEvent: (evt) => {
      (listeners[evt.type] || []).forEach((fn) => { fn(evt); });
    },
    focus: () => {},
    setSelectionRange: (s, e) => {
      el.selectionStart = s;
      el.selectionEnd = e;
    },
    getBoundingClientRect: () => ({
      left: 0,
      right: 200,
      top: 50,
      bottom: 70,
      width: 200,
      height: 20,
    }),
    querySelector: () => null,
  };
  return el;
}

function makeContext() {
  const body = makeEl("BODY");
  const ctx = {
    console,
    setTimeout,
    clearTimeout,
    Promise,
    document: {
      body,
      createElement: () => makeEl("DIV"),
    },
    window: {
      innerHeight: 1000,
      addEventListener: () => {},
    },
  };
  vm.createContext(ctx);
  vm.runInContext(widgetCode, ctx);
  return ctx;
}

// Yield long enough for a microtask chain (Promise.resolve + rAF-free).
async function tick(times = 2) {
  for (let i = 0; i < times; i++) await Promise.resolve();
}

function fire(el, type, overrides = {}) {
  const evt = {
    type,
    preventDefault: () => {},
    stopPropagation: () => {},
    ...overrides,
  };
  el.dispatchEvent(evt);
}

describe("attachAutocomplete", () => {
  let ctx;
  let ta;
  beforeEach(() => {
    ctx = makeContext();
    ta = makeEl("TEXTAREA");
  });

  it("no-ops when textarea is null", () => {
    const handle = ctx.attachAutocomplete(null, {
      shouldActivate: () => null,
      fetchItems: () => [],
      renderItem: () => makeEl(),
      onSelect: () => {},
    });
    expect(handle.isOpen()).toBe(false);
    // refresh/close are safe to call.
    handle.refresh();
    handle.close();
  });

  it("opens dropdown when shouldActivate returns a match and renders items", async () => {
    const opts = {
      shouldActivate: () => ({ query: "a", startIdx: 0 }),
      fetchItems: () => ["alpha", "amber"],
      renderItem: (row) => {
        const el = makeEl("DIV");
        el.textContent = row;
        return el;
      },
      onSelect: vi.fn(),
    };
    const handle = ctx.attachAutocomplete(ta, opts);
    fire(ta, "input");
    await tick();
    expect(handle.isOpen()).toBe(true);
    const body = ctx.document.body;
    const dd = body.children.find((n) =>
      n.classList.contains("mention-dropdown"),
    );
    expect(dd).toBeTruthy();
    expect(dd.children).toHaveLength(2);
    // First item auto-selected.
    expect(dd.children[0].classList.contains("mention-item-selected")).toBe(
      true,
    );
  });

  it("closes when shouldActivate returns null", async () => {
    let active = true;
    const handle = ctx.attachAutocomplete(ta, {
      shouldActivate: () => (active ? { query: "x", startIdx: 0 } : null),
      fetchItems: () => ["x"],
      renderItem: () => makeEl("DIV"),
      onSelect: () => {},
    });
    fire(ta, "input");
    await tick();
    expect(handle.isOpen()).toBe(true);
    active = false;
    fire(ta, "input");
    await tick();
    expect(handle.isOpen()).toBe(false);
  });

  it("Enter selects the highlighted row and calls onSelect with the match", async () => {
    const onSelect = vi.fn();
    ctx.attachAutocomplete(ta, {
      shouldActivate: () => ({ query: "", startIdx: 0 }),
      fetchItems: () => ["one", "two"],
      renderItem: (r) => {
        const el = makeEl("DIV");
        el.textContent = r;
        return el;
      },
      onSelect,
    });
    fire(ta, "input");
    await tick();
    fire(ta, "keydown", { key: "Enter" });
    expect(onSelect).toHaveBeenCalledOnce();
    expect(onSelect.mock.calls[0][0]).toBe("one");
    expect(onSelect.mock.calls[0][2]).toEqual({ query: "", startIdx: 0 });
  });

  it("ArrowDown advances selection and Enter selects the next row", async () => {
    const onSelect = vi.fn();
    ctx.attachAutocomplete(ta, {
      shouldActivate: () => ({ query: "", startIdx: 0 }),
      fetchItems: () => ["one", "two", "three"],
      renderItem: (r) => {
        const el = makeEl("DIV");
        el.textContent = r;
        return el;
      },
      onSelect,
    });
    fire(ta, "input");
    await tick();
    fire(ta, "keydown", { key: "ArrowDown" });
    fire(ta, "keydown", { key: "ArrowDown" });
    fire(ta, "keydown", { key: "Enter" });
    expect(onSelect.mock.calls[0][0]).toBe("three");
  });

  it("ArrowUp wraps around from the first row", async () => {
    const onSelect = vi.fn();
    ctx.attachAutocomplete(ta, {
      shouldActivate: () => ({ query: "", startIdx: 0 }),
      fetchItems: () => ["a", "b", "c"],
      renderItem: (r) => {
        const el = makeEl("DIV");
        el.textContent = r;
        return el;
      },
      onSelect,
    });
    fire(ta, "input");
    await tick();
    fire(ta, "keydown", { key: "ArrowUp" });
    fire(ta, "keydown", { key: "Enter" });
    expect(onSelect.mock.calls[0][0]).toBe("c");
  });

  it("Escape closes the dropdown and calls stopPropagation", async () => {
    const handle = ctx.attachAutocomplete(ta, {
      shouldActivate: () => ({ query: "", startIdx: 0 }),
      fetchItems: () => ["x"],
      renderItem: () => makeEl("DIV"),
      onSelect: () => {},
    });
    fire(ta, "input");
    await tick();
    expect(handle.isOpen()).toBe(true);
    const stop = vi.fn();
    fire(ta, "keydown", { key: "Escape", stopPropagation: stop });
    expect(stop).toHaveBeenCalledOnce();
    expect(handle.isOpen()).toBe(false);
  });

  it("renders the empty message when fetchItems returns no items", async () => {
    ctx.attachAutocomplete(ta, {
      shouldActivate: () => ({ query: "z", startIdx: 0 }),
      fetchItems: () => [],
      renderItem: () => makeEl("DIV"),
      onSelect: () => {},
      emptyMessage: "No matching files",
    });
    fire(ta, "input");
    await tick();
    const dd = ctx.document.body.children.find((n) =>
      n.classList.contains("mention-dropdown"),
    );
    expect(dd).toBeTruthy();
    expect(dd.children).toHaveLength(1);
    expect(dd.children[0].textContent).toBe("No matching files");
    expect(dd.children[0].classList.contains("mention-empty")).toBe(true);
  });

  it("closes instead of rendering empty state when emptyMessage is null", async () => {
    const handle = ctx.attachAutocomplete(ta, {
      shouldActivate: () => ({ query: "z", startIdx: 0 }),
      fetchItems: () => [],
      renderItem: () => makeEl("DIV"),
      onSelect: () => {},
      emptyMessage: null,
    });
    fire(ta, "input");
    await tick();
    expect(handle.isOpen()).toBe(false);
  });

  it("guards against stale async loads overwriting newer renders", async () => {
    // Two input events fire before either fetch resolves. The later fetch
    // resolves first; the older fetch's result must be dropped.
    const resolves = [];
    let call = 0;
    const fetchItems = () =>
      new Promise((r) => {
        const idx = call++;
        resolves.push(() => r([`gen${idx}`]));
      });
    ctx.attachAutocomplete(ta, {
      shouldActivate: () => ({ query: "", startIdx: 0 }),
      fetchItems,
      renderItem: (r) => {
        const el = makeEl("DIV");
        el.textContent = r;
        return el;
      },
      onSelect: () => {},
    });
    fire(ta, "input");
    fire(ta, "input");
    // Resolve the NEWER call first, then the older one.
    resolves[1]();
    await tick(3);
    resolves[0]();
    await tick(3);
    const dd = ctx.document.body.children.find((n) =>
      n.classList.contains("mention-dropdown"),
    );
    expect(dd.children[0].textContent).toBe("gen1");
  });
});
