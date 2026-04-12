/**
 * Tests for ui/js/sidebar-badge.js — the sidebar Board nav unread dot
 * that signals "new tasks you haven't seen" while the user is in Plan.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "sidebar-badge.js"), "utf8");

function makeDot() {
  const attrs = new Map();
  // Start hidden to match the HTML default.
  attrs.set("hidden", "");
  return {
    tagName: "SPAN",
    hasAttribute: (k) => attrs.has(k),
    setAttribute: (k, v) => attrs.set(k, v),
    removeAttribute: (k) => attrs.delete(k),
    _attrs: attrs,
  };
}

function makeContext(overrides = {}) {
  const dot = makeDot();
  const ctx = {
    console,
    document: {
      getElementById: (id) =>
        id === "sidebar-board-unread-dot" ? dot : null,
    },
    getCurrentMode: () => "spec",
    tasks: [],
    _dot: dot,
    ...overrides,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

function dotVisible(ctx) {
  return !ctx._dot.hasAttribute("hidden");
}

describe("sidebar-badge", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
  });

  it("starts hidden before the first snapshot", () => {
    expect(dotVisible(ctx)).toBe(false);
  });

  it("initBoardUnreadSeen hides the dot and seeds the seen-set", () => {
    ctx.initBoardUnreadSeen(["a", "b"]);
    expect(dotVisible(ctx)).toBe(false);
    // An existing task from the snapshot does not trigger the dot when
    // surfaced as "new" again by a flaky reducer.
    ctx.noteBoardNewTask("a");
    expect(dotVisible(ctx)).toBe(false);
  });

  it("TestBadge_AppearsOnNewTask — new task in Plan mode shows the dot", () => {
    ctx.initBoardUnreadSeen([]);
    ctx.noteBoardNewTask("fresh-1");
    expect(dotVisible(ctx)).toBe(true);
  });

  it("does not show the dot when the user is already in Board mode", () => {
    ctx = makeContext({ getCurrentMode: () => "board" });
    ctx.initBoardUnreadSeen([]);
    ctx.noteBoardNewTask("fresh-1");
    expect(dotVisible(ctx)).toBe(false);
  });

  it("TestBadge_ClearsOnBoardMode — clearBoardUnreadDot hides the dot", () => {
    ctx.initBoardUnreadSeen([]);
    ctx.noteBoardNewTask("fresh-1");
    expect(dotVisible(ctx)).toBe(true);
    ctx.clearBoardUnreadDot();
    expect(dotVisible(ctx)).toBe(false);
  });

  it("clearBoardUnreadDot also marks all current tasks as seen", () => {
    ctx = makeContext({
      tasks: [{ id: "t1" }, { id: "t2" }],
      getCurrentMode: () => "spec",
    });
    ctx.initBoardUnreadSeen([]);
    ctx.clearBoardUnreadDot();
    // After clearing, re-emitting t1/t2 as "new" must not re-raise the dot.
    ctx.noteBoardNewTask("t1");
    ctx.noteBoardNewTask("t2");
    expect(dotVisible(ctx)).toBe(false);
  });

  it("TestBadge_PersistsAcrossModeSwitches — dot persists until Board is entered", () => {
    ctx.initBoardUnreadSeen([]);
    ctx.noteBoardNewTask("fresh-1");
    expect(dotVisible(ctx)).toBe(true);
    // A second spec-mode switch doesn't clear the dot.
    ctx.noteBoardNewTask("fresh-2");
    expect(dotVisible(ctx)).toBe(true);
    // Only clearBoardUnreadDot (called from switchMode when mode === "board")
    // dismisses it.
    ctx.clearBoardUnreadDot();
    expect(dotVisible(ctx)).toBe(false);
  });

  it("noteBoardNewTask before init is absorbed into the seen-set", () => {
    // Simulate an early task-updated event that arrives before the first
    // snapshot — must not raise the dot.
    ctx.noteBoardNewTask("early");
    expect(dotVisible(ctx)).toBe(false);
    // After init, re-emitting "early" is still treated as seen.
    ctx.initBoardUnreadSeen([]);
    ctx.noteBoardNewTask("early");
    // Fresh init resets the set — so this second emission DOES raise.
    expect(dotVisible(ctx)).toBe(true);
  });
});
