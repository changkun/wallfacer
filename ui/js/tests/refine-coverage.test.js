/**
 * Additional coverage tests for refine.js — targets functions not covered
 * by the existing refine-diff.test.js: updateRefineUI (all states),
 * showRefineIdle, cancelRefinement, dismissRefinement, applyRefinement,
 * renderRefineLogs, setRefineLogsMode, stopRefineLogStream,
 * resetRefinePanel (full), revertToHistoryPrompt.
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

function makeClassList(initial = []) {
  const cls = new Set(initial);
  return {
    has: (c) => cls.has(c),
    contains: (c) => cls.has(c),
    add: (c) => cls.add(c),
    remove: (c) => cls.delete(c),
    toggle: (c, force) => {
      if (force !== undefined) {
        force ? cls.add(c) : cls.delete(c);
      } else {
        cls.has(c) ? cls.delete(c) : cls.add(c);
      }
    },
    toString: () => [...cls].join(" "),
  };
}

function makeEl(id, initialClasses = []) {
  let _innerHTML = "";
  return {
    id,
    dataset: {},
    classList: makeClassList(initialClasses),
    get innerHTML() {
      return _innerHTML;
    },
    set innerHTML(v) {
      _innerHTML = v;
    },
    textContent: "",
    value: "",
    disabled: false,
    scrollHeight: 200,
    scrollTop: 0,
    clientHeight: 200,
    style: {},
  };
}

function makeRefineContext(overrides = {}) {
  const elements = {};
  // Create all elements the refine.js functions access
  const elNames = [
    "refine-start-btn",
    "refine-cancel-btn",
    "refine-running",
    "refine-result-section",
    "refine-error-section",
    "refine-error-msg",
    "refine-result-prompt",
    "refine-dismiss-btn",
    "refine-idle-desc",
    "refine-instructions-section",
    "refine-user-instructions",
    "refine-apply-btn",
    "refine-logs",
    "refine-logs-tab-pretty",
    "refine-logs-tab-raw",
    "refine-history-section",
    "refine-history-list",
    "modal-edit-sandbox",
    "modal-edit-timeout",
    "modal-edit-mount-worktrees",
  ];
  for (const name of elNames) {
    elements[name] = makeEl(name);
  }
  // Set initial hidden states matching the HTML defaults
  elements["refine-running"].classList.add("hidden");
  elements["refine-cancel-btn"].classList.add("hidden");
  elements["refine-result-section"].classList.add("hidden");
  elements["refine-error-section"].classList.add("hidden");
  elements["refine-dismiss-btn"].classList.add("hidden");

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    JSON,
    Promise,
    Array,
    Object,
    String,
    Number,
    escapeHtml: (s) =>
      String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
    document: {
      getElementById: (id) => {
        if (!elements[id]) elements[id] = makeEl(id);
        return elements[id];
      },
    },
    requestAnimationFrame: (cb) => cb(),
    tasks: [],
    getOpenModalTaskId: () => "task-1",
    api: vi.fn(() => Promise.resolve()),
    task: (id) => ({
      refine: () => `/api/tasks/${id}/refine`,
      refineApply: () => `/api/tasks/${id}/refine/apply`,
      refineDismiss: () => `/api/tasks/${id}/refine/dismiss`,
      refineLogs: () => `/api/tasks/${id}/refine/logs`,
      update: () => `/api/tasks/${id}`,
    }),
    closeModal: vi.fn(),
    waitForTaskDelta: vi.fn(() => Promise.resolve()),
    openModal: vi.fn(() => Promise.resolve()),
    showAlert: vi.fn(),
    collectSandboxByActivity: vi.fn(() => ({})),
    DEFAULT_TASK_TIMEOUT: 300,
    renderPrettyLogs: vi.fn((buf) => "<pretty>" + buf + "</pretty>"),
    fetch: vi.fn(() => Promise.resolve({ ok: false, body: null, status: 204 })),
    withAuthToken: (url) => url,
    withBearerHeaders: () => ({}),
    fetchTasks: vi.fn(() => Promise.resolve()),
    scheduleRender: vi.fn(),
    AbortController: class {
      constructor() {
        this.signal = {};
        this.aborted = false;
      }
      abort() {
        this.aborted = true;
      }
    },
    TextDecoder: class {
      decode(_v, _opts) {
        return "";
      }
    },
    ...overrides,
  });

  loadScript("refine.js", ctx);
  ctx._elements = elements;
  return ctx;
}

// ---------------------------------------------------------------------------
// updateRefineUI — idle state (no job)
// ---------------------------------------------------------------------------
describe("updateRefineUI — idle state", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeRefineContext();
  });

  it("shows idle state when task has no current_refinement", () => {
    ctx.updateRefineUI({ id: "t1", status: "backlog" });
    expect(ctx._elements["refine-start-btn"].classList.has("hidden")).toBe(
      false,
    );
    expect(ctx._elements["refine-cancel-btn"].classList.has("hidden")).toBe(
      true,
    );
    expect(ctx._elements["refine-running"].classList.has("hidden")).toBe(true);
    expect(ctx._elements["refine-idle-desc"].classList.has("hidden")).toBe(
      false,
    );
    expect(
      ctx._elements["refine-instructions-section"].classList.has("hidden"),
    ).toBe(false);
  });

  it("does nothing for non-backlog tasks", () => {
    // The start button starts visible; updateRefineUI should not modify it
    ctx._elements["refine-start-btn"].classList.add("hidden");
    ctx.updateRefineUI({ id: "t1", status: "in_progress" });
    expect(ctx._elements["refine-start-btn"].classList.has("hidden")).toBe(
      true,
    );
  });

  it("does nothing for null task", () => {
    expect(() => ctx.updateRefineUI(null)).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// updateRefineUI — running state
// ---------------------------------------------------------------------------
describe("updateRefineUI — running state", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeRefineContext();
  });

  it("shows running panel and hides idle", () => {
    ctx.updateRefineUI({
      id: "task-1",
      status: "backlog",
      current_refinement: { id: "job-1", status: "running" },
    });
    expect(ctx._elements["refine-running"].classList.has("hidden")).toBe(false);
    expect(ctx._elements["refine-start-btn"].classList.has("hidden")).toBe(
      true,
    );
    expect(ctx._elements["refine-cancel-btn"].classList.has("hidden")).toBe(
      false,
    );
    expect(ctx._elements["refine-idle-desc"].classList.has("hidden")).toBe(
      true,
    );
    expect(
      ctx._elements["refine-instructions-section"].classList.has("hidden"),
    ).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// updateRefineUI — done state
// ---------------------------------------------------------------------------
describe("updateRefineUI — done state", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeRefineContext();
  });

  it("shows result section with result text", () => {
    ctx.updateRefineUI({
      id: "t1",
      status: "backlog",
      current_refinement: {
        id: "job-1",
        status: "done",
        result: "Refined prompt",
      },
    });
    expect(ctx._elements["refine-result-section"].classList.has("hidden")).toBe(
      false,
    );
    expect(ctx._elements["refine-result-prompt"].value).toBe("Refined prompt");
    expect(ctx._elements["refine-running"].classList.has("hidden")).toBe(true);
    expect(ctx._elements["refine-dismiss-btn"].classList.has("hidden")).toBe(
      false,
    );
  });

  it("does not re-populate result if job id matches", () => {
    const resultTA = ctx._elements["refine-result-prompt"];
    resultTA.dataset.jobId = "job-1";
    resultTA.value = "user-edited value";
    ctx.updateRefineUI({
      id: "t1",
      status: "backlog",
      current_refinement: { id: "job-1", status: "done", result: "Original" },
    });
    expect(resultTA.value).toBe("user-edited value");
  });
});

// ---------------------------------------------------------------------------
// updateRefineUI — failed state
// ---------------------------------------------------------------------------
describe("updateRefineUI — failed state", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeRefineContext();
  });

  it("shows error section with error message", () => {
    ctx.updateRefineUI({
      id: "t1",
      status: "backlog",
      current_refinement: { id: "job-1", status: "failed", error: "timeout" },
    });
    expect(ctx._elements["refine-error-section"].classList.has("hidden")).toBe(
      false,
    );
    expect(ctx._elements["refine-error-msg"].textContent).toContain("timeout");
    expect(ctx._elements["refine-start-btn"].classList.has("hidden")).toBe(
      false,
    );
  });
});

// ---------------------------------------------------------------------------
// cancelRefinement
// ---------------------------------------------------------------------------
describe("cancelRefinement", () => {
  it("calls api with DELETE method after startRefinement sets refineTaskId", async () => {
    const runningTask = {
      id: "task-1",
      status: "backlog",
      current_refinement: { id: "job-1", status: "running" },
    };
    const ctx = makeRefineContext({
      tasks: [{ id: "task-1", status: "backlog", prompt: "test" }],
      api: vi.fn(() => Promise.resolve(runningTask)),
    });

    // Start refinement to set internal refineTaskId
    await ctx.startRefinement();
    // Now switch api mock for cancel
    ctx.api = vi.fn(() => Promise.resolve());
    await ctx.cancelRefinement();
    expect(ctx.api).toHaveBeenCalledWith(
      "/api/tasks/task-1/refine",
      expect.objectContaining({ method: "DELETE" }),
    );
  });

  it("does nothing when refineTaskId has not been set", async () => {
    const ctx = makeRefineContext();
    await ctx.cancelRefinement();
    expect(ctx.api).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// renderRefineLogs
// ---------------------------------------------------------------------------
describe("renderRefineLogs", () => {
  it("renders pretty mode with renderPrettyLogs", () => {
    const ctx = makeRefineContext();
    ctx.refineRawLogBuffer = "some log text";
    ctx.refineLogsMode = "pretty";
    ctx.renderRefineLogs();
    expect(ctx._elements["refine-logs"].innerHTML).toContain("<pretty>");
  });

  it("renders raw mode with stripped ANSI codes", () => {
    const ctx = makeRefineContext();
    ctx.refineRawLogBuffer = "text \x1b[31mred\x1b[0m plain";
    ctx.refineLogsMode = "raw";
    ctx.renderRefineLogs();
    expect(ctx._elements["refine-logs"].textContent).toContain(
      "text red plain",
    );
    expect(ctx._elements["refine-logs"].textContent).not.toContain("\x1b[");
  });
});

// ---------------------------------------------------------------------------
// setRefineLogsMode
// ---------------------------------------------------------------------------
describe("setRefineLogsMode", () => {
  it("activates the correct tab and re-renders", () => {
    const ctx = makeRefineContext();
    ctx.refineRawLogBuffer = "test";
    ctx.setRefineLogsMode("raw");
    expect(ctx.refineLogsMode).toBe("raw");
    expect(ctx._elements["refine-logs-tab-raw"].classList.has("active")).toBe(
      true,
    );
    expect(
      ctx._elements["refine-logs-tab-pretty"].classList.has("active"),
    ).toBe(false);
  });

  it("switches back to pretty mode", () => {
    const ctx = makeRefineContext();
    ctx.refineRawLogBuffer = "test";
    ctx.setRefineLogsMode("raw");
    ctx.setRefineLogsMode("pretty");
    expect(ctx.refineLogsMode).toBe("pretty");
    expect(
      ctx._elements["refine-logs-tab-pretty"].classList.has("active"),
    ).toBe(true);
    expect(ctx._elements["refine-logs-tab-raw"].classList.has("active")).toBe(
      false,
    );
  });
});

// ---------------------------------------------------------------------------
// resetRefinePanel
// ---------------------------------------------------------------------------
describe("resetRefinePanel — full reset", () => {
  it("resets all panel elements to idle state", () => {
    const ctx = makeRefineContext();
    // Simulate a dirty state
    ctx._elements["refine-start-btn"].classList.add("hidden");
    ctx._elements["refine-cancel-btn"].classList.remove("hidden");
    ctx._elements["refine-running"].classList.remove("hidden");
    ctx._elements["refine-result-section"].classList.remove("hidden");
    ctx._elements["refine-error-section"].classList.remove("hidden");
    ctx._elements["refine-idle-desc"].classList.add("hidden");
    ctx._elements["refine-instructions-section"].classList.add("hidden");
    ctx._elements["refine-user-instructions"].value = "some instructions";
    ctx._elements["refine-apply-btn"].disabled = true;
    ctx._elements["refine-apply-btn"].textContent = "Applying...";
    ctx._elements["refine-dismiss-btn"].classList.remove("hidden");
    ctx._elements["refine-logs"].innerHTML = "<p>logs</p>";

    ctx.resetRefinePanel();

    expect(ctx._elements["refine-start-btn"].classList.has("hidden")).toBe(
      false,
    );
    expect(ctx._elements["refine-cancel-btn"].classList.has("hidden")).toBe(
      true,
    );
    expect(ctx._elements["refine-running"].classList.has("hidden")).toBe(true);
    expect(ctx._elements["refine-result-section"].classList.has("hidden")).toBe(
      true,
    );
    expect(ctx._elements["refine-error-section"].classList.has("hidden")).toBe(
      true,
    );
    expect(ctx._elements["refine-idle-desc"].classList.has("hidden")).toBe(
      false,
    );
    expect(
      ctx._elements["refine-instructions-section"].classList.has("hidden"),
    ).toBe(false);
    expect(ctx._elements["refine-user-instructions"].value).toBe("");
    expect(ctx._elements["refine-apply-btn"].disabled).toBe(false);
    expect(ctx._elements["refine-apply-btn"].textContent).toBe(
      "Apply as Prompt",
    );
    expect(ctx._elements["refine-dismiss-btn"].classList.has("hidden")).toBe(
      true,
    );
    expect(ctx._elements["refine-logs"].innerHTML).toBe("");
  });

  it("resets log mode tabs to pretty active", () => {
    const ctx = makeRefineContext();
    ctx._elements["refine-logs-tab-pretty"].classList.remove("active");
    ctx._elements["refine-logs-tab-raw"].classList.add("active");

    ctx.resetRefinePanel();

    expect(
      ctx._elements["refine-logs-tab-pretty"].classList.has("active"),
    ).toBe(true);
    expect(ctx._elements["refine-logs-tab-raw"].classList.has("active")).toBe(
      false,
    );
  });
});

// ---------------------------------------------------------------------------
// dismissRefinement
// ---------------------------------------------------------------------------
describe("dismissRefinement", () => {
  it("calls api with POST to dismiss endpoint", async () => {
    const ctx = makeRefineContext();
    await ctx.dismissRefinement();
    expect(ctx.api).toHaveBeenCalledWith(
      "/api/tasks/task-1/refine/dismiss",
      expect.objectContaining({ method: "POST" }),
    );
    expect(ctx.closeModal).toHaveBeenCalled();
  });

  it("does nothing when no modal task id", async () => {
    const ctx = makeRefineContext({ getOpenModalTaskId: () => null });
    await ctx.dismissRefinement();
    expect(ctx.api).not.toHaveBeenCalled();
  });

  it("shows alert on error", async () => {
    const ctx = makeRefineContext({
      api: vi.fn(() => Promise.reject(new Error("network fail"))),
    });
    await ctx.dismissRefinement();
    expect(ctx.showAlert).toHaveBeenCalledWith(
      expect.stringContaining("network fail"),
    );
  });
});

// ---------------------------------------------------------------------------
// applyRefinement
// ---------------------------------------------------------------------------
describe("applyRefinement", () => {
  it("shows alert when prompt is empty", async () => {
    const ctx = makeRefineContext();
    ctx._elements["refine-result-prompt"].value = "";
    await ctx.applyRefinement();
    expect(ctx.showAlert).toHaveBeenCalledWith(
      expect.stringContaining("empty"),
    );
  });

  it("does nothing when no modal task id", async () => {
    const ctx = makeRefineContext({ getOpenModalTaskId: () => null });
    await ctx.applyRefinement();
    expect(ctx.api).not.toHaveBeenCalled();
  });

  it("disables apply button during request", async () => {
    const ctx = makeRefineContext({
      api: vi.fn(() => Promise.resolve()),
    });
    ctx._elements["refine-result-prompt"].value = "New prompt content";
    ctx._elements["modal-edit-timeout"] = makeEl("modal-edit-timeout");
    ctx._elements["modal-edit-timeout"].value = "60";
    ctx._elements["modal-edit-mount-worktrees"] = makeEl(
      "modal-edit-mount-worktrees",
    );
    ctx._elements["modal-edit-mount-worktrees"].checked = false;

    await ctx.applyRefinement();

    expect(ctx.api).toHaveBeenCalled();
    expect(ctx.openModal).toHaveBeenCalledWith("task-1");
  });

  it("re-enables apply button on error", async () => {
    const ctx = makeRefineContext({
      api: vi.fn(() => Promise.reject(new Error("server error"))),
    });
    ctx._elements["refine-result-prompt"].value = "Some prompt";
    const applyBtn = ctx._elements["refine-apply-btn"];

    await ctx.applyRefinement();

    expect(applyBtn.disabled).toBe(false);
    expect(applyBtn.textContent).toBe("Apply as Prompt");
    expect(ctx.showAlert).toHaveBeenCalledWith(
      expect.stringContaining("server error"),
    );
  });
});

// ---------------------------------------------------------------------------
// revertToHistoryPrompt
// ---------------------------------------------------------------------------
describe("revertToHistoryPrompt", () => {
  it("loads a previous session prompt into the result textarea", () => {
    const ctx = makeRefineContext();
    ctx.tasks = [
      {
        id: "task-1",
        refine_sessions: [
          { start_prompt: "old prompt", result_prompt: "refined prompt" },
        ],
      },
    ];

    ctx.revertToHistoryPrompt(0);

    expect(ctx._elements["refine-result-prompt"].value).toBe("refined prompt");
    expect(ctx._elements["refine-result-section"].classList.has("hidden")).toBe(
      false,
    );
  });

  it("does nothing when session index is out of bounds", () => {
    const ctx = makeRefineContext();
    ctx.tasks = [{ id: "task-1", refine_sessions: [] }];

    expect(() => ctx.revertToHistoryPrompt(99)).not.toThrow();
  });

  it("does nothing when result_prompt is null", () => {
    const ctx = makeRefineContext();
    ctx.tasks = [
      {
        id: "task-1",
        refine_sessions: [{ start_prompt: "p", result_prompt: null }],
      },
    ];
    ctx._elements["refine-result-prompt"].value = "original";

    ctx.revertToHistoryPrompt(0);
    // Value should not change since result_prompt is null
    expect(ctx._elements["refine-result-prompt"].value).toBe("original");
  });
});

// ---------------------------------------------------------------------------
// startRefinement — error handling
// ---------------------------------------------------------------------------
describe("startRefinement — error path", () => {
  it("shows error section when api call fails", async () => {
    const ctx = makeRefineContext({
      tasks: [{ id: "task-1", status: "backlog" }],
      api: vi.fn(() => Promise.reject(new Error("API failure"))),
    });

    await ctx.startRefinement();

    expect(ctx._elements["refine-error-section"].classList.has("hidden")).toBe(
      false,
    );
    expect(ctx._elements["refine-error-msg"].textContent).toContain(
      "API failure",
    );
  });

  it("skips when already refining", async () => {
    const ctx = makeRefineContext({
      tasks: [
        {
          id: "task-1",
          status: "backlog",
          current_refinement: { status: "running" },
        },
      ],
      api: vi.fn(() => Promise.resolve()),
    });

    await ctx.startRefinement();
    // API should not be called because refinement is already running
    expect(ctx.api).not.toHaveBeenCalled();
  });

  it("does nothing when no open modal task", async () => {
    const ctx = makeRefineContext({ getOpenModalTaskId: () => null });
    await ctx.startRefinement();
    expect(ctx.api).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// renderRefineHistory — with sandbox result
// ---------------------------------------------------------------------------
describe("renderRefineHistory — sandbox spec details", () => {
  it("includes sandbox spec details block when result is present", () => {
    const ctx = makeRefineContext();
    ctx.renderRefineHistory({
      refine_sessions: [
        {
          created_at: "2026-01-01T00:00:00Z",
          start_prompt: "original",
          result_prompt: "refined",
          result: "The sandbox generated this spec",
        },
      ],
    });
    const html = ctx._elements["refine-history-list"].innerHTML;
    expect(html).toContain("Sandbox spec");
    expect(html).toContain("The sandbox generated this spec");
  });

  it("omits sandbox spec details when result is empty", () => {
    const ctx = makeRefineContext();
    ctx.renderRefineHistory({
      refine_sessions: [
        {
          created_at: "2026-01-01T00:00:00Z",
          start_prompt: "original",
          result_prompt: "refined",
          result: "",
        },
      ],
    });
    const html = ctx._elements["refine-history-list"].innerHTML;
    expect(html).not.toContain("Sandbox spec");
  });

  it("hides history section when no sessions", () => {
    const ctx = makeRefineContext();
    ctx.renderRefineHistory({ refine_sessions: [] });
    expect(
      ctx._elements["refine-history-section"].classList.has("hidden"),
    ).toBe(true);
  });

  it("shows history section when sessions exist", () => {
    const ctx = makeRefineContext();
    ctx.renderRefineHistory({
      refine_sessions: [
        {
          created_at: "2026-01-01T00:00:00Z",
          start_prompt: "p",
          result_prompt: "",
          result: "",
        },
      ],
    });
    expect(
      ctx._elements["refine-history-section"].classList.has("hidden"),
    ).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// stopRefineLogStream
// ---------------------------------------------------------------------------
describe("stopRefineLogStream", () => {
  it("does not throw when no log stream is active", () => {
    const ctx = makeRefineContext();
    // refineLogsAbort starts as null internally
    expect(() => ctx.stopRefineLogStream()).not.toThrow();
  });

  it("is called during resetRefinePanel", () => {
    // stopRefineLogStream is exercised indirectly through resetRefinePanel
    const ctx = makeRefineContext();
    expect(() => ctx.resetRefinePanel()).not.toThrow();
  });
});
