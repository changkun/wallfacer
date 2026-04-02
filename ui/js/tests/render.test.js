import { describe, it, expect, beforeEach, afterEach, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function loadScript(filename, ctx) {
  const libDeps = {
    "render.js": ["lib/scheduling.js"],
  };
  const deps = libDeps[filename];
  if (deps) {
    for (const dep of deps) {
      const depCode = readFileSync(join(jsDir, dep), "utf8");
      vm.runInContext(depCode, ctx, { filename: join(jsDir, dep) });
    }
  }
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
}

function createContext(options = {}) {
  const taskListeners = {};
  class MockEventSource {
    constructor(url) {
      this.url = url;
      this.readyState = 1;
      this.listeners = taskListeners;
      MockEventSource.instance = this;
    }

    addEventListener(name, handler) {
      if (!this.listeners[name]) this.listeners[name] = [];
      this.listeners[name].push(handler);
    }

    close() {}
  }
  MockEventSource.CLOSED = 2;

  const ctx = vm.createContext({
    module: { exports: {} },
    exports: {},
    console,
    Date,
    Math,
    JSON,
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
      createElement: () => ({ innerHTML: "" }),
      querySelectorAll: () => [],
      addEventListener: () => {},
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
    Routes: {
      tasks: {
        stream: () => "/api/tasks/stream",
        list: () => "/api/tasks",
      },
    },
    EventSource: MockEventSource,
    api: vi.fn(),
    escapeHtml: (s) => String(s || ""),
    renderMarkdown: (s) => String(s || ""),
    matchesFilter: () => true,
    updateIdeationFromTasks: () => {},
    updateBacklogSortButton: () => {},
    updateRefineUI: () => {},
    renderRefineHistory: () => {},
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
    activeWorkspaces: ["~/project"],
    getOpenModalTaskId: vi.fn(() => null),
    renderModalDependencies: vi.fn(),
    ...options,
  });

  return { ctx, taskListeners, MockEventSource };
}

function loadRenderHarness(options = {}) {
  const harness = createContext(options);
  loadScript("render.js", harness.ctx);
  return { ...harness, renderExports: harness.ctx.module.exports };
}

function loadRenderAndApiHarness(options = {}) {
  const harness = createContext(options);
  loadScript("render.js", harness.ctx);
  const renderExports = harness.ctx.module.exports;
  loadScript("task-stream.js", harness.ctx);
  loadScript("api.js", harness.ctx);
  return { ...harness, renderExports };
}

describe("render.js dependency helpers", () => {
  let ctx;
  let renderExports;

  beforeEach(() => {
    ({ ctx, renderExports } = loadRenderHarness());
    ctx.tasks = [];
    ctx.archivedTasks = [];
    renderExports.diffCache.clear();
    renderExports.cardOversightCache.clear();
  });

  it("returns false when depends_on is absent", () => {
    expect(renderExports.areDepsBlocked({ id: "task-a" })).toBe(false);
  });

  it("reads dependency ids from dependencies when depends_on is absent", () => {
    ctx.tasks = [{ id: "dep-1", status: "in_progress" }];

    expect(
      renderExports.getTaskDependencyIds({
        id: "task-a",
        dependencies: ["dep-1"],
      }),
    ).toEqual(["dep-1"]);
    expect(
      renderExports.areDepsBlocked({ id: "task-a", dependencies: ["dep-1"] }),
    ).toBe(true);
  });

  it("returns false when all dependencies are present and done", () => {
    ctx.tasks = [
      { id: "dep-1", status: "done" },
      { id: "dep-2", status: "done" },
    ];

    expect(
      renderExports.areDepsBlocked({
        id: "task-a",
        depends_on: ["dep-1", "dep-2"],
      }),
    ).toBe(false);
  });

  it("returns true when one dependency is still in progress", () => {
    ctx.tasks = [
      { id: "dep-1", status: "done" },
      { id: "dep-2", status: "in_progress" },
    ];

    expect(
      renderExports.areDepsBlocked({
        id: "task-a",
        depends_on: ["dep-1", "dep-2"],
      }),
    ).toBe(true);
  });

  it("returns true when a dependency id is missing from the task list", () => {
    ctx.tasks = [{ id: "dep-1", status: "done" }];

    expect(
      renderExports.areDepsBlocked({
        id: "task-a",
        depends_on: ["dep-1", "missing-dep"],
      }),
    ).toBe(true);
  });

  it("returns only non-done dependency names", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "done",
        title: "Finished task",
        prompt: "done prompt",
      },
      {
        id: "dep-2",
        status: "in_progress",
        title: "Active task",
        prompt: "active prompt",
      },
      { id: "dep-3", status: "failed", title: "", prompt: "Needs manual fix" },
    ];

    const names = renderExports.getBlockingTaskNames({
      id: "task-a",
      depends_on: ["dep-1", "dep-2", "dep-3"],
    });

    expect(names).toBe("Active task, Needs manual fix");
  });

  it("counts unmet dependencies including removed tasks", () => {
    ctx.tasks = [
      { id: "dep-1", status: "done" },
      { id: "dep-2", status: "waiting" },
    ];

    expect(
      renderExports.getUnmetDependencyCount({
        id: "task-a",
        depends_on: ["dep-1", "dep-2", "missing-dep"],
      }),
    ).toBe(2);
  });

  it("renders blocked backlog badges with the total dependency count", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "done",
        title: "Finished",
        prompt: "finished prompt",
      },
      {
        id: "dep-2",
        status: "in_progress",
        title: "Running",
        prompt: "running prompt",
      },
    ];

    const badge = renderExports.renderDependencyBadge({
      id: "task-a",
      status: "backlog",
      depends_on: ["dep-1", "dep-2"],
    });

    expect(badge).toContain("2 deps");
    expect(badge).toContain("badge-blocked");
  });

  it("renders a ready badge when all backlog dependencies are done", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "done",
        title: "Finished",
        prompt: "finished prompt",
      },
      {
        id: "dep-2",
        status: "done",
        title: "Also finished",
        prompt: "done prompt",
      },
    ];

    const badge = renderExports.renderDependencyBadge({
      id: "task-a",
      status: "backlog",
      depends_on: ["dep-1", "dep-2"],
    });

    expect(badge).toContain("ready");
    expect(badge).toContain("badge-deps-met");
  });

  it("renders a dependency-cancelled chip when a dep has status cancelled", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "done",
        title: "Finished",
        prompt: "finished prompt",
      },
      {
        id: "dep-2",
        status: "cancelled",
        title: "Cancelled",
        prompt: "cancelled prompt",
      },
    ];

    const badge = renderExports.renderDependencyBadge({
      id: "task-a",
      status: "backlog",
      depends_on: ["dep-1", "dep-2"],
    });

    expect(badge).toContain("dependency cancelled");
    expect(badge).toContain("badge-dep-cancelled");
  });

  it("renders a dependency-cancelled chip when a dep is absent from task list", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "done",
        title: "Finished",
        prompt: "finished prompt",
      },
    ];

    const badge = renderExports.renderDependencyBadge({
      id: "task-a",
      status: "backlog",
      depends_on: ["dep-1", "missing-dep"],
    });

    expect(badge).toContain("dependency cancelled");
    expect(badge).toContain("badge-dep-cancelled");
  });

  it("suppresses dependency badges outside backlog", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "in_progress",
        title: "Running",
        prompt: "running prompt",
      },
    ];

    expect(
      renderExports.renderDependencyBadge({
        id: "task-a",
        status: "in_progress",
        depends_on: ["dep-1"],
      }),
    ).toBe("");
  });

  it("returns false when all dependencies are cancelled", () => {
    ctx.tasks = [
      { id: "dep-1", status: "cancelled" },
      { id: "dep-2", status: "cancelled" },
    ];

    expect(
      renderExports.areDepsBlocked({
        id: "task-a",
        depends_on: ["dep-1", "dep-2"],
      }),
    ).toBe(false);
  });

  it("returns false when dependencies are a mix of done and cancelled", () => {
    ctx.tasks = [
      { id: "dep-1", status: "done" },
      { id: "dep-2", status: "cancelled" },
    ];

    expect(
      renderExports.areDepsBlocked({
        id: "task-a",
        depends_on: ["dep-1", "dep-2"],
      }),
    ).toBe(false);
  });

  it("does not include cancelled dependency names in blocking names", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "done",
        title: "Finished task",
        prompt: "done prompt",
      },
      {
        id: "dep-2",
        status: "cancelled",
        title: "Cancelled task",
        prompt: "cancelled prompt",
      },
      {
        id: "dep-3",
        status: "in_progress",
        title: "Active task",
        prompt: "active prompt",
      },
    ];

    const names = renderExports.getBlockingTaskNames({
      id: "task-a",
      depends_on: ["dep-1", "dep-2", "dep-3"],
    });

    expect(names).toBe("Active task");
  });
});

