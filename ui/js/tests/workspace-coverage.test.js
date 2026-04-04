/**
 * Additional coverage tests for workspace.js.
 *
 * Targets uncovered branches and functions not tested by workspace.test.js:
 * - hideWorkspacePicker (required vs. not required)
 * - renderWorkspaceSelectionSummary
 * - workspaceGroupsEqual (various edge cases)
 * - _shortenPath
 * - addWorkspaceSelection / removeWorkspaceSelection / clearWorkspaceSelection
 * - addCurrentWorkspaceFolder
 * - selectWorkspaceBrowserEntry
 * - openWorkspaceBrowserEntry / openWorkspaceBrowserEntry2
 * - workspaceBrowserPathKeydown
 * - workspaceBrowserListKeydown (ArrowDown/ArrowUp)
 * - getVisibleWorkspaceBrowserEntries
 * - toggleWorkspaceBrowserHidden
 * - workspaceBrowserIncludeHidden
 * - renderWorkspaceBrowser (breadcrumb, parent entry, empty state)
 * - editWorkspaceGroup / deleteWorkspaceGroup / renameWorkspaceGroup
 * - setWorkspaceGroupSwitching
 * - workspaceSwitchSpinnerHtml
 * - hideHeaderWorkspaceGroups / toggleHeaderWorkspaceGroups (no-ops)
 * - populateSandboxSelects (option building with usable/unusable states)
 * - updateWorkspaceGroupBadges
 * - saveWorkspaceGroups
 * - applyWorkspaceSelection (error path)
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeInput(initial = false) {
  return { checked: initial, value: "" };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Date,
    Math,
    setTimeout: overrides.setTimeout || (() => {}),
    clearTimeout: () => {},
    Set,
    Array,
    JSON,
    String,
    Promise,
    api: overrides.api || vi.fn().mockResolvedValue({}),
    stopTasksStream: vi.fn(),
    stopGitStream: vi.fn(),
    startGitStream: vi.fn(),
    startTasksStream: vi.fn(),
    resetBoardState: vi.fn(),
    restartActiveStreams: vi.fn(),
    showAlert: vi.fn(),
    showPrompt: overrides.showPrompt || vi.fn().mockResolvedValue(null),
    scheduleRender: vi.fn(),
    updateAutomationActiveCount: vi.fn(),
    populateSandboxSelects: vi.fn(),
    updateIdeationConfig: vi.fn(),
    setBrainstormCategories: vi.fn(),
    updateWatcherHealth: vi.fn(),
    reloadExplorerTree: vi.fn(),
    applyTerminalVisibility: vi.fn(),
    initTerminal: vi.fn(),
    escapeHtml: (s) =>
      String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
    location: { hash: "" },
    localStorage: { getItem: vi.fn(), setItem: vi.fn() },
    Routes: overrides.Routes || {
      config: { get: () => "/api/config", update: () => "/api/config" },
      workspaces: {
        browse: () => "/api/workspaces/browse",
        update: () => "/api/workspaces",
        mkdir: () => "/api/workspaces/mkdir",
        rename: () => "/api/workspaces/rename",
      },
    },
    requestAnimationFrame: (fn) => fn(),
    ResizeObserver: class {
      observe() {}
      disconnect() {}
    },
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelectorAll: (selector) => {
        if (selector.includes("[data-sandbox-select]"))
          return elements.get("sandbox-selects") || [];
        return [];
      },
      querySelector: () => null,
      addEventListener: () => {},
      removeEventListener: () => {},
      documentElement: { setAttribute: () => {} },
      readyState: "complete",
      createElement: (tag) => {
        const el = {
          tagName: tag,
          type: "",
          value: "",
          textContent: "",
          disabled: false,
          title: "",
          className: "",
          innerHTML: "",
          style: { cssText: "" },
          id: "",
          children: [],
          appendChild: vi.fn(),
          focus: vi.fn(),
          select: vi.fn(),
          addEventListener: vi.fn(),
          remove: vi.fn(),
        };
        return el;
      },
      body: {
        appendChild: vi.fn(),
      },
    },
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  loadLibDeps(filename, ctx);
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

function setupCtx(overrides = {}) {
  const ctx = makeContext(overrides);
  loadScript(ctx, "state.js");
  loadScript(ctx, "utils.js");
  loadScript(ctx, "workspace.js");
  return ctx;
}

// ---------------------------------------------------------------------------
// hideWorkspacePicker
// ---------------------------------------------------------------------------
describe("hideWorkspacePicker", () => {
  it("does nothing when workspacePickerRequired is true", () => {
    const modal = {
      classList: {
        _set: new Set(),
        add(c) {
          this._set.add(c);
        },
        remove(c) {
          this._set.delete(c);
        },
      },
    };
    const ctx = setupCtx({
      elements: [["workspace-picker", modal]],
    });
    vm.runInContext("workspacePickerRequired = true", ctx);
    ctx.hideWorkspacePicker();
    // Modal should not have "hidden" added
    expect(modal.classList._set.has("hidden")).toBe(false);
  });

  it("hides modal when not required", () => {
    const modal = {
      classList: {
        _set: new Set(["flex"]),
        add(c) {
          this._set.add(c);
        },
        remove(c) {
          this._set.delete(c);
        },
      },
    };
    const ctx = setupCtx({
      elements: [["workspace-picker", modal]],
    });
    vm.runInContext("workspacePickerRequired = false", ctx);
    ctx.hideWorkspacePicker();
    expect(modal.classList._set.has("hidden")).toBe(true);
    expect(modal.classList._set.has("flex")).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// workspaceGroupsEqual
// ---------------------------------------------------------------------------
describe("workspaceGroupsEqual", () => {
  it("returns false for non-arrays", () => {
    const ctx = setupCtx();
    expect(ctx.workspaceGroupsEqual(null, [])).toBe(false);
    expect(ctx.workspaceGroupsEqual([], null)).toBe(false);
    expect(ctx.workspaceGroupsEqual("a", "a")).toBe(false);
  });

  it("returns false for different lengths", () => {
    const ctx = setupCtx();
    expect(ctx.workspaceGroupsEqual(["a"], ["a", "b"])).toBe(false);
  });

  it("returns false for different elements", () => {
    const ctx = setupCtx();
    expect(ctx.workspaceGroupsEqual(["a", "b"], ["a", "c"])).toBe(false);
  });

  it("returns true for identical arrays", () => {
    const ctx = setupCtx();
    expect(ctx.workspaceGroupsEqual(["a", "b"], ["a", "b"])).toBe(true);
  });

  it("returns true for empty arrays", () => {
    const ctx = setupCtx();
    expect(ctx.workspaceGroupsEqual([], [])).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// _shortenPath
// ---------------------------------------------------------------------------
describe("_shortenPath", () => {
  it("shortens /Users/x/... to ~/...", () => {
    const ctx = setupCtx();
    expect(ctx._shortenPath("/Users/john/dev/repo")).toBe("~/dev/repo");
  });

  it("shortens /home/x/... to ~/...", () => {
    const ctx = setupCtx();
    expect(ctx._shortenPath("/home/john/dev/repo")).toBe("~/dev/repo");
  });

  it("returns path unchanged when no home pattern matches", () => {
    const ctx = setupCtx();
    expect(ctx._shortenPath("/var/data/repo")).toBe("/var/data/repo");
  });
});

// ---------------------------------------------------------------------------
// renderWorkspaceSelectionSummary
// ---------------------------------------------------------------------------
describe("renderWorkspaceSelectionSummary", () => {
  it("shows no-workspaces message when empty", () => {
    const el = { innerHTML: "" };
    const ctx = setupCtx({ elements: [["settings-workspace-list", el]] });
    vm.runInContext("activeWorkspaces = []", ctx);
    ctx.renderWorkspaceSelectionSummary();
    expect(el.innerHTML).toContain("No workspaces configured");
  });

  it("renders active workspaces with monospace styling", () => {
    const el = { innerHTML: "" };
    const ctx = setupCtx({ elements: [["settings-workspace-list", el]] });
    vm.runInContext('activeWorkspaces = ["/Users/test/repo"]', ctx);
    ctx.renderWorkspaceSelectionSummary();
    expect(el.innerHTML).toContain("/Users/test/repo");
    expect(el.innerHTML).toContain("monospace");
  });
});

// ---------------------------------------------------------------------------
// addWorkspaceSelection / removeWorkspaceSelection / clearWorkspaceSelection
// ---------------------------------------------------------------------------
describe("addWorkspaceSelection", () => {
  it("adds path to draft and avoids duplicates", () => {
    const ctx = setupCtx({
      elements: [["workspace-selection-list", { innerHTML: "" }]],
    });
    vm.runInContext("workspaceSelectionDraft = []", ctx);
    ctx.addWorkspaceSelection("/a");
    expect(vm.runInContext("workspaceSelectionDraft.slice()", ctx)).toEqual([
      "/a",
    ]);
    ctx.addWorkspaceSelection("/a"); // duplicate
    expect(vm.runInContext("workspaceSelectionDraft.slice()", ctx)).toEqual([
      "/a",
    ]);
  });

  it("does nothing for empty path", () => {
    const ctx = setupCtx();
    vm.runInContext("workspaceSelectionDraft = []", ctx);
    ctx.addWorkspaceSelection("");
    expect(vm.runInContext("workspaceSelectionDraft.length", ctx)).toBe(0);
  });
});

describe("removeWorkspaceSelection", () => {
  it("removes the specified path from the draft", () => {
    const ctx = setupCtx({
      elements: [["workspace-selection-list", { innerHTML: "" }]],
    });
    vm.runInContext('workspaceSelectionDraft = ["/a", "/b"]', ctx);
    ctx.removeWorkspaceSelection("/a");
    expect(vm.runInContext("workspaceSelectionDraft.slice()", ctx)).toEqual([
      "/b",
    ]);
  });
});

describe("clearWorkspaceSelection", () => {
  it("empties the selection draft", () => {
    const ctx = setupCtx({
      elements: [["workspace-selection-list", { innerHTML: "" }]],
    });
    vm.runInContext('workspaceSelectionDraft = ["/a", "/b"]', ctx);
    ctx.clearWorkspaceSelection();
    expect(vm.runInContext("workspaceSelectionDraft.length", ctx)).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// addCurrentWorkspaceFolder
// ---------------------------------------------------------------------------
describe("addCurrentWorkspaceFolder", () => {
  it("adds the current browser path to the selection draft", () => {
    const ctx = setupCtx({
      elements: [["workspace-selection-list", { innerHTML: "" }]],
    });
    vm.runInContext('workspaceBrowserPath = "/Users/test/dev"', ctx);
    vm.runInContext("workspaceSelectionDraft = []", ctx);
    ctx.addCurrentWorkspaceFolder();
    expect(vm.runInContext("workspaceSelectionDraft.slice()", ctx)).toEqual([
      "/Users/test/dev",
    ]);
  });

  it("does nothing when workspaceBrowserPath is empty", () => {
    const ctx = setupCtx();
    vm.runInContext('workspaceBrowserPath = ""', ctx);
    vm.runInContext("workspaceSelectionDraft = []", ctx);
    ctx.addCurrentWorkspaceFolder();
    expect(vm.runInContext("workspaceSelectionDraft.length", ctx)).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// workspaceBrowserListKeydown — ArrowDown / ArrowUp
// ---------------------------------------------------------------------------
describe("workspaceBrowserListKeydown — arrow keys", () => {
  it("ArrowDown moves focus index forward", () => {
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { innerHTML: "" }],
        ["workspace-browser-list", {}],
      ],
    });
    vm.runInContext(
      `workspaceBrowserEntries = [
        { name: "a", path: "/a" },
        { name: "b", path: "/b" },
        { name: "c", path: "/c" }
      ];
      workspaceBrowserFocusIndex = 0;`,
      ctx,
    );
    ctx.workspaceBrowserListKeydown({
      key: "ArrowDown",
      preventDefault: vi.fn(),
    });
    expect(vm.runInContext("workspaceBrowserFocusIndex", ctx)).toBe(1);
  });

  it("ArrowUp moves focus index backward", () => {
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { innerHTML: "" }],
        ["workspace-browser-list", {}],
      ],
    });
    vm.runInContext(
      `workspaceBrowserEntries = [
        { name: "a", path: "/a" },
        { name: "b", path: "/b" }
      ];
      workspaceBrowserFocusIndex = 1;`,
      ctx,
    );
    ctx.workspaceBrowserListKeydown({
      key: "ArrowUp",
      preventDefault: vi.fn(),
    });
    expect(vm.runInContext("workspaceBrowserFocusIndex", ctx)).toBe(0);
  });

  it("ArrowDown clamps at the end", () => {
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { innerHTML: "" }],
        ["workspace-browser-list", {}],
      ],
    });
    vm.runInContext(
      `workspaceBrowserEntries = [{ name: "a", path: "/a" }];
      workspaceBrowserFocusIndex = 0;`,
      ctx,
    );
    ctx.workspaceBrowserListKeydown({
      key: "ArrowDown",
      preventDefault: vi.fn(),
    });
    expect(vm.runInContext("workspaceBrowserFocusIndex", ctx)).toBe(0);
  });

  it("ArrowUp clamps at zero", () => {
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { innerHTML: "" }],
        ["workspace-browser-list", {}],
      ],
    });
    vm.runInContext(
      `workspaceBrowserEntries = [{ name: "a", path: "/a" }];
      workspaceBrowserFocusIndex = 0;`,
      ctx,
    );
    ctx.workspaceBrowserListKeydown({
      key: "ArrowUp",
      preventDefault: vi.fn(),
    });
    expect(vm.runInContext("workspaceBrowserFocusIndex", ctx)).toBe(0);
  });

  it("Enter with meta key opens entry", () => {
    const apiFn = vi.fn().mockResolvedValue({ path: "/a", entries: [] });
    const ctx = setupCtx({
      api: apiFn,
      elements: [
        ["workspace-browser-path", { value: "/a" }],
        ["workspace-browser-status", { textContent: "" }],
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { innerHTML: "" }],
        ["workspace-browser-list", {}],
        ["workspace-browser-include-hidden", { checked: false }],
      ],
    });
    vm.runInContext(
      `workspaceBrowserEntries = [{ name: "a", path: "/a" }];
      workspaceBrowserFocusIndex = 0;`,
      ctx,
    );
    ctx.workspaceBrowserListKeydown({
      key: "Enter",
      preventDefault: vi.fn(),
      metaKey: true,
      ctrlKey: false,
    });
    // browseWorkspaces should have been called (api invoked)
    expect(apiFn).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// selectWorkspaceBrowserEntry
// ---------------------------------------------------------------------------
describe("selectWorkspaceBrowserEntry", () => {
  it("updates focus index", () => {
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { innerHTML: "" }],
        ["workspace-browser-list", {}],
      ],
    });
    vm.runInContext(
      `workspaceBrowserEntries = [{ name: "a", path: "/a" }, { name: "b", path: "/b" }];
      workspaceBrowserFocusIndex = 0;`,
      ctx,
    );
    ctx.selectWorkspaceBrowserEntry(1);
    expect(vm.runInContext("workspaceBrowserFocusIndex", ctx)).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// workspaceBrowserPathKeydown
// ---------------------------------------------------------------------------
describe("workspaceBrowserPathKeydown", () => {
  it("triggers browse on Enter", () => {
    const apiFn = vi.fn().mockResolvedValue({ path: "/test", entries: [] });
    const ctx = setupCtx({
      api: apiFn,
      elements: [
        ["workspace-browser-path", { value: "/test" }],
        ["workspace-browser-status", { textContent: "" }],
        ["workspace-browser-list", { innerHTML: "" }],
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { textContent: "" }],
        ["workspace-browser-include-hidden", { checked: false }],
      ],
    });
    ctx.workspaceBrowserPathKeydown({
      key: "Enter",
      preventDefault: vi.fn(),
    });
    expect(apiFn).toHaveBeenCalled();
  });

  it("does nothing for non-Enter keys", () => {
    const apiFn = vi.fn();
    const ctx = setupCtx({ api: apiFn });
    ctx.workspaceBrowserPathKeydown({
      key: "a",
      preventDefault: vi.fn(),
    });
    expect(apiFn).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// workspaceBrowserIncludeHidden
// ---------------------------------------------------------------------------
describe("workspaceBrowserIncludeHidden", () => {
  it("returns true when toggle is checked", () => {
    const ctx = setupCtx({
      elements: [["workspace-browser-include-hidden", { checked: true }]],
    });
    expect(ctx.workspaceBrowserIncludeHidden()).toBe(true);
  });

  it("returns false when toggle is not checked", () => {
    const ctx = setupCtx({
      elements: [["workspace-browser-include-hidden", { checked: false }]],
    });
    expect(ctx.workspaceBrowserIncludeHidden()).toBe(false);
  });

  it("returns false when element is missing", () => {
    const ctx = setupCtx();
    expect(ctx.workspaceBrowserIncludeHidden()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// toggleWorkspaceBrowserHidden
// ---------------------------------------------------------------------------
describe("toggleWorkspaceBrowserHidden", () => {
  it("calls browseWorkspaces with current path", () => {
    const apiFn = vi.fn().mockResolvedValue({ path: "/test", entries: [] });
    const ctx = setupCtx({
      api: apiFn,
      elements: [
        ["workspace-browser-path", { value: "/test" }],
        ["workspace-browser-status", { textContent: "" }],
        ["workspace-browser-list", { innerHTML: "" }],
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { textContent: "" }],
        ["workspace-browser-include-hidden", { checked: false }],
      ],
    });
    vm.runInContext('workspaceBrowserPath = "/test"', ctx);
    ctx.toggleWorkspaceBrowserHidden();
    expect(apiFn).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// getVisibleWorkspaceBrowserEntries
// ---------------------------------------------------------------------------
describe("getVisibleWorkspaceBrowserEntries", () => {
  it("returns all entries when no filter", () => {
    const ctx = setupCtx();
    vm.runInContext(
      `workspaceBrowserEntries = [
        { name: "a", path: "/a" },
        { name: "b", path: "/b" }
      ];
      workspaceBrowserFilterQuery = "";`,
      ctx,
    );
    const entries = ctx.getVisibleWorkspaceBrowserEntries();
    expect(entries.length).toBe(2);
  });

  it("filters by name", () => {
    const ctx = setupCtx();
    vm.runInContext(
      `workspaceBrowserEntries = [
        { name: "alpha", path: "/alpha" },
        { name: "beta", path: "/beta" }
      ];
      workspaceBrowserFilterQuery = "alp";`,
      ctx,
    );
    const entries = ctx.getVisibleWorkspaceBrowserEntries();
    expect(entries.length).toBe(1);
    expect(entries[0].name).toBe("alpha");
  });

  it("filters by path", () => {
    const ctx = setupCtx();
    vm.runInContext(
      `workspaceBrowserEntries = [
        { name: "a", path: "/Users/test/alpha" },
        { name: "b", path: "/Users/test/beta" }
      ];
      workspaceBrowserFilterQuery = "beta";`,
      ctx,
    );
    const entries = ctx.getVisibleWorkspaceBrowserEntries();
    expect(entries.length).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// renderWorkspaceBrowser
// ---------------------------------------------------------------------------
describe("renderWorkspaceBrowser", () => {
  it("renders breadcrumb with clickable segments", () => {
    const crumb = { innerHTML: "" };
    const list = {};
    const entriesEl = { innerHTML: "" };
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-breadcrumb", crumb],
        ["workspace-browser-list", list],
        ["workspace-browser-entries", entriesEl],
        ["workspace-selection-list", { innerHTML: "" }],
      ],
    });
    vm.runInContext(
      `workspaceBrowserPath = "/Users/test/dev";
      workspaceBrowserEntries = [];
      workspaceBrowserFocusIndex = -1;
      workspaceSelectionDraft = [];`,
      ctx,
    );
    ctx.renderWorkspaceBrowser();
    expect(crumb.innerHTML).toContain("Users");
    expect(crumb.innerHTML).toContain("test");
    expect(crumb.innerHTML).toContain("dev");
    expect(crumb.innerHTML).toContain("browseWorkspaces");
  });

  it("renders .. parent entry when path is not root", () => {
    const crumb = { innerHTML: "" };
    const list = {};
    const entriesEl = { innerHTML: "" };
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-breadcrumb", crumb],
        ["workspace-browser-list", list],
        ["workspace-browser-entries", entriesEl],
        ["workspace-selection-list", { innerHTML: "" }],
      ],
    });
    vm.runInContext(
      `workspaceBrowserPath = "/Users/test/dev";
      workspaceBrowserEntries = [{ name: "repo", path: "/Users/test/dev/repo", is_git_repo: true }];
      workspaceBrowserFocusIndex = 0;
      workspaceSelectionDraft = [];`,
      ctx,
    );
    ctx.renderWorkspaceBrowser();
    expect(entriesEl.innerHTML).toContain("..");
  });

  it("shows 'Empty.' when no entries and no filter", () => {
    const crumb = { innerHTML: "" };
    const list = {};
    const entriesEl = { innerHTML: "" };
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-breadcrumb", crumb],
        ["workspace-browser-list", list],
        ["workspace-browser-entries", entriesEl],
      ],
    });
    vm.runInContext(
      `workspaceBrowserPath = "/";
      workspaceBrowserEntries = [];
      workspaceBrowserFocusIndex = -1;
      workspaceBrowserFilterQuery = "";`,
      ctx,
    );
    ctx.renderWorkspaceBrowser();
    expect(entriesEl.innerHTML).toContain("Empty.");
  });

  it("shows 'No matches.' when filter yields nothing", () => {
    const crumb = { innerHTML: "" };
    const list = {};
    const entriesEl = { innerHTML: "" };
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-breadcrumb", crumb],
        ["workspace-browser-list", list],
        ["workspace-browser-entries", entriesEl],
      ],
    });
    vm.runInContext(
      `workspaceBrowserPath = "/";
      workspaceBrowserEntries = [{ name: "alpha", path: "/alpha" }];
      workspaceBrowserFocusIndex = -1;
      workspaceBrowserFilterQuery = "xyz";`,
      ctx,
    );
    ctx.renderWorkspaceBrowser();
    expect(entriesEl.innerHTML).toContain("No matches.");
  });

  it("marks already-selected entries with 'added'", () => {
    const crumb = { innerHTML: "" };
    const list = {};
    const entriesEl = { innerHTML: "" };
    const ctx = setupCtx({
      elements: [
        ["workspace-browser-breadcrumb", crumb],
        ["workspace-browser-list", list],
        ["workspace-browser-entries", entriesEl],
        ["workspace-selection-list", { innerHTML: "" }],
      ],
    });
    vm.runInContext(
      `workspaceBrowserPath = "/dev";
      workspaceBrowserEntries = [{ name: "repo", path: "/dev/repo", is_git_repo: false }];
      workspaceBrowserFocusIndex = 0;
      workspaceSelectionDraft = ["/dev/repo"];`,
      ctx,
    );
    ctx.renderWorkspaceBrowser();
    expect(entriesEl.innerHTML).toContain("added");
  });
});

// ---------------------------------------------------------------------------
// setWorkspaceGroupSwitching
// ---------------------------------------------------------------------------
describe("setWorkspaceGroupSwitching", () => {
  it("sets switching state and renders", () => {
    const groupsEl = { innerHTML: "" };
    const tabsEl = { innerHTML: "" };
    const ctx = setupCtx({
      elements: [
        ["settings-workspace-groups", groupsEl],
        ["workspace-group-tabs", tabsEl],
      ],
    });
    vm.runInContext("workspaceGroups = [{ workspaces: ['/a'] }]", ctx);
    vm.runInContext("activeWorkspaces = ['/a']", ctx);
    ctx.setWorkspaceGroupSwitching(0, true);
    expect(vm.runInContext("workspaceGroupSwitching", ctx)).toBe(true);
    expect(vm.runInContext("workspaceGroupSwitchingIndex", ctx)).toBe(0);
  });

  it("clears switching state", () => {
    const groupsEl = { innerHTML: "" };
    const tabsEl = { innerHTML: "" };
    const ctx = setupCtx({
      elements: [
        ["settings-workspace-groups", groupsEl],
        ["workspace-group-tabs", tabsEl],
      ],
    });
    vm.runInContext("workspaceGroups = []", ctx);
    ctx.setWorkspaceGroupSwitching(-1, false);
    expect(vm.runInContext("workspaceGroupSwitching", ctx)).toBe(false);
    expect(vm.runInContext("workspaceGroupSwitchingIndex", ctx)).toBe(-1);
  });
});

// ---------------------------------------------------------------------------
// workspaceSwitchSpinnerHtml
// ---------------------------------------------------------------------------
describe("workspaceSwitchSpinnerHtml", () => {
  it("returns spinner HTML", () => {
    const ctx = setupCtx();
    const html = ctx.workspaceSwitchSpinnerHtml();
    expect(html).toContain("spinner");
    expect(html).toContain("span");
  });
});

// ---------------------------------------------------------------------------
// hideHeaderWorkspaceGroups / toggleHeaderWorkspaceGroups (no-ops)
// ---------------------------------------------------------------------------
describe("no-op functions", () => {
  it("hideHeaderWorkspaceGroups does not throw", () => {
    const ctx = setupCtx();
    expect(() => ctx.hideHeaderWorkspaceGroups()).not.toThrow();
  });
  it("toggleHeaderWorkspaceGroups does not throw", () => {
    const ctx = setupCtx();
    expect(() => ctx.toggleHeaderWorkspaceGroups()).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// editWorkspaceGroup
// ---------------------------------------------------------------------------
describe("editWorkspaceGroup", () => {
  it("populates draft from group and opens picker", () => {
    const modal = {
      classList: { remove: vi.fn(), add: vi.fn() },
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    };
    const ctx = setupCtx({
      elements: [
        ["workspace-picker", modal],
        ["workspace-picker-close", { style: {} }],
        ["workspace-browser-filter", { value: "" }],
        ["workspace-selection-list", { innerHTML: "" }],
        ["workspace-browser-path", { value: "" }],
        ["workspace-browser-status", { textContent: "" }],
        ["workspace-browser-list", { innerHTML: "" }],
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { textContent: "" }],
        ["workspace-browser-include-hidden", { checked: false }],
      ],
    });
    vm.runInContext('workspaceGroups = [{ workspaces: ["/a", "/b"] }]', ctx);
    ctx.editWorkspaceGroup(0);
    expect(vm.runInContext("workspaceSelectionDraft.slice()", ctx)).toEqual([
      "/a",
      "/b",
    ]);
  });

  it("does nothing for invalid index", () => {
    const ctx = setupCtx();
    vm.runInContext("workspaceGroups = []", ctx);
    expect(() => ctx.editWorkspaceGroup(5)).not.toThrow();
  });
});

// ---------------------------------------------------------------------------
// deleteWorkspaceGroup
// ---------------------------------------------------------------------------
describe("deleteWorkspaceGroup", () => {
  it("removes a group and saves", async () => {
    const apiFn = vi.fn().mockResolvedValue({});
    const groupsEl = { innerHTML: "" };
    const tabsEl = { innerHTML: "" };
    const ctx = setupCtx({
      api: apiFn,
      elements: [
        ["settings-workspace-groups", groupsEl],
        ["workspace-group-tabs", tabsEl],
      ],
    });
    vm.runInContext(
      'workspaceGroups = [{ workspaces: ["/a"] }, { workspaces: ["/b"] }]',
      ctx,
    );
    vm.runInContext("activeWorkspaces = []", ctx);
    await ctx.deleteWorkspaceGroup(0);
    expect(vm.runInContext("workspaceGroups.length", ctx)).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// renameWorkspaceGroup
// ---------------------------------------------------------------------------
describe("renameWorkspaceGroup", () => {
  function makeRenameCtx(apiFn, promptResult) {
    const groupsEl = { innerHTML: "" };
    const tabsEl = { innerHTML: "" };
    const promptInput = { value: "", focus: vi.fn(), select: vi.fn() };
    const promptModal = {
      classList: {
        _set: new Set(["hidden"]),
        add(c) {
          this._set.add(c);
        },
        remove(c) {
          this._set.delete(c);
        },
      },
    };
    const ctx = setupCtx({
      api: apiFn,
      elements: [
        ["settings-workspace-groups", groupsEl],
        ["workspace-group-tabs", tabsEl],
        ["prompt-message", { textContent: "" }],
        ["prompt-input", promptInput],
        ["prompt-modal", promptModal],
        ["prompt-confirm", { onclick: null }],
        ["prompt-cancel", { onclick: null }],
      ],
    });
    // Override showPrompt after loading to control the result
    ctx.showPrompt = vi.fn().mockResolvedValue(promptResult);
    return ctx;
  }

  it("renames a group when user enters a new name", async () => {
    const apiFn = vi.fn().mockResolvedValue({});
    const ctx = makeRenameCtx(apiFn, "New Name");
    vm.runInContext(
      'workspaceGroups = [{ name: "Old", workspaces: ["/a"] }]',
      ctx,
    );
    vm.runInContext("activeWorkspaces = []", ctx);
    await ctx.renameWorkspaceGroup(0);
    expect(vm.runInContext("workspaceGroups[0].name", ctx)).toBe("New Name");
  });

  it("does nothing when user cancels", async () => {
    const apiFn = vi.fn();
    const ctx = makeRenameCtx(apiFn, null);
    vm.runInContext(
      'workspaceGroups = [{ name: "Old", workspaces: ["/a"] }]',
      ctx,
    );
    await ctx.renameWorkspaceGroup(0);
    expect(apiFn).not.toHaveBeenCalled();
    expect(vm.runInContext("workspaceGroups[0].name", ctx)).toBe("Old");
  });
});

// ---------------------------------------------------------------------------
// populateSandboxSelects (overriding the vm-internal version)
// ---------------------------------------------------------------------------
describe("populateSandboxSelects", () => {
  it("populates sandbox selects with available options", () => {
    const selectEl = {
      value: "",
      innerHTML: "",
      dataset: {
        sandboxSelect: "true",
        defaultText: "Default",
        defaultOption: "true",
      },
      selectedIndex: 0,
      id: "env-sandbox-implementation",
      _options: [],
      appendChild(opt) {
        this._options.push(opt);
      },
    };
    const ctx = makeContext({
      elements: [["sandbox-selects", [selectEl]]],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext(
      `availableSandboxes = ["claude", "codex"];
      defaultSandbox = "claude";
      sandboxUsable = { claude: true, codex: false };
      sandboxReasons = { codex: "Missing token" };`,
      ctx,
    );
    ctx.populateSandboxSelects();
    // Should have created option elements
    expect(selectEl._options.length).toBe(2);
    // The codex option should be disabled
    const codexOpt = selectEl._options.find((o) => o.value === "codex");
    expect(codexOpt.disabled).toBe(true);
    expect(codexOpt.title).toBe("Missing token");
  });
});

// ---------------------------------------------------------------------------
// browseWorkspaces — error path
// ---------------------------------------------------------------------------
describe("browseWorkspaces — error path", () => {
  it("shows error message and clears entries on failure", async () => {
    const apiFn = vi.fn().mockRejectedValue(new Error("Network error"));
    const statusEl = { textContent: "" };
    const ctx = setupCtx({
      api: apiFn,
      elements: [
        ["workspace-browser-path", { value: "/test" }],
        ["workspace-browser-status", statusEl],
        ["workspace-browser-list", { innerHTML: "" }],
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { textContent: "" }],
        ["workspace-browser-include-hidden", { checked: false }],
      ],
    });
    await ctx.browseWorkspaces("/test");
    expect(statusEl.textContent).toBe("Network error");
    expect(vm.runInContext("workspaceBrowserEntries.length", ctx)).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// updateWorkspaceGroupBadges
// ---------------------------------------------------------------------------
describe("updateWorkspaceGroupBadges", () => {
  it("updates badge innerHTML for matching groups", () => {
    const badgeEl = {
      getAttribute: (attr) => (attr === "data-wg-key" ? "abc" : null),
      innerHTML: "",
    };
    const tabsEl = {
      innerHTML: "",
      querySelectorAll: (sel) => {
        if (sel === ".wg-badge") return [badgeEl];
        return [];
      },
    };
    const ctx = setupCtx({
      elements: [["workspace-group-tabs", tabsEl]],
    });
    vm.runInContext(
      `workspaceGroups = [{ key: "abc", workspaces: ["/a"] }];
      activeWorkspaces = ["/a"];
      tasks = [{ status: "in_progress" }];`,
      ctx,
    );
    ctx.updateWorkspaceGroupBadges();
    expect(badgeEl.innerHTML).toContain("badge-in_progress");
  });
});
