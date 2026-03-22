/**
 * Frontend unit tests for pure utility functions.
 *
 * Each source file is loaded into an isolated vm context so there is no
 * dependency on a real browser DOM.  Only the minimal browser globals that
 * each script needs at module-evaluation time are provided.
 */
import { describe, it, expect, beforeAll, beforeEach, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

/**
 * Build a contextified sandbox that satisfies the browser globals used by
 * the scripts at load time.  Extra properties can be added via `extra`.
 */
function makeContext(extra = {}) {
  const ctx = {
    // Minimal document stub – getElementById returning null makes any IIFE
    // that checks for DOM elements bail out gracefully.
    document: {
      getElementById: (id) => {
        if (id === "container-monitor-modal") {
          return { addEventListener: () => {} };
        }
        return null;
      },
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: "complete",
      addEventListener: () => {},
    },
    window: {
      matchMedia: () => ({ matches: false, addEventListener: () => {} }),
    },
    localStorage: {
      getItem: () => null,
      setItem: () => {},
    },
    IntersectionObserver: class {
      constructor() {}
      observe() {}
      unobserve() {}
      disconnect() {}
    },
    // Host-environment globals needed for pure logic.
    Date,
    Math,
    console,
    clearInterval: () => {},
    setInterval: () => 0,
    ...extra,
  };
  return vm.createContext(ctx);
}

function loadScript(filename, ctx) {
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

// ---------------------------------------------------------------------------
// Test 1 – escapeHtml (utils.js)
// Verifies that user-supplied strings are safe to embed in HTML.
// ---------------------------------------------------------------------------
describe("escapeHtml", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("utils.js", ctx);
  });

  it('converts <, >, &, and " to their HTML entities', () => {
    expect(ctx.escapeHtml('<b class="x">a & b</b>')).toBe(
      "&lt;b class=&quot;x&quot;&gt;a &amp; b&lt;/b&gt;",
    );
  });
});

// ---------------------------------------------------------------------------
// Test 2 – timeAgo (utils.js)
// Verifies human-readable relative timestamps shown on task cards.
// ---------------------------------------------------------------------------
describe("timeAgo", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("utils.js", ctx);
  });

  it('returns "just now" for a timestamp less than 60 seconds in the past', () => {
    const thirtySecondsAgo = new Date(Date.now() - 30_000).toISOString();
    expect(ctx.timeAgo(thirtySecondsAgo)).toBe("just now");
  });
});

// ---------------------------------------------------------------------------
// Test 3 – formatTimeout (utils.js)
// Verifies the timeout display used in the task settings panel.
// ---------------------------------------------------------------------------
describe("formatTimeout", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("utils.js", ctx);
  });

  it("formats an even number of hours without a minute remainder", () => {
    expect(ctx.formatTimeout(60)).toBe("1h");
    expect(ctx.formatTimeout(120)).toBe("2h");
  });
});

describe("taskDisplayPrompt", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("utils.js", ctx);
  });

  it("prefers execution_prompt for idea-agent tasks", () => {
    const prompt = ctx.taskDisplayPrompt({
      kind: "idea-agent",
      prompt: "Analyzes the workspace and proposes 3 actionable improvements.",
      execution_prompt: "Generated brainstorm prompt with synthesized domains.",
    });
    expect(prompt).toBe(
      "Generated brainstorm prompt with synthesized domains.",
    );
  });

  it("falls back to prompt for non-idea tasks", () => {
    const prompt = ctx.taskDisplayPrompt({
      kind: "task",
      prompt: "Implement OAuth callback validation.",
      execution_prompt: "ignored for normal tasks",
    });
    expect(prompt).toBe("Implement OAuth callback validation.");
  });
});

// ---------------------------------------------------------------------------
// Test 4 – getResolvedTheme (theme.js)
// Verifies that explicit 'dark' / 'light' values are returned as-is without
// consulting the OS colour-scheme media query.
// ---------------------------------------------------------------------------
describe("getResolvedTheme", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("theme.js", ctx);
  });

  it("returns explicit mode values without querying matchMedia", () => {
    expect(ctx.getResolvedTheme("dark")).toBe("dark");
    expect(ctx.getResolvedTheme("light")).toBe("light");
  });
});

