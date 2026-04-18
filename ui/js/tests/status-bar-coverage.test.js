/**
 * Tests for status-bar.js
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeEl(extra = {}) {
  return {
    textContent: "",
    className: "",
    hidden: false,
    style: { display: "", height: "", userSelect: "", cursor: "" },
    classList: {
      _set: new Set(),
      add(c) {
        this._set.add(c);
      },
      remove(c) {
        this._set.delete(c);
      },
      contains(c) {
        return this._set.has(c);
      },
    },
    setAttribute: vi.fn(),
    offsetHeight: 200,
    addEventListener: vi.fn(),
    ...extra,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const listeners = {};
  const localStoreData = overrides.localStorageData || {};

  // Build localStorage with real methods - cannot be overridden by spread
  const lsData = { ...localStoreData };
  const localStorageObj = {
    getItem(k) {
      return lsData[k] !== undefined ? lsData[k] : null;
    },
    setItem(k, v) {
      lsData[k] = String(v);
    },
  };

  const ctx = {
    console,
    Date,
    Math,
    parseInt,
    String,
    Object,
    Array,
    Promise: overrides.Promise || Promise,
    setTimeout: (fn) => {
      if (fn) fn();
      return 0;
    },
    setInterval: () => 0,
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelector: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: overrides.readyState || "complete",
      addEventListener: (evt, fn) => {
        if (!listeners[evt]) listeners[evt] = [];
        listeners[evt].push(fn);
      },
      removeEventListener: vi.fn(),
      body: { style: { userSelect: "", cursor: "" } },
    },
    getComputedStyle: () => ({ getPropertyValue: () => "" }),
    ...overrides,
    // Re-assign localStorage after spread to prevent overrides from clobbering it
    localStorage: localStorageObj,
  };
  // window === context
  ctx.window = ctx;
  // Store listeners for test access
  ctx._listeners = listeners;
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "status-bar.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "status-bar.js") });
  return ctx;
}

// ---------------------------------------------------------------------------
// formatBytes
// ---------------------------------------------------------------------------
describe("formatBytes", () => {
  it("formats bytes", () => {
    const ctx = makeContext();
    loadScript(ctx);
    expect(vm.runInContext("formatBytes(0)", ctx)).toBe("0 B");
    expect(vm.runInContext("formatBytes(512)", ctx)).toBe("512 B");
    expect(vm.runInContext("formatBytes(1023)", ctx)).toBe("1023 B");
  });

  it("formats kilobytes", () => {
    const ctx = makeContext();
    loadScript(ctx);
    expect(vm.runInContext("formatBytes(1024)", ctx)).toBe("1.0 KB");
    expect(vm.runInContext("formatBytes(1536)", ctx)).toBe("1.5 KB");
  });

  it("formats megabytes", () => {
    const ctx = makeContext();
    loadScript(ctx);
    expect(vm.runInContext("formatBytes(1048576)", ctx)).toBe("1.0 MB");
    expect(vm.runInContext("formatBytes(2621440)", ctx)).toBe("2.5 MB");
  });
});

// ---------------------------------------------------------------------------
// _updateConnDot
// ---------------------------------------------------------------------------
describe("_updateConnDot", () => {
  function setup(state) {
    const dot = makeEl();
    const label = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-conn-dot", dot],
        ["status-bar-conn-label", label],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      _sseConnState: state,
    });
    loadScript(ctx);
    return { dot, label };
  }

  it("shows Connected for ok state", () => {
    const { dot, label } = setup("ok");
    expect(dot.className).toBe("status-bar-conn-dot status-bar-conn-dot--ok");
    expect(label.textContent).toBe("Connected");
    expect(dot.setAttribute).toHaveBeenCalledWith("aria-label", "Connected");
  });

  it("shows Reconnecting for reconnecting state", () => {
    const { dot, label } = setup("reconnecting");
    expect(dot.className).toContain("reconnecting");
    expect(label.textContent).toBe("Reconnecting\u2026");
  });

  it("shows Disconnected for closed state", () => {
    const { dot, label } = setup("closed");
    expect(dot.className).toContain("closed");
    expect(label.textContent).toBe("Disconnected");
  });

  it("defaults to closed when _sseConnState is undefined", () => {
    const dot = makeEl();
    const label = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-conn-dot", dot],
        ["status-bar-conn-label", label],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
    });
    // Do NOT set _sseConnState
    loadScript(ctx);
    expect(dot.className).toContain("closed");
    expect(label.textContent).toBe("Disconnected");
  });
});

// ---------------------------------------------------------------------------
// _updateCounts
// ---------------------------------------------------------------------------
describe("_updateCounts", () => {
  function setup(taskList) {
    const inProg = makeEl();
    const waiting = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-in-progress", inProg],
        ["status-bar-waiting", waiting],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      tasks: taskList,
    });
    loadScript(ctx);
    return { inProg, waiting };
  }

  it("counts in_progress and committing tasks", () => {
    const { inProg, waiting } = setup([
      { status: "in_progress" },
      { status: "committing" },
      { status: "done" },
    ]);
    expect(inProg.textContent).toBe("2");
    expect(waiting.textContent).toBe("0");
  });

  it("counts waiting and failed tasks", () => {
    const { inProg, waiting } = setup([
      { status: "waiting" },
      { status: "failed" },
      { status: "backlog" },
    ]);
    expect(inProg.textContent).toBe("0");
    expect(waiting.textContent).toBe("2");
  });

  it("handles empty task array", () => {
    const { inProg, waiting } = setup([]);
    expect(inProg.textContent).toBe("0");
    expect(waiting.textContent).toBe("0");
  });

  it("handles undefined tasks", () => {
    const inProg = makeEl();
    const waiting = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-in-progress", inProg],
        ["status-bar-waiting", waiting],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      // tasks not set
    });
    loadScript(ctx);
    expect(inProg.textContent).toBe("0");
    expect(waiting.textContent).toBe("0");
  });
});

// ---------------------------------------------------------------------------
// _updateWorkspace
// ---------------------------------------------------------------------------
describe("_updateWorkspace", () => {
  function setup(workspaces, groups) {
    const el = makeEl();
    const overrides = {
      elements: [
        ["status-bar-workspace", el],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
    };
    if (workspaces !== undefined) overrides.activeWorkspaces = workspaces;
    if (groups !== undefined) overrides.workspaceGroups = groups;
    const ctx = makeContext(overrides);
    loadScript(ctx);
    return el;
  }

  it("hides when no workspaces", () => {
    const el = setup([], []);
    expect(el.textContent).toBe("");
    expect(el.style.display).toBe("none");
  });

  it("shows basename of first workspace path", () => {
    const el = setup(["/home/user/projects/myapp"], []);
    expect(el.textContent).toBe("myapp");
    expect(el.style.display).toBe("");
  });

  it("strips trailing slash", () => {
    const el = setup(["/home/user/projects/myapp/"], []);
    expect(el.textContent).toBe("myapp");
  });

  it("shows group name when matching", () => {
    const el = setup(
      ["/a", "/b"],
      [{ name: "My Group", workspaces: ["/a", "/b"] }],
    );
    expect(el.textContent).toBe("My Group");
  });

  it("falls back to basename when group doesn't match", () => {
    const el = setup(
      ["/a", "/b"],
      [{ name: "Other", workspaces: ["/x", "/y"] }],
    );
    expect(el.textContent).toBe("a");
  });

  it("handles undefined activeWorkspaces", () => {
    const el = setup(undefined, undefined);
    expect(el.textContent).toBe("");
    expect(el.style.display).toBe("none");
  });
});

// ---------------------------------------------------------------------------
// toggleTerminalPanel
// ---------------------------------------------------------------------------
describe("toggleTerminalPanel", () => {
  function setup(panelHidden, termEnabled) {
    const panel = makeEl();
    if (panelHidden) panel.classList.add("hidden");
    const handle = makeEl();
    if (panelHidden) handle.classList.add("hidden");
    const btn = makeEl();
    const tabBar = makeEl();
    const depBtn = makeEl();
    const officePanel = makeEl();
    officePanel.classList.add("hidden");
    const officeBtn = makeEl();
    const connectTerminal = vi.fn();
    const scheduleRender = vi.fn();

    const ctx = makeContext({
      elements: [
        ["status-bar-panel", panel],
        ["status-bar-panel-resize", handle],
        ["status-bar-terminal-btn", btn],
        ["terminal-tab-bar", tabBar],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-depgraph-btn", depBtn],
        ["office-container", officePanel],
        ["status-bar-office-btn", officeBtn],
      ],
      terminalEnabled: termEnabled,
      connectTerminal,
      scheduleRender,
    });
    loadScript(ctx);
    return { ctx, panel, handle, btn, tabBar, connectTerminal, scheduleRender };
  }

  it("opens terminal panel when hidden and terminal enabled", () => {
    const { ctx, panel, connectTerminal } = setup(true, true);
    ctx.toggleTerminalPanel();
    expect(panel.classList.contains("hidden")).toBe(false);
    expect(connectTerminal).toHaveBeenCalled();
  });

  it("closes terminal panel when visible", () => {
    const { ctx, panel } = setup(false, true);
    ctx.toggleTerminalPanel();
    expect(panel.classList.contains("hidden")).toBe(true);
  });

  it("does nothing meaningful when terminal disabled and panel hidden", () => {
    const { ctx, panel, connectTerminal } = setup(true, false);
    ctx.toggleTerminalPanel();
    // Panel should remain hidden
    expect(panel.classList.contains("hidden")).toBe(true);
    expect(connectTerminal).not.toHaveBeenCalled();
  });

  it("hides panel when terminal disabled and panel somehow visible", () => {
    const { ctx, panel } = setup(false, false);
    ctx.toggleTerminalPanel();
    expect(panel.classList.contains("hidden")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// toggleOfficePanel
// ---------------------------------------------------------------------------
describe("toggleOfficePanel", () => {
  function setup(officeHidden) {
    const officePanel = makeEl();
    if (officeHidden) officePanel.classList.add("hidden");
    const officeBtn = makeEl();
    const termPanel = makeEl();
    termPanel.classList.add("hidden");
    const termHandle = makeEl();
    termHandle.classList.add("hidden");
    const termBtn = makeEl();
    const tabBar = makeEl();
    const depBtn = makeEl();
    const officeShow = vi.fn();
    const officeHideFn = vi.fn();
    const scheduleRender = vi.fn();

    const ctx = makeContext({
      elements: [
        ["office-container", officePanel],
        ["status-bar-office-btn", officeBtn],
        ["status-bar-panel", termPanel],
        ["status-bar-panel-resize", termHandle],
        ["status-bar-terminal-btn", termBtn],
        ["terminal-tab-bar", tabBar],
        ["status-bar-depgraph-btn", depBtn],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
      ],
      _officeShow: officeShow,
      _officeHide: officeHideFn,
      scheduleRender,
    });
    loadScript(ctx);
    return { ctx, officePanel, officeBtn, officeShow, officeHideFn };
  }

  it("opens office panel when hidden", () => {
    const { ctx, officePanel, officeShow } = setup(true);
    ctx.toggleOfficePanel();
    expect(officePanel.classList.contains("hidden")).toBe(false);
    expect(officeShow).toHaveBeenCalled();
  });

  it("closes office panel when visible", () => {
    const { ctx, officePanel, officeHideFn } = setup(false);
    ctx.toggleOfficePanel();
    expect(officePanel.classList.contains("hidden")).toBe(true);
    expect(officeHideFn).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// _cycleBottomPanel
// ---------------------------------------------------------------------------
describe("_cycleBottomPanel", () => {
  function setup(opts = {}) {
    const termPanel = makeEl();
    if (opts.termOpen) {
      /* leave unhidden */
    } else {
      termPanel.classList.add("hidden");
    }
    const termHandle = makeEl();
    termHandle.classList.add("hidden");
    const termBtn = makeEl();
    const tabBar = makeEl();
    const depBtn = makeEl();
    const officePanel = makeEl();
    if (opts.officeOpen) {
      /* leave unhidden */
    } else {
      officePanel.classList.add("hidden");
    }
    const officeBtn = makeEl();
    const connectTerminal = vi.fn();
    const scheduleRender = vi.fn();
    const officeShow = vi.fn();
    const officeHideFn = vi.fn();

    const overrides = {
      elements: [
        ["status-bar-panel", termPanel],
        ["status-bar-panel-resize", termHandle],
        ["status-bar-terminal-btn", termBtn],
        ["terminal-tab-bar", tabBar],
        ["status-bar-depgraph-btn", depBtn],
        ["office-container", officePanel],
        ["status-bar-office-btn", officeBtn],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
      ],
      connectTerminal,
      scheduleRender,
      _officeShow: officeShow,
      _officeHide: officeHideFn,
      _officeAssetAvailable: opts.officeAvailable ? () => true : undefined,
    };
    if (opts.termEnabled !== undefined)
      overrides.terminalEnabled = opts.termEnabled;
    if (opts.depOpen) overrides.depGraphEnabled = true;

    const ctx = makeContext(overrides);
    loadScript(ctx);
    return {
      ctx,
      termPanel,
      officePanel,
      connectTerminal,
      scheduleRender,
      officeShow,
      officeHideFn,
    };
  }

  it("opens terminal when nothing is open and terminal available", () => {
    const { ctx, termPanel, connectTerminal } = setup({ termEnabled: true });
    // Invoke via keydown
    const handler = ctx._listeners["keydown"][0];
    handler({
      key: "`",
      ctrlKey: true,
      metaKey: false,
      altKey: false,
      shiftKey: false,
      preventDefault: vi.fn(),
    });
    expect(termPanel.classList.contains("hidden")).toBe(false);
    expect(connectTerminal).toHaveBeenCalled();
  });

  it("opens office when nothing is open, terminal unavailable, office available", () => {
    const { ctx, officePanel, officeShow } = setup({
      termEnabled: false,
      officeAvailable: true,
    });
    const handler = ctx._listeners["keydown"][0];
    handler({
      key: "`",
      ctrlKey: true,
      metaKey: false,
      altKey: false,
      shiftKey: false,
      preventDefault: vi.fn(),
    });
    expect(officePanel.classList.contains("hidden")).toBe(false);
    expect(officeShow).toHaveBeenCalled();
  });

  it("closes terminal and opens office when terminal is open and office available", () => {
    const { ctx, termPanel, officePanel, officeShow } = setup({
      termEnabled: true,
      termOpen: true,
      officeAvailable: true,
    });
    const handler = ctx._listeners["keydown"][0];
    handler({
      key: "`",
      ctrlKey: true,
      metaKey: false,
      altKey: false,
      shiftKey: false,
      preventDefault: vi.fn(),
    });
    expect(termPanel.classList.contains("hidden")).toBe(true);
    expect(officePanel.classList.contains("hidden")).toBe(false);
    expect(officeShow).toHaveBeenCalled();
  });

  it("closes terminal without opening office when office not available", () => {
    const { ctx, termPanel, officePanel } = setup({
      termEnabled: true,
      termOpen: true,
      officeAvailable: false,
    });
    const handler = ctx._listeners["keydown"][0];
    handler({
      key: "`",
      ctrlKey: true,
      metaKey: false,
      altKey: false,
      shiftKey: false,
      preventDefault: vi.fn(),
    });
    expect(termPanel.classList.contains("hidden")).toBe(true);
    expect(officePanel.classList.contains("hidden")).toBe(true);
  });

  it("closes office panel when office is open", () => {
    const { ctx, officePanel, officeHideFn } = setup({ officeOpen: true });
    const handler = ctx._listeners["keydown"][0];
    handler({
      key: "`",
      ctrlKey: true,
      metaKey: false,
      altKey: false,
      shiftKey: false,
      preventDefault: vi.fn(),
    });
    expect(officePanel.classList.contains("hidden")).toBe(true);
    expect(officeHideFn).toHaveBeenCalled();
  });

  it("ignores non-backtick keys", () => {
    const { ctx, termPanel } = setup({ termEnabled: true });
    const handler = ctx._listeners["keydown"][0];
    handler({
      key: "a",
      ctrlKey: true,
      metaKey: false,
      altKey: false,
      shiftKey: false,
      preventDefault: vi.fn(),
    });
    // Panel should still be hidden (nothing happened)
    expect(termPanel.classList.contains("hidden")).toBe(true);
  });

  it("ignores when meta key is also pressed", () => {
    const { ctx, termPanel } = setup({ termEnabled: true });
    const handler = ctx._listeners["keydown"][0];
    handler({
      key: "`",
      ctrlKey: true,
      metaKey: true,
      altKey: false,
      shiftKey: false,
      preventDefault: vi.fn(),
    });
    expect(termPanel.classList.contains("hidden")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// applyTerminalVisibility
// ---------------------------------------------------------------------------
describe("applyTerminalVisibility", () => {
  it("shows button when terminal is enabled", () => {
    const btn = makeEl();
    btn.classList.add("hidden");
    const ctx = makeContext({
      elements: [
        ["status-bar-terminal-btn", btn],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      terminalEnabled: true,
    });
    loadScript(ctx);
    ctx.applyTerminalVisibility();
    expect(btn.classList.contains("hidden")).toBe(false);
  });

  it("hides button when terminal is disabled", () => {
    const btn = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-terminal-btn", btn],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      terminalEnabled: false,
    });
    loadScript(ctx);
    ctx.applyTerminalVisibility();
    expect(btn.classList.contains("hidden")).toBe(true);
  });

  it("hides button when terminalEnabled is undefined", () => {
    const btn = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-terminal-btn", btn],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
    });
    loadScript(ctx);
    ctx.applyTerminalVisibility();
    expect(btn.classList.contains("hidden")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// loadSystemStatus
// ---------------------------------------------------------------------------
describe("loadSystemStatus", () => {
  it("renders runtime data into the about section", async () => {
    const container = makeEl();
    const content = makeEl();
    const apiMock = vi.fn().mockResolvedValue({
      go_goroutine_count: 42,
      go_heap_alloc_bytes: 2097152,
      active_containers: 3,
      container_circuit: { state: "closed", failures: 0 },
      worker_stats: {
        enabled: true,
        active_workers: 2,
        creates: 5,
        execs: 10,
        fallbacks: 1,
        by_activity: {
          implementation: { execs: 8, creates: 3 },
          testing: { execs: 2, creates: 2 },
        },
      },
      task_states: {
        in_progress: 2,
        waiting: 1,
        backlog: 3,
        done: 5,
        failed: 0,
      },
    });

    const ctx = makeContext({
      elements: [
        ["about-system-status", container],
        ["about-system-status-content", content],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      api: apiMock,
      Routes: { debug: { runtime: () => "/api/debug/runtime" } },
      Promise,
    });
    loadScript(ctx);

    // Call and await the promise chain
    await ctx.loadSystemStatus();

    expect(apiMock).toHaveBeenCalledWith("/api/debug/runtime");
    expect(content.innerHTML).toContain("Goroutines");
    expect(content.innerHTML).toContain("42");
    expect(content.innerHTML).toContain("2.0 MB");
    expect(content.innerHTML).toContain("Active containers");
    expect(content.innerHTML).toContain("3");
    expect(content.innerHTML).toContain("Circuit breaker");
    expect(content.innerHTML).toContain("closed");
    expect(content.innerHTML).toContain("Task workers");
    expect(content.innerHTML).toContain("enabled");
    expect(content.innerHTML).toContain("implementation");
    expect(content.innerHTML).toContain("Tasks:");
    expect(container.style.display).toBe("");
  });

  it("renders circuit breaker with failures", async () => {
    const container = makeEl();
    const content = makeEl();
    const apiMock = vi.fn().mockResolvedValue({
      go_goroutine_count: 10,
      go_heap_alloc_bytes: 1024,
      active_containers: 0,
      container_circuit: { state: "open", failures: 5 },
    });

    const ctx = makeContext({
      elements: [
        ["about-system-status", container],
        ["about-system-status-content", content],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      api: apiMock,
      Routes: { debug: { runtime: () => "/api/debug/runtime" } },
      Promise,
    });
    loadScript(ctx);
    await ctx.loadSystemStatus();

    expect(content.innerHTML).toContain("open");
    expect(content.innerHTML).toContain("5 failures");
  });

  it("hides container on API error", async () => {
    const container = makeEl();
    container.style.display = "";
    const content = makeEl();
    // Use a real promise that rejects, and capture it so we can await
    let rejectPromise;
    const p = new Promise((_, rej) => {
      rejectPromise = rej;
    });
    // Catch to prevent unhandled rejection, but chain the .then/.catch in source
    const apiMock = vi.fn().mockReturnValue(p);

    const ctx = makeContext({
      elements: [
        ["about-system-status", container],
        ["about-system-status-content", content],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      api: apiMock,
      Routes: { debug: { runtime: () => "/api/debug/runtime" } },
    });
    loadScript(ctx);
    ctx.loadSystemStatus();
    rejectPromise(new Error("fail"));
    // Flush microtask queue
    await new Promise((r) => setTimeout(r, 0));

    expect(container.style.display).toBe("none");
  });

  it("returns early when elements are missing", () => {
    const ctx = makeContext({
      elements: [
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
    });
    loadScript(ctx);
    // Should not throw
    ctx.loadSystemStatus();
  });
});

// ---------------------------------------------------------------------------
// initStatusBar
// ---------------------------------------------------------------------------
describe("initStatusBar", () => {
  it("registers a keydown listener", () => {
    const ctx = makeContext({
      elements: [
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
    });
    loadScript(ctx);
    expect(ctx._listeners["keydown"]).toBeDefined();
    expect(ctx._listeners["keydown"].length).toBeGreaterThanOrEqual(1);
  });
});

// ---------------------------------------------------------------------------
// _initPanelResize
// ---------------------------------------------------------------------------
describe("_initPanelResize", () => {
  it("restores height from localStorage", () => {
    const panel = makeEl();
    const handle = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-panel", panel],
        ["status-bar-panel-resize", handle],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
      ],
      localStorageData: { "wallfacer-panel-height": "300" },
    });
    loadScript(ctx);
    expect(panel.style.height).toBe("300px");
  });

  it("ignores invalid stored height (too small)", () => {
    const panel = makeEl();
    const handle = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-panel", panel],
        ["status-bar-panel-resize", handle],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
      ],
      localStorageData: { "wallfacer-panel-height": "10" },
    });
    loadScript(ctx);
    expect(panel.style.height).toBe("");
  });

  it("ignores invalid stored height (too large)", () => {
    const panel = makeEl();
    const handle = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-panel", panel],
        ["status-bar-panel-resize", handle],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
      ],
      localStorageData: { "wallfacer-panel-height": "9999" },
    });
    loadScript(ctx);
    expect(panel.style.height).toBe("");
  });

  it("registers mousedown listener on handle", () => {
    const panel = makeEl();
    const handle = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-panel", panel],
        ["status-bar-panel-resize", handle],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
      ],
    });
    loadScript(ctx);
    expect(handle.addEventListener).toHaveBeenCalledWith(
      "mousedown",
      expect.any(Function),
    );
  });
});

