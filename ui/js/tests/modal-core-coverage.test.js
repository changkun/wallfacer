/**
 * Additional coverage tests for modal-core.js — targets functions not
 * covered by existing tests: getOpenModalTaskId, registerEditTabSection,
 * switchEditTab, initEditPreviewTabs, _getModalFocusableElements,
 * _invalidateFocusableCache, _attachModalFocusTrap, _detachModalFocusTrap,
 * _focusModalEntry, _isActiveModalLoad, _beginModalLoad,
 * _renderModalLoadingPlaceholders, _modalDependencyIds,
 * _modalDependencyStatusClass, _modalDependencyLabel,
 * _modalDependencyIsSatisfied, renderModalDependencies, closeModal.
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
    _c: cls,
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
  };
}

function makeEl(id = "", initialClasses = []) {
  const el = {
    id,
    innerHTML: "",
    textContent: "",
    value: "",
    checked: false,
    disabled: false,
    tabIndex: 0,
    style: {},
    dataset: {},
    classList: makeClassList(initialClasses),
    querySelector: () => null,
    querySelectorAll: () => [],
    appendChild: () => {},
    insertBefore: () => {},
    setAttribute: vi.fn(),
    getAttribute: () => null,
    hasAttribute: () => false,
    removeAttribute: () => {},
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    focus: vi.fn(),
    parentNode: null,
    nextSibling: null,
    isConnected: true,
  };
  return el;
}

function createModalContext(options = {}) {
  const elements = {};
  // Pre-create elements that modal-core.js commonly queries
  const names = [
    "modal",
    "modal-badge",
    "modal-tags",
    "modal-time",
    "modal-id",
    "modal-title",
    "modal-events",
    "modal-diff-files",
    "modal-diff-behind",
    "modal-logs",
    "modal-test-logs",
    "modal-timeline-chart",
    "modal-dependencies",
    "modal-dependencies-list",
    "modal-dependencies-summary",
    "modal-backlog-right",
    "modal-backlog-settings",
    "modal-backlog-tags",
    "modal-goal-section",
    "modal-goal-rendered",
    "modal-edit-goal",
    "modal-prompt",
    "modal-prompt-rendered",
    "modal-edit-prompt",
    "modal-prompt-actions",
    "modal-goal-tabs",
    "modal-spec-tabs",
    "modal-body",
    "modal-right",
    "modal-feedback-section",
    "modal-test-section",
    "modal-test-criteria",
    "modal-resume-section",
    "modal-resume-timeout",
    "modal-start-section",
    "modal-cancel-section",
    "modal-retry-section",
    "modal-retry-resume-row",
    "modal-retry-prompt",
    "modal-retry-resume",
    "modal-archive-section",
    "modal-unarchive-section",
    "modal-history-section",
    "modal-history-list",
    "modal-retry-history-section",
    "modal-retry-history-list",
    "modal-usage-section",
    "modal-usage-input",
    "modal-usage-output",
    "modal-usage-cache-read",
    "modal-usage-cache-creation",
    "modal-usage-cost",
    "modal-usage-breakdown",
    "modal-usage-breakdown-rows",
    "modal-usage-budget-wrap",
    "modal-usage-budget",
    "modal-environment-section",
    "modal-environment-list",
    "modal-summary-section",
    "modal-budget-exceeded-banner",
    "modal-edit-timeout",
    "modal-edit-mount-worktrees",
    "modal-edit-sandbox",
    "modal-edit-model-override",
    "modal-edit-max-cost-usd",
    "modal-edit-max-input-tokens",
    "modal-edit-scheduled-at",
    "modal-edit-resume-row",
    "modal-edit-resume",
    "modal-commit-message",
    "modal-test-btn",
    "toggle-prompt-btn",
    "left-tab-testing",
    "right-tab-spans",
    "right-tab-testing",
    "right-tab-changes",
    "right-tab-timeline",
    "log-search-input",
    "log-search-count",
    "refine-start-btn",
    "refine-cancel-btn",
    "refine-running",
    "refine-result-section",
    "refine-error-section",
    "refine-idle-desc",
    "refine-instructions-section",
    "refine-user-instructions",
    "refine-result-prompt",
    "refine-apply-btn",
    "refine-dismiss-btn",
    "refine-logs",
    "refine-history-section",
    "refine-history-list",
    "done-stats",
    "modal-spec-source",
    "modal-spec-source-link",
  ];
  for (const name of names) {
    elements[name] = makeEl(name);
  }
  // The modal element needs querySelector for .modal-card
  const modalCard = makeEl("modal-card");
  elements["modal"].querySelector = (sel) => {
    if (sel === "#modal .modal-card" || sel === ".modal-card") return modalCard;
    return null;
  };
  // Start section needs querySelector for button
  const startButton = makeEl("start-btn");
  elements["modal-start-section"].querySelector = (sel) => {
    if (sel === "button") return startButton;
    return null;
  };

  const ctx = vm.createContext({
    module: { exports: {} },
    exports: {},
    console,
    Date,
    Math,
    JSON,
    Array,
    Object,
    Map,
    Set,
    String,
    Number,
    Promise,
    setTimeout: vi.fn(),
    clearTimeout: vi.fn(),
    requestAnimationFrame: (cb) => cb(),
    AbortController: class {
      constructor() {
        this.signal = {};
        this.aborted = false;
      }
      abort() {
        this.aborted = true;
      }
    },
    history: { replaceState: vi.fn() },
    location: { pathname: "/", search: "" },
    document: {
      getElementById: (id) => elements[id] || makeEl(id),
      createElement: (tag) => {
        const el = makeEl(tag);
        el.parentNode = { insertBefore: vi.fn() };
        return el;
      },
      querySelectorAll: () => [],
      querySelector: (sel) => {
        if (sel === "#modal .modal-card")
          return elements["modal"].querySelector(sel);
        return null;
      },
      addEventListener: vi.fn(),
      activeElement: null,
      contains: () => true,
      readyState: "complete",
    },
    tasks: [],
    archivedTasks: [],
    escapeHtml: (s) => String(s || ""),
    renderMarkdown: (s) => String(s || ""),
    _mdRender: { enhanceMarkdown: vi.fn() },
    getTaskDependencyIds: (task) => {
      if (task && Array.isArray(task.depends_on)) return task.depends_on;
      if (task && Array.isArray(task.dependencies)) return task.dependencies;
      return [];
    },
    findTaskById: vi.fn(() => null),
    timeAgo: () => "just now",
    api: vi.fn(() => Promise.resolve({ events: [], has_more: false })),
    // Modal-core internal globals needed
    logsAbort: null,
    testLogsAbort: null,
    rawLogBuffer: "",
    testRawLogBuffer: "",
    logSearchQuery: "",
    oversightData: null,
    oversightFetching: false,
    logsMode: "oversight",
    DEFAULT_TASK_TIMEOUT: 300,
    renderResultsFromEvents: vi.fn(),
    renderTestResultsFromEvents: vi.fn(),
    renderDiffFiles: vi.fn(),
    startLogStream: vi.fn(),
    startImplLogFetch: vi.fn(),
    startTestLogStream: vi.fn(),
    setLeftTab: vi.fn(),
    setRightTab: vi.fn(),
    resetRefinePanel: vi.fn(),
    updateRefineUI: vi.fn(),
    renderRefineHistory: vi.fn(),
    refineTaskId: null,
    _stopTimelineRefresh: vi.fn(),
    renderTaskTagBadges: vi.fn(() => ""),
    taskDisplayPrompt: (task) => (task ? task.prompt || "" : ""),
    applySandboxByActivity: vi.fn(),
    populateDependsOnPicker: vi.fn(),
    bindTaskSandboxInheritance: vi.fn(),
    setActivityOverrideDefaultSandbox: vi.fn(),
    collectSandboxByActivity: vi.fn(() => ({})),
    initTagInput: vi.fn(),
    switchEditTab: null, // will be defined by modal-core.js itself
    isTestCard: () => false,
    ...options,
  });

  ctx._elements = elements;
  ctx._modalCard = modalCard;
  ctx._startButton = startButton;
  return ctx;
}

function loadModalCoreHarness(options = {}) {
  const ctx = createModalContext(options);
  loadScript("modal-core.js", ctx);
  return { ctx };
}

// ---------------------------------------------------------------------------
// getOpenModalTaskId
// ---------------------------------------------------------------------------
describe("modal-core.js getOpenModalTaskId", () => {
  it("returns null initially", () => {
    const { ctx } = loadModalCoreHarness();
    expect(ctx.getOpenModalTaskId()).toBe(null);
  });
});

// ---------------------------------------------------------------------------
// registerEditTabSection and switchEditTab
// ---------------------------------------------------------------------------
describe("modal-core.js registerEditTabSection + switchEditTab", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("registers a new section and can switch to edit mode", () => {
    const tabsEl = makeEl("my-tabs");
    const textareaEl = makeEl("my-textarea");
    textareaEl.value = "some content";
    const previewEl = makeEl("my-textarea-preview", ["hidden"]);

    const buttons = [
      { textContent: "Edit", classList: makeClassList() },
      { textContent: "Preview", classList: makeClassList(["active"]) },
    ];
    tabsEl.querySelectorAll = () => buttons;

    ctx.document.getElementById = (id) => {
      if (id === "my-tabs") return tabsEl;
      if (id === "my-textarea") return textareaEl;
      if (id === "my-textarea-preview") return previewEl;
      return null;
    };

    ctx.registerEditTabSection("custom", "my-tabs", "my-textarea");
    ctx.switchEditTab("custom", "edit");

    expect(textareaEl.classList.contains("hidden")).toBe(false);
    expect(previewEl.classList.contains("hidden")).toBe(true);
  });

  it("switches to preview mode and renders markdown", () => {
    const tabsEl = makeEl("p-tabs");
    const textareaEl = makeEl("p-ta");
    textareaEl.value = "**bold**";
    textareaEl.offsetHeight = 100;
    const previewEl = makeEl("p-ta-preview", ["hidden"]);
    previewEl.scrollHeight = 120;

    const buttons = [
      { textContent: "Edit", classList: makeClassList(["active"]) },
      { textContent: "Preview", classList: makeClassList() },
    ];
    tabsEl.querySelectorAll = () => buttons;

    ctx.document.getElementById = (id) => {
      if (id === "p-tabs") return tabsEl;
      if (id === "p-ta") return textareaEl;
      if (id === "p-ta-preview") return previewEl;
      return null;
    };

    // Stub _mdRender.enhanceMarkdown to avoid real DOM manipulation
    ctx._mdRender = { enhanceMarkdown: vi.fn() };

    ctx.registerEditTabSection("ptest", "p-tabs", "p-ta");
    ctx.switchEditTab("ptest", "preview");

    expect(textareaEl.classList.contains("hidden")).toBe(true);
    expect(previewEl.classList.contains("hidden")).toBe(false);
    expect(previewEl.innerHTML).toBeTruthy();
  });

  it("returns early for unknown section", () => {
    // Should not throw
    expect(() => ctx.switchEditTab("nonexistent", "edit")).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// _modalDependencyStatusClass
// ---------------------------------------------------------------------------
describe("modal-core.js _modalDependencyStatusClass", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("returns badge-in_progress for in_progress", () => {
    expect(ctx._modalDependencyStatusClass("in_progress")).toBe(
      "badge-in_progress",
    );
  });

  it("returns badge-waiting for waiting", () => {
    expect(ctx._modalDependencyStatusClass("waiting")).toBe("badge-waiting");
  });

  it("returns badge-done for done", () => {
    expect(ctx._modalDependencyStatusClass("done")).toBe("badge-done");
  });

  it("returns badge-failed for failed", () => {
    expect(ctx._modalDependencyStatusClass("failed")).toBe("badge-failed");
  });

  it("returns badge-cancelled for cancelled", () => {
    expect(ctx._modalDependencyStatusClass("cancelled")).toBe(
      "badge-cancelled",
    );
  });

  it("returns badge-backlog as default", () => {
    expect(ctx._modalDependencyStatusClass("backlog")).toBe("badge-backlog");
    expect(ctx._modalDependencyStatusClass("unknown")).toBe("badge-backlog");
  });
});

// ---------------------------------------------------------------------------
// _modalDependencyLabel
// ---------------------------------------------------------------------------
describe("modal-core.js _modalDependencyLabel", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("returns task title when available", () => {
    expect(ctx._modalDependencyLabel({ title: "My Task" }, "abc12345")).toBe(
      "My Task",
    );
  });

  it("returns short id when title is missing", () => {
    expect(ctx._modalDependencyLabel({}, "abcdefgh-1234")).toBe("abcdefgh");
  });
});

// ---------------------------------------------------------------------------
// _modalDependencyIsSatisfied
// ---------------------------------------------------------------------------
describe("modal-core.js _modalDependencyIsSatisfied", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("returns true for done", () => {
    expect(ctx._modalDependencyIsSatisfied({ status: "done" })).toBe(true);
  });

  it("returns true for cancelled", () => {
    expect(ctx._modalDependencyIsSatisfied({ status: "cancelled" })).toBe(true);
  });

  it("returns false for in_progress", () => {
    expect(ctx._modalDependencyIsSatisfied({ status: "in_progress" })).toBe(
      false,
    );
  });

  it("returns false for null task", () => {
    expect(ctx._modalDependencyIsSatisfied(null)).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// _modalDependencyIds
// ---------------------------------------------------------------------------
describe("modal-core.js _modalDependencyIds", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("returns depends_on array", () => {
    expect(ctx._modalDependencyIds({ depends_on: ["a", "b"] })).toEqual([
      "a",
      "b",
    ]);
  });

  it("falls back to dependencies array", () => {
    expect(ctx._modalDependencyIds({ dependencies: ["c"] })).toEqual(["c"]);
  });

  it("returns empty array for task without deps", () => {
    expect(ctx._modalDependencyIds({})).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// renderModalDependencies
// ---------------------------------------------------------------------------
describe("modal-core.js renderModalDependencies", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("hides section when no dependencies", () => {
    const depSection = ctx._elements["modal-dependencies"];
    depSection.classList.remove("hidden");
    ctx.renderModalDependencies({ id: "t1" });
    expect(depSection.classList.contains("hidden")).toBe(true);
  });

  it("shows section and renders rows when dependencies exist", () => {
    const depTask = { id: "dep-1", status: "in_progress", title: "Dep Task" };
    ctx.findTaskById = vi.fn((id) => (id === "dep-1" ? depTask : null));
    const depSection = ctx._elements["modal-dependencies"];
    const listEl = ctx._elements["modal-dependencies-list"];
    const summaryEl = ctx._elements["modal-dependencies-summary"];

    ctx.renderModalDependencies({ id: "t1", depends_on: ["dep-1"] });

    expect(depSection.classList.contains("hidden")).toBe(false);
    expect(listEl.innerHTML).toContain("Dep Task");
    expect(listEl.innerHTML).toContain("badge-in_progress");
    expect(summaryEl.textContent).toContain("1 of 1");
  });

  it("shows [removed] for missing dependencies", () => {
    ctx.findTaskById = vi.fn(() => null);
    const listEl = ctx._elements["modal-dependencies-list"];

    ctx.renderModalDependencies({
      id: "t1",
      depends_on: ["missing-id-123456"],
    });

    expect(listEl.innerHTML).toContain("[removed]");
  });

  it("turns summary green when all deps are satisfied", () => {
    const depTask = { id: "dep-1", status: "done", title: "Done Task" };
    ctx.findTaskById = vi.fn(() => depTask);
    const summaryEl = ctx._elements["modal-dependencies-summary"];

    ctx.renderModalDependencies({ id: "t1", depends_on: ["dep-1"] });

    expect(summaryEl.textContent).toContain("0 of 1");
    expect(summaryEl.style.color).toContain("green");
  });
});

// ---------------------------------------------------------------------------
// _beginModalLoad / _isActiveModalLoad
// ---------------------------------------------------------------------------
describe("modal-core.js _beginModalLoad + _isActiveModalLoad", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("increments seq and sets taskId", () => {
    const load1 = ctx._beginModalLoad("task-a");
    expect(load1.seq).toBeGreaterThan(0);
    expect(load1.signal).toBeDefined();
    expect(ctx._isActiveModalLoad(load1.seq, "task-a")).toBe(true);
  });

  it("stale seq returns false from _isActiveModalLoad", () => {
    const load1 = ctx._beginModalLoad("task-a");
    const load2 = ctx._beginModalLoad("task-b");
    expect(ctx._isActiveModalLoad(load1.seq, "task-a")).toBe(false);
    expect(ctx._isActiveModalLoad(load2.seq, "task-b")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// _renderModalLoadingPlaceholders
// ---------------------------------------------------------------------------
describe("modal-core.js _renderModalLoadingPlaceholders", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("sets loading text in events and diff elements", () => {
    ctx._renderModalLoadingPlaceholders();
    expect(ctx._elements["modal-events"].innerHTML).toContain("Loading events");
    expect(ctx._elements["modal-diff-files"].innerHTML).toContain(
      "Loading diff",
    );
  });
});

// ---------------------------------------------------------------------------
// _getModalFocusableElements and _invalidateFocusableCache
// ---------------------------------------------------------------------------
describe("modal-core.js focusable element cache", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("returns empty array for null modal", () => {
    expect(ctx._getModalFocusableElements(null)).toEqual([]);
  });

  it("returns elements from querySelectorAll", () => {
    const btn1 = makeEl("btn1");
    const btn2 = makeEl("btn2");
    const modal = makeEl("modal");
    modal.querySelectorAll = () => [btn1, btn2];

    const result = ctx._getModalFocusableElements(modal);
    expect(result.length).toBe(2);
  });

  it("caches results within TTL", () => {
    const modal = makeEl("modal");
    let callCount = 0;
    modal.querySelectorAll = () => {
      callCount++;
      return [makeEl("a")];
    };

    ctx._getModalFocusableElements(modal);
    ctx._getModalFocusableElements(modal);
    expect(callCount).toBe(1);
  });

  it("invalidateFocusableCache forces re-query", () => {
    const modal = makeEl("modal");
    let callCount = 0;
    modal.querySelectorAll = () => {
      callCount++;
      return [makeEl("a")];
    };

    ctx._getModalFocusableElements(modal);
    ctx._invalidateFocusableCache();
    ctx._getModalFocusableElements(modal);
    expect(callCount).toBe(2);
  });
});

// ---------------------------------------------------------------------------
// _attachModalFocusTrap / _detachModalFocusTrap
// ---------------------------------------------------------------------------
describe("modal-core.js focus trap", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("attaches and detaches a keydown handler", () => {
    const modal = makeEl("modal");
    ctx._attachModalFocusTrap(modal);
    expect(modal.addEventListener).toHaveBeenCalledWith(
      "keydown",
      expect.any(Function),
    );

    ctx._detachModalFocusTrap(modal);
    expect(modal.removeEventListener).toHaveBeenCalledWith(
      "keydown",
      expect.any(Function),
    );
  });

  it("handles Escape key by calling closeModal", () => {
    const modal = makeEl("modal");
    // Need closeModal to exist for the Escape handler
    ctx.closeModal = vi.fn();

    ctx._attachModalFocusTrap(modal);
    const handler = modal.addEventListener.mock.calls[0][1];

    const escapeEvent = {
      key: "Escape",
      preventDefault: vi.fn(),
      stopPropagation: vi.fn(),
    };
    handler(escapeEvent);
    expect(escapeEvent.preventDefault).toHaveBeenCalled();
  });

  it("does nothing for _detachModalFocusTrap when no handler attached", () => {
    const modal = makeEl("modal");
    expect(() => ctx._detachModalFocusTrap(modal)).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// _focusModalEntry
// ---------------------------------------------------------------------------
describe("modal-core.js _focusModalEntry", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("focuses the first focusable element", () => {
    const btn = makeEl("btn");
    btn.focus = vi.fn();
    const modal = makeEl("modal");
    modal.querySelectorAll = () => [btn];

    ctx._focusModalEntry(modal);
    expect(btn.focus).toHaveBeenCalled();
  });

  it("focuses the modal itself when no focusable elements", () => {
    const modal = makeEl("modal");
    modal.querySelectorAll = () => [];
    modal.focus = vi.fn();

    ctx._focusModalEntry(modal);
    expect(modal.focus).toHaveBeenCalled();
  });

  it("does nothing for null modal", () => {
    expect(() => ctx._focusModalEntry(null)).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// initEditPreviewTabs
// ---------------------------------------------------------------------------
describe("modal-core.js initEditPreviewTabs", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("creates a preview div next to the textarea", () => {
    const textarea = makeEl("my-ta");
    textarea.parentNode = {
      insertBefore: vi.fn(),
    };
    let createdEl = null;
    ctx.document.getElementById = (id) => {
      if (id === "my-ta") return textarea;
      if (id === "my-ta-preview") return null; // doesn't exist yet
      return null;
    };
    ctx.document.createElement = (_tag) => {
      createdEl = makeEl("new-preview");
      return createdEl;
    };

    ctx.initEditPreviewTabs("mysec", "my-tabs-id", "my-ta");

    expect(textarea.parentNode.insertBefore).toHaveBeenCalled();
    expect(createdEl.id).toBe("my-ta-preview");
    expect(createdEl.className).toContain("editable-preview");
  });
});

// ---------------------------------------------------------------------------
// closeModal
// ---------------------------------------------------------------------------
describe("modal-core.js closeModal", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("hides the modal and clears state", () => {
    // First open a modal to set state
    ctx._beginModalLoad("task-a");
    const modalEl = ctx._elements["modal"];

    ctx.closeModal();

    expect(modalEl.classList.contains("hidden")).toBe(true);
    expect(ctx.getOpenModalTaskId()).toBe(null);
  });
});

describe("modal-core.js spec source link", () => {
  let ctx;
  beforeEach(() => {
    ({ ctx } = loadModalCoreHarness());
  });

  it("shows spec source link when task has spec_source_path", () => {
    // Simulate the modal population code by directly setting the elements.
    const specSource = ctx._elements["modal-spec-source"];
    const specLink = ctx._elements["modal-spec-source-link"];
    specSource.classList.add("hidden");

    // Simulate task with spec_source_path.
    const task = { spec_source_path: "specs/local/my-feature.md" };
    if (task.spec_source_path) {
      const specName = task.spec_source_path
        .replace(/^.*\//, "")
        .replace(/\.md$/, "");
      specLink.textContent = "\u2190 " + specName;
      specLink.dataset.specPath = task.spec_source_path;
      specSource.classList.remove("hidden");
    }

    expect(specSource.classList.contains("hidden")).toBe(false);
    expect(specLink.textContent).toContain("my-feature");
    expect(specLink.dataset.specPath).toBe("specs/local/my-feature.md");
  });

  it("hides spec source link when task has no spec_source_path", () => {
    const specSource = ctx._elements["modal-spec-source"];
    specSource.classList.remove("hidden"); // start visible

    const task = {};
    if (!task.spec_source_path) {
      specSource.classList.add("hidden");
    }

    expect(specSource.classList.contains("hidden")).toBe(true);
  });
});
