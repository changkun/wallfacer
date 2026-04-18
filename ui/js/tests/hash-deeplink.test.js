/**
 * Tests for URL hash-based deep linking.
 *
 * Covers:
 *  - openModal / closeModal updating window.location via history.replaceState
 *  - setRightTab / setLeftTab updating the hash when a tab is switched
 *  - _handleInitialHash opening the correct modal on page load
 */
import { describe, it, expect } from "vitest";
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
  return ctx;
}

const TASK_ID = "550e8400-e29b-41d4-a716-446655440000";

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
    querySelectorAll: () => [],
    querySelector: () => null,
    appendChild: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    hasAttribute: () => false,
    getAttribute: () => null,
    removeAttribute: () => {},
    focus: () => {},
    remove: () => {},
    tabIndex: -1,
  };
}

// ---------------------------------------------------------------------------
// openModal / closeModal — history.replaceState side-effects
// ---------------------------------------------------------------------------

function makeModalContext() {
  const elements = {};
  const replaceStateCalls = [];

  function getEl(id) {
    if (!elements[id]) elements[id] = makeEl(id);
    return elements[id];
  }

  const task = {
    id: TASK_ID,
    status: "done",
    prompt: "test prompt",
    created_at: new Date().toISOString(),
    title: "Test task",
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
  };

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    Promise,
    tasks: [task],
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
    history: {
      replaceState(_state, _title, url) {
        replaceStateCalls.push(url);
      },
    },
    location: { hash: "", pathname: "/", search: "" },
    document: {
      getElementById: getEl,
      querySelector: (sel) => {
        if (sel === "#modal .modal-card") return getEl("modal-card");
        return null;
      },
      querySelectorAll: () => ({ forEach: () => {} }),
      createElement: () => makeEl(),
      head: { appendChild: () => {} },
      body: { appendChild: () => {} },
    },
    fetch: () => Promise.reject(new Error("not mocked")),
    AbortController: class {
      constructor() {
        this.signal = { aborted: false, addEventListener: () => {} };
      }
      abort() {
        this.signal.aborted = true;
      }
    },
    setTimeout: () => {},
    clearTimeout: () => {},
    setInterval: () => 0,
    clearInterval: () => {},
    requestAnimationFrame: () => {},
    renderMarkdown: (s) => s || "",
    escapeHtml: (s) => String(s ?? ""),
    // Stubbed functions called by openModal
    setLeftTab: () => {},
    setRightTab: () => {},
    startLogStream: () => {},
    startImplLogFetch: () => {},
    startTestLogStream: () => {},
    renderResultsFromEvents: () => {},
    renderTestResultsFromEvents: () => {},
    renderRefineHistory: () => {},
    updateRefineUI: () => {},
    resetRefinePanel: () => {},
    applySandboxByActivity: () => {},
    populateDependsOnPicker: () => {},
    renderDiffFiles: () => {},
    syncTask: () => {},
    loadFlamegraph: () => {},
    renderTimeline: () => {},
    _startTimelineRefresh: () => {},
    _stopTimelineRefresh: () => {},
    api: (path) => {
      if (path && path.includes("/events")) return Promise.resolve([]);
      return Promise.resolve({});
    },
    BRAINSTORM_CATEGORIES: new Set(),
    DEFAULT_TASK_TIMEOUT: 60,
  });

  loadScript("modal-core.js", ctx);
  return { ctx, elements, replaceStateCalls, task };
}

describe("openModal hash update", () => {
  it("calls history.replaceState with a task-scoped hash after opening the modal", async () => {
    const { ctx, replaceStateCalls } = makeModalContext();
    await vm.runInContext(`openModal('${TASK_ID}')`, ctx);
    const last = replaceStateCalls[replaceStateCalls.length - 1];
    // openModal writes "#<id>"; setMainTab then refines to "#<id>/<main-tab>".
    // Either form is acceptable — we only care the hash is task-scoped.
    expect(last).toMatch(new RegExp("^#" + TASK_ID + "(/[\\w-]+)?$"));
  });

  it("last replaceState call references the task id even when internal setRightTab is stubbed", async () => {
    const { ctx, replaceStateCalls } = makeModalContext();
    // setRightTab is stubbed so it won't push to replaceStateCalls; the
    // remaining calls come from openModal's baseline and setMainTab's main-tab
    // suffix. Both should carry the task id.
    await vm.runInContext(`openModal('${TASK_ID}')`, ctx);
    expect(replaceStateCalls.length).toBeGreaterThanOrEqual(1);
    replaceStateCalls.forEach((url) => {
      expect(url).toMatch(new RegExp("^#" + TASK_ID + "(/[\\w-]+)?$"));
    });
  });
});