// ---------------------------------------------------------------------------
// DOMContentLoaded branch
// ---------------------------------------------------------------------------
describe("DOMContentLoaded path", () => {
  it("defers init when readyState is loading", () => {
    const ctx = makeContext({
      readyState: "loading",
      elements: [
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
    });
    loadScript(ctx);
    // Should have registered DOMContentLoaded
    expect(ctx._listeners["DOMContentLoaded"]).toBeDefined();
    expect(ctx._listeners["DOMContentLoaded"].length).toBeGreaterThanOrEqual(1);
  });
});

// ---------------------------------------------------------------------------
// updateStatusBar (integration)
// ---------------------------------------------------------------------------
describe("updateStatusBar", () => {
  it("calls all sub-update functions", () => {
    const dot = makeEl();
    const label = makeEl();
    const inProg = makeEl();
    const waiting = makeEl();
    const ws = makeEl();
    const ctx = makeContext({
      elements: [
        ["status-bar-conn-dot", dot],
        ["status-bar-conn-label", label],
        ["status-bar-in-progress", inProg],
        ["status-bar-waiting", waiting],
        ["status-bar-workspace", ws],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      _sseConnState: "ok",
      tasks: [{ status: "in_progress" }],
      activeWorkspaces: ["/foo/bar"],
    });
    loadScript(ctx);

    // Reset to verify updateStatusBar re-runs
    dot.className = "";
    label.textContent = "";
    inProg.textContent = "";

    ctx.updateStatusBar();

    expect(dot.className).toContain("ok");
    expect(label.textContent).toBe("Connected");
    expect(inProg.textContent).toBe("1");
    expect(ws.textContent).toBe("bar");
  });
});

// ---------------------------------------------------------------------------
// Worker stats edge case: disabled workers, no by_activity
// ---------------------------------------------------------------------------
describe("loadSystemStatus worker stats edge cases", () => {
  it("renders disabled workers without by_activity", async () => {
    const container = makeEl();
    const content = makeEl();
    const apiMock = vi.fn().mockResolvedValue({
      go_goroutine_count: 5,
      go_heap_alloc_bytes: 512,
      active_containers: 0,
      worker_stats: {
        enabled: false,
        active_workers: 0,
        creates: 0,
        execs: 0,
        fallbacks: 0,
      },
    });

    const ctx = makeContext({
      elements: [
        ["about-system-status", container],
        ["about-system-status-content", content],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      api: apiMock,
      Routes: { debug: { runtime: () => "/api/debug/runtime" } },
      Promise,
    });
    loadScript(ctx);
    await ctx.loadSystemStatus();

    expect(content.innerHTML).toContain("disabled");
    expect(content.innerHTML).not.toContain("Creates:");
  });

  it("renders task_states with only some fields", async () => {
    const container = makeEl();
    const content = makeEl();
    const apiMock = vi.fn().mockResolvedValue({
      go_goroutine_count: 1,
      go_heap_alloc_bytes: 0,
      active_containers: 0,
      task_states: { done: 10 },
    });

    const ctx = makeContext({
      elements: [
        ["about-system-status", container],
        ["about-system-status-content", content],
        ["status-bar-conn-dot", makeEl()],
        ["status-bar-conn-label", makeEl()],
        ["status-bar-in-progress", makeEl()],
        ["status-bar-waiting", makeEl()],
        ["status-bar-panel", makeEl()],
        ["status-bar-panel-resize", makeEl()],
      ],
      api: apiMock,
      Routes: { debug: { runtime: () => "/api/debug/runtime" } },
      Promise,
    });
    loadScript(ctx);
    await ctx.loadSystemStatus();

    expect(content.innerHTML).toContain("10 done");
    expect(content.innerHTML).not.toContain("running");
  });
});
