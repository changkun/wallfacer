/**
 * Tests for workspace.js — server-config hydration, workspace-browser UI,
 * and workspace-group persistence.
 *
 * Each test loads only the minimal set of scripts it needs so failures are
 * localised to workspace.js rather than the full api.js bundle.
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
// Shared test infrastructure
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
    // workspace.js calls api(), stopTasksStream(), etc. — provide stubs.
    api: overrides.api || vi.fn().mockResolvedValue({}),
    stopTasksStream: vi.fn(),
    stopGitStream: vi.fn(),
    startGitStream: vi.fn(),
    startTasksStream: vi.fn(),
    resetBoardState: vi.fn(),
    restartActiveStreams: vi.fn(),
    showAlert: vi.fn(),
    scheduleRender: vi.fn(),
    updateAutomationActiveCount: vi.fn(),
    populateSandboxSelects: vi.fn(),
    updateIdeationConfig: vi.fn(),
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
      createElement: () => ({
        value: "",
        textContent: "",
        disabled: false,
        title: "",
        appendChild: () => {},
      }),
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

// ---------------------------------------------------------------------------
// sandbox helpers
// ---------------------------------------------------------------------------

describe("sandboxDisplayName", () => {
  it("formats sandbox labels consistently", () => {
    const ctx = makeContext();
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");
    expect(ctx.sandboxDisplayName("")).toBe("Default");
    expect(ctx.sandboxDisplayName("claude")).toBe("Claude");
    expect(ctx.sandboxDisplayName("codex")).toBe("Codex");
    expect(ctx.sandboxDisplayName("custom")).toBe("Custom");
  });
});

describe("collectSandboxByActivity / applySandboxByActivity", () => {
  it("collects non-empty sandbox values by activity key", () => {
    const ctx = makeContext({
      elements: [
        ["env-sandbox-implementation", { value: "claude" }],
        ["env-sandbox-testing", { value: "codex" }],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");
    expect(ctx.collectSandboxByActivity("env-sandbox-")).toEqual({
      implementation: "claude",
      testing: "codex",
    });
  });

  it("applies sandbox values to the matching elements", () => {
    const impl = { value: "" };
    const testing = { value: "codex" };
    const ctx = makeContext({
      elements: [
        ["env-sandbox-implementation", impl],
        ["env-sandbox-testing", testing],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");
    ctx.applySandboxByActivity("env-sandbox-", {
      implementation: "custom",
      oversight: "codex",
    });
    expect(impl.value).toBe("custom");
    expect(testing.value).toBe(""); // not in the values map → cleared
  });
});

// ---------------------------------------------------------------------------
// fetchConfig
// ---------------------------------------------------------------------------

describe("fetchConfig", () => {
  it("hydrates client config state and applies sandbox selectors", async () => {
    const cfg = {
      autopilot: true,
      autotest: true,
      autosubmit: false,
      workspaces: ["/Users/test/repo"],
      workspace_browser_path: "/Users/test/repo",
      workspace_groups: [{ workspaces: ["/Users/test/repo"] }],
      sandboxes: ["claude", "codex"],
      default_sandbox: "claude",
      activity_sandboxes: { implementation: "codex" },
      sandbox_usable: { claude: true },
      sandbox_reasons: { codex: "Missing token" },
    };
    const autopilotToggle = makeInput(false);
    const autotestToggle = makeInput(false);
    const autosubmitToggle = makeInput(false);
    const apiFn = vi.fn().mockResolvedValue(cfg);
    const ctx = makeContext({
      api: apiFn,
      elements: [
        ["autopilot-toggle", autopilotToggle],
        ["autotest-toggle", autotestToggle],
        ["autosubmit-toggle", autosubmitToggle],
        ["settings-workspace-groups", { innerHTML: "" }],
        ["settings-workspace-list", { innerHTML: "" }],
        [
          "workspace-group-switcher",
          {
            innerHTML: "",
            classList: { add: () => {}, remove: () => {}, toggle: () => {} },
          },
        ],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");
    const populateSpy = vi.spyOn(ctx, "populateSandboxSelects");

    await ctx.fetchConfig();

    expect(autopilotToggle.checked).toBe(true);
    expect(autotestToggle.checked).toBe(true);
    expect(autosubmitToggle.checked).toBe(false);
    expect(populateSpy).toHaveBeenCalled();
    expect(ctx.updateIdeationConfig).toHaveBeenCalledWith(cfg);
    expect(vm.runInContext("autopilot", ctx)).toBe(true);
    expect(vm.runInContext("autotest", ctx)).toBe(true);
    expect(vm.runInContext("autosubmit", ctx)).toBe(false);
    expect(vm.runInContext("workspaceBrowserPath", ctx)).toBe(
      "/Users/test/repo",
    );
    expect(vm.runInContext("workspaceGroups.length", ctx)).toBe(1);
  });

  it("prefers workspace_browser_path from config over an empty picker path", async () => {
    const apiFn = vi.fn().mockResolvedValue({
      workspaces: [],
      workspace_browser_path: "/Users/test/current",
    });
    const ctx = makeContext({ api: apiFn });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    await ctx.fetchConfig();

    expect(vm.runInContext("workspaceBrowserPath", ctx)).toBe(
      "/Users/test/current",
    );
  });

  it("calls stopTasksStream and showWorkspacePicker when no workspaces are configured", async () => {
    const apiFn = vi.fn().mockResolvedValue({ workspaces: [] });
    const modal = {
      classList: {
        _set: new Set(["hidden"]),
        remove(c) {
          this._set.delete(c);
        },
        add(c) {
          this._set.add(c);
        },
      },
    };
    const ctx = makeContext({
      api: apiFn,
      elements: [
        ["workspace-picker", modal],
        ["workspace-browser-filter", { value: "" }],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");
    // browseWorkspaces is called from showWorkspacePicker — stub it out.
    ctx.browseWorkspaces = vi.fn();

    await ctx.fetchConfig();

    expect(ctx.stopTasksStream).toHaveBeenCalled();
    expect(modal.classList._set.has("hidden")).toBe(false); // picker is visible
  });
});

// ---------------------------------------------------------------------------
// showWorkspacePicker
// ---------------------------------------------------------------------------

describe("showWorkspacePicker", () => {
  it("refreshes the workspace browser every time the picker opens", () => {
    const modal = {
      classList: { remove: vi.fn(), add: vi.fn() },
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    };
    const closeBtn = { style: {} };
    const filterInput = { value: "repo" };
    const ctx = makeContext({
      elements: [
        ["workspace-picker", modal],
        ["workspace-picker-close", closeBtn],
        ["workspace-browser-filter", filterInput],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    const browseSpy = vi
      .spyOn(ctx, "browseWorkspaces")
      .mockImplementation(() => {});
    vm.runInContext(
      'workspaceBrowserPath = "/Users/test/dev"; workspaceBrowserFilterQuery = "repo"; activeWorkspaces = []; workspaceSelectionDraft = [];',
      ctx,
    );

    ctx.showWorkspacePicker(true);

    expect(browseSpy).toHaveBeenCalledWith("/Users/test/dev");
    expect(closeBtn.style.display).toBe("none");
    expect(filterInput.value).toBe("");
    expect(vm.runInContext("workspaceBrowserFilterQuery", ctx)).toBe("");
  });
});

// ---------------------------------------------------------------------------
// renderWorkspaceSelectionDraft
// ---------------------------------------------------------------------------

describe("renderWorkspaceSelectionDraft", () => {
  it("renders a safe remove button handler for selected paths", () => {
    const listEl = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["workspace-selection-list", listEl]],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext('workspaceSelectionDraft = ["/Users/test/dev/repo"];', ctx);
    ctx.renderWorkspaceSelectionDraft();

    expect(listEl.innerHTML).toContain(
      'data-workspace-path="/Users/test/dev/repo"',
    );
    expect(listEl.innerHTML).toContain(
      'onclick="removeWorkspaceSelection(this.dataset.workspacePath)"',
    );
  });
});

// ---------------------------------------------------------------------------
// renderWorkspaceGroups
// ---------------------------------------------------------------------------

describe("renderWorkspaceGroups", () => {
  it("renders saved workspace groups in settings", () => {
    const groupsEl = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["settings-workspace-groups", groupsEl]],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext(
      `
      activeWorkspaces = ["/Users/test/repo-a", "/Users/test/repo-b"];
      workspaceGroups = [{ workspaces: ["/Users/test/repo-a", "/Users/test/repo-b"] }];
    `,
      ctx,
    );
    ctx.renderWorkspaceGroups();

    expect(groupsEl.innerHTML).toContain("repo-a + repo-b");
    expect(groupsEl.innerHTML).toContain("Current");
    expect(groupsEl.innerHTML).toContain("Use");
  });
});

// ---------------------------------------------------------------------------
// renderSidebarWorkspaceSwitch — sidebar group-switch label and popover
// (renderHeaderWorkspaceGroupTabs is a shim that delegates here so existing
//  call sites keep working).
// ---------------------------------------------------------------------------

function makeSwitchElements() {
  return {
    nameEl: { textContent: "" },
    dotEl: { textContent: "" },
    switchEl: {
      title: "",
      _attrs: {},
      setAttribute: function (k, v) {
        this._attrs[k] = v;
      },
      getAttribute: function (k) {
        return this._attrs[k];
      },
    },
    popEl: {
      innerHTML: "",
      _attrs: { hidden: "" },
      setAttribute: function (k, v) {
        this._attrs[k] = v;
      },
      removeAttribute: function (k) {
        delete this._attrs[k];
      },
      contains: () => false,
    },
  };
}

function makeSwitchContext() {
  const els = makeSwitchElements();
  const ctx = makeContext({
    elements: [
      ["sidebar-ws-name", els.nameEl],
      ["sidebar-ws-dot", els.dotEl],
      ["sidebar-ws-switch", els.switchEl],
      ["sidebar-ws-popover", els.popEl],
    ],
  });
  return { ctx, els };
}

describe("renderSidebarWorkspaceSwitch", () => {
  it("shows the active group name and first-letter dot", () => {
    const { ctx, els } = makeSwitchContext();
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext(
      `
      activeWorkspaces = ["/Users/test/repo-a", "/Users/test/repo-b"];
      workspaceGroups = [{ workspaces: ["/Users/test/repo-a", "/Users/test/repo-b"] }];
    `,
      ctx,
    );

    ctx.renderHeaderWorkspaceGroupTabs();

    expect(els.nameEl.textContent).toBe("repo-a + repo-b");
    expect(els.dotEl.textContent).toBe("R");
    expect(els.switchEl.title).toBe("repo-a + repo-b");
  });

  it("falls back to the first workspace basename when no group matches", () => {
    const { ctx, els } = makeSwitchContext();
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext(
      `
      activeWorkspaces = ["/Users/test/lone"];
      workspaceGroups = [];
    `,
      ctx,
    );

    ctx.renderHeaderWorkspaceGroupTabs();

    expect(els.nameEl.textContent).toBe("lone");
    expect(els.dotEl.textContent).toBe("L");
  });

  it("popover lists each group with the active entry flagged", () => {
    const { ctx, els } = makeSwitchContext();
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext(
      `
      activeWorkspaces = ["/Users/test/repo-a"];
      workspaceGroups = [
        { workspaces: ["/Users/test/repo-a"] },
        { workspaces: ["/Users/test/repo-b"] }
      ];
    `,
      ctx,
    );

    ctx.toggleWorkspaceGroupPopover({ stopPropagation: () => {} });

    expect(els.popEl.innerHTML).toContain("repo-a");
    expect(els.popEl.innerHTML).toContain("repo-b");
    expect(els.popEl.innerHTML).toContain("sb-ws-popover__item active");
    // Inactive entries get a useWorkspaceGroup click handler.
    expect(els.popEl.innerHTML).toContain("useWorkspaceGroup(1)");
    expect(els.switchEl.getAttribute("aria-expanded")).toBe("true");
  });

  it("popover shows an add-workspace-group entry", () => {
    const { ctx, els } = makeSwitchContext();
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext(
      `
      activeWorkspaces = [];
      workspaceGroups = [];
    `,
      ctx,
    );

    ctx.toggleWorkspaceGroupPopover({ stopPropagation: () => {} });

    expect(els.popEl.innerHTML).toContain("Add workspace group");
    expect(els.popEl.innerHTML).toContain("showWorkspacePicker(false)");
  });

  it("includes auto-collapsed groups in the + menu", () => {
    const tabsEl = { innerHTML: "" };
    const elements = new Map([["workspace-group-tabs", tabsEl]]);
    const ctx = makeContext({
      elements: [["workspace-group-tabs", tabsEl]],
    });
    // Override document methods to support the menu.
    let menuHtml = "";
    ctx.document.body = {
      appendChild: function (el) {
        menuHtml = el.innerHTML || "";
      },
    };
    const origGetById = ctx.document.getElementById;
    ctx.document.getElementById = function (id) {
      if (id === "workspace-group-add-menu") return null;
      return origGetById(id);
    };
    ctx.document.createElement = function () {
      return { id: "", style: { cssText: "" }, innerHTML: "", remove: vi.fn() };
    };
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext(
      `
      activeWorkspaces = ["/Users/test/repo-a"];
      workspaceGroups = [
        { workspaces: ["/Users/test/repo-a"] },
        { workspaces: ["/Users/test/repo-b"] },
        { workspaces: ["/Users/test/repo-c"] }
      ];
      _autoCollapsedGroupIndices = [2];
    `,
      ctx,
    );

    ctx.addWorkspaceGroupTab({
      currentTarget: {
        getBoundingClientRect: () => ({ bottom: 50, left: 10 }),
      },
    });

    // The auto-collapsed group (repo-c) should appear with useWorkspaceGroup action.
    expect(menuHtml).toContain("repo-c");
    expect(menuHtml).toContain("useWorkspaceGroup(2)");
  });
});

// ---------------------------------------------------------------------------
// workspaceGroupLabel — name preference
// ---------------------------------------------------------------------------

describe("workspaceGroupLabel", () => {
  it("prefers group.name over basenames when set", () => {
    const ctx = makeContext();
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    const label = ctx.workspaceGroupLabel({
      name: "My Project",
      workspaces: ["/Users/test/repo-a", "/Users/test/repo-b"],
    });
    expect(label).toBe("My Project");
  });

  it("falls back to basenames when name is empty", () => {
    const ctx = makeContext();
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    const label = ctx.workspaceGroupLabel({
      name: "",
      workspaces: ["/Users/test/repo-a", "/Users/test/repo-b"],
    });
    expect(label).toBe("repo-a + repo-b");
  });

  it("falls back to basenames when name is absent", () => {
    const ctx = makeContext();
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    const label = ctx.workspaceGroupLabel({
      workspaces: ["/Users/test/single-repo"],
    });
    expect(label).toBe("single-repo");
  });
});

// ---------------------------------------------------------------------------
// browseWorkspaces
// ---------------------------------------------------------------------------

describe("browseWorkspaces", () => {
  it("skips the include_hidden param by default", async () => {
    const pathInput = { value: "/Users/test/dev" };
    const statusEl = { textContent: "" };
    const apiFn = vi
      .fn()
      .mockResolvedValue({ path: "/Users/test/dev", entries: [] });
    const ctx = makeContext({
      api: apiFn,
      elements: [
        ["workspace-browser-path", pathInput],
        ["workspace-browser-status", statusEl],
        ["workspace-browser-list", { innerHTML: "" }],
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { textContent: "" }],
        ["workspace-browser-include-hidden", { checked: false }],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    await ctx.browseWorkspaces();

    expect(apiFn).toHaveBeenCalledWith(
      "/api/workspaces/browse?path=%2FUsers%2Ftest%2Fdev",
    );
  });

  it("appends include_hidden=true when the toggle is enabled", async () => {
    const pathInput = { value: "/Users/test/dev" };
    const statusEl = { textContent: "" };
    const apiFn = vi
      .fn()
      .mockResolvedValue({ path: "/Users/test/dev", entries: [] });
    const ctx = makeContext({
      api: apiFn,
      elements: [
        ["workspace-browser-path", pathInput],
        ["workspace-browser-status", statusEl],
        ["workspace-browser-list", { innerHTML: "" }],
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { textContent: "" }],
        ["workspace-browser-include-hidden", { checked: true }],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    await ctx.browseWorkspaces();

    expect(apiFn).toHaveBeenCalledWith(
      "/api/workspaces/browse?path=%2FUsers%2Ftest%2Fdev&include_hidden=true",
    );
  });
});

// ---------------------------------------------------------------------------
// workspace browser filter
// ---------------------------------------------------------------------------

describe("workspace browser filter", () => {
  it("filters the visible folder list client-side", () => {
    const entriesEl = { innerHTML: "" };
    const crumbEl = { textContent: "" };
    const ctx = makeContext({
      elements: [
        ["workspace-browser-list", {}],
        ["workspace-browser-entries", entriesEl],
        ["workspace-browser-breadcrumb", crumbEl],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext(
      `
      workspaceBrowserPath = "/Users/test/dev";
      workspaceBrowserEntries = [
        { name: "alpha-repo", path: "/Users/test/dev/alpha-repo", is_git_repo: true },
        { name: "beta-tools", path: "/Users/test/dev/beta-tools", is_git_repo: false },
        { name: "gamma-app", path: "/Users/test/dev/gamma-app", is_git_repo: true }
      ];
      workspaceBrowserFocusIndex = 0;
    `,
      ctx,
    );

    ctx.setWorkspaceBrowserFilter("app");

    expect(entriesEl.innerHTML).toContain("gamma-app");
    expect(entriesEl.innerHTML).not.toContain("alpha-repo");
    expect(entriesEl.innerHTML).not.toContain("beta-tools");
    expect(vm.runInContext("workspaceBrowserFocusIndex", ctx)).toBe(0);
  });

  it("adds the highlighted folder on Enter", () => {
    const ctx = makeContext();
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    vm.runInContext(
      `
      workspaceBrowserEntries = [
        { name: "alpha-repo", path: "/Users/test/dev/alpha-repo", is_git_repo: true },
        { name: "beta-tools", path: "/Users/test/dev/beta-tools", is_git_repo: false }
      ];
      workspaceBrowserFocusIndex = 1;
      workspaceSelectionDraft = [];
    `,
      ctx,
    );

    ctx.workspaceBrowserListKeydown({
      key: "Enter",
      preventDefault: vi.fn(),
      metaKey: false,
      ctrlKey: false,
    });

    expect(vm.runInContext("workspaceSelectionDraft.slice()", ctx)).toEqual([
      "/Users/test/dev/beta-tools",
    ]);
  });
});

// ---------------------------------------------------------------------------
// workspace-group switching spinner — integration seam
// ---------------------------------------------------------------------------

describe("useWorkspaceGroup — spinner lifecycle", () => {
  it("sets switching=true before applyWorkspaceSelection and false afterwards", async () => {
    const spinnerStates = [];
    const tabsEl = { innerHTML: "" };
    const apiFn = vi.fn().mockResolvedValue({});
    const ctx = makeContext({
      api: apiFn,
      elements: [
        ["workspace-group-tabs", tabsEl],
        ["workspace-apply-status", { textContent: "" }],
        ["settings-workspace-status", { textContent: "" }],
        ["settings-workspace-groups", { innerHTML: "" }],
        ["settings-workspace-list", { innerHTML: "" }],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");

    // Intercept renderHeaderWorkspaceGroupTabs to capture switching state.
    const origRender = ctx.renderHeaderWorkspaceGroupTabs.bind(ctx);
    ctx.renderHeaderWorkspaceGroupTabs = function () {
      spinnerStates.push(vm.runInContext("workspaceGroupSwitching", ctx));
      origRender();
    };

    // Mock fetchConfig so we don't need a full server round-trip.
    ctx.fetchConfig = vi.fn().mockResolvedValue(undefined);

    vm.runInContext(
      `
      workspaceGroups = [{ workspaces: ["/Users/test/repo"] }];
      workspaceSelectionDraft = [];
    `,
      ctx,
    );

    await ctx.useWorkspaceGroup(0);

    // Switching should have been true at some point and false at the end.
    expect(spinnerStates.some(Boolean)).toBe(true);
    expect(vm.runInContext("workspaceGroupSwitching", ctx)).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// createWorkspaceFolder
// ---------------------------------------------------------------------------

describe("createWorkspaceFolder", () => {
  it("calls mkdir API and refreshes the listing", async () => {
    const apiFn = vi
      .fn()
      .mockResolvedValue({ path: "/Users/test/dev/new-folder" });
    const ctx = makeContext({
      api: apiFn,
      Routes: {
        config: { get: () => "/api/config", update: () => "/api/config" },
        workspaces: {
          browse: () => "/api/workspaces/browse",
          update: () => "/api/workspaces",
          mkdir: () => "/api/workspaces/mkdir",
          rename: () => "/api/workspaces/rename",
        },
      },
      elements: [
        ["workspace-browser-path", { value: "/Users/test/dev" }],
        ["workspace-browser-status", { textContent: "" }],
        ["workspace-browser-list", { innerHTML: "" }],
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { textContent: "" }],
        ["workspace-browser-include-hidden", { checked: false }],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");
    // Override showPrompt after loading utils.js (which defines the real one).
    ctx.showPrompt = vi.fn().mockResolvedValue("new-folder");
    vm.runInContext('workspaceBrowserPath = "/Users/test/dev";', ctx);

    await ctx.createWorkspaceFolder();

    // First call is mkdir, second is browse (refresh).
    expect(apiFn).toHaveBeenCalledWith("/api/workspaces/mkdir", {
      method: "POST",
      body: JSON.stringify({ path: "/Users/test/dev", name: "new-folder" }),
    });
  });

  it("does nothing when prompt is cancelled", async () => {
    const apiFn = vi.fn().mockResolvedValue({});
    const ctx = makeContext({
      api: apiFn,
      Routes: {
        config: { get: () => "/api/config", update: () => "/api/config" },
        workspaces: {
          browse: () => "/api/workspaces/browse",
          update: () => "/api/workspaces",
          mkdir: () => "/api/workspaces/mkdir",
          rename: () => "/api/workspaces/rename",
        },
      },
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");
    ctx.showPrompt = vi.fn().mockResolvedValue(null);
    vm.runInContext('workspaceBrowserPath = "/Users/test/dev";', ctx);

    await ctx.createWorkspaceFolder();

    expect(apiFn).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// renameWorkspaceBrowserEntry
// ---------------------------------------------------------------------------

describe("renameWorkspaceBrowserEntry", () => {
  it("calls rename API and refreshes the listing", async () => {
    const apiFn = vi
      .fn()
      .mockResolvedValue({ path: "/Users/test/dev/renamed" });
    const ctx = makeContext({
      api: apiFn,
      Routes: {
        config: { get: () => "/api/config", update: () => "/api/config" },
        workspaces: {
          browse: () => "/api/workspaces/browse",
          update: () => "/api/workspaces",
          mkdir: () => "/api/workspaces/mkdir",
          rename: () => "/api/workspaces/rename",
        },
      },
      elements: [
        ["workspace-browser-path", { value: "/Users/test/dev" }],
        ["workspace-browser-status", { textContent: "" }],
        ["workspace-browser-list", { innerHTML: "" }],
        ["workspace-browser-entries", { innerHTML: "" }],
        ["workspace-browser-breadcrumb", { textContent: "" }],
        ["workspace-browser-include-hidden", { checked: false }],
      ],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");
    ctx.showPrompt = vi.fn().mockResolvedValue("renamed");
    vm.runInContext('workspaceBrowserPath = "/Users/test/dev";', ctx);

    await ctx.renameWorkspaceBrowserEntry(
      "/Users/test/dev/old-name",
      "old-name",
    );

    expect(apiFn).toHaveBeenCalledWith("/api/workspaces/rename", {
      method: "POST",
      body: JSON.stringify({
        path: "/Users/test/dev/old-name",
        name: "renamed",
      }),
    });
  });

  it("does nothing when name is unchanged", async () => {
    const apiFn = vi.fn().mockResolvedValue({});
    const ctx = makeContext({
      api: apiFn,
      Routes: {
        config: { get: () => "/api/config", update: () => "/api/config" },
        workspaces: {
          browse: () => "/api/workspaces/browse",
          update: () => "/api/workspaces",
          mkdir: () => "/api/workspaces/mkdir",
          rename: () => "/api/workspaces/rename",
        },
      },
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "utils.js");
    loadScript(ctx, "workspace.js");
    ctx.showPrompt = vi.fn().mockResolvedValue("same-name");
    vm.runInContext('workspaceBrowserPath = "/Users/test/dev";', ctx);

    await ctx.renameWorkspaceBrowserEntry(
      "/Users/test/dev/same-name",
      "same-name",
    );

    expect(apiFn).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// activeGroupBadgeHtml
// ---------------------------------------------------------------------------

describe("activeGroupBadgeHtml", () => {
  function setup({
    activeGroupsJSON = [],
    tasksJSON = [],
    activeWs = [],
  } = {}) {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["settings-workspace-groups", el]],
    });
    loadScript(ctx, "state.js");
    loadScript(ctx, "workspace.js");
    vm.runInContext(
      `activeGroups = ${JSON.stringify(activeGroupsJSON)};
       tasks = ${JSON.stringify(tasksJSON)};
       activeWorkspaces = ${JSON.stringify(activeWs)};`,
      ctx,
    );
    return ctx;
  }

  it("renders running badge from live tasks for viewed group", () => {
    const ctx = setup({
      activeWs: ["/ws/a"],
      tasksJSON: [
        { status: "in_progress" },
        { status: "in_progress" },
        { status: "backlog" },
      ],
    });
    const html = ctx.activeGroupBadgeHtml({
      key: "abc",
      workspaces: ["/ws/a"],
    });
    expect(html).toContain("badge-in_progress");
    expect(html).toContain("spinner");
    expect(html).toContain("2 running"); // title attr
  });

  it("renders waiting badge from live tasks for viewed group", () => {
    const ctx = setup({
      activeWs: ["/ws/a"],
      tasksJSON: [{ status: "waiting" }, { status: "waiting" }],
    });
    const html = ctx.activeGroupBadgeHtml({
      key: "abc",
      workspaces: ["/ws/a"],
    });
    expect(html).toContain("badge-waiting");
    expect(html).toContain("2 waiting"); // title attr
    expect(html).not.toContain("spinner");
  });

  it("uses server data for non-viewed group", () => {
    const ctx = setup({
      activeWs: ["/ws/a"],
      activeGroupsJSON: [{ key: "bg", in_progress: 5, waiting: 0 }],
    });
    const html = ctx.activeGroupBadgeHtml({
      key: "bg",
      workspaces: ["/ws/b"],
    });
    expect(html).toContain("badge-in_progress");
    expect(html).toContain("5 running"); // title attr
  });

  it("returns empty string when both counts are 0", () => {
    const ctx = setup({
      activeWs: ["/ws/a"],
      tasksJSON: [{ status: "done" }],
    });
    const html = ctx.activeGroupBadgeHtml({
      key: "abc",
      workspaces: ["/ws/a"],
    });
    expect(html).toBe("");
  });

  it("returns empty string for unknown background group", () => {
    const ctx = setup({
      activeGroupsJSON: [{ key: "other", in_progress: 5, waiting: 0 }],
    });
    const html = ctx.activeGroupBadgeHtml({
      key: "unknown",
      workspaces: ["/ws/x"],
    });
    expect(html).toBe("");
  });

  it("returns empty string when group has no key", () => {
    const ctx = setup({
      activeGroupsJSON: [{ key: "abc", in_progress: 5, waiting: 0 }],
    });
    const html = ctx.activeGroupBadgeHtml({ workspaces: ["/a"] });
    expect(html).toBe("");
  });
});