describe("render.js isTestCard", () => {
  let renderExports;

  beforeEach(() => {
    ({ renderExports } = loadRenderHarness());
  });

  it("returns true for tasks with a last test result and positive start turn", () => {
    expect(
      renderExports.isTestCard({
        last_test_result: "pass",
        test_run_start_turn: 1,
      }),
    ).toBe(true);
  });

  it("returns false when last_test_result is null", () => {
    expect(
      renderExports.isTestCard({
        last_test_result: null,
        test_run_start_turn: 1,
      }),
    ).toBe(false);
  });

  it("returns false when test_run_start_turn is zero", () => {
    expect(
      renderExports.isTestCard({
        last_test_result: "pass",
        test_run_start_turn: 0,
      }),
    ).toBe(false);
  });

  it("does not gate on task status", () => {
    expect(
      renderExports.isTestCard({
        status: "backlog",
        last_test_result: "pass",
        test_run_start_turn: 2,
      }),
    ).toBe(true);
  });
});

describe("render.js diffCache", () => {
  let ctx;
  let renderExports;

  beforeEach(() => {
    ({ ctx, renderExports } = loadRenderHarness());
    renderExports.diffCache.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("invalidates only the requested task cache entry", () => {
    renderExports.diffCache.set("task-a", {
      diff: "a",
      behindCounts: {},
      updatedAt: "u1",
      behindFetchedAt: 10,
    });
    renderExports.diffCache.set("task-b", {
      diff: "b",
      behindCounts: {},
      updatedAt: "u2",
      behindFetchedAt: 20,
    });

    renderExports.invalidateDiffBehindCounts("task-a");

    expect(renderExports.diffCache.get("task-a")).toEqual({
      diff: "a",
      behindCounts: {},
      updatedAt: "u1",
      behindFetchedAt: 0,
    });
    expect(renderExports.diffCache.get("task-b")).toEqual({
      diff: "b",
      behindCounts: {},
      updatedAt: "u2",
      behindFetchedAt: 20,
    });
  });

  it("treats behind-counts as stale after the TTL expires", async () => {
    vi.useFakeTimers();
    ({ ctx, renderExports } = loadRenderHarness());
    renderExports.diffCache.clear();

    const updatedAt = "2026-03-10T00:00:00Z";
    renderExports.diffCache.set("task-a", {
      diff: "cached diff",
      behindCounts: { repo: 1 },
      updatedAt,
      behindFetchedAt: Date.now(),
    });
    ctx.api.mockResolvedValue({
      diff: "fresh diff",
      behind_counts: { repo: 2 },
    });

    vi.advanceTimersByTime(renderExports.BEHIND_TTL_MS + 1);

    await ctx.fetchDiff({ querySelector: () => null }, "task-a", updatedAt);

    expect(ctx.api).toHaveBeenCalledWith("/api/tasks/task-a/diff");
    expect(renderExports.diffCache.get("task-a")).toMatchObject({
      diff: "fresh diff",
      behindCounts: { repo: 2 },
      updatedAt,
    });
  });

  it("marks in-flight loading sentinel as invalidated", () => {
    const sentinel = { loading: true };
    renderExports.diffCache.set("task-c", sentinel);

    renderExports.invalidateDiffBehindCounts("task-c");

    expect(sentinel.invalidated).toBe(true);
  });

  it("discards stale fetch result when invalidated during in-flight request", async () => {
    // Simulate a diff fetch that is in-flight when an SSE invalidation arrives.
    // The fetch should discard the stale result and schedule a re-render.
    let resolveApi;
    ctx.api = vi.fn(
      () =>
        new Promise((resolve) => {
          resolveApi = resolve;
        }),
    );

    const card = { querySelector: () => null };
    const fetchPromise = ctx.fetchDiff(card, "task-d", "u1");

    // Cache should now be the loading sentinel.
    const sentinel = renderExports.diffCache.get("task-d");
    expect(sentinel).toBeTruthy();
    expect(sentinel.loading).toBe(true);

    // Simulate SSE invalidation arriving while fetch is in-flight.
    renderExports.invalidateDiffBehindCounts("task-d");
    expect(sentinel.invalidated).toBe(true);

    // Resolve the API call with stale data.
    ctx.scheduleRender = vi.fn();
    resolveApi({ diff: "stale diff", behind_counts: { repo: 3 } });
    await fetchPromise;

    // The stale result should have been discarded.
    expect(renderExports.diffCache.has("task-d")).toBe(false);
    // A re-render should have been scheduled.
    expect(ctx.scheduleRender).toHaveBeenCalled();
  });
});

describe("render.js cardOversightCache", () => {
  let ctx;
  let renderExports;

  beforeEach(() => {
    ({ ctx, renderExports } = loadRenderAndApiHarness());
    ctx.tasks = [{ id: "task-a", status: "waiting", title: "Task A" }];
    ctx.archivedTasks = [];
    renderExports.cardOversightCache.clear();
  });

  it("evicts the updated task before the next render cycle on SSE updates", () => {
    renderExports.cardOversightCache.set("task-a", {
      phase_count: 1,
      phases: [{ title: "Cached" }],
    });
    ctx.scheduleRender = vi.fn(() => {
      expect(renderExports.cardOversightCache.has("task-a")).toBe(false);
    });

    ctx.startTasksStream();
    const handler = ctx.EventSource.instance.listeners["task-updated"][0];
    handler({
      data: JSON.stringify({
        id: "task-a",
        status: "done",
        title: "Task A",
        updated_at: "2026-03-10T00:00:00Z",
      }),
      lastEventId: "evt-1",
    });

    expect(renderExports.cardOversightCache.has("task-a")).toBe(false);
    expect(ctx.scheduleRender).toHaveBeenCalledTimes(1);
  });
});

describe("api.js SSE modal dependency refresh", () => {
  let ctx;

  beforeEach(() => {
    ({ ctx } = loadRenderAndApiHarness({
      activeWorkspaces: ["~/project"],
      getOpenModalTaskId: vi.fn(() => "task-b"),
      renderModalDependencies: vi.fn(),
    }));
    ctx.tasks = [
      { id: "task-a", status: "in_progress", title: "Task A" },
      {
        id: "task-b",
        status: "backlog",
        title: "Task B",
        depends_on: ["task-a"],
      },
    ];
    ctx.archivedTasks = [];
  });

  it("calls renderModalDependencies when a dependency of the open task changes status", () => {
    ctx.startTasksStream();
    const handler = ctx.EventSource.instance.listeners["task-updated"][0];

    handler({
      data: JSON.stringify({
        id: "task-a",
        status: "done",
        title: "Task A",
        updated_at: "2026-03-10T00:00:00Z",
      }),
      lastEventId: "evt-1",
    });

    expect(ctx.renderModalDependencies).toHaveBeenCalledTimes(1);
    expect(ctx.renderModalDependencies.mock.calls[0][0].id).toBe("task-b");
  });

  it("does not call renderModalDependencies when the updated task is not a dependency of the open task", () => {
    ctx.startTasksStream();
    const handler = ctx.EventSource.instance.listeners["task-updated"][0];

    handler({
      data: JSON.stringify({
        id: "task-c",
        status: "done",
        title: "Task C",
        updated_at: "2026-03-10T00:00:00Z",
      }),
      lastEventId: "evt-2",
    });

    expect(ctx.renderModalDependencies).not.toHaveBeenCalled();
  });

  it("does not call renderModalDependencies when no modal is open", () => {
    ctx.getOpenModalTaskId = vi.fn(() => null);
    ctx.startTasksStream();
    const handler = ctx.EventSource.instance.listeners["task-updated"][0];

    handler({
      data: JSON.stringify({
        id: "task-a",
        status: "done",
        title: "Task A",
        updated_at: "2026-03-10T00:00:00Z",
      }),
      lastEventId: "evt-3",
    });

    expect(ctx.renderModalDependencies).not.toHaveBeenCalled();
  });

  it("refreshes when the open task uses the dependencies field", () => {
    ctx.tasks = [
      { id: "task-a", status: "in_progress", title: "Task A" },
      {
        id: "task-b",
        status: "backlog",
        title: "Task B",
        dependencies: ["task-a"],
      },
    ];
    ctx.startTasksStream();
    const handler = ctx.EventSource.instance.listeners["task-updated"][0];

    handler({
      data: JSON.stringify({
        id: "task-a",
        status: "done",
        title: "Task A",
        updated_at: "2026-03-10T00:00:00Z",
      }),
      lastEventId: "evt-4",
    });

    expect(ctx.renderModalDependencies).toHaveBeenCalledTimes(1);
    expect(ctx.renderModalDependencies.mock.calls[0][0].id).toBe("task-b");
  });

  it("refreshes when the open task itself receives a dependency update", () => {
    ctx.tasks = [
      { id: "task-a", status: "in_progress", title: "Task A" },
      { id: "task-b", status: "backlog", title: "Task B", dependencies: [] },
    ];
    ctx.getOpenModalTaskId = vi.fn(() => "task-b");
    ctx.startTasksStream();
    const handler = ctx.EventSource.instance.listeners["task-updated"][0];

    handler({
      data: JSON.stringify({
        id: "task-b",
        status: "backlog",
        title: "Task B",
        dependencies: ["task-a"],
        updated_at: "2026-03-10T00:00:00Z",
      }),
      lastEventId: "evt-4b",
    });

    expect(ctx.renderModalDependencies).toHaveBeenCalledTimes(1);
    expect(ctx.renderModalDependencies.mock.calls[0][0].id).toBe("task-b");
  });

  it("refreshes when a dependency of the open task is deleted", () => {
    ctx.startTasksStream();
    const handler = ctx.EventSource.instance.listeners["task-deleted"][0];

    handler({
      data: JSON.stringify({ id: "task-a" }),
      lastEventId: "evt-5",
    });

    expect(ctx.renderModalDependencies).toHaveBeenCalledTimes(1);
    expect(ctx.renderModalDependencies.mock.calls[0][0].id).toBe("task-b");
  });
});

describe("render.js backlog dependency badge", () => {
  let ctx;

  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
    ctx.tasks = [];
    ctx.archivedTasks = [];
  });

  function makeCard() {
    return {
      dataset: {},
      style: {},
      classList: {
        add: () => {},
        remove: () => {},
      },
      addEventListener: () => {},
      removeEventListener: () => {},
      setAttribute: () => {},
      removeAttribute: () => {},
      querySelector: () => null,
      appendChild: () => {},
      innerHTML: "",
    };
  }

  it("renders an unmet dependency badge on backlog cards", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "in_progress",
        title: "Dependency task",
        prompt: "Dependency task",
      },
    ];
    const card = makeCard();
    const task = {
      id: "task-a",
      status: "backlog",
      position: 0,
      prompt: "Test task",
      title: "Test task",
      depends_on: ["dep-1"],
      tags: [],
      sandbox_by_activity: {},
      worktree_paths: {},
      created_at: "2026-03-10T00:00:00Z",
      updated_at: "2026-03-10T00:00:00Z",
    };

    ctx.updateCard(card, task, 0);

    expect(card.innerHTML).toContain("badge-blocked");
    expect(card.innerHTML).toContain("1 dep");
    expect(card.innerHTML).toContain("<svg");
  });

  it("renders a ready badge when all dependencies are satisfied", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "done",
        title: "Dependency task",
        prompt: "Dependency task",
      },
    ];
    const card = makeCard();
    const task = {
      id: "task-a",
      status: "backlog",
      position: 0,
      prompt: "Test task",
      title: "Test task",
      depends_on: ["dep-1"],
      tags: [],
      sandbox_by_activity: {},
      worktree_paths: {},
      created_at: "2026-03-10T00:00:00Z",
      updated_at: "2026-03-10T00:00:00Z",
    };

    ctx.updateCard(card, task, 0);

    expect(card.innerHTML).toContain("badge-deps-met");
    expect(card.innerHTML).toContain("ready");
  });

  it("does not render a dependency badge for non-backlog cards", () => {
    ctx.tasks = [
      {
        id: "dep-1",
        status: "in_progress",
        title: "Dependency task",
        prompt: "Dependency task",
      },
    ];
    const card = makeCard();
    const task = {
      id: "task-a",
      status: "waiting",
      position: 0,
      prompt: "Test task",
      title: "Test task",
      depends_on: ["dep-1"],
      tags: [],
      sandbox_by_activity: {},
      worktree_paths: {},
      created_at: "2026-03-10T00:00:00Z",
      updated_at: "2026-03-10T00:00:00Z",
    };

    ctx.updateCard(card, task, 0);

    expect(card.innerHTML).not.toContain("badge-blocked");
    expect(card.innerHTML).not.toContain("badge-deps-met");
  });
});