// ---------------------------------------------------------------------------
// Test 5 – containerStateColor (containers.js)
// Verifies the status-dot colours shown in the container monitor modal.
// ---------------------------------------------------------------------------
describe("containerStateColor", () => {
  let ctx;
  beforeAll(() => {
    // containers.js references escapeHtml inside renderContainers (not at
    // load time), so a stub is enough to avoid ReferenceError if any path
    // ever reaches it during test setup.
    ctx = makeContext({ escapeHtml: (s) => String(s ?? "") });
    loadScript("containers.js", ctx);
  });

  it("maps known container states to their designated hex colours", () => {
    expect(ctx.containerStateColor("running")).toBe("#45b87a");
    expect(ctx.containerStateColor("dead")).toBe("#d46868");
    expect(ctx.containerStateColor("paused")).toBe("#d4a030");
    expect(ctx.containerStateColor(null)).toBe("#9c9890"); // default / unknown
  });
});

// ---------------------------------------------------------------------------
// Test 6 – updateMaxParallelTag (render.js)
// Verifies that the "max N" badge in the In Progress column header reflects
// the current maxParallelTasks global and responds to changes so that the UI
// stays in sync when system settings are updated.
// ---------------------------------------------------------------------------
describe("updateMaxParallelTag", () => {
  let ctx;
  let tagEl;

  beforeAll(() => {
    tagEl = {
      textContent: "",
      classList: {
        _hidden: true,
        add(cls) {
          if (cls === "hidden") this._hidden = true;
        },
        remove(cls) {
          if (cls === "hidden") this._hidden = false;
        },
      },
    };

    ctx = makeContext({
      document: {
        getElementById: (id) => {
          if (id === "max-parallel-tag") return tagEl;
          if (id === "container-monitor-modal")
            return { addEventListener: () => {} };
          return null;
        },
        querySelectorAll: () => ({ forEach: () => {} }),
        documentElement: { setAttribute: () => {} },
        readyState: "complete",
        addEventListener: () => {},
      },
    });

    loadScript("state.js", ctx);
    loadScript("render.js", ctx);
  });

  // Use vm.runInContext to assign into the let binding created by state.js.
  // Direct ctx property assignment would create a shadowed property, not
  // update the let binding that the function closes over.
  function setMax(n) {
    vm.runInContext(`maxParallelTasks = ${n};`, ctx);
  }

  it('shows "max N" and removes hidden class when maxParallelTasks > 0', () => {
    setMax(5);
    ctx.updateMaxParallelTag();
    expect(tagEl.textContent).toBe("max 5");
    expect(tagEl.classList._hidden).toBe(false);
  });

  it("updates the label when maxParallelTasks changes (simulates settings save)", () => {
    setMax(10);
    ctx.updateMaxParallelTag();
    expect(tagEl.textContent).toBe("max 10");
    expect(tagEl.classList._hidden).toBe(false);
  });

  it("hides the tag when maxParallelTasks is 0 (not yet loaded)", () => {
    setMax(0);
    ctx.updateMaxParallelTag();
    expect(tagEl.classList._hidden).toBe(true);
  });
});

describe("backlog impact sorting helpers", () => {
  let ctx;

  beforeAll(() => {
    ctx = makeContext();
    loadScript("state.js", ctx);
    loadScript("render.js", ctx);
  });

  it("reads impact score from idea-agent tags", () => {
    expect(ctx.getTaskImpactScore({ tags: ["idea-agent", "impact:87"] })).toBe(
      87,
    );
    expect(ctx.getTaskImpactScore({ tags: ["impact: not-a-number"] })).toBe(
      null,
    );
  });

  it("sorts scored backlog tasks by impact descending and keeps manual fallback order", () => {
    const tasks = [
      { id: "manual", position: 0, tags: [] },
      { id: "low", position: 2, tags: ["impact:40"] },
      { id: "high", position: 1, tags: ["idea-agent", "impact:90"] },
      { id: "none", position: 3, tags: ["idea-agent"] },
    ];
    vm.runInContext(`backlogSortMode = 'impact';`, ctx);
    const sorted = ctx.sortBacklogTasks(tasks.slice()).map((task) => task.id);
    expect(sorted).toEqual(["high", "low", "manual", "none"]);
  });

  it("falls back to saved manual priority order when impact sort is disabled", () => {
    const tasks = [
      { id: "b", position: 1, tags: ["impact:90"] },
      { id: "a", position: 0, tags: ["impact:20"] },
    ];
    vm.runInContext(`backlogSortMode = 'manual';`, ctx);
    const sorted = ctx.sortBacklogTasks(tasks.slice()).map((task) => task.id);
    expect(sorted).toEqual(["a", "b"]);
  });
});

