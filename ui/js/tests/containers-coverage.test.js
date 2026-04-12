/**
 * Coverage tests for containers.js.
 *
 * Tests for:
 * - containerStateColor (all states)
 * - relativeTime (seconds, minutes, hours, days)
 * - showContainerMonitor / closeContainerMonitor / refreshContainerMonitor
 * - setContainerMonitorState
 * - fetchContainers / fetchContainersQuiet
 * - renderContainers (empty, with data, task linking, no-task, created_at)
 * - _containerMonitorCtrl lifecycle (open/close via createModalController)
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
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

function makeContainersContext(overrides = {}) {
  const elements = {};

  function makeEl(id) {
    const el = {
      id,
      innerHTML: "",
      textContent: "",
      style: { display: "", cssText: "" },
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
            this._classes.has(c)
              ? this._classes.delete(c)
              : this._classes.add(c);
          }
        },
      },
      appendChild: vi.fn(function (child) {
        if (child) el._children.push(child);
      }),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      _children: [],
    };
    elements[id] = el;
    return el;
  }

  const ctx = vm.createContext({
    console,
    Date,
    Math,
    setInterval: overrides.setInterval || vi.fn(() => 42),
    clearInterval: overrides.clearInterval || vi.fn(),
    setTimeout: vi.fn(),
    window: { state: overrides.state || { tasks: [] } },
    state: overrides.state || { tasks: [] },
    fetch: overrides.fetch || (() => Promise.reject(new Error("not mocked"))),
    apiGet: overrides.apiGet || undefined,
    loadJsonEndpoint: overrides.loadJsonEndpoint || vi.fn(),
    closeSettings: vi.fn(),
    createHoverRow: function (_cells) {
      const tr = {
        tagName: "TR",
        innerHTML: "",
        style: { cssText: "" },
        appendChild: vi.fn(),
        addEventListener: vi.fn(),
      };
      return tr;
    },
    escapeHtml: (s) =>
      String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
    document: {
      getElementById: (id) => {
        if (!elements[id]) makeEl(id);
        return elements[id];
      },
      createElement: (tag) => {
        const el = {
          tagName: tag.toUpperCase(),
          innerHTML: "",
          textContent: "",
          style: { cssText: "" },
          appendChild: vi.fn(),
          addEventListener: vi.fn(),
        };
        return el;
      },
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    },
  });

  loadScript("containers.js", ctx);
  return { ctx, elements };
}

// ---------------------------------------------------------------------------
// containerStateColor
// ---------------------------------------------------------------------------
describe("containerStateColor", () => {
  let ctx;
  beforeEach(() => ({ ctx } = makeContainersContext()));

  it("returns green for running", () => {
    expect(ctx.containerStateColor("running")).toBe("#45b87a");
  });

  it("returns grey for exited", () => {
    expect(ctx.containerStateColor("exited")).toBe("#9c9890");
  });

  it("returns yellow for paused", () => {
    expect(ctx.containerStateColor("paused")).toBe("#d4a030");
  });

  it("returns blue for created", () => {
    expect(ctx.containerStateColor("created")).toBe("#6da0dc");
  });

  it("returns red for dead", () => {
    expect(ctx.containerStateColor("dead")).toBe("#d46868");
  });

  it("returns grey for unknown state", () => {
    expect(ctx.containerStateColor("unknown")).toBe("#9c9890");
    expect(ctx.containerStateColor("")).toBe("#9c9890");
    expect(ctx.containerStateColor(null)).toBe("#9c9890");
    expect(ctx.containerStateColor(undefined)).toBe("#9c9890");
  });

  it("is case-insensitive", () => {
    expect(ctx.containerStateColor("Running")).toBe("#45b87a");
    expect(ctx.containerStateColor("PAUSED")).toBe("#d4a030");
  });
});

// ---------------------------------------------------------------------------
// relativeTime
// ---------------------------------------------------------------------------
describe("relativeTime", () => {
  let ctx;
  beforeEach(() => ({ ctx } = makeContainersContext()));

  it("shows seconds ago", () => {
    const now = Date.now();
    expect(ctx.relativeTime(now - 5000)).toBe("5s ago");
  });

  it("shows minutes ago", () => {
    const now = Date.now();
    expect(ctx.relativeTime(now - 120000)).toBe("2m ago");
  });

  it("shows hours ago", () => {
    const now = Date.now();
    expect(ctx.relativeTime(now - 7200000)).toBe("2h ago");
  });

  it("shows days ago", () => {
    const now = Date.now();
    expect(ctx.relativeTime(now - 172800000)).toBe("2d ago");
  });

  it("returns 0s ago for very recent timestamps", () => {
    expect(ctx.relativeTime(Date.now())).toBe("0s ago");
  });
});

// ---------------------------------------------------------------------------
// setContainerMonitorState
// ---------------------------------------------------------------------------
describe("setContainerMonitorState", () => {
  it("initializes the state controller on first call", () => {
    const { ctx, elements } = makeContainersContext();
    // Pre-create the elements that createModalStateController will look for
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.setContainerMonitorState("loading");
    expect(elements["container-monitor-loading"].style.display).not.toBe(
      "none",
    );
  });

  it("switches to empty state", () => {
    const { ctx, elements } = makeContainersContext();
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.setContainerMonitorState("empty");
    expect(elements["container-monitor-empty"].style.display).not.toBe("none");
  });

  it("switches to table state", () => {
    const { ctx, elements } = makeContainersContext();
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.setContainerMonitorState("table");
    expect(elements["container-monitor-table-wrap"].style.display).not.toBe(
      "none",
    );
  });
});

// ---------------------------------------------------------------------------
// renderContainers — empty
// ---------------------------------------------------------------------------
describe("renderContainers", () => {
  it("sets empty state when containers array is empty", () => {
    const { ctx, elements } = makeContainersContext();
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    ctx.document.getElementById("container-monitor-tbody");
    ctx.renderContainers([]);
    expect(elements["container-monitor-empty"].style.display).not.toBe("none");
  });

  it("sets empty state when containers is null", () => {
    const { ctx, elements } = makeContainersContext();
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    ctx.document.getElementById("container-monitor-tbody");
    ctx.renderContainers(null);
    expect(elements["container-monitor-empty"].style.display).not.toBe("none");
  });

  it("renders container rows with task data", () => {
    const { ctx, elements } = makeContainersContext({
      state: {
        tasks: [
          {
            id: "task-1",
            title: "My Task",
            prompt: "Do something",
            status: "in_progress",
          },
        ],
      },
    });
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    const tbody = ctx.document.getElementById("container-monitor-tbody");

    ctx.renderContainers([
      {
        id: "abc123def456",
        name: "wallfacer-task-1",
        state: "running",
        status: "Up 2 hours",
        task_id: "task-1",
        task_title: null,
        created_at: Math.floor(Date.now() / 1000) - 60,
      },
    ]);

    expect(tbody._children.length).toBe(1);
    const row = tbody._children[0];
    expect(row.innerHTML).toContain("abc123def456".substring(0, 12));
    expect(row.innerHTML).toContain("My Task");
    expect(row.innerHTML).toContain("running");
    expect(row.innerHTML).toContain("Up 2 hours");
  });

  it("renders container with no task_id", () => {
    const { ctx, elements } = makeContainersContext();
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    const tbody = ctx.document.getElementById("container-monitor-tbody");

    ctx.renderContainers([
      {
        id: "abc123456789",
        name: "orphan-container",
        state: "exited",
        status: "Exited (0)",
        task_id: "",
        created_at: 0,
      },
    ]);

    expect(tbody._children.length).toBe(1);
  });

  it("renders container with task_title from server", () => {
    const { ctx, elements } = makeContainersContext({
      state: { tasks: [] },
    });
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    const tbody = ctx.document.getElementById("container-monitor-tbody");

    ctx.renderContainers([
      {
        id: "xyz123456789",
        name: "wallfacer-bg",
        state: "running",
        status: "Up 5 min",
        task_id: "task-2",
        task_title: "Server-Provided Title",
        created_at: Math.floor(Date.now() / 1000) - 3600,
      },
    ]);

    expect(tbody._children.length).toBe(1);
    const row = tbody._children[0];
    expect(row.innerHTML).toContain("Server-Provided Title");
  });

  it("falls back to short task_id when no title or task found", () => {
    const { ctx, elements } = makeContainersContext({
      state: { tasks: [] },
    });
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    const tbody = ctx.document.getElementById("container-monitor-tbody");

    ctx.renderContainers([
      {
        id: "xyz123456789",
        name: "wallfacer-unknown",
        state: "created",
        status: "Created",
        task_id: "abcdefgh-1234-5678-9012-abcdefabcdef",
        task_title: "",
        created_at: 0,
      },
    ]);

    expect(tbody._children.length).toBe(1);
    const row = tbody._children[0];
    expect(row.innerHTML).toContain("abcdefgh");
  });

  it("shows dash when created_at is 0", () => {
    const { ctx } = makeContainersContext();
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    const tbody = ctx.document.getElementById("container-monitor-tbody");

    ctx.renderContainers([
      {
        id: "xyz123456789",
        state: "running",
        task_id: "",
        created_at: 0,
      },
    ]);

    const row = tbody._children[0];
    // The last cell should contain a dash for created time
    expect(row.innerHTML).toMatch(/—/);
  });
});

// ---------------------------------------------------------------------------
// fetchContainers
// ---------------------------------------------------------------------------
describe("fetchContainers", () => {
  it("calls loadJsonEndpoint with the correct URL", () => {
    const loadFn = vi.fn();
    const { ctx } = makeContainersContext({ loadJsonEndpoint: loadFn });
    ctx.fetchContainers();
    expect(loadFn).toHaveBeenCalledWith(
      "/api/containers",
      expect.any(Function),
      expect.any(Function),
    );
  });
});

// ---------------------------------------------------------------------------
// fetchContainersQuiet — success
// ---------------------------------------------------------------------------
describe("fetchContainersQuiet", () => {
  it("calls apiGet when available and renders on success", async () => {
    const apiGetFn = vi.fn().mockResolvedValue([
      {
        id: "abc123456789",
        state: "running",
        task_id: "",
        created_at: 0,
      },
    ]);
    const { ctx, elements } = makeContainersContext({ apiGet: apiGetFn });
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    ctx.document.getElementById("container-monitor-tbody");
    await ctx.fetchContainersQuiet();
    expect(apiGetFn).toHaveBeenCalledWith("/api/containers", {});
  });

  it("falls back to fetch when apiGet is not available", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      json: () => Promise.resolve([]),
    });
    const { ctx, elements } = makeContainersContext({
      fetch: fetchFn,
      apiGet: undefined,
    });
    // Remove apiGet
    vm.runInContext("apiGet = undefined", ctx);
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    ctx.document.getElementById("container-monitor-tbody");
    await ctx.fetchContainersQuiet();
    expect(fetchFn).toHaveBeenCalledWith("/api/containers");
  });

  it("sets error state on failure", async () => {
    const apiGetFn = vi.fn().mockRejectedValue(new Error("Network error"));
    const { ctx, elements } = makeContainersContext({ apiGet: apiGetFn });
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    await ctx.fetchContainersQuiet();
    // error state should have been set
    expect(elements["container-monitor-error"].style.display).not.toBe("none");
  });
});

// ---------------------------------------------------------------------------
// showContainerMonitor
// ---------------------------------------------------------------------------
describe("showContainerMonitor", () => {
  it("opens the modal and starts auto-refresh", () => {
    const setIntervalFn = vi.fn(() => 99);
    const loadFn = vi.fn();
    const { ctx, elements } = makeContainersContext({
      setInterval: setIntervalFn,
      loadJsonEndpoint: loadFn,
    });
    ctx.document.getElementById("container-monitor-modal");
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");

    const event = { stopPropagation: vi.fn() };
    ctx.showContainerMonitor(event);

    expect(event.stopPropagation).toHaveBeenCalled();
    expect(setIntervalFn).toHaveBeenCalled();
    expect(loadFn).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// closeContainerMonitor
// ---------------------------------------------------------------------------
describe("closeContainerMonitor", () => {
  it("closes the modal", () => {
    const { ctx, elements } = makeContainersContext();
    ctx.document.getElementById("container-monitor-modal");
    // Open first
    ctx.showContainerMonitor({ stopPropagation: vi.fn() });
    ctx.closeContainerMonitor();
    // Modal should have hidden class
    expect(
      elements["container-monitor-modal"].classList.contains("hidden"),
    ).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// refreshContainerMonitor
// ---------------------------------------------------------------------------
describe("refreshContainerMonitor", () => {
  it("sets refreshing text and calls fetchContainersQuiet", async () => {
    const apiGetFn = vi.fn().mockResolvedValue([]);
    const { ctx, elements } = makeContainersContext({ apiGet: apiGetFn });
    ctx.document.getElementById("container-monitor-loading");
    ctx.document.getElementById("container-monitor-error");
    ctx.document.getElementById("container-monitor-empty");
    ctx.document.getElementById("container-monitor-table-wrap");
    ctx.document.getElementById("container-monitor-updated");
    ctx.document.getElementById("container-monitor-tbody");

    ctx.refreshContainerMonitor();

    expect(elements["container-monitor-updated"].textContent).toBe(
      "Refreshing\u2026",
    );
    // fetchContainersQuiet was called via apiGet
    expect(apiGetFn).toHaveBeenCalled();
  });
});
