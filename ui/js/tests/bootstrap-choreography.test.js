/**
 * Tests for ui/js/bootstrap-choreography.js — the first-spec bootstrap
 * sequence that fires when a chat-first workspace receives its first
 * non-empty spec-tree snapshot.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "bootstrap-choreography.js"), "utf8");

function makeEl(tag) {
  const attrs = new Map();
  const classes = new Set();
  const listeners = new Map();
  const children = [];
  const el = {
    tagName: String(tag || "div").toUpperCase(),
    style: {},
    textContent: "",
    children,
    classList: {
      add: (c) => classes.add(c),
      remove: (c) => classes.delete(c),
      contains: (c) => classes.has(c),
      toggle: (c, force) => {
        if (force === true) classes.add(c);
        else if (force === false) classes.delete(c);
        else (classes.has(c) ? classes.delete(c) : classes.add(c));
      },
    },
    _classes: classes,
    get className() {
      return Array.from(classes).join(" ");
    },
    set className(v) {
      classes.clear();
      String(v || "")
        .split(/\s+/)
        .filter(Boolean)
        .forEach((c) => classes.add(c));
    },
    setAttribute: (k, v) => attrs.set(k, v),
    getAttribute: (k) => (attrs.has(k) ? attrs.get(k) : null),
    hasAttribute: (k) => attrs.has(k),
    appendChild: (c) => {
      children.push(c);
      c.parentNode = el;
      return c;
    },
    removeChild: (c) => {
      const i = children.indexOf(c);
      if (i >= 0) children.splice(i, 1);
      c.parentNode = null;
      return c;
    },
    addEventListener: (type, fn) => {
      if (!listeners.has(type)) listeners.set(type, []);
      listeners.get(type).push(fn);
    },
    dispatchEvent: (type) => {
      const fns = listeners.get(type) || [];
      for (const fn of fns) fn({ type, target: el });
    },
    parentNode: null,
  };
  return el;
}

function makeContext({ reducedMotion = false, focusSpec } = {}) {
  const body = makeEl("body");
  const timers = [];
  const ctx = {
    console,
    document: {
      body,
      createElement: (t) => makeEl(t),
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
    focusSpec: focusSpec || vi.fn(),
    activeWorkspaces: ["/workspace/repo"],
    _timers: timers,
    _body: body,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

function fireDue(ctx, maxMs) {
  // Fire timers whose delay is <= maxMs and that haven't fired yet,
  // in the order they were scheduled.
  for (const t of ctx._timers) {
    if (!t.fired && t.ms <= maxMs) {
      t.fired = true;
      t.fn();
    }
  }
}

function latestToast(ctx) {
  const kids = ctx._body.children;
  return kids.length > 0 ? kids[kids.length - 1] : null;
}

describe("BootstrapChoreography", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
    ctx.BootstrapChoreography.__resetForTests();
  });

  it("TestChoreography_AutoFocusAt130ms — focusSpec fires at the 130ms mark", () => {
    ctx.BootstrapChoreography.trigger("specs/local/first.md");
    // Before any timer fires, focusSpec is still unused.
    expect(ctx.focusSpec).not.toHaveBeenCalled();
    fireDue(ctx, ctx.BootstrapChoreography.AUTO_FOCUS_DELAY_MS);
    expect(ctx.focusSpec).toHaveBeenCalledTimes(1);
    expect(ctx.focusSpec).toHaveBeenCalledWith(
      "specs/local/first.md",
      "/workspace/repo",
    );
  });

  it("TestChoreography_ToastAppearsAt160ms — toast mounts with the expected text", () => {
    ctx.BootstrapChoreography.trigger("specs/local/first.md");
    expect(latestToast(ctx)).toBeNull();
    fireDue(ctx, ctx.BootstrapChoreography.TOAST_DELAY_MS);
    const toast = latestToast(ctx);
    expect(toast).not.toBeNull();
    expect(toast.className).toContain("bootstrap-toast");
    expect(toast.textContent).toContain("specs/local/first.md");
    expect(toast.textContent).toContain("first spec was created");
  });

  it("toast auto-dismisses after TOAST_DISMISS_MS", () => {
    ctx.BootstrapChoreography.trigger("specs/local/first.md");
    fireDue(ctx, ctx.BootstrapChoreography.TOAST_DELAY_MS);
    expect(latestToast(ctx)).not.toBeNull();
    // The dismissal timer is scheduled when the toast is shown.
    fireDue(
      ctx,
      ctx.BootstrapChoreography.TOAST_DELAY_MS +
        ctx.BootstrapChoreography.TOAST_DISMISS_MS +
        1,
    );
    expect(latestToast(ctx)).toBeNull();
  });

  it("clicking the toast dismisses it immediately", () => {
    ctx.BootstrapChoreography.trigger("specs/local/first.md");
    fireDue(ctx, ctx.BootstrapChoreography.TOAST_DELAY_MS);
    const toast = latestToast(ctx);
    expect(toast).not.toBeNull();
    toast.dispatchEvent("click");
    expect(latestToast(ctx)).toBeNull();
  });

  it("TestChoreography_ReducedMotion — toast still appears, no-motion class set", () => {
    ctx = makeContext({ reducedMotion: true });
    ctx.BootstrapChoreography.__resetForTests();
    ctx.BootstrapChoreography.trigger("specs/local/first.md");
    fireDue(ctx, ctx.BootstrapChoreography.TOAST_DELAY_MS);
    const toast = latestToast(ctx);
    expect(toast).not.toBeNull();
    expect(toast.className).toContain("bootstrap-toast--no-motion");
  });

  it("trigger is idempotent — a second call in the same session is a no-op", () => {
    ctx.BootstrapChoreography.trigger("specs/local/first.md");
    ctx.BootstrapChoreography.trigger("specs/local/other.md");
    fireDue(ctx, ctx.BootstrapChoreography.TOAST_DELAY_MS);
    const toast = latestToast(ctx);
    expect(toast.textContent).toContain("specs/local/first.md");
    expect(toast.textContent).not.toContain("specs/local/other.md");
    // focusSpec also only fired once — with the first path.
    expect(ctx.focusSpec).toHaveBeenCalledTimes(1);
    expect(ctx.focusSpec.mock.calls[0][0]).toBe("specs/local/first.md");
  });

  it("trigger with an empty path is a no-op", () => {
    ctx.BootstrapChoreography.trigger("");
    fireDue(ctx, ctx.BootstrapChoreography.TOAST_DELAY_MS);
    expect(latestToast(ctx)).toBeNull();
    expect(ctx.focusSpec).not.toHaveBeenCalled();
  });

  it("dismiss() removes an active toast", () => {
    ctx.BootstrapChoreography.trigger("specs/local/first.md");
    fireDue(ctx, ctx.BootstrapChoreography.TOAST_DELAY_MS);
    expect(latestToast(ctx)).not.toBeNull();
    ctx.BootstrapChoreography.dismiss();
    expect(latestToast(ctx)).toBeNull();
  });

  it("__resetForTests allows re-triggering in subsequent tests", () => {
    ctx.BootstrapChoreography.trigger("specs/local/a.md");
    fireDue(ctx, ctx.BootstrapChoreography.TOAST_DELAY_MS);
    ctx.BootstrapChoreography.__resetForTests();
    ctx.BootstrapChoreography.trigger("specs/local/b.md");
    fireDue(ctx, ctx.BootstrapChoreography.TOAST_DELAY_MS);
    const toast = latestToast(ctx);
    expect(toast.textContent).toContain("specs/local/b.md");
  });
});
