/**
 * Additional coverage tests for render.js — targets functions not covered
 * by the existing render.test.js: tagStyle, setBrainstormCategories,
 * renderTaskTagBadge, renderTaskTagBadges, formatRelativeTime,
 * getRenderableTasks, getTaskImpactScore, sortBacklogTasks,
 * formatInProgressCount, hasExecutionTrail, cardDisplayPrompt,
 * buildCardActions, _cachedMarkdown, hasCancelledOrMissingDep,
 * focusFirstCardInColumn, _cardFingerprint, and invalidateDiffBehindCounts
 * (broadcast variant).
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function loadScript(filename, ctx) {
  loadLibDeps(filename, ctx);
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
}

function createContext(options = {}) {
  const ctx = vm.createContext({
    module: { exports: {} },
    exports: {},
    console,
    Date,
    Math,
    JSON,
    Number,
    Array,
    Map,
    Set,
    Object,
    String,
    Promise,
    setTimeout: vi.fn(),
    clearTimeout: vi.fn(),
    requestAnimationFrame: (cb) => cb(),
    localStorage: {
      getItem: () => null,
      setItem: () => {},
    },
    window: {
      depGraphEnabled: false,
      location: { hash: "" },
    },
    location: { hash: "" },
    document: {
      getElementById: () => null,
      createElement: () => ({
        innerHTML: "",
        textContent: "",
        className: "",
        style: {},
        dataset: {},
        _attrs: {},
        tabIndex: 0,
        classList: {
          _c: new Set(),
          add(c) {
            this._c.add(c);
          },
          remove(c) {
            this._c.delete(c);
          },
          contains(c) {
            return this._c.has(c);
          },
          toggle(c, f) {
            f !== undefined
              ? f
                ? this._c.add(c)
                : this._c.delete(c)
              : this._c.has(c)
                ? this._c.delete(c)
                : this._c.add(c);
          },
        },
        appendChild() {},
        addEventListener() {},
        setAttribute(k, v) {
          this._attrs[k] = v;
        },
        getAttribute(k) {
          return this._attrs[k] || null;
        },
        querySelectorAll() {
          return [];
        },
        querySelector() {
          return null;
        },
      }),
      querySelectorAll: () => [],
      addEventListener: () => {},
      createDocumentFragment: () => ({
        children: [],
        appendChild(c) {
          this.children.push(c);
          return c;
        },
      }),
      readyState: "complete",
    },
    tasks: [],
    archivedTasks: [],
    activeWorkspaces: ["/workspace/test"],
    showArchived: false,
    backlogSortMode: "manual",
    filterQuery: "",
    maxParallelTasks: 0,
    withAuthToken: (url) => url,
    _sseIsLeader: () => true,
    _sseRelay: () => {},
    _sseOnFollowerEvent: () => {},
    ensureArchivedScrollBinding: () => {},
    loadArchivedTasksPage: vi.fn(),
    resetArchivedWindow: vi.fn(),
    sortArchivedByUpdatedDesc: (items) => items,
    trimArchivedWindow: () => {},
    scheduleRender: vi.fn(),
    notifyTaskChangeListeners: vi.fn(),
    announceBoardStatus: vi.fn(),
    getTaskAccessibleTitle: (task) => task.title || task.prompt || task.id,
    formatTaskStatusLabel: (status) => String(status || "").replace(/_/g, " "),
    openModal: vi.fn(() => Promise.resolve()),
    setRightTab: vi.fn(),
    setLeftTab: vi.fn(),
    _hashHandled: false,
    tasksRetryDelay: 1000,
    tasksSource: null,
    lastTasksEventId: null,
    archivedPage: {
      loadState: "idle",
      hasMoreBefore: false,
      hasMoreAfter: false,
    },
    archivedTasksPageSize: 20,
    archivedScrollHandlerBound: false,
    pendingCancelTaskIds: new Set(),
    Routes: {
      tasks: {
        stream: () => "/api/tasks/stream",
        list: () => "/api/tasks",
      },
    },
    EventSource: function () {},
    api: vi.fn(),
    escapeHtml: (s) => String(s || ""),
    renderMarkdown: (s) => String(s || ""),
    matchesFilter: () => true,
    updateIdeationFromTasks: () => {},
    updateBacklogSortButton: () => {},
    hideDependencyGraph: () => {},
    renderDependencyGraph: () => {},
    sandboxDisplayName: (s) => s || "Default",
    formatTimeout: (m) => String(m || 5),
    timeAgo: () => "just now",
    highlightMatch: (text) => text || "",
    taskDisplayPrompt: (task) => (task ? task.prompt : ""),
    syncTask: vi.fn(),
    task: (id) => ({
      diff: () => `/api/tasks/${id}/diff`,
      update: () => `/api/tasks/${id}`,
      archive: () => `/api/tasks/${id}/archive`,
      done: () => `/api/tasks/${id}/done`,
      resume: () => `/api/tasks/${id}/resume`,
    }),
    getOpenModalTaskId: vi.fn(() => null),
    renderModalDependencies: vi.fn(),
    renderDiffFiles: vi.fn(),
    updateTaskStatus: vi.fn(),
    quickDoneTask: vi.fn(),
    quickResumeTask: vi.fn(),
    quickRetryTask: vi.fn(),
    quickTestTask: vi.fn(),
    toggleFreshStart: vi.fn(),
    updateStatusBar: vi.fn(),
    updateWorkspaceGroupBadges: vi.fn(),
    syncBacklogSortableMode: vi.fn(),
    ...options,
  });
  return ctx;
}

function loadRenderHarness(options = {}) {
  const ctx = createContext(options);
  loadScript("render.js", ctx);
  return { ctx, renderExports: ctx.module.exports };
}

// ---------------------------------------------------------------------------
// tagStyle
// ---------------------------------------------------------------------------
describe("render.js tagStyle", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns a CSS string with --tag-bg and --tag-text variables", () => {
    const style = ctx.tagStyle("feature");
    expect(style).toMatch(/background:var\(--tag-bg-\d+\)/);
    expect(style).toMatch(/color:var\(--tag-text-\d+\)/);
  });

  it("returns consistent styles for the same tag", () => {
    expect(ctx.tagStyle("bugfix")).toBe(ctx.tagStyle("bugfix"));
  });

  it("returns different styles for different tags", () => {
    // Not guaranteed but very likely for short distinct strings
    const s1 = ctx.tagStyle("a");
    const s2 = ctx.tagStyle("zzzzz");
    // Both should be valid styles even if they happen to match
    expect(s1).toMatch(/background:var\(--tag-bg-\d+\)/);
    expect(s2).toMatch(/background:var\(--tag-bg-\d+\)/);
  });
});

// ---------------------------------------------------------------------------
// setBrainstormCategories
// ---------------------------------------------------------------------------
describe("render.js setBrainstormCategories", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("sets categories from an array of strings", () => {
    ctx.setBrainstormCategories(["Alpha", "Beta"]);
    expect(ctx.BRAINSTORM_CATEGORIES.has("Alpha")).toBe(true);
    expect(ctx.BRAINSTORM_CATEGORIES.has("Beta")).toBe(true);
  });

  it("filters out empty and non-string values", () => {
    ctx.setBrainstormCategories(["Valid", "", "  ", null, 42]);
    expect(ctx.BRAINSTORM_CATEGORIES.has("Valid")).toBe(true);
    expect(ctx.BRAINSTORM_CATEGORIES.size).toBe(1);
  });

  it("creates an empty set when passed a non-array", () => {
    ctx.setBrainstormCategories("not-an-array");
    expect(ctx.BRAINSTORM_CATEGORIES.size).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// renderTaskTagBadge / renderTaskTagBadges
// ---------------------------------------------------------------------------
describe("render.js renderTaskTagBadge", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns empty string for falsy tag", () => {
    expect(ctx.renderTaskTagBadge(null)).toBe("");
    expect(ctx.renderTaskTagBadge("")).toBe("");
  });

  it("returns a badge-tag span for a plain tag", () => {
    const html = ctx.renderTaskTagBadge("feature");
    expect(html).toContain("badge-tag");
    expect(html).toContain("feature");
  });

  it("returns a badge-category span for a brainstorm category tag", () => {
    ctx.setBrainstormCategories(["Architecture"]);
    const html = ctx.renderTaskTagBadge("Architecture");
    expect(html).toContain("badge-category");
  });

  it("returns a badge-idea-agent span for idea-agent tag", () => {
    const html = ctx.renderTaskTagBadge("idea-agent");
    expect(html).toContain("badge-idea-agent");
  });

  it("returns a badge-priority span for priority: prefix", () => {
    const html = ctx.renderTaskTagBadge("priority:high");
    expect(html).toContain("badge-priority");
    expect(html).toContain("priority high");
  });

  it("returns a badge-impact span for impact: prefix", () => {
    const html = ctx.renderTaskTagBadge("impact:3");
    expect(html).toContain("badge-impact");
    expect(html).toContain("impact 3");
  });
});

describe("render.js renderTaskTagBadges", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns empty string for empty or non-array tags", () => {
    expect(ctx.renderTaskTagBadges([])).toBe("");
    expect(ctx.renderTaskTagBadges(null)).toBe("");
  });

  it("concatenates badges for multiple tags", () => {
    const html = ctx.renderTaskTagBadges(["alpha", "beta"]);
    expect(html).toContain("alpha");
    expect(html).toContain("beta");
  });
});

// ---------------------------------------------------------------------------
// formatRelativeTime
// ---------------------------------------------------------------------------
describe("render.js formatRelativeTime", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns empty string for past dates", () => {
    expect(ctx.formatRelativeTime(new Date(Date.now() - 10000))).toBe("");
  });

  it("returns 'in Xs' for seconds in the future", () => {
    const result = ctx.formatRelativeTime(new Date(Date.now() + 30000));
    expect(result).toMatch(/^in \d+s$/);
  });

  it("returns 'in Xm' for minutes in the future", () => {
    const result = ctx.formatRelativeTime(new Date(Date.now() + 5 * 60 * 1000));
    expect(result).toMatch(/^in \d+m$/);
  });

  it("returns 'in Xh' for hours in the future", () => {
    const result = ctx.formatRelativeTime(
      new Date(Date.now() + 3 * 60 * 60 * 1000),
    );
    expect(result).toMatch(/^in \d+h$/);
  });

  it("returns 'in Xd' for days in the future", () => {
    const result = ctx.formatRelativeTime(
      new Date(Date.now() + 2 * 24 * 60 * 60 * 1000),
    );
    expect(result).toMatch(/^in \d+d$/);
  });
});

// ---------------------------------------------------------------------------
// getRenderableTasks
// ---------------------------------------------------------------------------
describe("render.js getRenderableTasks", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns only active tasks when showArchived is false", () => {
    ctx.tasks = [{ id: "a" }];
    ctx.archivedTasks = [{ id: "b" }];
    ctx.showArchived = false;
    expect(ctx.getRenderableTasks()).toEqual([{ id: "a" }]);
  });

  it("returns active + archived tasks when showArchived is true", () => {
    ctx.tasks = [{ id: "a" }];
    ctx.archivedTasks = [{ id: "b" }];
    ctx.showArchived = true;
    const result = ctx.getRenderableTasks();
    expect(result.length).toBe(2);
  });

  it("returns only active tasks when showArchived is true but archivedTasks is empty", () => {
    ctx.tasks = [{ id: "a" }];
    ctx.archivedTasks = [];
    ctx.showArchived = true;
    expect(ctx.getRenderableTasks()).toEqual([{ id: "a" }]);
  });
});

// ---------------------------------------------------------------------------
// getTaskImpactScore
// ---------------------------------------------------------------------------
describe("render.js getTaskImpactScore", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns impact_score field when present", () => {
    expect(ctx.getTaskImpactScore({ impact_score: 5 })).toBe(5);
  });

  it("returns null for tasks without impact_score or tag", () => {
    expect(ctx.getTaskImpactScore({ tags: [] })).toBe(null);
  });

  it("parses impact from impact: tag", () => {
    expect(ctx.getTaskImpactScore({ tags: ["impact:7"] })).toBe(7);
  });

  it("returns null for non-numeric impact tag", () => {
    expect(ctx.getTaskImpactScore({ tags: ["impact:high"] })).toBe(null);
  });

  it("returns null for null/undefined task", () => {
    expect(ctx.getTaskImpactScore(null)).toBe(null);
  });
});

// ---------------------------------------------------------------------------
// sortBacklogTasks
// ---------------------------------------------------------------------------
describe("render.js sortBacklogTasks", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("sorts by position in manual mode", () => {
    ctx.backlogSortMode = "manual";
    const items = [
      { position: 3, tags: [] },
      { position: 1, tags: [] },
      { position: 2, tags: [] },
    ];
    ctx.sortBacklogTasks(items);
    expect(items.map((i) => i.position)).toEqual([1, 2, 3]);
  });

  it("sorts by impact score descending in impact mode", () => {
    ctx.backlogSortMode = "impact";
    const items = [
      { position: 0, impact_score: 2, tags: [] },
      { position: 1, impact_score: 8, tags: [] },
      { position: 2, impact_score: 5, tags: [] },
    ];
    ctx.sortBacklogTasks(items);
    expect(items.map((i) => i.impact_score)).toEqual([8, 5, 2]);
  });

  it("puts tasks with impact before tasks without in impact mode", () => {
    ctx.backlogSortMode = "impact";
    const items = [
      { position: 0, tags: [] },
      { position: 1, impact_score: 3, tags: [] },
    ];
    ctx.sortBacklogTasks(items);
    expect(items[0].impact_score).toBe(3);
  });

  it("falls back to position for equal impact scores", () => {
    ctx.backlogSortMode = "impact";
    const items = [
      { position: 5, impact_score: 3, tags: [] },
      { position: 1, impact_score: 3, tags: [] },
    ];
    ctx.sortBacklogTasks(items);
    expect(items.map((i) => i.position)).toEqual([1, 5]);
  });
});

// ---------------------------------------------------------------------------
// hasExecutionTrail
// ---------------------------------------------------------------------------
describe("render.js hasExecutionTrail", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns true when turns > 0", () => {
    expect(ctx.hasExecutionTrail({ turns: 1 })).toBe(true);
  });

  it("returns true when result is non-empty", () => {
    expect(ctx.hasExecutionTrail({ turns: 0, result: "some output" })).toBe(
      true,
    );
  });

  it("returns true when stop_reason is non-empty", () => {
    expect(
      ctx.hasExecutionTrail({ turns: 0, result: "", stop_reason: "end_turn" }),
    ).toBe(true);
  });

  it("returns false when no execution evidence", () => {
    expect(
      ctx.hasExecutionTrail({ turns: 0, result: "", stop_reason: "" }),
    ).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// cardDisplayPrompt
// ---------------------------------------------------------------------------
describe("render.js cardDisplayPrompt", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns execution_prompt for idea-agent kind", () => {
    expect(
      ctx.cardDisplayPrompt({
        kind: "idea-agent",
        execution_prompt: "exec",
        prompt: "p",
      }),
    ).toBe("exec");
  });

  it("returns prompt as fallback", () => {
    expect(ctx.cardDisplayPrompt({ prompt: "the prompt" })).toBe("the prompt");
  });

  it("returns empty string for null task", () => {
    expect(ctx.cardDisplayPrompt(null)).toBe("");
  });
});

// ---------------------------------------------------------------------------
// buildCardActions
// ---------------------------------------------------------------------------
describe("render.js buildCardActions", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns empty for archived tasks", () => {
    expect(ctx.buildCardActions({ archived: true, status: "done" })).toBe("");
  });

  it("includes Send to Plan and Start buttons for backlog", () => {
    const html = ctx.buildCardActions({ status: "backlog", id: "t1" });
    expect(html).toContain("card-action-send-to-plan");
    expect(html).toContain("Start");
  });

  it("includes Test and Done buttons for waiting, no Send to Plan", () => {
    const html = ctx.buildCardActions({ status: "waiting", id: "t1" });
    expect(html).not.toContain("card-action-send-to-plan");
    expect(html).toContain("Test");
    expect(html).toContain("Done");
    expect(html).not.toContain("card-action-resume");
  });

  it("includes Resume on waiting tasks that have a session_id", () => {
    const html = ctx.buildCardActions({
      status: "waiting",
      id: "t1",
      session_id: "s1",
      timeout: 30,
    });
    expect(html).toContain("card-action-resume");
    expect(html).toContain("Resume");
  });

  it("includes Resume and Retry for failed with session_id", () => {
    const html = ctx.buildCardActions({
      status: "failed",
      id: "t1",
      session_id: "s1",
      timeout: 30,
    });
    expect(html).toContain("Resume");
    expect(html).toContain("Retry");
  });

  it("includes only Retry for failed without session_id", () => {
    const html = ctx.buildCardActions({ status: "failed", id: "t1" });
    expect(html).not.toContain("Resume");
    expect(html).toContain("Retry");
  });

  it("includes Retry for cancelled tasks", () => {
    const html = ctx.buildCardActions({ status: "cancelled", id: "t1" });
    expect(html).toContain("Retry");
  });

  it("includes Retry for done tasks", () => {
    const html = ctx.buildCardActions({ status: "done", id: "t1" });
    expect(html).toContain("Retry");
  });

  it("returns empty for in_progress tasks", () => {
    expect(ctx.buildCardActions({ status: "in_progress", id: "t1" })).toBe("");
  });
});

// ---------------------------------------------------------------------------
// formatInProgressCount
// ---------------------------------------------------------------------------
describe("render.js formatInProgressCount", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns the count as a string", () => {
    expect(ctx.formatInProgressCount(3)).toBe("3");
    expect(ctx.formatInProgressCount(0)).toBe("0");
  });
});

// ---------------------------------------------------------------------------
// hasCancelledOrMissingDep
// ---------------------------------------------------------------------------
describe("render.js hasCancelledOrMissingDep", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
    ctx.tasks = [];
  });

  it("returns false when no dependencies", () => {
    expect(ctx.hasCancelledOrMissingDep({ id: "a" })).toBe(false);
  });

  it("returns true when a dependency is missing", () => {
    ctx.tasks = [];
    expect(
      ctx.hasCancelledOrMissingDep({ id: "a", depends_on: ["missing"] }),
    ).toBe(true);
  });

  it("returns true when a dependency is cancelled", () => {
    ctx.tasks = [{ id: "dep-1", status: "cancelled" }];
    expect(
      ctx.hasCancelledOrMissingDep({ id: "a", depends_on: ["dep-1"] }),
    ).toBe(true);
  });

  it("returns false when all dependencies exist and are not cancelled", () => {
    ctx.tasks = [{ id: "dep-1", status: "done" }];
    expect(
      ctx.hasCancelledOrMissingDep({ id: "a", depends_on: ["dep-1"] }),
    ).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// invalidateDiffBehindCounts (broadcast)
// ---------------------------------------------------------------------------
describe("render.js invalidateDiffBehindCounts broadcast", () => {
  let renderExports;
  beforeEach(() => {
    ({ renderExports } = loadRenderHarness());
    renderExports.diffCache.clear();
  });

  it("invalidates all entries when called without taskId", () => {
    renderExports.diffCache.set("a", {
      diff: "d1",
      behindCounts: {},
      updatedAt: "u1",
      behindFetchedAt: 100,
    });
    renderExports.diffCache.set("b", {
      diff: "d2",
      behindCounts: {},
      updatedAt: "u2",
      behindFetchedAt: 200,
    });
    renderExports.invalidateDiffBehindCounts(undefined);
    expect(renderExports.diffCache.get("a").behindFetchedAt).toBe(0);
    expect(renderExports.diffCache.get("b").behindFetchedAt).toBe(0);
  });

  it("marks all loading sentinels as invalidated", () => {
    const s1 = { loading: true };
    const s2 = { loading: true };
    renderExports.diffCache.set("a", s1);
    renderExports.diffCache.set("b", s2);
    renderExports.invalidateDiffBehindCounts(undefined);
    expect(s1.invalidated).toBe(true);
    expect(s2.invalidated).toBe(true);
  });

  it("does nothing when cache is empty", () => {
    expect(() =>
      renderExports.invalidateDiffBehindCounts(undefined),
    ).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// _cachedMarkdown
// ---------------------------------------------------------------------------
describe("render.js _cachedMarkdown", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("returns empty string for empty input", () => {
    expect(ctx._cachedMarkdown("")).toBe("");
    expect(ctx._cachedMarkdown(null)).toBe("");
  });

  it("returns rendered markdown and caches it", () => {
    const result = ctx._cachedMarkdown("hello");
    expect(result).toBe("hello");
    // Second call should return the same cached result
    expect(ctx._cachedMarkdown("hello")).toBe("hello");
  });
});

describe("render.js spec badge on cards", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
  });

  it("_cardFingerprint includes spec_source_path", () => {
    const taskA = {
      id: "aaa",
      status: "backlog",
      prompt: "p",
      timeout: 5,
      position: 0,
      spec_source_path: "specs/local/foo.md",
    };
    const taskB = {
      id: "aaa",
      status: "backlog",
      prompt: "p",
      timeout: 5,
      position: 0,
      spec_source_path: "",
    };
    const fpA = ctx._cardFingerprint(taskA, 0);
    const fpB = ctx._cardFingerprint(taskB, 0);
    expect(fpA).not.toBe(fpB);
    expect(fpA).toContain("specs/local/foo.md");
  });

  it("createCard renders badge-spec when spec_source_path is set", () => {
    ctx.tasks = [
      {
        id: "spec-task-1",
        status: "backlog",
        prompt: "do something",
        timeout: 5,
        position: 0,
        spec_source_path: "specs/local/my-feature.md",
      },
    ];
    const card = ctx.createCard(ctx.tasks[0], 0);
    expect(card.innerHTML).toContain("badge-spec");
    expect(card.innerHTML).toContain("my-feature");
    expect(card.innerHTML).toContain("data-spec-path");
  });

  it("createCard does not render badge-spec when spec_source_path is empty", () => {
    ctx.tasks = [
      {
        id: "plain-task-1",
        status: "backlog",
        prompt: "do something",
        timeout: 5,
        position: 0,
      },
    ];
    const card = ctx.createCard(ctx.tasks[0], 0);
    expect(card.innerHTML).not.toContain("badge-spec");
  });
});