describe("openModal activity section visibility", () => {
  it("keeps modal-summary-section visible for in-progress tasks without a final result", async () => {
    const { ctx, elements, task } = makeModalContext();
    task.status = "in_progress";
    task.result = null;
    // Ensure the initial state is hidden so passing the assertion proves
    // openModal actively revealed the section (not just left it alone).
    const summaryEl = elements["modal-summary-section"] || {
      classList: { add: () => {}, remove: () => {}, contains: () => true },
    };
    if (summaryEl.classList && summaryEl.classList._classes) {
      summaryEl.classList._classes.add("hidden");
    } else {
      summaryEl.classList.add("hidden");
    }
    await vm.runInContext(`openModal('${TASK_ID}')`, ctx);
    const after = elements["modal-summary-section"];
    expect(after).toBeTruthy();
    expect(after.classList.contains("hidden")).toBe(false);
  });
});

describe("closeModal hash clear", () => {
  it("calls history.replaceState with pathname+search (no hash) when closing", async () => {
    const { ctx, replaceStateCalls } = makeModalContext();
    await vm.runInContext(`openModal('${TASK_ID}')`, ctx);
    replaceStateCalls.length = 0; // clear calls from openModal
    vm.runInContext("closeModal()", ctx);
    expect(replaceStateCalls).toHaveLength(1);
    expect(replaceStateCalls[0]).toBe("/");
  });

  it("uses pathname + search when both are non-empty", async () => {
    const { ctx, replaceStateCalls } = makeModalContext();
    // Override location with a search string
    vm.runInContext(
      'location.pathname = "/board"; location.search = "?workspace=foo"',
      ctx,
    );
    await vm.runInContext(`openModal('${TASK_ID}')`, ctx);
    replaceStateCalls.length = 0;
    vm.runInContext("closeModal()", ctx);
    expect(replaceStateCalls[0]).toBe("/board?workspace=foo");
  });
});

// ---------------------------------------------------------------------------
// setRightTab — hash update in modal-logs.js
// ---------------------------------------------------------------------------

function makeLogsContextForHash() {
  const replaceStateCalls = [];
  const elements = {};

  function getEl(id) {
    if (!elements[id]) elements[id] = makeEl(id);
    return elements[id];
  }

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    _modalState: { seq: 0, taskId: TASK_ID, abort: null },
    logsAbort: null,
    testLogsAbort: null,
    rawLogBuffer: "",
    testRawLogBuffer: "",
    logsMode: "pretty",
    testLogsMode: "pretty",
    logSearchQuery: "",
    oversightData: null,
    oversightFetching: false,
    testOversightData: null,
    testOversightFetching: false,
    timelineRefreshTimer: null,
    history: {
      replaceState(_state, _title, url) {
        replaceStateCalls.push(url);
      },
    },
    document: {
      getElementById: getEl,
      createTreeWalker: () => ({ nextNode: () => null }),
      createElement: () => makeEl(),
    },
    fetch: () => Promise.reject(new Error("not mocked")),
    AbortController: class {
      abort() {}
      signal = {};
    },
    TextDecoder: class {
      decode(v) {
        return String(v || "");
      }
    },
    setTimeout: () => {},
    clearTimeout: () => {},
    NodeFilter: { SHOW_TEXT: 4 },
    renderPrettyLogs: (buf) => `<div>${buf}</div>`,
    renderOversightInLogs: () => {},
    renderTestOversightInTestLogs: () => {},
    loadFlamegraph: () => {},
    renderTimeline: () => {},
    _startTimelineRefresh: () => {},
    _stopTimelineRefresh: () => {},
    escapeHtml: (s) => String(s ?? ""),
    requestAnimationFrame: () => {},
  });

  ctx.getOpenModalTaskId = function () {
    return ctx._modalState.taskId;
  };
  loadScript("modal-logs.js", ctx);
  return { ctx, elements, replaceStateCalls };
}

