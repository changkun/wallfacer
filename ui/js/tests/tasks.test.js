/**
 * Tests for the "Send to Plan" card action button.
 *
 * sendToPlanButton_visibleStates — rendered on backlog and waiting, hidden otherwise.
 * sendToPlanButton_invokesHelper — clicking the button calls openPlanForTask with the task id.
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
      createElement: () => ({ innerHTML: "" }),
      querySelectorAll: () => [],
      addEventListener: () => {},
      readyState: "complete",
    },
    tasks: [],
    archivedTasks: [],
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
    EventSource: class {
      constructor() {
        this.readyState = 1;
      }
      addEventListener() {}
      close() {}
      static get CLOSED() {
        return 2;
      }
    },
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
    activeWorkspaces: ["~/project"],
    getOpenModalTaskId: vi.fn(() => null),
    renderModalDependencies: vi.fn(),
    openPlanForTask: vi.fn(),
    ...options,
  });

  return ctx;
}

// ---------------------------------------------------------------------------
// sendToPlanButton_visibleStates
// ---------------------------------------------------------------------------
describe("sendToPlanButton_visibleStates", () => {
  let ctx;

  beforeEach(() => {
    ctx = createContext();
    loadScript("render.js", ctx);
  });

  it("renders Send to Plan button for backlog tasks", () => {
    const html = ctx.buildCardActions({ status: "backlog", id: "t1" });
    expect(html).toContain("card-action-send-to-plan");
  });

  it("renders Send to Plan button for waiting tasks", () => {
    const html = ctx.buildCardActions({ status: "waiting", id: "t1" });
    expect(html).toContain("card-action-send-to-plan");
  });

  it("does not render Send to Plan for in_progress tasks", () => {
    const html = ctx.buildCardActions({ status: "in_progress", id: "t1" });
    expect(html).not.toContain("card-action-send-to-plan");
  });

  it("does not render Send to Plan for done tasks", () => {
    const html = ctx.buildCardActions({ status: "done", id: "t1" });
    expect(html).not.toContain("card-action-send-to-plan");
  });

  it("does not render Send to Plan for failed tasks", () => {
    const html = ctx.buildCardActions({ status: "failed", id: "t1" });
    expect(html).not.toContain("card-action-send-to-plan");
  });

  it("does not render Send to Plan for cancelled tasks", () => {
    const html = ctx.buildCardActions({ status: "cancelled", id: "t1" });
    expect(html).not.toContain("card-action-send-to-plan");
  });

  it("does not render Send to Plan for archived tasks", () => {
    const html = ctx.buildCardActions({
      archived: true,
      status: "done",
      id: "t1",
    });
    expect(html).toBe("");
  });
});

// ---------------------------------------------------------------------------
// sendToPlanButton_invokesHelper
// ---------------------------------------------------------------------------
describe("sendToPlanButton_invokesHelper", () => {
  let ctx;

  beforeEach(() => {
    ctx = createContext();
    loadScript("render.js", ctx);
  });

  it("backlog card button calls openPlanForTask with the task id", () => {
    const html = ctx.buildCardActions({ status: "backlog", id: "task-abc" });
    expect(html).toContain("openPlanForTask('task-abc')");
  });

  it("waiting card button calls openPlanForTask with the task id", () => {
    const html = ctx.buildCardActions({ status: "waiting", id: "task-xyz" });
    expect(html).toContain("openPlanForTask('task-xyz')");
  });
});