// ---------------------------------------------------------------------------
// Test 7 – buildCardActions: Start button disabled during refinement (render.js)
// Verifies that the Start button on backlog cards is disabled when a refinement
// job is actively running, preventing accidental task start mid-refinement.
// ---------------------------------------------------------------------------
describe("buildCardActions refinement guard", () => {
  let ctx;

  beforeAll(() => {
    ctx = makeContext({
      escapeHtml: (s) => String(s ?? ""),
      maxParallelTasks: 0,
      // render.js uses these globals at various points; provide stubs
      fetchBehindCount: () => {},
    });
    loadScript("state.js", ctx);
    loadScript("render.js", ctx);
  });

  it("Start button is enabled when there is no current_refinement", () => {
    const task = { id: "abc", status: "backlog", current_refinement: null };
    const html = ctx.buildCardActions(task);
    // disabled attribute must not be present
    expect(html).not.toMatch(/disabled/);
    expect(html).toContain("card-action-start");
  });

  it("Start button is disabled when refinement is done (requires review)", () => {
    const task = {
      id: "abc",
      status: "backlog",
      current_refinement: { status: "done" },
    };
    const html = ctx.buildCardActions(task);
    expect(html).toContain("disabled");
    expect(html).toContain("card-action-start");
  });

  it("Start button is disabled when refinement is running", () => {
    const task = {
      id: "abc",
      status: "backlog",
      current_refinement: { status: "running" },
    };
    const html = ctx.buildCardActions(task);
    expect(html).toContain("disabled");
    expect(html).toContain("card-action-start");
  });

  it("Start button is enabled when refinement has failed", () => {
    const task = {
      id: "abc",
      status: "backlog",
      current_refinement: { status: "failed" },
    };
    const html = ctx.buildCardActions(task);
    expect(html).not.toMatch(/disabled/);
    expect(html).toContain("card-action-start");
  });
});

// ---------------------------------------------------------------------------
// Test 8 – populateDependsOnPicker ordering (tasks.js)
// Verifies that tasks in the dep-picker are ordered by status priority:
// in_progress → waiting → backlog → done.
// ---------------------------------------------------------------------------
describe("populateDependsOnPicker status ordering", () => {
  let ctx;
  let appendedItems;

  beforeEach(() => {
    appendedItems = [];

    function makeEl() {
      const el = {
        _children: [],
        className: "",
        textContent: "",
        type: "",
        value: "",
        checked: false,
        innerHTML: "",
        style: {},
        classList: {
          toggle: () => {},
          add: () => {},
          remove: () => {},
          contains: () => false,
        },
        appendChild(child) {
          this._children.push(child);
        },
        addEventListener() {},
        closest() {
          return null;
        },
        querySelector(sel) {
          if (sel === ".dep-picker-item-text") return el._children[1] || null;
          return null;
        },
        querySelectorAll() {
          return { forEach: () => {} };
        },
      };
      return el;
    }

    const listEl = makeEl();
    listEl.appendChild = function (child) {
      appendedItems.push(child);
    };

    const chipsEl = makeEl();

    const wrapEl = makeEl();
    wrapEl.querySelector = function (sel) {
      if (sel === ".dep-picker-list") return listEl;
      if (sel === ".dep-picker-search") return null;
      if (sel === ".dep-picker-chips") return chipsEl;
      return null;
    };
    wrapEl.querySelectorAll = function (sel) {
      if (sel.includes("input[type=checkbox]:checked")) return [];
      return [];
    };
    wrapEl.dataset = {};

    ctx = makeContext({
      document: {
        getElementById(id) {
          if (id === "dep-picker") return wrapEl;
          if (id === "container-monitor-modal")
            return { addEventListener: () => {} };
          // tasks.js attaches event listeners to these at module load time
          if (id === "modal-edit-prompt") return { addEventListener: () => {} };
          if (id === "modal-edit-goal") return { addEventListener: () => {} };
          if (id === "modal-edit-timeout")
            return { addEventListener: () => {} };
          return null;
        },
        createElement() {
          return makeEl();
        },
        querySelectorAll: () => ({ forEach: () => {} }),
        documentElement: { setAttribute: () => {} },
        readyState: "complete",
        addEventListener: () => {},
      },
    });

    loadScript("tasks.js", ctx);

    vm.runInContext(
      `
      tasks = [
        { id: '1', status: 'done',        prompt: 'done task',        title: 'Done Task' },
        { id: '2', status: 'backlog',      prompt: 'backlog task',     title: 'Backlog Task' },
        { id: '3', status: 'waiting',      prompt: 'waiting task',     title: 'Waiting Task' },
        { id: '4', status: 'in_progress',  prompt: 'in progress task', title: 'In Progress Task' },
      ];
    `,
      ctx,
    );
  });

  it("orders items as in_progress → waiting → backlog → done", () => {
    ctx.populateDependsOnPicker("dep-picker", null, []);
    // Each appended item is a label; its 3rd child (index 2) is the badge
    // with className 'badge badge-<status>'.
    const statuses = appendedItems.map(function (item) {
      const badge = item._children[2];
      return badge.className.replace("badge badge-", "");
    });
    expect(statuses).toEqual(["in_progress", "waiting", "backlog", "done"]);
  });
});

