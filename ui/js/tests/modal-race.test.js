import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function loadScript(filename, ctx) {
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

function makeEl(id = "") {
  return {
    id,
    innerHTML: "",
    textContent: "",
    value: "",
    checked: false,
    style: {},
    dataset: {},
    classList: {
      _classes: new Set(),
      add(c) {
        this._classes.add(c);
      },
      remove(c) {
        this._classes.delete(c);
      },
      contains(c) {
        return this._classes.has(c);
      },
      toggle(c, force) {
        if (force !== undefined) {
          force ? this._classes.add(c) : this._classes.delete(c);
        } else {
          this._classes.has(c) ? this._classes.delete(c) : this._classes.add(c);
        }
      },
    },
    querySelector: () => null,
    querySelectorAll: () => [],
    appendChild: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    hasAttribute: () => false,
    focus: () => {},
    remove: () => {},
  };
}

class MockAbortController {
  constructor() {
    const handlers = [];
    this.signal = {
      aborted: false,
      addEventListener(type, cb) {
        if (type === "abort") handlers.push(cb);
      },
    };
    this._handlers = handlers;
  }

  abort() {
    if (this.signal.aborted) return;
    this.signal.aborted = true;
    this._handlers.forEach((fn) => fn());
  }
}

function makeRaceContext(overrides = {}) {
  const elements = {};
  function getEl(id) {
    if (!elements[id]) elements[id] = makeEl(id);
    return elements[id];
  }

  const now = new Date().toISOString();
  const tasks = [
    {
      id: "task-1",
      status: "done",
      prompt: "p1",
      created_at: now,
      title: "T1",
      tags: [],
      usage: null,
      usage_breakdown: null,
      worktree_paths: { repo: "/tmp/repo" },
      prompt_history: [],
      session_id: null,
      turns: 1,
      is_test_run: false,
      last_test_result: null,
      test_run_start_turn: 0,
      archived: false,
      result: null,
    },
    {
      id: "task-2",
      status: "done",
      prompt: "p2",
      created_at: now,
      title: "T2",
      tags: [],
      usage: null,
      usage_breakdown: null,
      worktree_paths: { repo: "/tmp/repo" },
      prompt_history: [],
      session_id: null,
      turns: 1,
      is_test_run: false,
      last_test_result: null,
      test_run_start_turn: 0,
      archived: false,
      result: null,
    },
  ];

  const delays = {
    "/api/tasks/task-1/events": 70,
    "/api/tasks/task-1/diff": 80,
    "/api/tasks/task-2/events": 10,
    "/api/tasks/task-2/diff": 15,
  };

  const payloads = {
    "/api/tasks/task-1/events": {
      events: [
        {
          event_type: "output",
          created_at: now,
          data: { result: "result-from-task-1", stop_reason: "end_turn" },
        },
      ],
      next_after: 1,
      has_more: false,
      total_filtered: 1,
    },
    "/api/tasks/task-1/diff": { diff: "diff-from-task-1", behind_counts: {} },
    "/api/tasks/task-2/events": {
      events: [
        {
          event_type: "output",
          created_at: now,
          data: { result: "result-from-task-2", stop_reason: "end_turn" },
        },
      ],
      next_after: 1,
      has_more: false,
      total_filtered: 1,
    },
    "/api/tasks/task-2/diff": { diff: "diff-from-task-2", behind_counts: {} },
    ...(overrides.payloads || {}),
  };

  function api(path, opts = {}) {
    // Strip query params for payload/delay lookup so cursor pagination params
    // (limit, types, after) don't break the test fixture matching.
    const basePath = path.split("?")[0];
    return new Promise((resolve, reject) => {
      if (opts.signal && opts.signal.aborted) {
        const err = new Error("aborted");
        err.name = "AbortError";
        reject(err);
        return;
      }
      const timer = setTimeout(
        () => resolve(payloads[basePath]),
        (overrides.delays && overrides.delays[basePath]) ||
          delays[basePath] ||
          0,
      );
      if (opts.signal && typeof opts.signal.addEventListener === "function") {
        opts.signal.addEventListener("abort", () => {
          clearTimeout(timer);
          const err = new Error("aborted");
          err.name = "AbortError";
          reject(err);
        });
      }
    });
  }

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    Promise,
    AbortController: MockAbortController,
    tasks,
    logsAbort: null,
    testLogsAbort: null,
    rawLogBuffer: "",
    testRawLogBuffer: "",
    logsMode: "pretty",
    logSearchQuery: "",
    oversightData: null,
    oversightFetching: false,
    timelineRefreshTimer: null,
    refineTaskId: null,
    refineRawLogBuffer: "",
    refineLogsMode: "pretty",
    history: { replaceState: () => {} },
    location: { hash: "", pathname: "/", search: "" },
    document: {
      getElementById: getEl,
      querySelector: (sel) =>
        sel === "#modal .modal-card" ? getEl("modal-card") : null,
      querySelectorAll: () => ({ forEach: () => {} }),
      createElement: () => makeEl(),
      head: { appendChild: () => {} },
      body: { appendChild: () => {} },
    },
    setTimeout,
    clearTimeout,
    setInterval: () => 0,
    clearInterval: () => {},
    requestAnimationFrame: (cb) => cb(),
    renderMarkdown: (s) => s || "",
    escapeHtml: (s) => String(s ?? ""),
    setLeftTab: () => {},
    setRightTab: () => {},
    startLogStream: () => {},
    startImplLogFetch: () => {},
    startTestLogStream: () => {},
    renderResultsFromEvents: (results) => {
      getEl("modal-results-list").innerHTML = results.join("|");
      getEl("modal-summary-section").classList.remove("hidden");
    },
    renderTestResultsFromEvents: () => {},
    renderRefineHistory: () => {},
    updateRefineUI: () => {},
    resetRefinePanel: () => {},
    applySandboxByActivity: () => {},
    populateDependsOnPicker: () => {},
    renderDiffFiles: (el, diff) => {
      el.innerHTML = diff || "";
    },
    syncTask: () => {},
    loadFlamegraph: () => {},
    renderTimeline: () => {},
    _startTimelineRefresh: () => {},
    _stopTimelineRefresh: () => {},
    api,
    BRAINSTORM_CATEGORIES: new Set(),
    DEFAULT_TASK_TIMEOUT: 60,
  });

  ctx.findTaskById = function (taskId) {
    return (ctx.tasks || []).find((task) => task.id === taskId) || null;
  };

  loadScript("modal-core.js", ctx);
  return { ctx, elements };
}

