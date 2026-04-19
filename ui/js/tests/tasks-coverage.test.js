/**
 * Tests for task helpers in tasks.js.
 *
 * Pattern: vitest + vm.createContext (same as envconfig.test.js).
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// ---------------------------------------------------------------------------
// DOM element factories
// ---------------------------------------------------------------------------

function makeEl(tag, overrides = {}) {
  const listeners = {};
  const children = [];
  const classList = new Set();
  const dataset = {};
  const style = {};

  return {
    tagName: tag,
    value: "",
    textContent: "",
    innerHTML: "",
    checked: false,
    disabled: false,
    placeholder: "",
    scrollHeight: 40,
    selectedIndex: 0,
    type: "",
    title: "",
    className: "",
    id: overrides.id || "",
    children,
    childNodes: children,
    dataset,
    style,
    classList: {
      add(c) {
        classList.add(c);
      },
      remove(c) {
        classList.delete(c);
      },
      contains(c) {
        return classList.has(c);
      },
      toggle(c, force) {
        if (force === undefined) {
          if (classList.has(c)) {
            classList.delete(c);
            return false;
          }
          classList.add(c);
          return true;
        }
        if (force) {
          classList.add(c);
        } else {
          classList.delete(c);
        }
        return force;
      },
    },
    addEventListener(evt, fn) {
      if (!listeners[evt]) listeners[evt] = [];
      listeners[evt].push(fn);
    },
    _fire(evt, data) {
      (listeners[evt] || []).forEach((fn) => {
        fn(data);
      });
    },
    querySelector(_sel) {
      return null;
    },
    querySelectorAll(_sel) {
      return [];
    },
    closest(_sel) {
      return null;
    },
    appendChild(child) {
      children.push(child);
    },
    focus() {},
    ...overrides,
  };
}

function makeInput(value = "") {
  return makeEl("INPUT", { value });
}

function makeCheckbox(checked = false) {
  return makeEl("INPUT", { type: "checkbox", checked });
}

function makeSelect(value = "") {
  const el = makeEl("SELECT", { value });
  el.dataset.sandboxSelect = "true";
  return el;
}

// ---------------------------------------------------------------------------
// Context factory
// ---------------------------------------------------------------------------

function makeContext(overrides = {}) {
  // These elements are required at script load time (top-level addEventListener calls).
  const baseElements = new Map([
    ["modal-edit-prompt", makeEl("TEXTAREA", { id: "modal-edit-prompt" })],
    ["modal-edit-timeout", makeEl("INPUT", { id: "modal-edit-timeout" })],
  ]);
  for (const [k, v] of overrides.elements || []) {
    baseElements.set(k, v);
  }
  const elements = baseElements;
  const createdElements = [];

  // A minimal window object for the dep-picker's window[cbName] check.
  const windowObj = overrides.window || {};

  const ctx = {
    console,
    Date,
    Math,
    JSON,
    Array,
    Set,
    Map,
    String,
    parseInt,
    parseFloat,
    URLSearchParams,
    Promise,
    Error,
    setTimeout:
      overrides.setTimeout ||
      ((fn) => {
        fn();
        return 0;
      }),
    clearTimeout: overrides.clearTimeout || (() => {}),
    setInterval: overrides.setInterval || (() => 0),
    clearInterval: overrides.clearInterval || (() => {}),
    localStorage: overrides.localStorage || {
      _store: {},
      getItem(k) {
        return this._store[k] ?? null;
      },
      setItem(k, v) {
        this._store[k] = v;
      },
      removeItem(k) {
        delete this._store[k];
      },
    },
    window: windowObj,
    alert: overrides.alert || (() => {}),
    // Stubs for functions called from tasks.js
    api: overrides.api || vi.fn().mockResolvedValue({}),
    showAlert: overrides.showAlert || vi.fn(),
    showConfirm: overrides.showConfirm || vi.fn().mockResolvedValue(true),
    showPrompt: overrides.showPrompt || vi.fn().mockResolvedValue(null),
    fetchTasks: overrides.fetchTasks || vi.fn(),
    scheduleRender: overrides.scheduleRender || vi.fn(),
    escapeHtml: overrides.escapeHtml || ((s) => s),
    renderMarkdown: overrides.renderMarkdown || ((s) => s),
    closeModal: overrides.closeModal || vi.fn(),
    openModal: overrides.openModal || vi.fn(),
    waitForTaskDelta:
      overrides.waitForTaskDelta || vi.fn().mockResolvedValue(undefined),
    waitForTaskTitle:
      overrides.waitForTaskTitle || vi.fn().mockResolvedValue(undefined),
    findTaskById: overrides.findTaskById || (() => null),
    announceBoardStatus: overrides.announceBoardStatus || vi.fn(),
    formatTaskStatusLabel: overrides.formatTaskStatusLabel || ((s) => s),
    getTaskAccessibleTitle:
      overrides.getTaskAccessibleTitle || ((t) => t.title || t.prompt),
    tagStyle: overrides.tagStyle || (() => ""),
    _mdRender: overrides._mdRender || { enhanceMarkdown: vi.fn() },
    collectSandboxByActivity:
      overrides.collectSandboxByActivity || vi.fn().mockReturnValue({}),
    applySandboxByActivity: overrides.applySandboxByActivity || vi.fn(),
    populateSandboxSelects: overrides.populateSandboxSelects || vi.fn(),
    initTagInput: undefined, // will be defined by the script
    getTagValues: undefined, // will be defined by the script

    // State globals
    tasks: overrides.tasks || [],
    archivedTasks: overrides.archivedTasks || [],
    editDebounce: null,
    pendingCancelTaskIds: overrides.pendingCancelTaskIds || new Set(),
    diffCache: overrides.diffCache || new Map(),
    defaultSandbox: overrides.defaultSandbox || "",
    defaultSandboxByActivity: overrides.defaultSandboxByActivity || {},
    SANDBOX_ACTIVITY_KEYS: overrides.SANDBOX_ACTIVITY_KEYS || [
      "implementation",
      "testing",
      "refinement",
      "title",
      "oversight",
      "commit_message",
      "idea_agent",
    ],
    availableSandboxes: overrides.availableSandboxes || ["claude", "codex"],
    sandboxUsable: overrides.sandboxUsable || {},
    sandboxReasons: overrides.sandboxReasons || {},

    // Routes
    Routes: overrides.Routes || {
      flows: {
        list: () => "/api/flows",
        get: () => "/api/flows/{slug}",
      },
      tasks: {
        create: () => "/api/tasks",
        archiveDone: () => "/api/tasks/archive-done",
        generateTitles: () => "/api/tasks/generate-titles",
        generateOversight: () => "/api/tasks/generate-oversight",
        task: (id) => ({
          update: () => `/api/tasks/${id}`,
          delete: () => `/api/tasks/${id}`,
          feedback: () => `/api/tasks/${id}/feedback`,
          done: () => `/api/tasks/${id}/done`,
          cancel: () => `/api/tasks/${id}/cancel`,
          resume: () => `/api/tasks/${id}/resume`,
          archive: () => `/api/tasks/${id}/archive`,
          unarchive: () => `/api/tasks/${id}/unarchive`,
          test: () => `/api/tasks/${id}/test`,
          sync: () => `/api/tasks/${id}/sync`,
          oversight: () => `/api/tasks/${id}/oversight`,
        }),
      },
    },
    task: undefined, // set below

    // Modal state
    _modalState: { seq: 0, taskId: null, abort: null },

    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: (tag) => {
        const el = makeEl(tag);
        createdElements.push(el);
        return el;
      },
      querySelector: (_sel) => null,
      querySelectorAll: (_sel) => [],
      addEventListener: () => {},
      readyState: "complete",
    },

    _createdElements: createdElements,
  };

  // task() convenience alias
  ctx.task = ctx.Routes.tasks.task;

  // getOpenModalTaskId reads from _modalState
  ctx.getOpenModalTaskId = function () {
    return ctx._modalState.taskId;
  };

  return vm.createContext(ctx);
}

function loadTasks(ctx) {
  const code = readFileSync(join(jsDir, "tasks.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "tasks.js") });
  return ctx;
}

// ---------------------------------------------------------------------------
// Tag input helpers
// ---------------------------------------------------------------------------

describe("tag input helpers", () => {
  it("initTagInput normalizes and lowercases tags", () => {
    const container = makeEl("DIV", { id: "tag-test" });
    container.querySelector = () => null; // tag-chip-input
    const ctx = makeContext({ elements: [["tag-test", container]] });
    loadTasks(ctx);

    vm.runInContext('initTagInput("tag-test", ["  Foo ", "BAR", ""])', ctx);
    expect(container._tags).toEqual(["foo", "bar"]);
  });

  it("getTagValues returns empty array for missing element", () => {
    const ctx = makeContext();
    loadTasks(ctx);
    const result = vm.runInContext('getTagValues("nonexistent")', ctx);
    expect(result).toEqual([]);
  });

  it("getTagValues returns tags from container", () => {
    const container = makeEl("DIV", { id: "tags-c" });
    container._tags = ["alpha", "beta"];
    const ctx = makeContext({ elements: [["tags-c", container]] });
    loadTasks(ctx);
    const result = vm.runInContext('getTagValues("tags-c")', ctx);
    expect(result).toEqual(["alpha", "beta"]);
  });

  it("_addTag deduplicates and trims", () => {
    const container = makeEl("DIV", { id: "t2" });
    container._tags = ["existing"];
    container.querySelector = () => null;
    const ctx = makeContext({ elements: [["t2", container]] });
    loadTasks(ctx);

    vm.runInContext(
      'var c = document.getElementById("t2"); _addTag(c, "  EXISTING  ")',
      ctx,
    );
    expect(container._tags).toEqual(["existing"]);

    vm.runInContext(
      'var c = document.getElementById("t2"); _addTag(c, "New")',
      ctx,
    );
    expect(container._tags).toEqual(["existing", "new"]);
  });

  it("_removeTagAt removes by index", () => {
    const container = makeEl("DIV", { id: "t3" });
    container._tags = ["a", "b", "c"];
    container.querySelector = () => null;
    const ctx = makeContext({ elements: [["t3", container]] });
    loadTasks(ctx);

    vm.runInContext(
      'var c = document.getElementById("t3"); _removeTagAt(c, 1)',
      ctx,
    );
    expect(container._tags).toEqual(["a", "c"]);
  });

  it("_removeTagAt is safe with null container", () => {
    const ctx = makeContext();
    loadTasks(ctx);
    // Should not throw
    vm.runInContext("_removeTagAt(null, 0)", ctx);
  });
});

// ---------------------------------------------------------------------------
// Dependency picker helpers
// ---------------------------------------------------------------------------

describe("getDepPickerValues", () => {
  it("returns empty array when wrapper is missing", () => {
    const ctx = makeContext();
    loadTasks(ctx);
    const result = vm.runInContext('getDepPickerValues("nope")', ctx);
    expect(result).toEqual([]);
  });

  it("returns checked checkbox values", () => {
    const cb1 = makeEl("INPUT", {
      type: "checkbox",
      checked: true,
      value: "id-1",
    });
    const cb2 = makeEl("INPUT", {
      type: "checkbox",
      checked: false,
      value: "id-2",
    });
    const cb3 = makeEl("INPUT", {
      type: "checkbox",
      checked: true,
      value: "id-3",
    });
    const wrap = makeEl("DIV", {
      id: "picker-1",
      querySelectorAll: (sel) => {
        if (sel.includes(":checked")) return [cb1, cb3];
        return [cb1, cb2, cb3];
      },
    });
    const ctx = makeContext({ elements: [["picker-1", wrap]] });
    loadTasks(ctx);
    const result = vm.runInContext('getDepPickerValues("picker-1")', ctx);
    expect(result).toEqual(["id-1", "id-3"]);
  });
});

describe("updateDepPickerChips", () => {
  it("shows 'None' placeholder when no checkboxes are checked", () => {
    const chipsEl = makeEl("DIV");
    const wrap = makeEl("DIV", {
      id: "picker-chips",
      querySelector: (sel) => {
        if (sel === ".dep-picker-chips") return chipsEl;
        return null;
      },
      querySelectorAll: (sel) => {
        if (sel.includes(":checked")) return [];
        return [];
      },
    });
    const ctx = makeContext({ elements: [["picker-chips", wrap]] });
    loadTasks(ctx);
    vm.runInContext('updateDepPickerChips("picker-chips", false)', ctx);
    expect(chipsEl.innerHTML).toContain("None");
  });

  it("creates chip spans for checked items", () => {
    const chipsEl = makeEl("DIV");
    const textSpan = makeEl("SPAN", { textContent: "Task A" });
    const item = makeEl("LABEL", {
      querySelector: (sel) => {
        if (sel === ".dep-picker-item-text") return textSpan;
        return null;
      },
    });
    const cb = makeEl("INPUT", {
      type: "checkbox",
      checked: true,
      closest: (sel) => {
        if (sel === ".dep-picker-item") return item;
        return null;
      },
    });

    const createdChips = [];
    const wrap = makeEl("DIV", {
      id: "picker-chips2",
      querySelector: (sel) => {
        if (sel === ".dep-picker-chips") return chipsEl;
        return null;
      },
      querySelectorAll: (sel) => {
        if (sel.includes(":checked")) return [cb];
        return [];
      },
    });

    const ctx = makeContext({ elements: [["picker-chips2", wrap]] });
    loadTasks(ctx);
    vm.runInContext('updateDepPickerChips("picker-chips2", false)', ctx);
    // chipsEl was cleared and a chip was appended
    expect(chipsEl.innerHTML).toBe("");
    expect(chipsEl.childNodes.length).toBe(1);
    expect(chipsEl.childNodes[0].textContent).toBe("Task A");
    expect(chipsEl.childNodes[0].className).toBe("dep-picker-chip");
  });

  it("returns early when wrapper is missing", () => {
    const ctx = makeContext();
    loadTasks(ctx);
    // Should not throw
    vm.runInContext('updateDepPickerChips("nonexistent", false)', ctx);
  });
});

describe("populateDependsOnPicker", () => {
  it("shows empty message when no candidate tasks", () => {
    const list = makeEl("DIV");
    const chipsEl = makeEl("DIV");
    const wrap = makeEl("DIV", {
      id: "deps-picker",
      querySelector: (sel) => {
        if (sel === ".dep-picker-list") return list;
        if (sel === ".dep-picker-search") return null;
        if (sel === ".dep-picker-chips") return chipsEl;
        return null;
      },
      querySelectorAll: (_sel) => [],
    });

    const ctx = makeContext({
      tasks: [],
      elements: [["deps-picker", wrap]],
    });
    loadTasks(ctx);
    vm.runInContext('populateDependsOnPicker("deps-picker", null, [])', ctx);
    expect(list.innerHTML).toContain("No other tasks");
  });

  it("excludes the specified task and sorts by status priority", () => {
    const list = makeEl("DIV");
    const chipsEl = makeEl("DIV");
    const search = makeEl("INPUT", { value: "old" });
    const wrap = makeEl("DIV", {
      id: "deps-picker2",
      querySelector: (sel) => {
        if (sel === ".dep-picker-list") return list;
        if (sel === ".dep-picker-search") return search;
        if (sel === ".dep-picker-chips") return chipsEl;
        return null;
      },
      querySelectorAll: (_sel) => [],
    });

    const taskData = [
      { id: "t1", status: "done", prompt: "done task", title: "Done" },
      {
        id: "t2",
        status: "in_progress",
        prompt: "active task",
        title: "Active",
      },
      { id: "t3", status: "backlog", prompt: "backlog task", title: "Backlog" },
      {
        id: "exclude-me",
        status: "backlog",
        prompt: "excluded",
        title: "Excluded",
      },
    ];

    const ctx = makeContext({
      tasks: taskData,
      elements: [["deps-picker2", wrap]],
    });
    loadTasks(ctx);
    vm.runInContext(
      'populateDependsOnPicker("deps-picker2", "exclude-me", ["t1"])',
      ctx,
    );

    // Search input should be cleared
    expect(search.value).toBe("");
    // 3 items should be appended (t1, t2, t3 - not exclude-me)
    expect(list.childNodes.length).toBe(3);
    // First item should be in_progress (priority 0), then backlog (2), then done (3)
    // Check the order of checkbox values
    const ids = list.childNodes.map((item) => {
      // Each item has a checkbox as first child
      return item.childNodes[0].value;
    });
    expect(ids).toEqual(["t2", "t3", "t1"]); // in_progress, backlog, done
  });

  it("returns early when wrapper is missing", () => {
    const ctx = makeContext();
    loadTasks(ctx);
    vm.runInContext('populateDependsOnPicker("missing", null, [])', ctx);
    // No crash
  });
});

// ---------------------------------------------------------------------------
// setActivityOverrideDefaultSandbox
// ---------------------------------------------------------------------------

describe("setActivityOverrideDefaultSandbox", () => {
  it("sets dataset.defaultSandbox on activity elements", () => {
    const implEl = makeEl("SELECT", { id: "pfx-implementation" });
    const testEl = makeEl("SELECT", { id: "pfx-testing" });
    const populateSandboxSelects = vi.fn();

    const ctx = makeContext({
      elements: [
        ["pfx-implementation", implEl],
        ["pfx-testing", testEl],
      ],
      populateSandboxSelects,
    });
    loadTasks(ctx);

    vm.runInContext('setActivityOverrideDefaultSandbox("pfx-", "codex")', ctx);
    expect(implEl.dataset.defaultSandbox).toBe("codex");
    expect(testEl.dataset.defaultSandbox).toBe("codex");
    expect(populateSandboxSelects).toHaveBeenCalled();
  });

  it("deletes dataset.defaultSandbox when sandbox is falsy", () => {
    const implEl = makeEl("SELECT", { id: "pfx-implementation" });
    implEl.dataset.defaultSandbox = "claude";
    const populateSandboxSelects = vi.fn();

    const ctx = makeContext({
      elements: [["pfx-implementation", implEl]],
      populateSandboxSelects,
    });
    loadTasks(ctx);

    vm.runInContext('setActivityOverrideDefaultSandbox("pfx-", "")', ctx);
    expect(implEl.dataset.defaultSandbox).toBeUndefined();
  });
});

// ---------------------------------------------------------------------------
// bindTaskSandboxInheritance
// ---------------------------------------------------------------------------

describe("bindTaskSandboxInheritance", () => {
  it("binds a change listener that calls setActivityOverrideDefaultSandbox", () => {
    const selectEl = makeEl("SELECT", { id: "my-sandbox", value: "claude" });
    const implEl = makeEl("SELECT", { id: "pfx-implementation" });
    const populateSandboxSelects = vi.fn();

    const ctx = makeContext({
      elements: [
        ["my-sandbox", selectEl],
        ["pfx-implementation", implEl],
      ],
      populateSandboxSelects,
    });
    loadTasks(ctx);

    vm.runInContext('bindTaskSandboxInheritance("my-sandbox", "pfx-")', ctx);
    expect(selectEl.dataset.inheritanceBound).toBe("true");

    // Simulate change
    selectEl.value = "codex";
    selectEl._fire("change");
    expect(implEl.dataset.defaultSandbox).toBe("codex");
  });

  it("does not double-bind", () => {
    const selectEl = makeEl("SELECT", { id: "sb2" });
    selectEl.dataset.inheritanceBound = "true";
    let listenerCount = 0;
    const origAddEventListener = selectEl.addEventListener;
    selectEl.addEventListener = (...args) => {
      listenerCount++;
      origAddEventListener.apply(selectEl, args);
    };

    const ctx = makeContext({ elements: [["sb2", selectEl]] });
    loadTasks(ctx);

    vm.runInContext('bindTaskSandboxInheritance("sb2", "x-")', ctx);
    expect(listenerCount).toBe(0);
  });

  it("returns early for missing element", () => {
    const ctx = makeContext();
    loadTasks(ctx);
    vm.runInContext('bindTaskSandboxInheritance("nope", "x-")', ctx);
    // No crash
  });
});

// ---------------------------------------------------------------------------
// createTask
// ---------------------------------------------------------------------------

describe("createTask", () => {
  function makeCreateTaskContext(overrides = {}) {
    const textarea = makeEl("TEXTAREA", {
      id: "new-prompt",
      value: "Test prompt",
    });
    const timeoutEl = makeInput("30");
    const mountEl = makeCheckbox(true);
    const sandboxEl = makeSelect("claude");
    const maxCostEl = makeInput("5.00");
    const maxTokensEl = makeInput("1000");
    const scheduledAtEl = makeInput("");
    const tagContainer = makeEl("DIV", { id: "new-task-tag-input" });
    tagContainer._tags = ["tag1"];

    const formEl = makeEl("DIV", { id: "new-task-form" });
    const btnEl = makeEl("BUTTON", { id: "new-task-btn" });
    const depPicker = makeEl("DIV", {
      id: "new-depends-on-picker",
      querySelectorAll: () => [],
      querySelector: (sel) => {
        if (sel === ".dep-picker-list") return makeEl("DIV");
        if (sel === ".dep-picker-chips") return makeEl("DIV");
        if (sel === ".dep-picker-dropdown") return makeEl("DIV");
        return null;
      },
    });

    const api = vi.fn().mockResolvedValue({ id: "new-id" });

    const elements = [
      ["new-prompt", textarea],
      ["new-timeout", timeoutEl],
      ["new-mount-worktrees", mountEl],
      ["new-sandbox", sandboxEl],
      ["new-max-cost-usd", maxCostEl],
      ["new-max-input-tokens", maxTokensEl],
      ["new-scheduled-at", scheduledAtEl],
      ["new-task-tag-input", tagContainer],
      ["new-task-form", formEl],
      ["new-task-btn", btnEl],
      ["new-depends-on-picker", depPicker],
      ...(overrides.extraElements || []),
    ];

    return makeContext({
      api,
      elements,
      // Don't execute setTimeout callbacks immediately for createTask
      setTimeout: (_fn, _ms) => 0,
      ...overrides,
    });
  }

  it("sends correct payload and clears draft on success", async () => {
    const ctx = makeCreateTaskContext();
    loadTasks(ctx);

    // Set a draft in localStorage
    vm.runInContext(
      'localStorage.setItem("wallfacer-new-task-draft", "old draft")',
      ctx,
    );

    await vm.runInContext("createTask()", ctx);

    const api = ctx.api;
    expect(api).toHaveBeenCalledWith(
      "/api/tasks",
      expect.objectContaining({
        method: "POST",
      }),
    );

    // Parse the body to check fields
    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.prompt).toBe("Test prompt");
    expect(body.timeout).toBe(30);
    expect(body.mount_worktrees).toBe(true);
    expect(body.tags).toEqual(["tag1"]);
    expect(body.max_cost_usd).toBe(5);
    expect(body.max_input_tokens).toBe(1000);
    // Flow picker replaced the old Type picker — POST now carries a
    // flow slug. The sandbox + sandbox_by_activity fields are gone
    // now that harness choice lives on the agent definition.
    expect(body.flow).toBe("implement");
    expect(body.kind).toBeUndefined();
    expect(body.sandbox).toBeUndefined();
    expect(body.sandbox_by_activity).toBeUndefined();

    // Draft should be cleared
    expect(ctx.localStorage.getItem("wallfacer-new-task-draft")).toBeNull();
  });

  it("sends flow=implement by default when no flow element is set", async () => {
    const ctx = makeCreateTaskContext();
    loadTasks(ctx);
    await vm.runInContext("createTask()", ctx);
    const body = JSON.parse(ctx.api.mock.calls[0][1].body);
    expect(body.flow).toBe("implement");
  });

  it("sends the selected flow slug when the composer picks brainstorm", async () => {
    const flowEl = makeSelect("brainstorm");
    flowEl.id = "new-task-flow";
    const ctx = makeCreateTaskContext({
      extraElements: [["new-task-flow", flowEl]],
    });
    loadTasks(ctx);
    // Seed the _flowsCache so the empty-prompt rule and payload match
    // the brainstorm built-in.
    vm.runInContext(
      '_flowsCache = [{slug:"brainstorm", name:"Brainstorm", spawn_kind:"idea-agent"}, {slug:"implement", name:"Implement"}];',
      ctx,
    );
    // Brainstorm allows an empty prompt.
    ctx.document.getElementById("new-prompt").value = "";
    await vm.runInContext("createTask()", ctx);
    expect(ctx.api).toHaveBeenCalled();
    const body = JSON.parse(ctx.api.mock.calls[0][1].body);
    expect(body.flow).toBe("brainstorm");
    expect(body.prompt).toBe("");
  });

  it("still rejects an empty prompt for flows that require one", async () => {
    const flowEl = makeSelect("implement");
    flowEl.id = "new-task-flow";
    const ctx = makeCreateTaskContext({
      extraElements: [["new-task-flow", flowEl]],
    });
    loadTasks(ctx);
    ctx.document.getElementById("new-prompt").value = "   ";
    await vm.runInContext("createTask()", ctx);
    expect(ctx.api).not.toHaveBeenCalled();
  });

  it("rejects empty prompt with visual feedback", async () => {
    const ctx = makeCreateTaskContext();
    loadTasks(ctx);

    // Set prompt to empty
    const textarea = ctx.document.getElementById("new-prompt");
    textarea.value = "   ";

    await vm.runInContext("createTask()", ctx);

    // API should NOT have been called
    expect(ctx.api).not.toHaveBeenCalled();
    expect(textarea.style.borderColor).toBe("#dc2626");
  });

  it("shows alert on API error", async () => {
    const api = vi.fn().mockRejectedValue(new Error("Network error"));
    const ctx = makeCreateTaskContext({ api });
    loadTasks(ctx);

    await vm.runInContext("createTask()", ctx);

    expect(ctx.showAlert).toHaveBeenCalledWith(
      "Error creating task: Network error",
    );
  });

  it("sets dependencies when dep picker has values", async () => {
    const depPicker = makeEl("DIV", {
      id: "new-depends-on-picker",
      querySelectorAll: (sel) => {
        if (sel.includes(":checked")) {
          return [
            makeEl("INPUT", { value: "dep-1", checked: true }),
            makeEl("INPUT", { value: "dep-2", checked: true }),
          ];
        }
        return [];
      },
      querySelector: (sel) => {
        if (sel === ".dep-picker-list") return makeEl("DIV");
        if (sel === ".dep-picker-chips") return makeEl("DIV");
        if (sel === ".dep-picker-dropdown") return makeEl("DIV");
        return null;
      },
    });

    const api = vi.fn().mockResolvedValue({ id: "new-id" });
    const ctx = makeCreateTaskContext({
      api,
      extraElements: [["new-depends-on-picker", depPicker]],
    });
    // Override the depPicker element
    ctx.document.getElementById = (id) => {
      if (id === "new-depends-on-picker") return depPicker;
      const elements = new Map(ctx._elements);
      return elements.get(id) || null;
    };
    // Rebuild elements with the override
    const elementsMap = new Map();
    const textarea = makeEl("TEXTAREA", { id: "new-prompt", value: "Test" });
    const timeoutEl = makeInput("30");
    const mountEl = makeCheckbox(false);
    const sandboxEl = makeSelect("");
    const maxCostEl = makeInput("");
    const maxTokensEl = makeInput("");
    const scheduledAtEl = makeInput("");
    const tagContainer = makeEl("DIV", { id: "new-task-tag-input" });
    tagContainer._tags = [];
    const formEl = makeEl("DIV", { id: "new-task-form" });
    const btnEl = makeEl("BUTTON", { id: "new-task-btn" });

    elementsMap.set("new-prompt", textarea);
    elementsMap.set("new-timeout", timeoutEl);
    elementsMap.set("new-mount-worktrees", mountEl);
    elementsMap.set("new-sandbox", sandboxEl);
    elementsMap.set("new-max-cost-usd", maxCostEl);
    elementsMap.set("new-max-input-tokens", maxTokensEl);
    elementsMap.set("new-scheduled-at", scheduledAtEl);
    elementsMap.set("new-task-tag-input", tagContainer);
    elementsMap.set("new-task-form", formEl);
    elementsMap.set("new-task-btn", btnEl);
    elementsMap.set("new-depends-on-picker", depPicker);

    const ctx2 = makeContext({
      api,
      elements: [...elementsMap.entries()],
      setTimeout: (_fn, _ms) => 0,
    });
    loadTasks(ctx2);

    await vm.runInContext("createTask()", ctx2);

    // First call: POST create, second call: PATCH with depends_on
    expect(api).toHaveBeenCalledTimes(2);
    const patchBody = JSON.parse(api.mock.calls[1][1].body);
    expect(patchBody.depends_on).toEqual(["dep-1", "dep-2"]);
  });
});

// ---------------------------------------------------------------------------
// showNewTaskForm / hideNewTaskForm
// ---------------------------------------------------------------------------

describe("showNewTaskForm", () => {
  function makeFormContext(overrides = {}) {
    const btnEl = makeEl("BUTTON", { id: "new-task-btn" });
    const formEl = makeEl("DIV", { id: "new-task-form" });
    const textarea = makeEl("TEXTAREA", { id: "new-prompt", value: "" });
    const timeoutEl = makeInput("");
    const sandboxEl = makeSelect("");
    const depsRow = makeEl("DIV", { id: "new-depends-on-row" });
    const tagContainer = makeEl("DIV", { id: "new-task-tag-input" });
    const depPicker = makeEl("DIV", {
      id: "new-depends-on-picker",
      querySelector: (sel) => {
        if (sel === ".dep-picker-list") return makeEl("DIV");
        if (sel === ".dep-picker-search") return null;
        if (sel === ".dep-picker-chips") return makeEl("DIV");
        return null;
      },
      querySelectorAll: () => [],
    });

    return makeContext({
      elements: [
        ["new-task-btn", btnEl],
        ["new-task-form", formEl],
        ["new-prompt", textarea],
        ["new-timeout", timeoutEl],
        ["new-sandbox", sandboxEl],
        ["new-depends-on-row", depsRow],
        ["new-depends-on-picker", depPicker],
        ["new-task-tag-input", tagContainer],
      ],
      tasks: [{ id: "t1", status: "backlog", prompt: "test", title: "T" }],
      ...overrides,
    });
  }

  it("shows the form and hides the button", () => {
    const ctx = makeFormContext();
    loadTasks(ctx);
    vm.runInContext("showNewTaskForm()", ctx);

    const btn = ctx.document.getElementById("new-task-btn");
    const form = ctx.document.getElementById("new-task-form");
    expect(btn.classList.contains("hidden")).toBe(true);
    expect(form.classList.contains("hidden")).toBe(false);
  });

  it("sets default timeout", () => {
    const ctx = makeFormContext();
    loadTasks(ctx);
    vm.runInContext("showNewTaskForm()", ctx);

    const timeout = ctx.document.getElementById("new-timeout");
    expect(timeout.value).toBe(60); // DEFAULT_TASK_TIMEOUT
  });

  it("restores draft from localStorage", () => {
    const ctx = makeFormContext();
    ctx.localStorage.setItem("wallfacer-new-task-draft", "saved draft");
    loadTasks(ctx);
    vm.runInContext("showNewTaskForm()", ctx);

    const textarea = ctx.document.getElementById("new-prompt");
    expect(textarea.value).toBe("saved draft");
  });

  it("shows deps row when tasks exist", () => {
    const ctx = makeFormContext();
    loadTasks(ctx);
    vm.runInContext("showNewTaskForm()", ctx);

    const depsRow = ctx.document.getElementById("new-depends-on-row");
    expect(depsRow.style.display).toBe("");
  });

  it("hides deps row when no tasks", () => {
    const ctx = makeFormContext({ tasks: [] });
    loadTasks(ctx);
    vm.runInContext("showNewTaskForm()", ctx);

    const depsRow = ctx.document.getElementById("new-depends-on-row");
    expect(depsRow.style.display).toBe("none");
  });
});

describe("hideNewTaskForm", () => {
  it("hides form and shows button, clears fields", () => {
    const btnEl = makeEl("BUTTON", { id: "new-task-btn" });
    const formEl = makeEl("DIV", { id: "new-task-form" });
    const textarea = makeEl("TEXTAREA", { id: "new-prompt", value: "stuff" });
    const mountEl = makeCheckbox(true);
    mountEl.id = "new-mount-worktrees";
    const sandboxEl = makeSelect("claude");
    sandboxEl.id = "new-sandbox";
    const maxCostEl = makeInput("10");
    maxCostEl.id = "new-max-cost-usd";
    const maxTokensEl = makeInput("500");
    maxTokensEl.id = "new-max-input-tokens";
    const scheduledAtEl = makeInput("2026-01-01");
    scheduledAtEl.id = "new-scheduled-at";
    const tagContainer = makeEl("DIV", { id: "new-task-tag-input" });
    const depList = makeEl("DIV");
    const depChips = makeEl("DIV");
    const depDropdown = makeEl("DIV");
    const depPicker = makeEl("DIV", {
      id: "new-depends-on-picker",
      querySelector: (sel) => {
        if (sel === ".dep-picker-list") return depList;
        if (sel === ".dep-picker-chips") return depChips;
        if (sel === ".dep-picker-dropdown") return depDropdown;
        return null;
      },
      querySelectorAll: () => [],
    });

    const ctx = makeContext({
      elements: [
        ["new-task-btn", btnEl],
        ["new-task-form", formEl],
        ["new-prompt", textarea],
        ["new-mount-worktrees", mountEl],
        ["new-sandbox", sandboxEl],
        ["new-max-cost-usd", maxCostEl],
        ["new-max-input-tokens", maxTokensEl],
        ["new-scheduled-at", scheduledAtEl],
        ["new-task-tag-input", tagContainer],
        ["new-depends-on-picker", depPicker],
      ],
    });
    loadTasks(ctx);
    vm.runInContext("hideNewTaskForm()", ctx);

    expect(formEl.classList.contains("hidden")).toBe(true);
    expect(btnEl.classList.contains("hidden")).toBe(false);
    expect(textarea.value).toBe("");
    expect(mountEl.checked).toBe(false);
    expect(maxCostEl.value).toBe("");
    expect(maxTokensEl.value).toBe("");
    expect(scheduledAtEl.value).toBe("");
  });
});

// ---------------------------------------------------------------------------
// updateTaskStatus
// ---------------------------------------------------------------------------

describe("updateTaskStatus", () => {
  it("calls PATCH with new status and announces", async () => {
    const api = vi.fn().mockResolvedValue({});
    const announceBoardStatus = vi.fn();
    const taskData = [
      { id: "t1", status: "backlog", prompt: "do stuff", title: "My Task" },
    ];

    const ctx = makeContext({
      api,
      tasks: taskData,
      announceBoardStatus,
    });
    loadTasks(ctx);

    await vm.runInContext('updateTaskStatus("t1", "in_progress")', ctx);

    expect(api).toHaveBeenCalledWith(
      "/api/tasks/t1",
      expect.objectContaining({
        method: "PATCH",
      }),
    );
    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.status).toBe("in_progress");
    expect(announceBoardStatus).toHaveBeenCalled();
  });

  it("shows alert and refetches on error", async () => {
    const api = vi.fn().mockRejectedValue(new Error("fail"));
    const showAlert = vi.fn();
    const fetchTasks = vi.fn();

    const ctx = makeContext({ api, showAlert, fetchTasks });
    loadTasks(ctx);

    await vm.runInContext('updateTaskStatus("t1", "done")', ctx);

    expect(showAlert).toHaveBeenCalledWith("Error updating task: fail");
    expect(fetchTasks).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// toggleFreshStart
// ---------------------------------------------------------------------------

describe("toggleFreshStart", () => {
  it("sends PATCH with fresh_start", async () => {
    const api = vi.fn().mockResolvedValue({});
    const ctx = makeContext({ api });
    loadTasks(ctx);

    await vm.runInContext('toggleFreshStart("t1", true)', ctx);

    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.fresh_start).toBe(true);
  });

  it("shows alert on error", async () => {
    const api = vi.fn().mockRejectedValue(new Error("oops"));
    const showAlert = vi.fn();
    const ctx = makeContext({ api, showAlert });
    loadTasks(ctx);

    await vm.runInContext('toggleFreshStart("t1", false)', ctx);

    expect(showAlert).toHaveBeenCalledWith("Error updating task: oops");
  });
});

// ---------------------------------------------------------------------------
// deleteTask
// ---------------------------------------------------------------------------

describe("deleteTask", () => {
  it("calls DELETE on the task endpoint", async () => {
    const api = vi.fn().mockResolvedValue({});
    const ctx = makeContext({ api });
    loadTasks(ctx);

    await vm.runInContext('deleteTask("task-42")', ctx);

    expect(api).toHaveBeenCalledWith("/api/tasks/task-42", {
      method: "DELETE",
    });
  });

  it("shows alert on error", async () => {
    const api = vi.fn().mockRejectedValue(new Error("nope"));
    const showAlert = vi.fn();
    const ctx = makeContext({ api, showAlert });
    loadTasks(ctx);

    await vm.runInContext('deleteTask("t1")', ctx);

    expect(showAlert).toHaveBeenCalledWith("Error deleting task: nope");
  });
});

// ---------------------------------------------------------------------------
// deleteCurrentTask
// ---------------------------------------------------------------------------

describe("deleteCurrentTask", () => {
  it("does nothing when no modal task is open", async () => {
    const api = vi.fn().mockResolvedValue({});
    const ctx = makeContext({ api });
    loadTasks(ctx);

    await vm.runInContext("deleteCurrentTask()", ctx);

    expect(api).not.toHaveBeenCalled();
  });

  it("deletes the modal task after confirmation", async () => {
    const api = vi.fn().mockResolvedValue({});
    const showConfirm = vi.fn().mockResolvedValue(true);
    const closeModal = vi.fn();

    const ctx = makeContext({ api, showConfirm, closeModal });
    ctx._modalState.taskId = "modal-task";
    loadTasks(ctx);

    await vm.runInContext("deleteCurrentTask()", ctx);

    expect(showConfirm).toHaveBeenCalled();
    expect(api).toHaveBeenCalledWith("/api/tasks/modal-task", {
      method: "DELETE",
    });
    expect(closeModal).toHaveBeenCalled();
  });

  it("does not delete when confirmation is rejected", async () => {
    const api = vi.fn().mockResolvedValue({});
    const showConfirm = vi.fn().mockResolvedValue(false);

    const ctx = makeContext({ api, showConfirm });
    ctx._modalState.taskId = "modal-task";
    loadTasks(ctx);

    await vm.runInContext("deleteCurrentTask()", ctx);

    expect(api).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// submitFeedback
// ---------------------------------------------------------------------------

describe("submitFeedback", () => {
  it("sends feedback and closes modal", async () => {
    const feedbackEl = makeEl("TEXTAREA", {
      id: "modal-feedback",
      value: "looks good",
    });
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({
      api,
      closeModal,
      elements: [["modal-feedback", feedbackEl]],
    });
    ctx._modalState.taskId = "t-fb";
    loadTasks(ctx);

    await vm.runInContext("submitFeedback()", ctx);

    expect(api).toHaveBeenCalledWith(
      "/api/tasks/t-fb/feedback",
      expect.objectContaining({
        method: "POST",
      }),
    );
    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.message).toBe("looks good");
    expect(feedbackEl.value).toBe("");
    expect(closeModal).toHaveBeenCalled();
  });

  it("does nothing with empty feedback", async () => {
    const feedbackEl = makeEl("TEXTAREA", {
      id: "modal-feedback",
      value: "   ",
    });
    const api = vi.fn();

    const ctx = makeContext({
      api,
      elements: [["modal-feedback", feedbackEl]],
    });
    ctx._modalState.taskId = "t-fb";
    loadTasks(ctx);

    await vm.runInContext("submitFeedback()", ctx);

    expect(api).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// completeTask
// ---------------------------------------------------------------------------

describe("completeTask", () => {
  it("sends POST done and closes modal", async () => {
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({ api, closeModal });
    ctx._modalState.taskId = "t-done";
    loadTasks(ctx);

    await vm.runInContext("completeTask()", ctx);

    expect(api).toHaveBeenCalledWith("/api/tasks/t-done/done", {
      method: "POST",
    });
    expect(closeModal).toHaveBeenCalled();
  });

  it("does nothing when no modal task is open", async () => {
    const api = vi.fn();
    const ctx = makeContext({ api });
    loadTasks(ctx);

    await vm.runInContext("completeTask()", ctx);

    expect(api).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// resumeTask
// ---------------------------------------------------------------------------

describe("resumeTask", () => {
  it("sends POST resume with timeout", async () => {
    const timeoutEl = makeInput("45");
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({
      api,
      closeModal,
      elements: [["modal-resume-timeout", timeoutEl]],
    });
    ctx._modalState.taskId = "t-resume";
    loadTasks(ctx);

    await vm.runInContext("resumeTask()", ctx);

    expect(api).toHaveBeenCalledWith(
      "/api/tasks/t-resume/resume",
      expect.objectContaining({
        method: "POST",
      }),
    );
    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.timeout).toBe(45);
  });

  it("uses DEFAULT_TASK_TIMEOUT when no timeout element exists", async () => {
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({ api, closeModal });
    ctx._modalState.taskId = "t-resume";
    loadTasks(ctx);

    await vm.runInContext("resumeTask()", ctx);

    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.timeout).toBe(60);
  });
});

// ---------------------------------------------------------------------------
// startTask
// ---------------------------------------------------------------------------

describe("startTask", () => {
  it("patches status to in_progress", async () => {
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({ api, closeModal });
    ctx._modalState.taskId = "t-start";
    loadTasks(ctx);

    await vm.runInContext("startTask()", ctx);

    expect(api).toHaveBeenCalledWith(
      "/api/tasks/t-start",
      expect.objectContaining({
        method: "PATCH",
      }),
    );
    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.status).toBe("in_progress");
    expect(closeModal).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// archiveAllDone
// ---------------------------------------------------------------------------

describe("archiveAllDone", () => {
  it("calls archive-done endpoint and refetches", async () => {
    const api = vi.fn().mockResolvedValue({});
    const fetchTasks = vi.fn();

    const ctx = makeContext({ api, fetchTasks });
    loadTasks(ctx);

    await vm.runInContext("archiveAllDone()", ctx);

    expect(api).toHaveBeenCalledWith("/api/tasks/archive-done", {
      method: "POST",
    });
    expect(fetchTasks).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// archiveTask / unarchiveTask
// ---------------------------------------------------------------------------

describe("archiveTask", () => {
  it("archives the modal task", async () => {
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({ api, closeModal });
    ctx._modalState.taskId = "t-arch";
    loadTasks(ctx);

    await vm.runInContext("archiveTask()", ctx);

    expect(api).toHaveBeenCalledWith("/api/tasks/t-arch/archive", {
      method: "POST",
    });
    expect(closeModal).toHaveBeenCalled();
  });
});

describe("unarchiveTask", () => {
  it("unarchives the modal task", async () => {
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({ api, closeModal });
    ctx._modalState.taskId = "t-unarch";
    loadTasks(ctx);

    await vm.runInContext("unarchiveTask()", ctx);

    expect(api).toHaveBeenCalledWith("/api/tasks/t-unarch/unarchive", {
      method: "POST",
    });
    expect(closeModal).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Quick card actions
// ---------------------------------------------------------------------------

describe("quickDoneTask", () => {
  it("sends POST done for the given id", async () => {
    const api = vi.fn().mockResolvedValue({});
    const ctx = makeContext({
      api,
      tasks: [{ id: "qd1", status: "waiting", prompt: "p", title: "T" }],
    });
    loadTasks(ctx);

    await vm.runInContext('quickDoneTask("qd1")', ctx);

    expect(api).toHaveBeenCalledWith("/api/tasks/qd1/done", { method: "POST" });
  });
});

describe("quickResumeTask", () => {
  it("sends POST resume with timeout", async () => {
    const api = vi.fn().mockResolvedValue({});
    const ctx = makeContext({ api });
    loadTasks(ctx);

    await vm.runInContext('quickResumeTask("qr1", 90)', ctx);

    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.timeout).toBe(90);
  });
});

describe("quickRetryTask", () => {
  it("patches status to backlog", async () => {
    const api = vi.fn().mockResolvedValue({});
    const ctx = makeContext({ api });
    loadTasks(ctx);

    await vm.runInContext('quickRetryTask("qr2")', ctx);

    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.status).toBe("backlog");
  });
});

describe("quickTestTask", () => {
  it("sends POST test with empty criteria", async () => {
    const api = vi.fn().mockResolvedValue({});
    const ctx = makeContext({ api });
    loadTasks(ctx);

    await vm.runInContext('quickTestTask("qt1")', ctx);

    expect(api).toHaveBeenCalledWith(
      "/api/tasks/qt1/test",
      expect.objectContaining({
        method: "POST",
      }),
    );
    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.criteria).toBe("");
  });
});

// ---------------------------------------------------------------------------
// filterDepPicker
// ---------------------------------------------------------------------------

describe("filterDepPicker", () => {
  it("hides items that do not match search", () => {
    const items = [
      makeEl("LABEL", {
        style: {},
        querySelector: () => makeEl("SPAN", { textContent: "Alpha Task" }),
      }),
      makeEl("LABEL", {
        style: {},
        querySelector: () => makeEl("SPAN", { textContent: "Beta Task" }),
      }),
    ];
    const list = makeEl("DIV", {
      querySelectorAll: () => items,
    });
    const dropdown = makeEl("DIV", {
      querySelector: () => list,
    });
    const inputEl = makeEl("INPUT", {
      value: "alpha",
      closest: () => dropdown,
    });

    const ctx = makeContext();
    loadTasks(ctx);
    vm.runInContext("filterDepPicker", ctx)(inputEl);

    expect(items[0].style.display).toBe("");
    expect(items[1].style.display).toBe("none");
  });
});

// ---------------------------------------------------------------------------
// retryTask
// ---------------------------------------------------------------------------

describe("retryTask", () => {
  it("sends PATCH with backlog status and new prompt", async () => {
    const retryPrompt = makeEl("TEXTAREA", {
      id: "modal-retry-prompt",
      value: "try again",
    });
    const retryResumeRow = makeEl("DIV", { id: "modal-retry-resume-row" });
    retryResumeRow.classList.add("hidden");
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({
      api,
      closeModal,
      elements: [
        ["modal-retry-prompt", retryPrompt],
        ["modal-retry-resume-row", retryResumeRow],
      ],
    });
    ctx._modalState.taskId = "t-retry";
    loadTasks(ctx);

    await vm.runInContext("retryTask()", ctx);

    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.status).toBe("backlog");
    expect(body.prompt).toBe("try again");
    expect(body.fresh_start).toBeUndefined();
    expect(closeModal).toHaveBeenCalled();
  });

  it("includes fresh_start when resume row is visible", async () => {
    const retryPrompt = makeEl("TEXTAREA", {
      id: "modal-retry-prompt",
      value: "retry",
    });
    const retryResumeRow = makeEl("DIV", { id: "modal-retry-resume-row" });
    // Not hidden = visible
    const retryResumeCheckbox = makeCheckbox(true);
    retryResumeCheckbox.id = "modal-retry-resume";
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({
      api,
      closeModal,
      elements: [
        ["modal-retry-prompt", retryPrompt],
        ["modal-retry-resume-row", retryResumeRow],
        ["modal-retry-resume", retryResumeCheckbox],
      ],
    });
    ctx._modalState.taskId = "t-retry2";
    loadTasks(ctx);

    await vm.runInContext("retryTask()", ctx);

    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.fresh_start).toBe(false); // !checked
  });

  it("does nothing with empty prompt", async () => {
    const retryPrompt = makeEl("TEXTAREA", {
      id: "modal-retry-prompt",
      value: "  ",
    });
    const api = vi.fn();

    const ctx = makeContext({
      api,
      elements: [["modal-retry-prompt", retryPrompt]],
    });
    ctx._modalState.taskId = "t-retry";
    loadTasks(ctx);

    await vm.runInContext("retryTask()", ctx);

    expect(api).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// saveResumeOption
// ---------------------------------------------------------------------------

describe("saveResumeOption", () => {
  it("patches fresh_start as inverse of resume argument", async () => {
    const statusEl = makeEl("SPAN", { id: "modal-edit-status" });
    const api = vi.fn().mockResolvedValue({});

    const ctx = makeContext({
      api,
      elements: [["modal-edit-status", statusEl]],
      // Don't run the setTimeout callback to clear "Saved" text
      setTimeout: () => 0,
    });
    ctx._modalState.taskId = "t-save-resume";
    loadTasks(ctx);

    await vm.runInContext("saveResumeOption(true)", ctx);

    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.fresh_start).toBe(false); // !true
    expect(statusEl.textContent).toBe("Saved");
  });

  it("shows failure message on error", async () => {
    const statusEl = makeEl("SPAN", { id: "modal-edit-status" });
    const api = vi.fn().mockRejectedValue(new Error("err"));

    const ctx = makeContext({
      api,
      elements: [["modal-edit-status", statusEl]],
    });
    ctx._modalState.taskId = "t-save-fail";
    loadTasks(ctx);

    await vm.runInContext("saveResumeOption(false)", ctx);

    expect(statusEl.textContent).toBe("Save failed");
  });
});

// ---------------------------------------------------------------------------
// DEFAULT_TASK_TIMEOUT constant
// ---------------------------------------------------------------------------

describe("DEFAULT_TASK_TIMEOUT", () => {
  it("is 60", () => {
    const ctx = makeContext();
    loadTasks(ctx);
    expect(vm.runInContext("DEFAULT_TASK_TIMEOUT", ctx)).toBe(60);
  });
});

// ---------------------------------------------------------------------------
// _syncInFlight dedup
// ---------------------------------------------------------------------------

describe("syncTask", () => {
  it("calls sync endpoint and clears diff cache", async () => {
    const api = vi.fn().mockResolvedValue({ status: "ok" });
    const diffCache = new Map([["t-sync", { diff: "old" }]]);

    const ctx = makeContext({
      api,
      diffCache,
      document: {
        getElementById: () => null,
        createElement: (tag) => makeEl(tag),
        querySelector: () => null,
        querySelectorAll: () => [],
        addEventListener: () => {},
        readyState: "complete",
      },
    });
    loadTasks(ctx);

    await vm.runInContext('syncTask("t-sync")', ctx);

    expect(api).toHaveBeenCalledWith("/api/tasks/t-sync/sync", {
      method: "POST",
    });
    expect(diffCache.has("t-sync")).toBe(false);
  });

  it("shows alert for already_syncing response", async () => {
    const api = vi.fn().mockResolvedValue({ status: "already_syncing" });
    const showAlert = vi.fn();

    const ctx = makeContext({
      api,
      showAlert,
      document: {
        getElementById: () => null,
        createElement: (tag) => makeEl(tag),
        querySelector: () => null,
        querySelectorAll: () => [],
        addEventListener: () => {},
        readyState: "complete",
      },
    });
    loadTasks(ctx);

    await vm.runInContext('syncTask("t-sync2")', ctx);

    expect(showAlert).toHaveBeenCalledWith(
      "Sync is already in progress for this task.",
    );
  });
});

// ---------------------------------------------------------------------------
// runTestTask
// ---------------------------------------------------------------------------

describe("runTestTask", () => {
  it("sends test criteria and closes modal", async () => {
    const criteriaEl = makeEl("TEXTAREA", {
      id: "modal-test-criteria",
      value: "all tests pass",
    });
    const api = vi.fn().mockResolvedValue({});
    const closeModal = vi.fn();

    const ctx = makeContext({
      api,
      closeModal,
      elements: [["modal-test-criteria", criteriaEl]],
    });
    ctx._modalState.taskId = "t-test";
    loadTasks(ctx);

    await vm.runInContext("runTestTask()", ctx);

    expect(api).toHaveBeenCalledWith(
      "/api/tasks/t-test/test",
      expect.objectContaining({
        method: "POST",
      }),
    );
    const body = JSON.parse(api.mock.calls[0][1].body);
    expect(body.criteria).toBe("all tests pass");
    expect(closeModal).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// toggleTestSection
// ---------------------------------------------------------------------------

describe("toggleTestSection", () => {
  it("toggles hidden class and focuses criteria", () => {
    const criteriaEl = makeEl("TEXTAREA", { id: "modal-test-criteria" });
    let focused = false;
    criteriaEl.focus = () => {
      focused = true;
    };
    const section = makeEl("DIV", { id: "modal-test-section" });
    section.classList.add("hidden");

    const ctx = makeContext({
      elements: [
        ["modal-test-section", section],
        ["modal-test-criteria", criteriaEl],
      ],
    });
    loadTasks(ctx);

    vm.runInContext("toggleTestSection()", ctx);

    expect(section.classList.contains("hidden")).toBe(false);
    expect(focused).toBe(true);
  });
});