// ---------------------------------------------------------------------------
// Test 9 – invalidateDiffBehindCounts and fetchDiff TTL (render.js)
// Verifies that:
//   (a) invalidateDiffBehindCounts(taskId) only zeroes the specified entry;
//   (b) invalidateDiffBehindCounts() with no arg zeroes every entry;
//   (c) fetchDiff skips the network when the cached behind-count is fresh;
//   (d) fetchDiff re-fetches when the behind-count has expired (> BEHIND_TTL_MS old).
// ---------------------------------------------------------------------------
describe("invalidateDiffBehindCounts and fetchDiff TTL", () => {
  let ctx;
  let apiCalls;

  beforeEach(() => {
    apiCalls = [];
    ctx = makeContext({
      // Provide a stub api() that records paths and returns minimal diff data.
      api: async (path) => {
        apiCalls.push(path);
        return { diff: "stub", behind_counts: {} };
      },
      // render.js uses escapeHtml inside applyDiffToCard; provide a stub so
      // loading the script doesn't throw if any path exercises it.
      escapeHtml: (s) => String(s ?? ""),
      requestAnimationFrame: (fn) => {
        fn();
      },
    });
    loadScript("state.js", ctx);
    loadScript("render.js", ctx);
    // Start each test with an empty cache.
    vm.runInContext("diffCache.clear();", ctx);
  });

  it("invalidateDiffBehindCounts(taskId) zeroes only the named entry", () => {
    vm.runInContext(
      `
      diffCache.set('task-a', { diff: '', behindCounts: {}, updatedAt: 'ts1', behindFetchedAt: 12345 });
      diffCache.set('task-b', { diff: '', behindCounts: {}, updatedAt: 'ts2', behindFetchedAt: 67890 });
    `,
      ctx,
    );

    ctx.invalidateDiffBehindCounts("task-a");

    const a = vm.runInContext('diffCache.get("task-a").behindFetchedAt', ctx);
    const b = vm.runInContext('diffCache.get("task-b").behindFetchedAt', ctx);
    expect(a).toBe(0);
    expect(b).toBe(67890);
  });

  it("invalidateDiffBehindCounts() with no argument zeroes every cached entry", () => {
    vm.runInContext(
      `
      diffCache.set('task-a', { diff: '', behindCounts: {}, updatedAt: 'ts1', behindFetchedAt: 11111 });
      diffCache.set('task-b', { diff: '', behindCounts: {}, updatedAt: 'ts2', behindFetchedAt: 22222 });
    `,
      ctx,
    );

    ctx.invalidateDiffBehindCounts();

    const a = vm.runInContext('diffCache.get("task-a").behindFetchedAt', ctx);
    const b = vm.runInContext('diffCache.get("task-b").behindFetchedAt', ctx);
    expect(a).toBe(0);
    expect(b).toBe(0);
  });

  it("fetchDiff skips the network request when cached entry is fresh", async () => {
    // Seed the cache with a timestamp that is well within BEHIND_TTL_MS.
    vm.runInContext(
      `
      diffCache.set('task-x', { diff: 'cached', behindCounts: {}, updatedAt: 'ts-x', behindFetchedAt: Date.now() });
    `,
      ctx,
    );
    const card = { querySelector: () => null };

    await ctx.fetchDiff(card, "task-x", "ts-x");

    expect(apiCalls).toHaveLength(0);
  });

  it("fetchDiff re-fetches when the behind-count has aged past BEHIND_TTL_MS", async () => {
    // Seed the cache with a behindFetchedAt that is one millisecond beyond the TTL.
    vm.runInContext(
      `
      diffCache.set('task-y', { diff: 'old', behindCounts: {}, updatedAt: 'ts-y', behindFetchedAt: Date.now() - BEHIND_TTL_MS - 1 });
    `,
      ctx,
    );
    const card = { querySelector: () => null };

    await ctx.fetchDiff(card, "task-y", "ts-y");

    expect(apiCalls).toHaveLength(1);
    expect(apiCalls[0]).toContain("task-y");
  });
});