describe("modal open race safety", () => {
  it("keeps only the latest task data when openModal is called rapidly", async () => {
    const { ctx, elements } = makeRaceContext();

    const first = vm.runInContext("openModal('task-1')", ctx);
    await new Promise((r) => setTimeout(r, 5));
    const second = vm.runInContext("openModal('task-2')", ctx);

    await Promise.allSettled([first, second]);
    await new Promise((r) => setTimeout(r, 100));

    expect(elements["modal-id"].textContent).toBe("ID: task-2");
    expect(elements["modal-results-list"].innerHTML).toContain(
      "result-from-task-2",
    );
    expect(elements["modal-results-list"].innerHTML).not.toContain(
      "result-from-task-1",
    );
    expect(elements["modal-diff-files"].innerHTML).toContain(
      "diff-from-task-2",
    );
    expect(elements["modal-diff-files"].innerHTML).not.toContain(
      "diff-from-task-1",
    );
  });

  it("opens modal for tasks present only in archived window", async () => {
    const { ctx, elements } = makeRaceContext();
    vm.runInContext(
      `tasks = []; archivedTasks = [{
        id: 'archived-1',
        status: 'done',
        prompt: 'archived prompt',
        created_at: '${new Date().toISOString()}',
        title: 'Archived',
        tags: [],
        usage: null,
        usage_breakdown: null,
        worktree_paths: { repo: '/tmp/repo' },
        prompt_history: [],
        session_id: null,
        turns: 1,
        is_test_run: false,
        last_test_result: null,
        test_run_start_turn: 0,
        archived: true,
        result: null
      }];`,
      ctx,
    );

    await vm.runInContext("openModal('archived-1')", ctx);
    expect(elements["modal-id"].textContent).toBe("ID: archived-1");
  });

  it("renders structured conflict resolver events clearly", async () => {
    const { ctx, elements } = makeRaceContext({
      payloads: {
        "/api/tasks/task-1/events": {
          events: [
            {
              event_type: "system",
              created_at: new Date().toISOString(),
              data: {
                phase: "conflict_resolver",
                status: "handoff",
                trigger: "sync",
                repo: "repo-a",
                attempt: 3,
                max_attempts: 3,
                result:
                  "Automatic conflict resolver exhausted retries for repo-a. Handing off to the main agent for interactive resolution.",
              },
            },
          ],
          next_after: 1,
          has_more: false,
          total_filtered: 1,
        },
      },
    });

    await vm.runInContext("openModal('task-1')", ctx);
    expect(elements["modal-events"].innerHTML).toContain("Conflict Resolution");
    expect(elements["modal-events"].innerHTML).toContain("resolver handoff");
    expect(elements["modal-events"].innerHTML).toContain("repo-a");
    expect(elements["modal-events"].innerHTML).toContain("attempt 3/3");
  });

  it("renders dependency rows with title fallback, removed entries, and summary text", () => {
    const { ctx, elements } = makeRaceContext();
    vm.runInContext(
      `tasks = [
        {
          id: 'dep-1',
          status: 'in_progress',
          prompt: 'dependency prompt',
          created_at: '${new Date().toISOString()}',
          title: '',
          tags: [],
          usage: null,
          usage_breakdown: null,
          worktree_paths: {},
          prompt_history: [],
          session_id: null,
          turns: 0,
          is_test_run: false,
          last_test_result: null,
          test_run_start_turn: 0,
          archived: false,
          result: null
        },
        {
          id: 'task-1',
          status: 'backlog',
          prompt: 'task prompt',
          created_at: '${new Date().toISOString()}',
          title: 'Task 1',
          depends_on: ['dep-1', 'missing-dep'],
          tags: [],
          usage: null,
          usage_breakdown: null,
          worktree_paths: {},
          prompt_history: [],
          session_id: null,
          turns: 0,
          is_test_run: false,
          last_test_result: null,
          test_run_start_turn: 0,
          archived: false,
          result: null
        }
      ];`,
      ctx,
    );

    vm.runInContext("renderModalDependencies(tasks[1])", ctx);

    expect(elements["modal-dependencies"].classList.contains("hidden")).toBe(
      false,
    );
    expect(elements["modal-dependencies-list"].innerHTML).toContain(
      "in progress",
    );
    expect(elements["modal-dependencies-list"].innerHTML).toContain(
      "openModal('dep-1')",
    );
    expect(elements["modal-dependencies-list"].innerHTML).toContain("dep-1");
    expect(elements["modal-dependencies-list"].innerHTML).toContain(
      "[removed] missing-",
    );
    expect(elements["modal-dependencies-summary"].textContent).toBe(
      "Waiting on 2 of 2 tasks",
    );
  });
});
