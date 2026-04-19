/**
 * Additional coverage tests for api.js.
 *
 * Targets uncovered paths: SSE follower tab logic, task-deleted handler with
 * modal dependencies, automation toggle variants, watcher health rendering,
 * archived pagination edge cases (after direction), deep-link hash with tab
 * names and spec deep-links, visibility change, stopTasksStream/stopGitStream,
 * resetBoardState, restartActiveStreams, onDoneColumnScroll, and
 * toggleShowArchived branches.
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

function makeInput(initial = false) {
  return { checked: initial, value: "" };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Date,
    Math,
    setTimeout,
    clearTimeout,
    setInterval: overrides.setInterval || (() => 0),
    clearInterval: () => 0,
    Promise,
    JSON,
    Object,
    Array,
    Set,
    EventSource: function MockEventSource() {},
    location: { hash: "" },
    api: overrides.api || vi.fn().mockResolvedValue(null),
    fetch: overrides.fetch,
    showAlert: vi.fn(),
    openModal: vi.fn().mockResolvedValue(undefined),
    setRightTab: vi.fn(),
    setLeftTab: vi.fn(),
    announceBoardStatus: vi.fn(),
    getTaskAccessibleTitle: vi.fn((t) => (t && (t.title || t.id)) || ""),
    formatTaskStatusLabel: vi.fn((s) => s),
    scheduleRender: vi.fn(),
    invalidateDiffBehindCounts: vi.fn(),
    withAuthToken: (url) => url,
    _sseIsLeader: () => true,
    _sseRelay: vi.fn(),
    _sseOnFollowerEvent: vi.fn(),
    renderWorkspaces: vi.fn(),
    startGitStream: vi.fn(),
    stopGitStream: vi.fn(),
    fetchConfig: vi.fn().mockResolvedValue(undefined),
    stopTasksStream: vi.fn(),
    resetBoardState: vi.fn(),
    updateAutomationActiveCount: vi.fn(),
    updateStatusBar: vi.fn(),
    updateWorkspaceGroupBadges: vi.fn(),
    escapeHtml: (s) =>
      String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
    localStorage: { getItem: vi.fn(), setItem: vi.fn() },
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelectorAll: () => [],
      querySelector: () => null,
      addEventListener: vi.fn(),
      documentElement: { setAttribute: () => {} },
      readyState: "complete",
      visibilityState: "visible",
    },
    Routes: overrides.Routes || {
      config: { get: () => "/api/config", update: () => "/api/config" },
      tasks: {
        list: () => "/api/tasks",
        stream: () => "/api/tasks/stream",
        task: (id) => ({ update: () => "/api/tasks/" + id }),
      },
    },
    ...overrides,
  };
  ctx.EventSource.CLOSED = 2;
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  loadLibDeps(filename, ctx);
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

function loadApiCoreStack(ctx) {
  loadScript(ctx, "state.js");
  loadScript(ctx, "task-stream.js");
  loadScript(ctx, "api.js");
  return ctx;
}

function task(id, fields = {}) {
  return {
    id,
    title: fields.title || id,
    status: fields.status || "backlog",
    archived: !!fields.archived,
    updated_at: fields.updated_at || "2026-03-10T00:00:00Z",
    position: fields.position || 0,
    prompt: fields.prompt || "",
    ...fields,
  };
}

// ---------------------------------------------------------------------------
// SSE follower tab path
// ---------------------------------------------------------------------------

describe("startTasksStream follower tab", () => {
  it("registers follower event handlers and fetches initial state", async () => {
    const followerHandlers = {};
    const apiFn = vi.fn().mockResolvedValue([task("t1")]);
    const ctx = makeContext({
      api: apiFn,
      _sseIsLeader: () => false,
      _sseOnFollowerEvent: vi.fn((name, handler) => {
        followerHandlers[name] = handler;
      }),
    });
    loadApiCoreStack(ctx);
    vm.runInContext('activeWorkspaces = ["/repo"];', ctx);

    ctx.startTasksStream();

    // Should register follower handlers
    expect(ctx._sseOnFollowerEvent).toHaveBeenCalledWith(
      "tasks-snapshot",
      expect.any(Function),
    );
    expect(ctx._sseOnFollowerEvent).toHaveBeenCalledWith(
      "tasks-updated",
      expect.any(Function),
    );
    expect(ctx._sseOnFollowerEvent).toHaveBeenCalledWith(
      "tasks-deleted",
      expect.any(Function),
    );

    // fetchTasks should be called for initial state
    expect(apiFn).toHaveBeenCalledWith("/api/tasks");

    // Simulate a follower event
    followerHandlers["tasks-snapshot"]([task("t2")], "evt-1");
    const tasks = vm.runInContext("tasks", ctx);
    expect(tasks.some((t) => t.id === "t2")).toBe(true);

    // Simulate task-updated via follower
    followerHandlers["tasks-updated"](task("t2", { status: "done" }), "evt-2");
    const updated = vm.runInContext(
      'tasks.find(function(t) { return t.id === "t2"; })',
      ctx,
    );
    expect(updated.status).toBe("done");

    // Simulate task-deleted via follower
    followerHandlers["tasks-deleted"]({ id: "t2" }, "evt-3");
    const remaining = vm.runInContext("tasks", ctx);
    expect(remaining.find((t) => t.id === "t2")).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// SSE active_groups event
// ---------------------------------------------------------------------------

describe("startTasksStream active_groups event", () => {
  it("updates activeGroups on active_groups SSE event", () => {
    const instances = [];
    class MockEventSource {
      constructor(url) {
        this.url = url;
        this.readyState = 1;
        this.listeners = {};
        instances.push(this);
      }
      addEventListener(type, handler) {
        if (!this.listeners[type]) this.listeners[type] = [];
        this.listeners[type].push(handler);
      }
      close() {}
    }
    MockEventSource.CLOSED = 2;

    const ctx = makeContext({ EventSource: MockEventSource });
    loadApiCoreStack(ctx);
    vm.runInContext('activeWorkspaces = ["/repo"];', ctx);

    ctx.startTasksStream();
    const es = instances[0];

    const groups = [{ key: "grp1", in_progress: 2, waiting: 1 }];
    es.listeners["active_groups"][0]({
      data: JSON.stringify(groups),
    });

    const stored = vm.runInContext("activeGroups", ctx);
    expect(stored).toEqual(groups);
  });
});

// ---------------------------------------------------------------------------
// _handleTaskDeleted — modal dependency refresh
// ---------------------------------------------------------------------------

describe("_handleTaskDeleted modal dependencies", () => {
  it("refreshes modal when a deleted task is a dependency of the open modal task", () => {
    const instances = [];
    class MockEventSource {
      constructor(url) {
        this.url = url;
        this.readyState = 1;
        this.listeners = {};
        instances.push(this);
      }
      addEventListener(type, handler) {
        if (!this.listeners[type]) this.listeners[type] = [];
        this.listeners[type].push(handler);
      }
      close() {}
    }
    MockEventSource.CLOSED = 2;

    const renderModalDependencies = vi.fn();
    const ctx = makeContext({
      EventSource: MockEventSource,
      renderModalDependencies,
      getOpenModalTaskId: vi.fn(() => "task-1"),
      findTaskById: vi.fn((id) => {
        const tasks = vm.runInContext("tasks", ctx);
        return tasks.find((t) => t.id === id) || null;
      }),
    });
    loadApiCoreStack(ctx);
    vm.runInContext(
      `
      activeWorkspaces = ["/repo"];
      tasks = [
        ${JSON.stringify(task("dep-1", { status: "done" }))},
        ${JSON.stringify(task("task-1", { depends_on: ["dep-1"] }))},
      ];
    `,
      ctx,
    );

    ctx.startTasksStream();
    instances[0].listeners["task-deleted"][0]({
      data: JSON.stringify({ id: "dep-1" }),
      lastEventId: "evt-del",
    });

    expect(renderModalDependencies).toHaveBeenCalledTimes(1);
    expect(renderModalDependencies.mock.calls[0][0].id).toBe("task-1");
  });
});

// ---------------------------------------------------------------------------
// _handleInitialHash — spec deep-link and tab deep-link
// ---------------------------------------------------------------------------

describe("_handleInitialHash extended", () => {
  it("handles legacy #spec/ deep-link hash", async () => {
    const switchMode = vi.fn();
    const focusSpec = vi.fn();
    const ctx = makeContext({
      location: { hash: "#spec/specs/local/foo.md" },
      switchMode,
      focusSpec,
    });
    loadApiCoreStack(ctx);
    vm.runInContext(`activeWorkspaces = ["/repo"]; _hashHandled = false;`, ctx);

    await ctx._handleInitialHash();
    expect(switchMode).toHaveBeenCalledWith("spec");
    expect(focusSpec).toHaveBeenCalledWith("specs/local/foo.md", "/repo");
  });

  it("handles #plan/ deep-link hash identically to #spec/", async () => {
    const switchMode = vi.fn();
    const focusSpec = vi.fn();
    const ctx = makeContext({
      location: { hash: "#plan/specs/local/foo.md" },
      switchMode,
      focusSpec,
    });
    loadApiCoreStack(ctx);
    vm.runInContext(`activeWorkspaces = ["/repo"]; _hashHandled = false;`, ctx);

    await ctx._handleInitialHash();
    expect(switchMode).toHaveBeenCalledWith("spec");
    expect(focusSpec).toHaveBeenCalledWith("specs/local/foo.md", "/repo");
  });

  it("opens modal with right tab for hash with tab name", async () => {
    const taskId = "11111111-1111-1111-1111-111111111111";
    const ctx = makeContext({
      location: { hash: "#" + taskId + "/changes" },
    });
    loadApiCoreStack(ctx);
    vm.runInContext(
      `tasks = [{ id: "${taskId}", title: "T" }]; _hashHandled = false;`,
      ctx,
    );

    await ctx._handleInitialHash();
    // Wait for the .then chain
    await new Promise((r) => setTimeout(r, 10));
    expect(ctx.openModal).toHaveBeenCalledWith(taskId);
    expect(ctx.setRightTab).toHaveBeenCalledWith("changes");
  });

  it("ignores hash that doesn't match any pattern", async () => {
    const ctx = makeContext({ location: { hash: "#randomstuff" } });
    loadApiCoreStack(ctx);
    vm.runInContext("_hashHandled = false;", ctx);

    await ctx._handleInitialHash();
    expect(ctx.openModal).not.toHaveBeenCalled();
  });

  it("handles task not found in tasks list", async () => {
    const taskId = "33333333-3333-3333-3333-333333333333";
    const ctx = makeContext({ location: { hash: "#" + taskId } });
    loadApiCoreStack(ctx);
    vm.runInContext(
      "tasks = []; archivedTasks = []; _hashHandled = false;",
      ctx,
    );

    await ctx._handleInitialHash();
    expect(ctx.openModal).not.toHaveBeenCalled();
  });

  it("finds task in archivedTasks", async () => {
    const taskId = "44444444-4444-4444-4444-444444444444";
    const ctx = makeContext({ location: { hash: "#" + taskId } });
    loadApiCoreStack(ctx);
    vm.runInContext(
      `tasks = []; archivedTasks = [{ id: "${taskId}", title: "Archived" }]; _hashHandled = false;`,
      ctx,
    );

    await ctx._handleInitialHash();
    expect(ctx.openModal).toHaveBeenCalledWith(taskId);
  });
});

// ---------------------------------------------------------------------------
// stopTasksStream / stopGitStream / resetBoardState
// ---------------------------------------------------------------------------

describe("stopTasksStream", () => {
  it("closes the event source and resets state", () => {
    const ctx = makeContext();
    loadApiCoreStack(ctx);
    const mockSource = { close: vi.fn() };
    vm.runInContext(
      "tasksSource = _es;",
      Object.assign(ctx, { _es: mockSource }),
    );

    ctx.stopTasksStream();
    expect(mockSource.close).toHaveBeenCalled();
    expect(vm.runInContext("tasksSource", ctx)).toBeNull();
    expect(vm.runInContext("_sseConnState", ctx)).toBe("closed");
  });
});

describe("stopGitStream", () => {
  it("stops the git status handle", () => {
    const ctx = makeContext();
    loadApiCoreStack(ctx);
    const mockHandle = { stop: vi.fn() };
    vm.runInContext(
      "gitStatusHandle = _gh;",
      Object.assign(ctx, { _gh: mockHandle }),
    );

    ctx.stopGitStream();
    expect(mockHandle.stop).toHaveBeenCalled();
    expect(vm.runInContext("gitStatusHandle", ctx)).toBeNull();
  });
});

describe("resetBoardState", () => {
  it("clears all board state arrays", () => {
    const ctx = makeContext();
    loadApiCoreStack(ctx);
    vm.runInContext(
      `
      tasks = [{ id: "1" }];
      archivedTasks = [{ id: "2" }];
      gitStatuses = [{ workspace: "/ws" }];
      lastTasksEventId = "evt-1";
      rawLogBuffer = "some logs";
      testRawLogBuffer = "test logs";
    `,
      ctx,
    );

    ctx.resetBoardState();

    expect(vm.runInContext("tasks.length", ctx)).toBe(0);
    expect(vm.runInContext("archivedTasks.length", ctx)).toBe(0);
    expect(vm.runInContext("gitStatuses.length", ctx)).toBe(0);
    expect(vm.runInContext("lastTasksEventId", ctx)).toBeNull();
    expect(vm.runInContext("rawLogBuffer", ctx)).toBe("");
    expect(vm.runInContext("testRawLogBuffer", ctx)).toBe("");
  });
});

// ---------------------------------------------------------------------------
// restartActiveStreams
// ---------------------------------------------------------------------------

describe("restartActiveStreams", () => {
  it("stops and restarts streams when workspaces are active", () => {
    const instances = [];
    class MockEventSource {
      constructor(url) {
        this.url = url;
        this.readyState = 1;
        this.listeners = {};
        instances.push(this);
      }
      addEventListener(type, handler) {
        if (!this.listeners[type]) this.listeners[type] = [];
        this.listeners[type].push(handler);
      }
      close() {
        this.closed = true;
      }
    }
    MockEventSource.CLOSED = 2;

    const ctx = makeContext({ EventSource: MockEventSource });
    loadApiCoreStack(ctx);
    vm.runInContext('activeWorkspaces = ["/repo"];', ctx);

    ctx.restartActiveStreams();

    // startGitStream should be called
    expect(ctx.startGitStream).toHaveBeenCalled();
    // A new EventSource should be created for the tasks stream
    expect(instances.length).toBeGreaterThan(0);
  });

  it("only stops streams when no workspaces are active", () => {
    const ctx = makeContext();
    loadApiCoreStack(ctx);
    vm.runInContext("activeWorkspaces = [];", ctx);

    ctx.restartActiveStreams();

    expect(ctx.startGitStream).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// loadArchivedTasksPage — after direction
// ---------------------------------------------------------------------------

describe("loadArchivedTasksPage after direction", () => {
  it("appends archived_after cursor when loading after direction", async () => {
    const apiFn = vi.fn().mockResolvedValue({
      tasks: [],
      has_more_before: false,
      has_more_after: false,
    });
    const ctx = makeContext({ api: apiFn });
    loadApiCoreStack(ctx);
    vm.runInContext(
      `
      showArchived = true;
      archivedTasks = [${JSON.stringify(task("arch-newest", { archived: true }))}];
      archivedPage = { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: true };
    `,
      ctx,
    );

    await ctx.loadArchivedTasksPage("after");

    const url = apiFn.mock.calls[0][0];
    expect(url).toContain("archived_after=arch-newest");
  });

  it("skips after-page request when no existing archived tasks", async () => {
    const apiFn = vi.fn();
    const ctx = makeContext({ api: apiFn });
    loadApiCoreStack(ctx);
    vm.runInContext(
      `
      showArchived = true;
      archivedTasks = [];
      archivedPage = { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: true };
    `,
      ctx,
    );

    await ctx.loadArchivedTasksPage("after");
    expect(apiFn).not.toHaveBeenCalled();
  });

  it("skips after-page when hasMoreAfter is false", async () => {
    const apiFn = vi.fn();
    const ctx = makeContext({ api: apiFn });
    loadApiCoreStack(ctx);
    vm.runInContext(
      `
      showArchived = true;
      archivedTasks = [${JSON.stringify(task("a1", { archived: true }))}];
      archivedPage = { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false };
    `,
      ctx,
    );

    await ctx.loadArchivedTasksPage("after");
    expect(apiFn).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// onDoneColumnScroll
// ---------------------------------------------------------------------------

describe("onDoneColumnScroll", () => {
  it("triggers loadArchivedTasksPage before when near bottom", async () => {
    const apiFn = vi.fn().mockResolvedValue({
      tasks: [],
      has_more_before: false,
      has_more_after: false,
    });
    const col = {
      scrollTop: 500,
      clientHeight: 400,
      scrollHeight: 960,
      addEventListener: vi.fn(),
    };
    const ctx = makeContext({
      api: apiFn,
      elements: [["col-done", col]],
    });
    loadApiCoreStack(ctx);
    vm.runInContext(
      `
      showArchived = true;
      archivedTasks = [${JSON.stringify(task("a1", { archived: true }))}];
      archivedPage = { loadState: 'idle', hasMoreBefore: true, hasMoreAfter: false };
    `,
      ctx,
    );

    ctx.onDoneColumnScroll();
    // 500 + 400 = 900 >= 960 - 160 = 800, so "before" should trigger
    expect(apiFn).toHaveBeenCalled();
  });

  it("triggers loadArchivedTasksPage after when near top", async () => {
    const apiFn = vi.fn().mockResolvedValue({
      tasks: [],
      has_more_before: false,
      has_more_after: false,
    });
    const col = {
      scrollTop: 50,
      clientHeight: 400,
      scrollHeight: 1000,
      addEventListener: vi.fn(),
    };
    const ctx = makeContext({
      api: apiFn,
      elements: [["col-done", col]],
    });
    loadApiCoreStack(ctx);
    vm.runInContext(
      `
      showArchived = true;
      archivedTasks = [${JSON.stringify(task("a1", { archived: true }))}];
      archivedPage = { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: true };
    `,
      ctx,
    );

    ctx.onDoneColumnScroll();
    // scrollTop 50 <= 80, so "after" should trigger
    expect(apiFn).toHaveBeenCalled();
  });

  it("does nothing when showArchived is false", () => {
    const apiFn = vi.fn();
    const ctx = makeContext({ api: apiFn });
    loadApiCoreStack(ctx);
    vm.runInContext("showArchived = false;", ctx);

    ctx.onDoneColumnScroll();
    expect(apiFn).not.toHaveBeenCalled();
  });

  it("does nothing when col-done element is not found", () => {
    const apiFn = vi.fn();
    const ctx = makeContext({ api: apiFn });
    loadApiCoreStack(ctx);
    vm.runInContext("showArchived = true;", ctx);

    ctx.onDoneColumnScroll();
    expect(apiFn).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Automation toggles — other variants
// ---------------------------------------------------------------------------

describe("toggleAutotest", () => {
  it("reverts checkbox on API failure", async () => {
    const toggle = makeInput(true);
    const apiFn = vi.fn().mockRejectedValue(new Error("fail"));
    const ctx = makeContext({
      elements: [["autotest-toggle", toggle]],
      api: apiFn,
    });
    loadApiCoreStack(ctx);
    vm.runInContext("autotest = false;", ctx);

    await ctx.toggleAutotest();

    expect(ctx.showAlert).toHaveBeenCalled();
    expect(toggle.checked).toBe(false);
  });

  it("updates state on successful toggle", async () => {
    const toggle = makeInput(true);
    const apiFn = vi.fn().mockResolvedValue({ autotest: true });
    const ctx = makeContext({
      elements: [["autotest-toggle", toggle]],
      api: apiFn,
    });
    loadApiCoreStack(ctx);
    vm.runInContext("autotest = false;", ctx);

    await ctx.toggleAutotest();

    expect(vm.runInContext("autotest", ctx)).toBe(true);
    expect(toggle.checked).toBe(true);
  });
});

describe("toggleAutosubmit", () => {
  it("reverts on failure", async () => {
    const toggle = makeInput(true);
    const apiFn = vi.fn().mockRejectedValue(new Error("fail"));
    const ctx = makeContext({
      elements: [["autosubmit-toggle", toggle]],
      api: apiFn,
    });
    loadApiCoreStack(ctx);
    vm.runInContext("autosubmit = false;", ctx);

    await ctx.toggleAutosubmit();
    expect(toggle.checked).toBe(false);
  });
});

describe("toggleAutosync", () => {
  it("reverts on failure", async () => {
    const toggle = makeInput(true);
    const apiFn = vi.fn().mockRejectedValue(new Error("fail"));
    const ctx = makeContext({
      elements: [["autosync-toggle", toggle]],
      api: apiFn,
    });
    loadApiCoreStack(ctx);
    vm.runInContext("autosync = false;", ctx);

    await ctx.toggleAutosync();
    expect(toggle.checked).toBe(false);
  });
});

describe("toggleAutopush", () => {
  it("reverts on failure", async () => {
    const toggle = makeInput(true);
    const apiFn = vi.fn().mockRejectedValue(new Error("fail"));
    const ctx = makeContext({
      elements: [["autopush-toggle", toggle]],
      api: apiFn,
    });
    loadApiCoreStack(ctx);
    vm.runInContext("autopush = false;", ctx);

    await ctx.toggleAutopush();
    expect(toggle.checked).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// updateAutomationActiveCount
// ---------------------------------------------------------------------------

describe("updateAutomationActiveCount", () => {
  it("counts active toggles and shows badge", () => {
    const badge = { textContent: "", style: { display: "" } };
    const toggle1 = makeInput(true);
    const toggle2 = makeInput(false);
    const toggle3 = makeInput(true);
    const ctx = makeContext({
      elements: [
        ["autopilot-toggle", toggle1],
        ["autotest-toggle", toggle2],
        ["autosubmit-toggle", toggle3],
        ["automation-active-count", badge],
      ],
    });
    loadApiCoreStack(ctx);

    ctx.updateAutomationActiveCount();
    expect(badge.textContent).toBe(2);
    expect(badge.style.display).toBe("");
  });

  it("hides badge when no toggles are active", () => {
    const badge = { textContent: "", style: { display: "" } };
    const ctx = makeContext({
      elements: [["automation-active-count", badge]],
    });
    loadApiCoreStack(ctx);

    ctx.updateAutomationActiveCount();
    expect(badge.style.display).toBe("none");
  });
});

// ---------------------------------------------------------------------------
// updateWatcherHealth
// ---------------------------------------------------------------------------

describe("updateWatcherHealth", () => {
  it("renders all-healthy message when no tripped breakers", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["watcher-health-section", el]],
    });
    loadApiCoreStack(ctx);

    ctx.updateWatcherHealth([{ name: "auto-promote", healthy: true }]);
    expect(el.innerHTML).toContain("All healthy");
  });

  it("renders tripped breaker details", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["watcher-health-section", el]],
    });
    loadApiCoreStack(ctx);

    ctx.updateWatcherHealth([
      {
        name: "auto-retry",
        healthy: false,
        failures: 3,
        retry_at: new Date(Date.now() + 60000).toISOString(),
        last_reason: "timeout",
      },
    ]);
    expect(el.innerHTML).toContain("Retry");
    expect(el.innerHTML).toContain("3 failures");
  });

  it("clears section when entries is empty", () => {
    const el = { innerHTML: "old content" };
    const ctx = makeContext({
      elements: [["watcher-health-section", el]],
    });
    loadApiCoreStack(ctx);

    ctx.updateWatcherHealth([]);
    expect(el.innerHTML).toBe("");
  });

  it("clears section when entries is null", () => {
    const el = { innerHTML: "old content" };
    const ctx = makeContext({
      elements: [["watcher-health-section", el]],
    });
    loadApiCoreStack(ctx);

    ctx.updateWatcherHealth(null);
    expect(el.innerHTML).toBe("");
  });

  it("does nothing when element is not found", () => {
    const ctx = makeContext();
    loadApiCoreStack(ctx);
    // Should not throw
    ctx.updateWatcherHealth([{ name: "x", healthy: false }]);
  });
});

// ---------------------------------------------------------------------------
// toggleAutomationMenu / hideAutomationMenu
// ---------------------------------------------------------------------------

describe("toggleAutomationMenu", () => {
  it("toggles the hidden class on the automation menu", () => {
    const menu = {
      classList: {
        _classes: new Set(["hidden"]),
        toggle(cls) {
          if (this._classes.has(cls)) this._classes.delete(cls);
          else this._classes.add(cls);
        },
        add(cls) {
          this._classes.add(cls);
        },
        has(cls) {
          return this._classes.has(cls);
        },
      },
    };
    const ctx = makeContext({
      elements: [["automation-menu", menu]],
    });
    loadApiCoreStack(ctx);

    ctx.toggleAutomationMenu({ stopPropagation: vi.fn() });
    expect(menu.classList._classes.has("hidden")).toBe(false);

    ctx.toggleAutomationMenu();
    expect(menu.classList._classes.has("hidden")).toBe(true);
  });
});

describe("hideAutomationMenu", () => {
  it("adds the hidden class", () => {
    const menu = {
      classList: {
        _classes: new Set(),
        add(cls) {
          this._classes.add(cls);
        },
      },
    };
    const ctx = makeContext({
      elements: [["automation-menu", menu]],
    });
    loadApiCoreStack(ctx);

    ctx.hideAutomationMenu();
    expect(menu.classList._classes.has("hidden")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// waitForTaskDelta — fallback to fetchTasks
// ---------------------------------------------------------------------------

describe("waitForTaskDelta", () => {
  it("falls back to fetchTasks when no tasksSource", async () => {
    const apiFn = vi.fn().mockResolvedValue([]);
    const ctx = makeContext({ api: apiFn });
    loadApiCoreStack(ctx);
    vm.runInContext("tasksSource = null;", ctx);

    await ctx.waitForTaskDelta("t1");
    expect(apiFn).toHaveBeenCalledWith("/api/tasks");
  });

  it("falls back to fetchTasks when tasksSource is closed", async () => {
    const apiFn = vi.fn().mockResolvedValue([]);
    const ctx = makeContext({ api: apiFn });
    loadApiCoreStack(ctx);
    vm.runInContext("tasksSource = { readyState: 2 };", ctx); // CLOSED = 2

    await ctx.waitForTaskDelta("t1");
    expect(apiFn).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// findTaskById
// ---------------------------------------------------------------------------

describe("findTaskById", () => {
  it("finds task in active tasks", () => {
    const ctx = makeContext();
    loadApiCoreStack(ctx);
    vm.runInContext('tasks = [{ id: "t1", title: "A" }];', ctx);

    const found = ctx.findTaskById("t1");
    expect(found.id).toBe("t1");
  });

  it("finds task in archived tasks", () => {
    const ctx = makeContext();
    loadApiCoreStack(ctx);
    vm.runInContext(
      'tasks = []; archivedTasks = [{ id: "a1", title: "Archived" }];',
      ctx,
    );

    const found = ctx.findTaskById("a1");
    expect(found.id).toBe("a1");
  });

  it("returns null when task not found", () => {
    const ctx = makeContext();
    loadApiCoreStack(ctx);
    vm.runInContext("tasks = []; archivedTasks = [];", ctx);

    const found = ctx.findTaskById("missing");
    expect(found).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// SSE error handling — reconnect with jittered delay
// ---------------------------------------------------------------------------

describe("SSE error reconnect", () => {
  it("schedules reconnect with jittered delay on stream close", () => {
    const instances = [];
    class MockEventSource {
      constructor(url) {
        this.url = url;
        this.readyState = 1;
        this.listeners = {};
        instances.push(this);
      }
      addEventListener(type, handler) {
        if (!this.listeners[type]) this.listeners[type] = [];
        this.listeners[type].push(handler);
      }
      close() {
        this.closed = true;
      }
    }
    MockEventSource.CLOSED = 2;

    const scheduled = [];
    const ctx = makeContext({
      EventSource: MockEventSource,
      setTimeout: vi.fn((fn, delay) => {
        scheduled.push({ fn, delay });
        return 1;
      }),
    });
    loadApiCoreStack(ctx);
    vm.runInContext('activeWorkspaces = ["/repo"];', ctx);

    ctx.startTasksStream();
    const es = instances[0];

    // Simulate closed state
    es.readyState = MockEventSource.CLOSED;
    es.onerror();

    expect(scheduled.length).toBe(1);
    // Delay should be between 1000 and 2000 (jittered)
    expect(scheduled[0].delay).toBeGreaterThanOrEqual(1000);
    expect(scheduled[0].delay).toBeLessThanOrEqual(2000);
  });

  it("sets reconnecting state on non-closed error", () => {
    const instances = [];
    class MockEventSource {
      constructor(url) {
        this.url = url;
        this.readyState = 1;
        this.listeners = {};
        instances.push(this);
      }
      addEventListener(type, handler) {
        if (!this.listeners[type]) this.listeners[type] = [];
        this.listeners[type].push(handler);
      }
      close() {}
    }
    MockEventSource.CLOSED = 2;

    const ctx = makeContext({ EventSource: MockEventSource });
    loadApiCoreStack(ctx);
    vm.runInContext('activeWorkspaces = ["/repo"];', ctx);

    ctx.startTasksStream();
    // readyState is still 1 (not CLOSED), so it's a temporary error
    instances[0].onerror();

    expect(vm.runInContext("_sseConnState", ctx)).toBe("reconnecting");
  });
});

// ---------------------------------------------------------------------------
// SSE open event
// ---------------------------------------------------------------------------

describe("SSE open event", () => {
  it("sets connection state to ok on open", () => {
    const instances = [];
    class MockEventSource {
      constructor(url) {
        this.url = url;
        this.readyState = 1;
        this.listeners = {};
        instances.push(this);
      }
      addEventListener(type, handler) {
        if (!this.listeners[type]) this.listeners[type] = [];
        this.listeners[type].push(handler);
      }
      close() {}
    }
    MockEventSource.CLOSED = 2;

    const ctx = makeContext({ EventSource: MockEventSource });
    loadApiCoreStack(ctx);
    vm.runInContext('activeWorkspaces = ["/repo"];', ctx);

    ctx.startTasksStream();
    instances[0].listeners["open"][0]();

    expect(vm.runInContext("_sseConnState", ctx)).toBe("ok");
  });
});

// ---------------------------------------------------------------------------
// ensureArchivedScrollBinding
// ---------------------------------------------------------------------------

describe("ensureArchivedScrollBinding", () => {
  it("binds scroll handler only once", () => {
    const col = { addEventListener: vi.fn() };
    const ctx = makeContext({
      elements: [["col-done", col]],
    });
    loadApiCoreStack(ctx);

    ctx.ensureArchivedScrollBinding();
    ctx.ensureArchivedScrollBinding();

    expect(col.addEventListener).toHaveBeenCalledTimes(1);
    expect(col.addEventListener).toHaveBeenCalledWith(
      "scroll",
      expect.any(Function),
    );
  });
});