describe("render.js column aria-live attributes", () => {
  let ctx;

  beforeEach(() => {
    ({ ctx } = loadRenderHarness());
    ctx.tasks = [];
    ctx.archivedTasks = [];
  });

  it('sets aria-live="polite" on rendered column containers', () => {
    const attrs = {};
    const mockEl = {
      dataset: {},
      children: [],
      hasAttribute: (k) => k in attrs,
      getAttribute: (k) => attrs[k] ?? null,
      setAttribute: (k, v) => {
        attrs[k] = v;
      },
      insertBefore: () => {},
      appendChild(c) {
        return c;
      },
      get textContent() {
        return "";
      },
      set textContent(_v) {
        this.children = [];
      },
    };
    ctx.document = {
      getElementById: (id) => (id === "col-backlog" ? mockEl : null),
      createElement: () => ({ innerHTML: "" }),
      createDocumentFragment: () => ({
        children: [],
        appendChild(c) {
          this.children.push(c);
          return c;
        },
      }),
      querySelectorAll: () => [],
      addEventListener: () => {},
      readyState: "complete",
    };
    ctx.getOpenModalTaskId = () => null;
    ctx.getRenderableTasks = () => [];
    ctx.render();
    expect(mockEl.getAttribute("aria-live")).toBe("polite");
  });
});
