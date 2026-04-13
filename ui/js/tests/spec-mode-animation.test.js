/**
 * Tests for focused-view crossfade (focused-view-crossfade spec).
 *
 * The actual animation is CSS-driven so we cannot observe mid-frame
 * opacity values without a browser. These tests instead exercise the
 * JS-visible touchpoints:
 *   - _scheduleFocusedCrossfade sets opacity:0, runs replaceFn after
 *     the 40ms outgoing window, then schedules opacity:1 on the next
 *     animation frame.
 *   - Epoch guard: a second crossfade started during the first
 *     abandons the first's fade-in so the latest content is what ends
 *     up at opacity 1.
 *   - prefers-reduced-motion bypasses the timers entirely — replaceFn
 *     runs synchronously and no inline transition style is left
 *     behind.
 *   - The .spec-focused-view--index class is the only toggle the CSS
 *     uses to hide spec-only affordances; JS sets it on focus of the
 *     Roadmap and clears it on focus of a regular spec.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-mode.js"), "utf8");

function makeEl(tag, id, registry) {
  const classes = new Set();
  const attrs = new Map();
  const style = {};
  const el = {
    tagName: tag,
    id,
    innerHTML: "",
    textContent: "",
    style,
    className: "",
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
    setAttribute: (k, v) => attrs.set(k, v),
    getAttribute: (k) => (attrs.has(k) ? attrs.get(k) : null),
    hasAttribute: (k) => attrs.has(k),
    removeAttribute: (k) => attrs.delete(k),
    _classes: classes,
    _attrs: attrs,
  };
  registry.set(id, el);
  return el;
}

function makeContext(opts = {}) {
  const registry = new Map();
  const ids = [
    "sidebar-nav-board",
    "sidebar-nav-spec",
    "sidebar-nav-docs",
    "board",
    "spec-mode-container",
    "spec-focused-view",
    "spec-focused-title",
    "spec-focused-status",
    "spec-focused-kind",
    "spec-focused-effort",
    "spec-focused-meta",
    "spec-focused-body",
    "spec-focused-body-inner",
    "spec-dispatch-btn",
    "spec-summarize-btn",
    "spec-archive-btn",
    "spec-unarchive-btn",
    "spec-archived-banner",
    "docs-mode-container",
    "explorer-panel",
  ];
  for (const id of ids) makeEl("DIV", id, registry);

  const timers = [];
  const rafs = [];
  const storage = new Map();

  const reducedMotion = !!opts.reducedMotion;
  const ctx = {
    console,
    document: {
      getElementById: (id) => registry.get(id) || null,
      addEventListener: () => {},
    },
    localStorage: {
      getItem: (k) => storage.get(k) ?? null,
      setItem: (k, v) => storage.set(k, v),
    },
    window: {
      matchMedia: (q) => ({
        matches:
          q === "(prefers-reduced-motion: reduce)" ? reducedMotion : false,
      }),
    },
    setTimeout: (fn, ms) => {
      timers.push({ fn, ms, fired: false });
      return timers.length;
    },
    clearTimeout: (id) => {
      const t = timers[id - 1];
      if (t) t.fired = true;
    },
    setInterval: () => 0,
    clearInterval: () => {},
    requestAnimationFrame: (fn) => {
      rafs.push(fn);
      return rafs.length;
    },
    fetch: () => Promise.reject(new Error("stubbed")),
    Routes: { explorer: { readFile: () => "/api/explorer/file" } },
    withBearerHeaders: () => ({}),
    renderMarkdown: (s) => String(s || ""),
    _mdRender: { enhanceMarkdown: () => Promise.resolve() },
    buildFloatingToc: () => {},
    teardownFloatingToc: () => {},
    location: { hash: "", pathname: "/" },
    history: { replaceState: () => {} },
    showConfirm: () => Promise.resolve(true),
    showAlert: () => {},
    Promise,
    registry,
    _timers: timers,
    _rafs: rafs,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

function drainTimers(ctx) {
  // Fire every pending setTimeout callback once.
  for (const t of ctx._timers) {
    if (!t.fired) {
      t.fired = true;
      t.fn();
    }
  }
}

function drainRafs(ctx) {
  const snapshot = ctx._rafs.slice();
  ctx._rafs.length = 0;
  for (const fn of snapshot) fn();
}

// ---------------------------------------------------------------------------
// _scheduleFocusedCrossfade — outgoing fade, content swap, fade-in
// ---------------------------------------------------------------------------

describe("_scheduleFocusedCrossfade", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
  });

  it("TestFocusedViewCrossfade_FadesOutOld — drops opacity to 0 immediately", () => {
    ctx._scheduleFocusedCrossfade(() => {});
    const inner = ctx.registry.get("spec-focused-body-inner");
    expect(inner.style.opacity).toBe("0");
    // Transition style captures the accelerate curve.
    expect(inner.style.transition).toContain("140ms");
    expect(inner.style.transition).toContain("cubic-bezier(0.3, 0, 0.8, 0.15)");
  });

  it("TestFocusedViewCrossfade_FadesInNew — replace runs after 40ms then opacity → 1", () => {
    const replace = vi.fn();
    ctx._scheduleFocusedCrossfade(replace);
    // Before the 40ms timer fires, replaceFn has not been called.
    expect(replace).not.toHaveBeenCalled();
    drainTimers(ctx);
    expect(replace).toHaveBeenCalledTimes(1);
    // The rAF tick schedules the fade-in.
    drainRafs(ctx);
    const inner = ctx.registry.get("spec-focused-body-inner");
    expect(inner.style.opacity).toBe("1");
    expect(inner.style.transition).toContain("180ms");
    expect(inner.style.transition).toContain("cubic-bezier(0.2, 0, 0, 1)");
  });

  it("TestFocusedViewCrossfade_ClickSpam — only the latest swap wins", () => {
    const first = vi.fn();
    const second = vi.fn();
    const third = vi.fn();
    ctx._scheduleFocusedCrossfade(first);
    ctx._scheduleFocusedCrossfade(second);
    ctx._scheduleFocusedCrossfade(third);
    drainTimers(ctx);
    drainRafs(ctx);
    // First and second replace callbacks are dropped (their epoch is
    // stale by the time the 40ms timer fires).
    expect(first).not.toHaveBeenCalled();
    expect(second).not.toHaveBeenCalled();
    expect(third).toHaveBeenCalledTimes(1);
    const inner = ctx.registry.get("spec-focused-body-inner");
    expect(inner.style.opacity).toBe("1");
  });

  it("TestCrossfade_ReducedMotion — resolves synchronously with no timers", () => {
    ctx = makeContext({ reducedMotion: true });
    const replace = vi.fn();
    ctx._scheduleFocusedCrossfade(replace);
    // Replacement happened inline — no need to drain timers.
    expect(replace).toHaveBeenCalledTimes(1);
    const inner = ctx.registry.get("spec-focused-body-inner");
    // No inline opacity/transition is left behind: the default (1) holds.
    expect(inner.style.opacity).toBe("");
    expect(inner.style.transition).toBe("");
  });
});

// ---------------------------------------------------------------------------
// Spec-affordance hiding toggle — .spec-focused-view--index class.
// ---------------------------------------------------------------------------

describe("focusRoadmapIndex / focusSpec affordance toggle", () => {
  const INDEX_META = {
    path: "specs/README.md",
    workspace: "/ws",
    title: "Custom",
  };

  it("TestAffordances_HiddenOnIndex — marker class applied when roadmap focused", () => {
    const ctx = makeContext();
    ctx.fetch = () =>
      Promise.resolve({ ok: true, text: () => Promise.resolve("") });
    ctx.focusRoadmapIndex(INDEX_META);
    const view = ctx.registry.get("spec-focused-view");
    expect(view._classes.has("spec-focused-view--index")).toBe(true);
  });

  it("TestAffordances_AppearOnSpec — marker class cleared when a spec focuses", () => {
    const ctx = makeContext();
    ctx.fetch = () =>
      Promise.resolve({ ok: true, text: () => Promise.resolve("") });
    ctx.focusRoadmapIndex(INDEX_META);
    const view = ctx.registry.get("spec-focused-view");
    expect(view._classes.has("spec-focused-view--index")).toBe(true);
    ctx.focusSpec("specs/local/foo.md", "/ws");
    expect(view._classes.has("spec-focused-view--index")).toBe(false);
  });
});