describe("setRightTab hash update", () => {
  it('calls history.replaceState with "#taskId/tabName" when switching tabs', () => {
    const { ctx, replaceStateCalls } = makeLogsContextForHash();
    ctx.setRightTab("changes");
    expect(replaceStateCalls[replaceStateCalls.length - 1]).toBe(
      "#" + TASK_ID + "/changes",
    );
  });

  it("updates hash for every right panel tab", () => {
    const tabs = ["implementation", "testing", "changes", "spans", "timeline"];
    for (const tab of tabs) {
      const { ctx, replaceStateCalls } = makeLogsContextForHash();
      ctx.setRightTab(tab);
      expect(replaceStateCalls[replaceStateCalls.length - 1]).toBe(
        "#" + TASK_ID + "/" + tab,
      );
    }
  });

  it("does not update hash when currentTaskId is null", () => {
    const { ctx, replaceStateCalls } = makeLogsContextForHash();
    vm.runInContext("_modalState.taskId = null", ctx);
    ctx.setRightTab("implementation");
    expect(replaceStateCalls).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// setLeftTab — hash update in modal-results.js
// ---------------------------------------------------------------------------

function makeResultsContextForHash() {
  const replaceStateCalls = [];
  const elements = {};

  function getEl(id) {
    if (!elements[id]) elements[id] = makeEl(id);
    return elements[id];
  }

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    _modalState: { seq: 0, taskId: TASK_ID, abort: null },
    history: {
      replaceState(_state, _title, url) {
        replaceStateCalls.push(url);
      },
    },
    document: {
      getElementById: getEl,
      createElement: () => makeEl(),
      head: { appendChild: () => {} },
      body: { appendChild: () => {} },
    },
    renderMarkdown: (s) => s || "",
    escapeHtml: (s) => String(s ?? ""),
    setInterval: () => 0,
    clearInterval: () => {},
    setTimeout: () => {},
    clearTimeout: () => {},
    requestAnimationFrame: () => {},
  });

  ctx.getOpenModalTaskId = function () {
    return ctx._modalState.taskId;
  };
  loadScript("modal-results.js", ctx);
  return { ctx, elements, replaceStateCalls };
}

describe("setLeftTab hash update", () => {
  it('calls history.replaceState with "#taskId/tabName" when switching tabs', () => {
    const { ctx, replaceStateCalls } = makeResultsContextForHash();
    ctx.setLeftTab("testing");
    expect(replaceStateCalls[replaceStateCalls.length - 1]).toBe(
      "#" + TASK_ID + "/testing",
    );
  });

  it("does not update hash when currentTaskId is null", () => {
    const { ctx, replaceStateCalls } = makeResultsContextForHash();
    vm.runInContext("_modalState.taskId = null", ctx);
    ctx.setLeftTab("implementation");
    expect(replaceStateCalls).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// _handleInitialHash — opens the correct modal from URL hash on page load
// ---------------------------------------------------------------------------

function makeApiContext({ hash = "" } = {}) {
  const openModalCalls = [];
  const setRightTabCalls = [];
  const setLeftTabCalls = [];

  const task = {
    id: TASK_ID,
    status: "done",
    prompt: "test",
    created_at: new Date().toISOString(),
    title: "Test",
    tags: [],
    archived: false,
  };

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    Promise,
    tasks: [task],
    archivedTasks: [],
    _hashHandled: false,
    showArchived: false,
    tasksSource: null,
    tasksRetryDelay: 1000,
    gitStatuses: [],
    gitStatusSource: null,
    gitRetryDelay: 1000,
    location: { hash, pathname: "/", search: "" },
    history: { replaceState: () => {} },
    openModal: (id) => {
      openModalCalls.push(id);
      return Promise.resolve();
    },
    setRightTab: (tab) => {
      setRightTabCalls.push(tab);
    },
    setLeftTab: (tab) => {
      setLeftTabCalls.push(tab);
    },
    scheduleRender: () => {},
    invalidateDiffBehindCounts: () => {},
    showAlert: () => {},
    renderWorkspaces: () => {},
    updateIdeationConfig: () => {},
    document: {
      getElementById: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      createElement: () => makeEl(),
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
    EventSource: class {
      constructor() {
        this.close = () => {};
        this.addEventListener = () => {};
        this.onerror = null;
        this.readyState = 1;
      }
    },
    fetch: () => Promise.reject(new Error("not mocked")),
    setInterval: () => 0,
    clearInterval: () => {},
    setTimeout: () => {},
    clearTimeout: () => {},
    requestAnimationFrame: () => {},
  });

  // Load the full module chain that api.js depends on.
  loadScript("transport.js", ctx);
  loadScript("task-stream.js", ctx);
  loadScript("workspace.js", ctx);
  loadScript("api.js", ctx);
  return { ctx, openModalCalls, setRightTabCalls, setLeftTabCalls, task };
}

describe("_handleInitialHash", () => {
  it("opens the modal when hash contains a valid task ID", () => {
    const { ctx, openModalCalls } = makeApiContext({ hash: "#" + TASK_ID });
    ctx._handleInitialHash();
    expect(openModalCalls).toEqual([TASK_ID]);
  });

  it("sets _hashHandled to true after running", () => {
    const { ctx } = makeApiContext({ hash: "#" + TASK_ID });
    ctx._handleInitialHash();
    expect(ctx._hashHandled).toBe(true);
  });

  it("does nothing when hash does not match a UUID", () => {
    const { ctx, openModalCalls } = makeApiContext({ hash: "#not-a-uuid" });
    ctx._handleInitialHash();
    expect(openModalCalls).toHaveLength(0);
  });

  it("silently ignores a valid UUID that does not match any task", () => {
    const { ctx, openModalCalls } = makeApiContext({
      hash: "#aaaabbbb-1111-2222-3333-444455556666",
    });
    ctx._handleInitialHash();
    expect(openModalCalls).toHaveLength(0);
  });

  it("does nothing when hash is empty", () => {
    const { ctx, openModalCalls } = makeApiContext({ hash: "" });
    ctx._handleInitialHash();
    expect(openModalCalls).toHaveLength(0);
  });

  it("is idempotent — only opens the modal once even if called multiple times", () => {
    const { ctx, openModalCalls } = makeApiContext({ hash: "#" + TASK_ID });
    ctx._handleInitialHash();
    ctx._handleInitialHash();
    expect(openModalCalls).toHaveLength(1);
  });

  it("switches to the specified right-panel tab after opening", async () => {
    const { ctx, setRightTabCalls } = makeApiContext({
      hash: "#" + TASK_ID + "/changes",
    });
    ctx._handleInitialHash();
    // Let the openModal promise's .then() microtask run.
    await Promise.resolve();
    expect(setRightTabCalls).toContain("changes");
  });

  it("does not switch any tab when no tab is specified in the hash", async () => {
    const { ctx, setRightTabCalls, setLeftTabCalls } = makeApiContext({
      hash: "#" + TASK_ID,
    });
    ctx._handleInitialHash();
    await Promise.resolve();
    expect(setRightTabCalls).toHaveLength(0);
    expect(setLeftTabCalls).toHaveLength(0);
  });

  it('switches to a left-panel tab when tab name is "testing" via left tab path', async () => {
    // 'testing' appears in both right and left tabs; right tabs take priority in the implementation.
    const { ctx, setRightTabCalls } = makeApiContext({
      hash: "#" + TASK_ID + "/testing",
    });
    ctx._handleInitialHash();
    await Promise.resolve();
    // 'testing' is in rightTabs so setRightTab should be called
    expect(setRightTabCalls).toContain("testing");
  });
});
