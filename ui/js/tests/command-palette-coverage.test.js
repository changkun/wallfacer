/**
 * Additional coverage tests for command-palette.js.
 *
 * Targets uncovered paths: fuzzy match scoring, section ordering by mode,
 * spec/doc row building, _searchLocal mode branches, keyboard navigation
 * edge cases, _buildTaskListSections rendering, _updateContextActions,
 * open/close/toggle, and global key handler branches.
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeClassList() {
  const set = new Set();
  return {
    _items: set,
    add(cls) {
      set.add(cls);
    },
    remove(cls) {
      set.delete(cls);
    },
    toggle(cls, force) {
      if (force === undefined) {
        if (set.has(cls)) {
          set.delete(cls);
          return false;
        }
        set.add(cls);
        return true;
      }
      if (force) {
        set.add(cls);
        return true;
      }
      set.delete(cls);
      return false;
    },
    contains(cls) {
      return set.has(cls);
    },
  };
}

function createElement(overrides = {}) {
  const node = {
    _children: [],
    _parent: null,
    _listeners: {},
    classList: makeClassList(),
    style: {},
    dataset: {},
    textContent: "",
    innerHTML: "",
    value: "",
    hidden: false,
    selectionStart: 0,
    selectionEnd: 0,
    tagName: overrides.tagName || "div",
    addEventListener(type, handler) {
      this._listeners[type] = this._listeners[type] || [];
      this._listeners[type].push(handler);
    },
    dispatchEvent(evt) {
      (this._listeners[evt.type] || []).forEach((fn) => fn(evt));
    },
    appendChild(child) {
      child._parent = this;
      this._children.push(child);
      return child;
    },
    remove() {
      if (!this._parent) return;
      this._parent._children = this._parent._children.filter(
        (child) => child !== this,
      );
    },
    querySelectorAll(selector) {
      const result = [];
      const isMatch = (el) => {
        if (!selector || selector === "*") return true;
        if (selector.startsWith(".")) {
          return el.classList.contains(selector.slice(1));
        }
        return el.tagName === selector.toUpperCase();
      };
      const visit = (current) => {
        current._children.forEach((child) => {
          if (isMatch(child)) result.push(child);
          visit(child);
        });
      };
      visit(this);
      return result;
    },
    focus() {
      this.focused = true;
    },
    setSelectionRange(start, end) {
      this.selectionStart = start;
      this.selectionEnd = end;
    },
  };
  Object.defineProperty(node, "className", {
    get() {
      return Array.from(node.classList._items || []).join(" ");
    },
    set(value = "") {
      const next = String(value).split(/\s+/).filter(Boolean);
      node.classList = makeClassList();
      next.forEach((cls) => node.classList.add(cls));
      node.classList._items = new Set(next);
    },
  });
  return Object.assign(node, overrides);
}

function makeContext(extra = {}) {
  const storage = new Map();
  const elements = new Map(extra.elements || []);
  const body = createElement({ tagName: "BODY" });
  const ctx = {
    console,
    Math,
    Date,
    setTimeout,
    clearTimeout,
    setInterval: () => 0,
    clearInterval: () => 0,
    Promise,
    Object,
    Number,
    String,
    Array,
    Set,
    document: {
      body,
      createElement: () => createElement(),
      getElementById: (id) => elements.get(id) || null,
      querySelector: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      readyState: "complete",
      addEventListener: () => {},
      documentElement: { setAttribute: () => {} },
      activeElement: null,
    },
    window: {
      addEventListener: () => {},
    },
    localStorage: {
      getItem(key) {
        return storage.has(key) ? storage.get(key) : null;
      },
      setItem(key, value) {
        storage.set(key, String(value));
      },
      removeItem(key) {
        storage.delete(key);
      },
      clear() {
        storage.clear();
      },
    },
    apiGet: extra.fetch
      ? (path) =>
          extra
            .fetch(path)
            .then((r) => (r.ok ? r.json() : Promise.reject(r.status)))
      : () => Promise.resolve(null),
    // Provide the task() route helper
    task: (id) => ({
      archive: () => `/api/tasks/${id}/archive`,
      update: () => `/api/tasks/${id}`,
      done: () => `/api/tasks/${id}/done`,
      resume: () => `/api/tasks/${id}/resume`,
      test: () => `/api/tasks/${id}/test`,
    }),
    waitForTaskDelta: vi.fn(),
    ...extra,
  };
  ctx.window.localStorage = ctx.localStorage;
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  loadLibDeps(filename, ctx);
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

function setupCommandPaletteContext(helpers = {}) {
  const palette = createElement({
    tagName: "DIV",
    id: "command-palette",
    classList: makeClassList(),
  });
  const panel = createElement({ tagName: "DIV", id: "command-palette-panel" });
  const input = createElement({
    tagName: "INPUT",
    id: "command-palette-input",
  });
  const results = createElement({
    tagName: "DIV",
    id: "command-palette-results",
  });
  const hint = createElement({
    tagName: "DIV",
    id: "command-palette-hint-keys",
  });

  palette.appendChild(panel);
  palette.appendChild(input);
  palette.appendChild(results);
  palette.appendChild(hint);

  const ctx = makeContext({
    elements: [
      ["command-palette", palette],
      ["command-palette-panel", panel],
      ["command-palette-input", input],
      ["command-palette-results", results],
      ["command-palette-hint-keys", hint],
    ],
    task: (id) => ({
      archive: () => `/api/tasks/${id}/archive`,
    }),
    waitForTaskDelta: vi.fn(),
    ...helpers,
  });

  loadScript(ctx, "state.js");
  loadScript(ctx, "utils.js");
  loadScript(ctx, "command-palette.js");
  return ctx;
}

// ---------------------------------------------------------------------------
// commandPaletteFuzzyMatch — scoring branches
// ---------------------------------------------------------------------------

describe("commandPaletteFuzzyMatch scoring", () => {
  it("returns max score for empty query", () => {
    const ctx = setupCommandPaletteContext();
    const result = ctx.commandPaletteFuzzyMatch("anything", "");
    expect(result.matched).toBe(true);
    expect(result.score).toBe(Number.MAX_SAFE_INTEGER);
  });

  it("returns exact match score based on position", () => {
    const ctx = setupCommandPaletteContext();
    const r1 = ctx.commandPaletteFuzzyMatch("hello world", "hello");
    expect(r1.matched).toBe(true);
    expect(r1.score).toBe(10000); // exact at position 0

    const r2 = ctx.commandPaletteFuzzyMatch("say hello", "hello");
    expect(r2.matched).toBe(true);
    expect(r2.score).toBe(10000 - 4); // exact at position 4
  });

  it("falls back to fuzzy scoring when no exact match", () => {
    const ctx = setupCommandPaletteContext();
    const result = ctx.commandPaletteFuzzyMatch("abcdef", "adf");
    expect(result.matched).toBe(true);
    expect(result.score).toBeLessThan(1000);
  });

  it("returns not matched when character is missing", () => {
    const ctx = setupCommandPaletteContext();
    const result = ctx.commandPaletteFuzzyMatch("abc", "xyz");
    expect(result.matched).toBe(false);
    expect(result.score).toBe(0);
  });

  it("handles null/undefined text gracefully", () => {
    const ctx = setupCommandPaletteContext();
    const result = ctx.commandPaletteFuzzyMatch(null, "q");
    expect(result.matched).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// commandPaletteMatchTask — multi-field matching
// ---------------------------------------------------------------------------

describe("commandPaletteMatchTask multi-field", () => {
  it("returns max score for empty query", () => {
    const ctx = setupCommandPaletteContext();
    const result = ctx.commandPaletteMatchTask({ id: "x" }, "");
    expect(result.matched).toBe(true);
    expect(result.score).toBe(Number.MAX_SAFE_INTEGER);
  });

  it("picks the best score across fields", () => {
    const ctx = setupCommandPaletteContext();
    const task = {
      id: "abcd1234-0000-0000-0000-000000000000",
      title: "Something else",
      prompt: "exact match here",
    };
    const result = ctx.commandPaletteMatchTask(task, "exact match");
    expect(result.matched).toBe(true);
    // Should match the prompt with a higher score than the short ID
    expect(result.score).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// commandPaletteSearchTasks — sort by score then title
// ---------------------------------------------------------------------------

describe("commandPaletteSearchTasks sort order", () => {
  it("sorts by score descending, then by title alphabetically", () => {
    const ctx = setupCommandPaletteContext();
    const tasks = [
      { id: "1", title: "Zebra task", prompt: "no match at all" },
      { id: "2", title: "Alpha task", prompt: "no match at all" },
      { id: "3", title: "search match", prompt: "" },
    ];
    const result = ctx.commandPaletteSearchTasks("search", tasks);
    // Only task 3 has "search" in its fields
    expect(result.length).toBe(1);
    expect(result[0].id).toBe("3");
  });

  it("returns all tasks when query is empty", () => {
    const ctx = setupCommandPaletteContext();
    const tasks = [
      { id: "1", title: "A" },
      { id: "2", title: "B" },
    ];
    const result = ctx.commandPaletteSearchTasks("", tasks);
    expect(result.length).toBe(2);
  });

  it("returns empty array when no tasks match", () => {
    const ctx = setupCommandPaletteContext();
    const tasks = [{ id: "1", title: "hello" }];
    const result = ctx.commandPaletteSearchTasks("zzz", tasks);
    expect(result.length).toBe(0);
  });

  it("uses global tasks when sourceTasks is not provided", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext('tasks = [{ id: "g1", title: "global task" }]', ctx);
    const result = ctx.commandPaletteSearchTasks("global");
    expect(result.length).toBe(1);
    expect(result[0].id).toBe("g1");
  });
});

// ---------------------------------------------------------------------------
// commandPaletteTaskActions — status-gated action coverage
// ---------------------------------------------------------------------------

describe("commandPaletteTaskActions extended", () => {
  it("returns empty for null task", () => {
    const ctx = setupCommandPaletteContext();
    expect(ctx.commandPaletteTaskActions(null)).toEqual([]);
  });

  it("returns empty for task without id", () => {
    const ctx = setupCommandPaletteContext();
    expect(ctx.commandPaletteTaskActions({ status: "backlog" })).toEqual([]);
  });

  it("blocks start action when refinement is running", () => {
    const ctx = setupCommandPaletteContext();
    const task = {
      id: "t1",
      status: "backlog",
      current_refinement: { status: "running" },
    };
    const actions = ctx.commandPaletteTaskActions(task).map((a) => a.id);
    expect(actions).not.toContain("start-task");
    expect(actions).toContain("open-task");
  });

  it("blocks start action when refinement is done", () => {
    const ctx = setupCommandPaletteContext();
    const task = {
      id: "t1",
      status: "backlog",
      current_refinement: { status: "done" },
    };
    const actions = ctx.commandPaletteTaskActions(task).map((a) => a.id);
    expect(actions).not.toContain("start-task");
  });

  it("includes archive for cancelled non-archived tasks", () => {
    const ctx = setupCommandPaletteContext();
    const task = { id: "c1", status: "cancelled", archived: false };
    const actions = ctx.commandPaletteTaskActions(task).map((a) => a.id);
    expect(actions).toContain("archive-task");
  });

  it("excludes archive for already-archived tasks", () => {
    const ctx = setupCommandPaletteContext();
    const task = { id: "d1", status: "done", archived: true };
    const actions = ctx.commandPaletteTaskActions(task).map((a) => a.id);
    expect(actions).not.toContain("archive-task");
  });

  it("includes flamegraph and timeline when turns > 0", () => {
    const ctx = setupCommandPaletteContext();
    const task = { id: "t1", status: "done", turns: 5 };
    const actions = ctx.commandPaletteTaskActions(task).map((a) => a.id);
    expect(actions).toContain("open-task-spans");
    expect(actions).toContain("open-task-timeline");
  });

  it("excludes flamegraph and timeline when turns is 0", () => {
    const ctx = setupCommandPaletteContext();
    const task = { id: "t1", status: "done", turns: 0 };
    const actions = ctx.commandPaletteTaskActions(task).map((a) => a.id);
    expect(actions).not.toContain("open-task-spans");
    expect(actions).not.toContain("open-task-timeline");
  });

  it("includes testing and changes for non-backlog tasks", () => {
    const ctx = setupCommandPaletteContext();
    const task = {
      id: "t1",
      status: "in_progress",
      worktree_paths: { ws: "/path" },
    };
    const actions = ctx.commandPaletteTaskActions(task).map((a) => a.id);
    expect(actions).toContain("open-task-testing");
    expect(actions).toContain("open-task-changes");
  });

  it("excludes testing and changes for backlog tasks", () => {
    const ctx = setupCommandPaletteContext();
    const task = { id: "t1", status: "backlog" };
    const actions = ctx.commandPaletteTaskActions(task).map((a) => a.id);
    expect(actions).not.toContain("open-task-testing");
    expect(actions).not.toContain("open-task-changes");
  });

  it("does not include resume for failed tasks without session_id", () => {
    const ctx = setupCommandPaletteContext();
    const task = { id: "f1", status: "failed" };
    const actions = ctx.commandPaletteTaskActions(task).map((a) => a.id);
    expect(actions).not.toContain("resume-task");
    expect(actions).toContain("retry-task");
  });

  it("includes sync for waiting and failed tasks", () => {
    const ctx = setupCommandPaletteContext();
    const waiting = { id: "w1", status: "waiting" };
    const failed = { id: "f1", status: "failed" };
    expect(
      ctx.commandPaletteTaskActions(waiting).some((a) => a.id === "sync-task"),
    ).toBe(true);
    expect(
      ctx.commandPaletteTaskActions(failed).some((a) => a.id === "sync-task"),
    ).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// _searchLocal — mode-based section ordering
// ---------------------------------------------------------------------------

describe("_searchLocal mode-based ordering", () => {
  it("puts Docs first in docs mode", () => {
    const ctx = setupCommandPaletteContext({
      getCurrentMode: () => "docs",
    });
    vm.runInContext(
      `
      tasks = [{ id: "1", title: "My Task", prompt: "test" }];
      _docsEntries = [{ title: "Guide", slug: "guide/usage" }];
      _specTreeData = { nodes: [{ path: "specs/foo.md", spec: { title: "Foo" } }] };
    `,
      ctx,
    );

    ctx._searchLocal("");
    const state = ctx.window.__wallfacerTestState.commandPalette();
    // First rows should include docs entries
    const types = state.rows.map((r) => r.type);
    const firstDocIdx = types.indexOf("doc");
    const firstTaskIdx = types.indexOf("task");
    expect(firstDocIdx).toBeLessThan(firstTaskIdx);
  });

  it("puts Specs first in spec mode", () => {
    const ctx = setupCommandPaletteContext({
      getCurrentMode: () => "spec",
    });
    vm.runInContext(
      `
      tasks = [{ id: "1", title: "My Task", prompt: "test" }];
      _specTreeData = { nodes: [{ path: "specs/foo.md", spec: { title: "Foo" } }] };
    `,
      ctx,
    );

    ctx._searchLocal("");
    const state = ctx.window.__wallfacerTestState.commandPalette();
    const types = state.rows.map((r) => r.type);
    const firstSpecIdx = types.indexOf("spec");
    const firstTaskIdx = types.indexOf("task");
    expect(firstSpecIdx).toBeLessThan(firstTaskIdx);
  });

  it("puts Tasks first in board mode (default)", () => {
    const ctx = setupCommandPaletteContext({
      getCurrentMode: () => "board",
    });
    vm.runInContext(
      `
      tasks = [{ id: "1", title: "My Task", prompt: "test" }];
      _specTreeData = { nodes: [{ path: "specs/foo.md", spec: { title: "Foo" } }] };
    `,
      ctx,
    );

    ctx._searchLocal("");
    const state = ctx.window.__wallfacerTestState.commandPalette();
    const types = state.rows.map((r) => r.type);
    const firstTaskIdx = types.indexOf("task");
    const firstSpecIdx = types.indexOf("spec");
    expect(firstTaskIdx).toBeLessThan(firstSpecIdx);
  });
});

// ---------------------------------------------------------------------------
// _localSpecRowsForQuery
// ---------------------------------------------------------------------------

describe("_localSpecRowsForQuery", () => {
  it("filters specs by title and path", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext(
      `_specTreeData = {
        nodes: [
          { path: "specs/auth.md", spec: { title: "Authentication", status: "drafted" } },
          { path: "specs/sandbox.md", spec: { title: "Sandbox", status: "validated" } },
          { path: "specs/misc.md", spec: { title: "Misc" } },
        ]
      }`,
      ctx,
    );
    const rows = ctx._localSpecRowsForQuery("auth");
    expect(rows.length).toBe(1);
    expect(rows[0].title).toBe("Authentication");
  });

  it("returns all specs for empty query", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext(
      `_specTreeData = {
        nodes: [
          { path: "specs/a.md", spec: { title: "A" } },
          { path: "specs/b.md", spec: { title: "B" } },
        ]
      }`,
      ctx,
    );
    const rows = ctx._localSpecRowsForQuery("");
    expect(rows.length).toBe(2);
  });

  it("returns empty when _specTreeData is undefined", () => {
    const ctx = setupCommandPaletteContext();
    const rows = ctx._localSpecRowsForQuery("test");
    expect(rows).toEqual([]);
  });

  it("skips nodes without spec", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext(
      `_specTreeData = {
        nodes: [
          { path: "specs/dir/", spec: null },
          { path: "specs/a.md", spec: { title: "A" } },
        ]
      }`,
      ctx,
    );
    const rows = ctx._localSpecRowsForQuery("");
    expect(rows.length).toBe(1);
  });

  it("ranks title-starts-with higher than title-contains", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext(
      `_specTreeData = {
        nodes: [
          { path: "specs/b.md", spec: { title: "Boxtask" } },
          { path: "specs/a.md", spec: { title: "task runner" } },
        ]
      }`,
      ctx,
    );
    const rows = ctx._localSpecRowsForQuery("task");
    // Both match ("Boxtask" contains "task", "task runner" starts with "task")
    expect(rows.length).toBe(2);
    // "task runner" should rank first (starts-with = score 3 vs contains = score 2)
    expect(rows[0].title).toBe("task runner");
  });
});

// ---------------------------------------------------------------------------
// _localDocRowsForQuery
// ---------------------------------------------------------------------------

describe("_localDocRowsForQuery", () => {
  it("filters docs by title and slug", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext(
      `_docsEntries = [
        { title: "Getting Started", slug: "guide/getting-started" },
        { title: "Configuration", slug: "guide/configuration" },
      ]`,
      ctx,
    );
    const rows = ctx._localDocRowsForQuery("config");
    expect(rows.length).toBe(1);
    expect(rows[0].title).toBe("Configuration");
  });

  it("returns all docs for empty query", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext(
      `_docsEntries = [
        { title: "A", slug: "a" },
        { title: "B", slug: "b" },
      ]`,
      ctx,
    );
    const rows = ctx._localDocRowsForQuery("");
    expect(rows.length).toBe(2);
  });

  it("returns empty when _docsEntries is undefined", () => {
    const ctx = setupCommandPaletteContext();
    const rows = ctx._localDocRowsForQuery("test");
    expect(rows).toEqual([]);
  });

  it("matches by slug when title doesn't match", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext(
      `_docsEntries = [
        { title: "Guide", slug: "guide/automation" },
      ]`,
      ctx,
    );
    const rows = ctx._localDocRowsForQuery("automation");
    expect(rows.length).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// _buildTaskListSections — empty results
// ---------------------------------------------------------------------------

describe("_buildTaskListSections empty state", () => {
  it("renders No matches message when all groups are empty", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      { title: "Tasks", rows: [] },
      { title: "Specs", rows: [] },
    ]);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.rows.length).toBe(0);
    expect(state.activeIndex).toBe(-1);
  });

  it("renders empty section within a group that has no rows", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      { title: "Tasks", rows: [] },
      {
        title: "Actions",
        rows: [
          { type: "action", id: "a1", label: "Do thing", execute: () => {} },
        ],
      },
    ]);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.rows.length).toBe(1);
    expect(state.activeIndex).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// _buildTaskListSections — row type rendering
// ---------------------------------------------------------------------------

describe("_buildTaskListSections row types", () => {
  it("renders spec rows with status badge", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      {
        title: "Specs",
        rows: [
          {
            type: "spec",
            id: "spec:foo.md",
            title: "Foo Spec",
            status: "drafted",
            hint: "specs/foo.md",
            execute: () => {},
          },
        ],
      },
    ]);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.rows.length).toBe(1);
    expect(state.rows[0].type).toBe("spec");
  });

  it("renders doc rows", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      {
        title: "Docs",
        rows: [
          {
            type: "doc",
            id: "doc:guide",
            title: "Guide",
            hint: "guide/usage",
            execute: () => {},
          },
        ],
      },
    ]);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.rows.length).toBe(1);
    expect(state.rows[0].type).toBe("doc");
  });

  it("renders task rows with snippet", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      {
        title: "Tasks",
        rows: [
          {
            type: "task",
            id: "t1",
            title: "Test Task",
            status: "done",
            idHint: "t1234567",
            snippet: "<mark>matched</mark> text",
            prompt: "do something",
            execute: () => {},
          },
        ],
      },
    ]);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.rows[0].snippet).toBe("<mark>matched</mark> text");
  });

  it("renders task rows with prompt fallback when no snippet", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      {
        title: "Tasks",
        rows: [
          {
            type: "task",
            id: "t1",
            title: "Test Task",
            status: "backlog",
            idHint: "t1234567",
            snippet: "",
            prompt: "the task prompt content",
            execute: () => {},
          },
        ],
      },
    ]);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.rows[0].prompt).toBe("the task prompt content");
  });
});

// ---------------------------------------------------------------------------
// Keyboard navigation — edge cases
// ---------------------------------------------------------------------------

describe("keyboard navigation edge cases", () => {
  it("wraps from top to bottom when moving up from first item", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      {
        title: "Tasks",
        rows: [
          {
            type: "task",
            id: "1",
            title: "A",
            taskObj: { id: "1" },
            execute: () => {},
          },
          {
            type: "task",
            id: "2",
            title: "B",
            taskObj: { id: "2" },
            execute: () => {},
          },
        ],
      },
    ]);

    const state = () => ctx.window.__wallfacerTestState.commandPalette();
    expect(state().activeIndex).toBe(0);
    ctx.commandPaletteMoveUp();
    expect(state().activeIndex).toBe(1); // wrapped to bottom
  });

  it("handles moveDown/Up when no rows exist", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([{ title: "Tasks", rows: [] }]);

    ctx.commandPaletteMoveDown();
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.activeIndex).toBe(-1);

    ctx.commandPaletteMoveUp();
    expect(state.activeIndex).toBe(-1);
  });

  it("initializes active index when starting from -1 going down", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      {
        title: "Tasks",
        rows: [
          {
            type: "task",
            id: "1",
            title: "A",
            taskObj: { id: "1" },
            execute: () => {},
          },
        ],
      },
    ]);
    // Force activeIndex to -1
    vm.runInContext("_commandPaletteActiveIndex = -1", ctx);
    ctx.commandPaletteMoveDown();
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.activeIndex).toBe(0);
  });

  it("initializes active index when starting from -1 going up", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      {
        title: "Tasks",
        rows: [
          {
            type: "task",
            id: "1",
            title: "A",
            taskObj: { id: "1" },
            execute: () => {},
          },
          {
            type: "task",
            id: "2",
            title: "B",
            taskObj: { id: "2" },
            execute: () => {},
          },
        ],
      },
    ]);
    vm.runInContext("_commandPaletteActiveIndex = -1", ctx);
    ctx.commandPaletteMoveUp();
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.activeIndex).toBe(1); // last item
  });
});

// ---------------------------------------------------------------------------
// executeCommandPaletteActiveRow
// ---------------------------------------------------------------------------

describe("executeCommandPaletteActiveRow", () => {
  it("executes the active row and closes the palette", () => {
    const executeFn = vi.fn();
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      {
        title: "Tasks",
        rows: [
          {
            type: "task",
            id: "1",
            title: "A",
            taskObj: { id: "1" },
            execute: executeFn,
          },
        ],
      },
    ]);
    vm.runInContext("_commandPaletteOpen = true", ctx);
    ctx.executeCommandPaletteActiveRow();
    expect(executeFn).toHaveBeenCalled();
  });

  it("does nothing when no active row", () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([{ title: "Tasks", rows: [] }]);
    // Should not throw
    ctx.executeCommandPaletteActiveRow();
  });
});

// ---------------------------------------------------------------------------
// open/close/toggle
// ---------------------------------------------------------------------------

describe("open/close/toggle", () => {
  it("openCommandPalette sets visibility and focuses input", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext('tasks = [{ id: "1", title: "Test", prompt: "p" }]', ctx);
    ctx.openCommandPalette();
    expect(ctx.isCommandPaletteOpen()).toBe(true);
  });

  it("closeCommandPalette resets state", () => {
    const ctx = setupCommandPaletteContext();
    ctx.openCommandPalette();
    ctx.closeCommandPalette();
    expect(ctx.isCommandPaletteOpen()).toBe(false);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.activeIndex).toBe(-1);
    expect(state.rows.length).toBe(0);
  });

  it("commandPaletteToggle opens when closed", () => {
    const ctx = setupCommandPaletteContext();
    ctx.commandPaletteToggle();
    expect(ctx.isCommandPaletteOpen()).toBe(true);
  });

  it("commandPaletteToggle closes when open", () => {
    const ctx = setupCommandPaletteContext();
    ctx.openCommandPalette();
    ctx.commandPaletteToggle();
    expect(ctx.isCommandPaletteOpen()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// commandPaletteHandleGlobalKey
// ---------------------------------------------------------------------------

describe("commandPaletteHandleGlobalKey", () => {
  it("opens palette on Ctrl+K when not in an input", () => {
    const ctx = setupCommandPaletteContext();
    ctx.document.activeElement = { tagName: "DIV" };
    const event = {
      key: "k",
      ctrlKey: true,
      metaKey: false,
      preventDefault: vi.fn(),
      stopImmediatePropagation: vi.fn(),
    };
    ctx.commandPaletteHandleGlobalKey(event);
    expect(ctx.isCommandPaletteOpen()).toBe(true);
    expect(event.preventDefault).toHaveBeenCalled();
  });

  it("does not open palette on Ctrl+K when in an INPUT", () => {
    const ctx = setupCommandPaletteContext();
    ctx.document.activeElement = { tagName: "INPUT" };
    const event = {
      key: "k",
      ctrlKey: true,
      metaKey: false,
      preventDefault: vi.fn(),
      stopImmediatePropagation: vi.fn(),
    };
    ctx.commandPaletteHandleGlobalKey(event);
    expect(ctx.isCommandPaletteOpen()).toBe(false);
  });

  it("does not open palette on Ctrl+K when in a TEXTAREA", () => {
    const ctx = setupCommandPaletteContext();
    ctx.document.activeElement = { tagName: "TEXTAREA" };
    const event = {
      key: "k",
      ctrlKey: true,
      metaKey: false,
      preventDefault: vi.fn(),
      stopImmediatePropagation: vi.fn(),
    };
    ctx.commandPaletteHandleGlobalKey(event);
    expect(ctx.isCommandPaletteOpen()).toBe(false);
  });

  it("does not open palette on Ctrl+K when in contentEditable", () => {
    const ctx = setupCommandPaletteContext();
    ctx.document.activeElement = { tagName: "DIV", isContentEditable: true };
    const event = {
      key: "k",
      ctrlKey: true,
      metaKey: false,
      preventDefault: vi.fn(),
      stopImmediatePropagation: vi.fn(),
    };
    ctx.commandPaletteHandleGlobalKey(event);
    expect(ctx.isCommandPaletteOpen()).toBe(false);
  });

  it("closes palette on Escape when open", () => {
    const ctx = setupCommandPaletteContext();
    ctx.openCommandPalette();
    const event = {
      key: "Escape",
      preventDefault: vi.fn(),
      stopImmediatePropagation: vi.fn(),
    };
    ctx.commandPaletteHandleGlobalKey(event);
    expect(ctx.isCommandPaletteOpen()).toBe(false);
  });

  it("handles ArrowDown when palette is open", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext('tasks = [{ id: "1", title: "A", prompt: "p" }]', ctx);
    ctx.openCommandPalette();
    const event = {
      key: "ArrowDown",
      preventDefault: vi.fn(),
      stopImmediatePropagation: vi.fn(),
    };
    ctx.commandPaletteHandleGlobalKey(event);
    expect(event.preventDefault).toHaveBeenCalled();
  });

  it("handles ArrowUp when palette is open", () => {
    const ctx = setupCommandPaletteContext();
    vm.runInContext('tasks = [{ id: "1", title: "A", prompt: "p" }]', ctx);
    ctx.openCommandPalette();
    const event = {
      key: "ArrowUp",
      preventDefault: vi.fn(),
      stopImmediatePropagation: vi.fn(),
    };
    ctx.commandPaletteHandleGlobalKey(event);
    expect(event.preventDefault).toHaveBeenCalled();
  });

  it("handles Enter when palette is open", () => {
    const ctx = setupCommandPaletteContext();
    const executeFn = vi.fn();
    ctx._buildTaskListSections([
      {
        title: "Tasks",
        rows: [
          {
            type: "task",
            id: "1",
            title: "A",
            taskObj: { id: "1" },
            execute: executeFn,
          },
        ],
      },
    ]);
    vm.runInContext("_commandPaletteOpen = true", ctx);
    const event = {
      key: "Enter",
      preventDefault: vi.fn(),
      stopImmediatePropagation: vi.fn(),
    };
    ctx.commandPaletteHandleGlobalKey(event);
    expect(event.preventDefault).toHaveBeenCalled();
    expect(executeFn).toHaveBeenCalled();
  });

  it("ignores non-palette keys when palette is closed", () => {
    const ctx = setupCommandPaletteContext();
    const event = {
      key: "Escape",
      preventDefault: vi.fn(),
      stopImmediatePropagation: vi.fn(),
    };
    ctx.commandPaletteHandleGlobalKey(event);
    expect(event.preventDefault).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// _searchRemote — short query guard
// ---------------------------------------------------------------------------

describe("_searchRemote edge cases", () => {
  it("shows empty results for queries shorter than 2 chars after @", async () => {
    const ctx = setupCommandPaletteContext();
    ctx._commandPaletteServerSeq = 1;
    await ctx._searchRemote("@a", 1);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.rows.length).toBe(0);
  });

  it("handles fetch failure gracefully", async () => {
    const fetch = vi.fn(() => Promise.reject(new Error("network")));
    const ctx = setupCommandPaletteContext({ fetch });
    ctx._commandPaletteServerSeq = 1;
    await ctx._searchRemote("@test query", 1);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.rows.length).toBe(0);
  });

  it("ignores stale responses (seq mismatch)", async () => {
    const fetch = vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () =>
          Promise.resolve([
            { id: "r1", title: "Result", status: "done", snippet: "" },
          ]),
      }),
    );
    const ctx = setupCommandPaletteContext({ fetch });
    ctx._commandPaletteServerSeq = 1;
    // Send with seq=1 but bump serverSeq before resolution
    const promise = ctx._searchRemote("@test query", 1);
    ctx._commandPaletteServerSeq = 2;
    await promise;
    // Results should be discarded since seq doesn't match
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.taskRows.length).toBe(0);
  });

  it("renders results when seq matches", async () => {
    const openModal = vi.fn(() => Promise.resolve());
    const fetch = vi.fn(() =>
      Promise.resolve({
        ok: true,
        json: () =>
          Promise.resolve([
            { id: "r1", title: "Found", status: "done", snippet: "result" },
          ]),
      }),
    );
    const ctx = setupCommandPaletteContext({ fetch, openModal });
    ctx._commandPaletteServerSeq = 5;
    await ctx._searchRemote("@found item", 5);
    const state = ctx.window.__wallfacerTestState.commandPalette();
    expect(state.taskRows.length).toBe(1);
    expect(state.taskRows[0].title).toBe("Found");
  });
});

// ---------------------------------------------------------------------------
// _archiveTask — error handling
// ---------------------------------------------------------------------------

describe("_archiveTask error handling", () => {
  it("shows alert on archive failure", async () => {
    const api = vi.fn(() => Promise.reject(new Error("fail")));
    const ctx = setupCommandPaletteContext({ api });
    // Override showAlert after utils.js loaded it, so we bypass DOM access
    ctx.showAlert = vi.fn();
    await ctx._archiveTask("t1");
    expect(ctx.showAlert).toHaveBeenCalled();
  });

  it("does nothing for empty taskId", async () => {
    const api = vi.fn();
    const ctx = setupCommandPaletteContext({ api });
    await ctx._archiveTask("");
    expect(api).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Helper functions
// ---------------------------------------------------------------------------

describe("helper functions", () => {
  it("_toTaskId returns empty for null", () => {
    const ctx = setupCommandPaletteContext();
    expect(ctx._toTaskId(null)).toBe("");
  });

  it("_shortTaskId returns first 8 chars", () => {
    const ctx = setupCommandPaletteContext();
    expect(ctx._shortTaskId({ id: "abcdefgh-1234" })).toBe("abcdefgh");
  });

  it("_getTaskTitle prefers title over prompt", () => {
    const ctx = setupCommandPaletteContext();
    expect(ctx._getTaskTitle({ title: "Title", prompt: "Prompt" })).toBe(
      "Title",
    );
  });

  it("_getTaskTitle falls back to prompt", () => {
    const ctx = setupCommandPaletteContext();
    expect(ctx._getTaskTitle({ title: "", prompt: "Prompt" })).toBe("Prompt");
  });

  it("_hasWorktree returns false for empty paths", () => {
    const ctx = setupCommandPaletteContext();
    expect(ctx._hasWorktree({ worktree_paths: {} })).toBe(false);
  });

  it("_hasWorktree returns true for non-empty paths", () => {
    const ctx = setupCommandPaletteContext();
    expect(ctx._hasWorktree({ worktree_paths: { ws: "/p" } })).toBe(true);
  });
});
