/**
 * Regression test for the shared cancel-button state bug.
 *
 * Before the fix, cancelTask mutated the single #modal-cancel-btn element's
 * innerHTML/disabled to "Shutting down…" and only restored it in a finally
 * block that ran after the HTTP round-trip. If the user closed the modal
 * and opened another task's modal while the cancel was still in flight,
 * the second modal showed the same stale "Shutting down…" state, making it
 * look as if every task was cancelling.
 *
 * The fix derives the button's state from the per-task pendingCancelTaskIds
 * set, keyed on whichever task is currently displayed in the modal.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import vm from "node:vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const tasksSrc = readFileSync(join(__dirname, "..", "tasks.js"), "utf8");

function makeBtn() {
  const label = { textContent: "Cancel" };
  const hint = { textContent: "discard changes" };
  const btn = {
    disabled: false,
    querySelector(sel) {
      if (sel === ".aside-action__label") return label;
      if (sel === ".aside-action__hint") return hint;
      return null;
    },
  };
  return { btn, label, hint };
}

function makeCtx(pendingIds) {
  const { btn, label, hint } = makeBtn();
  const ctx = {
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
    localStorage: { getItem: () => null, setItem: () => {} },
    location: { hash: "" },
    window: { location: { hash: "" } },
    document: {
      getElementById: (id) => {
        if (id === "modal-cancel-btn") return btn;
        // tasks.js has top-level wiring on these elements; return stubs so
        // `vm.runInContext` does not throw before the function under test
        // becomes callable.
        return { addEventListener: () => {}, value: "", innerHTML: "" };
      },
      createElement: () => ({ innerHTML: "" }),
      querySelectorAll: () => [],
      addEventListener: () => {},
      readyState: "complete",
    },
    tasks: [],
    archivedTasks: [],
    pendingCancelTaskIds: new Set(pendingIds || []),
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
    getTaskAccessibleTitle: () => "",
    formatTaskStatusLabel: (s) => s,
    openModal: vi.fn(() => Promise.resolve()),
    setRightTab: vi.fn(),
    setLeftTab: vi.fn(),
    _hashHandled: false,
    tasksRetryDelay: 1000,
    tasksSource: null,
    lastTasksEventId: null,
    archivedPage: { loadState: "idle" },
    archivedTasksPageSize: 20,
    archivedScrollHandlerBound: false,
    Routes: { tasks: { list: () => "/api/tasks", stream: () => "/" } },
    EventSource: class {
      addEventListener() {}
      close() {}
    },
    api: vi.fn(),
    escapeHtml: (s) => String(s || ""),
    renderMarkdown: (s) => String(s || ""),
    matchesFilter: () => true,
    updateIdeationFromTasks: () => {},
    updateBacklogSortButton: () => {},
    hideDependencyGraph: () => {},
    renderDependencyGraph: () => {},
    sandboxDisplayName: (s) => s,
    formatTimeout: (m) => String(m),
    timeAgo: () => "now",
    highlightMatch: (t) => t,
    taskDisplayPrompt: () => "",
    syncTask: vi.fn(),
    task: (id) => ({
      diff: () => `/api/tasks/${id}/diff`,
      update: () => `/api/tasks/${id}`,
      archive: () => `/api/tasks/${id}/archive`,
      done: () => `/api/tasks/${id}/done`,
      resume: () => `/api/tasks/${id}/resume`,
    }),
    activeWorkspaces: ["~/project"],
    getOpenModalTaskId: () => null,
    renderModalDependencies: vi.fn(),
    openPlanForTask: vi.fn(),
  };
  vm.createContext(ctx);
  vm.runInContext(tasksSrc, ctx, { filename: "tasks.js" });
  return { ctx, btn, label, hint };
}

describe("syncCancelButtonForTask", () => {
  it("shows 'Cancel' for a task not in the pending set", () => {
    const { ctx, btn, label, hint } = makeCtx([]);
    ctx.syncCancelButtonForTask("task-abc");
    expect(btn.disabled).toBe(false);
    expect(label.textContent).toBe("Cancel");
    expect(hint.textContent).toBe("discard changes");
  });

  it("shows 'Shutting down…' for a task that is in the pending set", () => {
    const { ctx, btn, label } = makeCtx(["task-abc"]);
    ctx.syncCancelButtonForTask("task-abc");
    expect(btn.disabled).toBe(true);
    expect(label.textContent).toContain("Shutting down");
  });

  it("shows 'Cancel' for task B when only task A is being cancelled", () => {
    // Regression: this is the user-reported bug. With task A in-flight, the
    // button must still show the ready state when the modal displays task B.
    const { ctx, btn, label } = makeCtx(["task-A"]);
    ctx.syncCancelButtonForTask("task-B");
    expect(btn.disabled).toBe(false);
    expect(label.textContent).toBe("Cancel");
  });

  it("is safe when the modal's task id is unknown", () => {
    const { ctx, btn, label } = makeCtx(["task-A"]);
    ctx.syncCancelButtonForTask(null);
    expect(btn.disabled).toBe(false);
    expect(label.textContent).toBe("Cancel");
  });
});
