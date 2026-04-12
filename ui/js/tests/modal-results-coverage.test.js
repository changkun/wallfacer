/**
 * Additional coverage tests for modal-results.js.
 * Covers: _phaseColor, _humanSpanLabel, _spanIsOpen, _buildTimelineHtml,
 * _ensureTimelineStyles, _attachTimelineTips, _startTimelineRefresh,
 * _stopTimelineRefresh, renderTimeline, copyResultEntry, toggleResultEntryRaw,
 * setLeftTab, and branch paths in renderResultsFromEvents.
 */
import { describe, it, expect, beforeAll, beforeEach, } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext(extra = {}) {
  return vm.createContext({
    console,
    Math,
    Date,
    parseInt,
    isNaN,
    Infinity,
    Array,
    String,
    Object,
    JSON,
    Promise,
    encodeURIComponent,
    setTimeout: extra.setTimeout || (() => 0),
    setInterval: extra.setInterval || (() => 99),
    clearInterval: extra.clearInterval || (() => {}),
    ...extra,
  });
}

function loadScript(filename, ctx) {
  loadLibDeps(filename, ctx);
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

// ---------------------------------------------------------------------------
// _phaseColor
// ---------------------------------------------------------------------------
describe("_phaseColor", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("modal-results.js", ctx);
  });

  it("returns known color for worktree_setup", () => {
    expect(ctx._phaseColor("worktree_setup")).toBe("#5e81ac");
  });

  it("returns known color for agent_turn", () => {
    expect(ctx._phaseColor("agent_turn")).toBe("var(--accent)");
  });

  it("returns known color for commit", () => {
    expect(ctx._phaseColor("commit")).toBe("#d97706");
  });

  it("returns known color for container_run", () => {
    expect(ctx._phaseColor("container_run")).toBe("#9e6ec7");
  });

  it("returns known color for feedback_waiting", () => {
    expect(ctx._phaseColor("feedback_waiting")).toBe("#eab308");
  });

  it("returns known color for worktree_cleanup", () => {
    expect(ctx._phaseColor("worktree_cleanup")).toBe("#6366f1");
  });

  it("returns fallback color for unknown phase", () => {
    expect(ctx._phaseColor("unknown_phase")).toBe("var(--text-muted)");
  });
});

// ---------------------------------------------------------------------------
// _humanSpanLabel
// ---------------------------------------------------------------------------
describe("_humanSpanLabel", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("modal-results.js", ctx);
  });

  it("formats agent_turn implementation labels", () => {
    expect(ctx._humanSpanLabel("agent_turn", "implementation_3")).toBe(
      "Implementation Turn 3",
    );
  });

  it("formats agent_turn test labels", () => {
    expect(ctx._humanSpanLabel("agent_turn", "test_2")).toBe("Test Turn 2");
  });

  it("formats legacy agent_turn labels", () => {
    expect(ctx._humanSpanLabel("agent_turn", "agent_turn_5")).toBe("Turn 5");
  });

  it("returns raw label for unrecognized agent_turn", () => {
    expect(ctx._humanSpanLabel("agent_turn", "custom_label")).toBe(
      "custom_label",
    );
  });

  it("formats container_run with known activities", () => {
    expect(ctx._humanSpanLabel("container_run", "implementation")).toBe(
      "Container (Implementation)",
    );
    expect(ctx._humanSpanLabel("container_run", "test")).toBe(
      "Container (Test)",
    );
    expect(ctx._humanSpanLabel("container_run", "commit_message")).toBe(
      "Container (Commit Message)",
    );
    expect(ctx._humanSpanLabel("container_run", "oversight")).toBe(
      "Container (Oversight)",
    );
    expect(ctx._humanSpanLabel("container_run", "oversight_test")).toBe(
      "Container (Oversight Test)",
    );
    expect(ctx._humanSpanLabel("container_run", "refinement")).toBe(
      "Container (Refinement)",
    );
    expect(ctx._humanSpanLabel("container_run", "title")).toBe(
      "Container (Title)",
    );
    expect(ctx._humanSpanLabel("container_run", "idea_agent")).toBe(
      "Container (Ideation)",
    );
    expect(ctx._humanSpanLabel("container_run", "container_run")).toBe(
      "Container",
    );
  });

  it("formats container_run with unknown activity", () => {
    expect(ctx._humanSpanLabel("container_run", "custom")).toBe(
      "Container (custom)",
    );
  });

  it("formats worktree_setup phase", () => {
    expect(ctx._humanSpanLabel("worktree_setup", "anything")).toBe(
      "Worktree Setup",
    );
  });

  it("formats commit phase with label", () => {
    expect(ctx._humanSpanLabel("commit", "push-to-remote")).toBe(
      "push-to-remote",
    );
  });

  it("formats commit phase without label", () => {
    expect(ctx._humanSpanLabel("commit", "")).toBe("Commit & Push");
  });

  it("formats refinement phase", () => {
    expect(ctx._humanSpanLabel("refinement", "anything")).toBe("Refinement");
  });

  it("formats board_context with turn number", () => {
    expect(ctx._humanSpanLabel("board_context", "board_context_7")).toBe(
      "Board Sync (Turn 7)",
    );
  });

  it("formats board_context without turn number", () => {
    expect(ctx._humanSpanLabel("board_context", "other")).toBe("Board Sync");
  });

  it("formats feedback_waiting phase", () => {
    expect(ctx._humanSpanLabel("feedback_waiting", "")).toBe(
      "Waiting for Feedback",
    );
  });

  it("formats worktree_cleanup phase", () => {
    expect(ctx._humanSpanLabel("worktree_cleanup", "")).toBe(
      "Worktree Cleanup",
    );
  });

  it("returns label for unknown phase", () => {
    expect(ctx._humanSpanLabel("unknown_phase", "my_label")).toBe("my_label");
  });

  it("returns phase when unknown phase has no label", () => {
    expect(ctx._humanSpanLabel("unknown_phase", "")).toBe("unknown_phase");
  });
});

// ---------------------------------------------------------------------------
// _spanIsOpen
// ---------------------------------------------------------------------------
describe("_spanIsOpen", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("modal-results.js", ctx);
  });

  it("returns true when ended_at is missing", () => {
    expect(ctx._spanIsOpen({})).toBe(true);
    expect(ctx._spanIsOpen({ ended_at: "" })).toBe(true);
    expect(ctx._spanIsOpen({ ended_at: null })).toBe(true);
  });

  it("returns true for Go zero time", () => {
    expect(ctx._spanIsOpen({ ended_at: "0001-01-01T00:00:00Z" })).toBe(true);
  });

  it("returns false for a valid ended_at", () => {
    expect(ctx._spanIsOpen({ ended_at: "2025-06-01T12:00:00Z" })).toBe(false);
  });

  it("returns true for invalid date string", () => {
    expect(ctx._spanIsOpen({ ended_at: "not-a-date" })).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// _ensureTimelineStyles
// ---------------------------------------------------------------------------
describe("_ensureTimelineStyles", () => {
  it("creates style and tooltip elements once", () => {
    const elements = {};
    const bodyChildren = [];
    const headChildren = [];
    const ctx = makeContext({
      document: {
        getElementById(id) {
          return elements[id] || null;
        },
        createElement(tag) {
          const el = { id: "", textContent: "", style: {} };
          return el;
        },
        head: {
          appendChild(el) {
            headChildren.push(el);
            if (el.id) elements[el.id] = el;
          },
        },
        body: {
          appendChild(el) {
            bodyChildren.push(el);
            if (el.id) elements[el.id] = el;
          },
        },
      },
    });
    loadScript("modal-results.js", ctx);

    ctx._ensureTimelineStyles();
    expect(headChildren.length).toBe(1);
    expect(headChildren[0].id).toBe("tl-css");
    expect(bodyChildren.length).toBe(1);
    expect(bodyChildren[0].id).toBe("tl-tip");

    // Calling again should not add more elements
    ctx._ensureTimelineStyles();
    expect(headChildren.length).toBe(1);
    expect(bodyChildren.length).toBe(1);
  });
});

// ---------------------------------------------------------------------------
// _attachTimelineTips
// ---------------------------------------------------------------------------
describe("_attachTimelineTips", () => {
  it("attaches mousemove and mouseleave listeners to .tl-bar elements", () => {
    const tip = {
      style: { display: "none", left: "", top: "" },
      textContent: "",
      offsetHeight: 20,
    };
    const bars = [];
    const barEl = {
      dataset: { tip: "Test tooltip" },
      _listeners: {},
      addEventListener(ev, fn) {
        this._listeners[ev] = fn;
      },
    };
    bars.push(barEl);

    const ctx = makeContext({
      document: {
        getElementById(id) {
          if (id === "tl-tip") return tip;
          return null;
        },
      },
    });
    loadScript("modal-results.js", ctx);

    const container = {
      querySelectorAll(sel) {
        if (sel === ".tl-bar[data-tip]") return bars;
        return [];
      },
    };

    ctx._attachTimelineTips(container);
    expect(barEl._listeners.mousemove).toBeDefined();
    expect(barEl._listeners.mouseleave).toBeDefined();

    // Simulate mousemove
    barEl._listeners.mousemove({ clientX: 100, clientY: 200 });
    expect(tip.textContent).toBe("Test tooltip");
    expect(tip.style.display).toBe("block");

    // Simulate mouseleave
    barEl._listeners.mouseleave();
    expect(tip.style.display).toBe("none");
  });

  it("does nothing if tip element is missing", () => {
    const ctx = makeContext({
      document: {
        getElementById() {
          return null;
        },
      },
    });
    loadScript("modal-results.js", ctx);

    // Should not throw
    ctx._attachTimelineTips({
      querySelectorAll() {
        return [];
      },
    });
  });
});

// ---------------------------------------------------------------------------
// _buildTimelineHtml
// ---------------------------------------------------------------------------
describe("_buildTimelineHtml", () => {
  let ctx;
  beforeAll(() => {
    // Need buildTimeMap from time-map.js
    const elements = {};
    ctx = makeContext({
      document: {
        getElementById(id) {
          return elements[id] || null;
        },
        createElement(tag) {
          const el = { id: "", textContent: "", style: {} };
          return el;
        },
        head: {
          appendChild(el) {
            if (el.id) elements[el.id] = el;
          },
        },
        body: {
          appendChild(el) {
            if (el.id) elements[el.id] = el;
          },
        },
      },
    });
    // Load time-map.js dependency
    const tmCode = readFileSync(join(jsDir, "build/time-map.js"), "utf8");
    vm.runInContext(tmCode, ctx, {
      filename: join(jsDir, "build/time-map.js"),
    });
    loadScript("modal-results.js", ctx);
  });

  it("returns no-data message for empty spans", () => {
    const html = ctx._buildTimelineHtml([]);
    expect(html).toContain("No timing data yet.");
  });

  it("returns no-data message for null spans", () => {
    const html = ctx._buildTimelineHtml(null);
    expect(html).toContain("No timing data yet.");
  });

  it("renders bars for valid spans", () => {
    const now = Date.now();
    const spans = [
      {
        phase: "worktree_setup",
        label: "worktree_setup",
        started_at: new Date(now - 10000).toISOString(),
        ended_at: new Date(now - 8000).toISOString(),
      },
      {
        phase: "agent_turn",
        label: "implementation_1",
        started_at: new Date(now - 8000).toISOString(),
        ended_at: new Date(now - 2000).toISOString(),
      },
      {
        phase: "commit",
        label: "",
        started_at: new Date(now - 2000).toISOString(),
        ended_at: new Date(now - 500).toISOString(),
      },
    ];
    const html = ctx._buildTimelineHtml(spans);
    expect(html).toContain("tl-bar");
    expect(html).toContain("Worktree Setup");
    expect(html).toContain("Implementation Turn 1");
    expect(html).toContain("Commit &amp; Push");
    // Legend should include seen phases
    expect(html).toContain("Agent Turn");
  });

  it("renders animated bar for open spans", () => {
    const now = Date.now();
    const spans = [
      {
        phase: "agent_turn",
        label: "implementation_1",
        started_at: new Date(now - 5000).toISOString(),
        ended_at: null,
      },
    ];
    const html = ctx._buildTimelineHtml(spans);
    expect(html).toContain("tl-stripe"); // animation keyframe reference
    expect(html).toContain("running");
  });
});

// ---------------------------------------------------------------------------
// _stopTimelineRefresh / _startTimelineRefresh
// ---------------------------------------------------------------------------
describe("_stopTimelineRefresh", () => {
  it("clears the interval timer", () => {
    const clearCalls = [];
    const ctx = makeContext({
      clearInterval(id) {
        clearCalls.push(id);
      },
      timelineRefreshTimer: 42,
    });
    loadScript("modal-results.js", ctx);

    ctx._stopTimelineRefresh();
    expect(clearCalls).toContain(42);
  });
});

describe("_startTimelineRefresh", () => {
  it("returns early when tasks or timer var is undefined", () => {
    const clearCalls = [];
    const ctx = makeContext({
      clearInterval(id) {
        clearCalls.push(id);
      },
      timelineRefreshTimer: null,
      // tasks is undefined -- should return early
    });
    loadScript("modal-results.js", ctx);

    ctx._startTimelineRefresh("task-1");
    // Should not throw, timer stays null
  });

  it("starts a timer for in_progress tasks", () => {
    let intervalFn = null;
    let intervalDelay = 0;
    const ctx = makeContext({
      clearInterval() {},
      setInterval(fn, delay) {
        intervalFn = fn;
        intervalDelay = delay;
        return 123;
      },
      timelineRefreshTimer: null,
      tasks: [{ id: "t1", status: "in_progress" }],
      currentTaskId: "t1",
      renderTimeline() {},
    });
    loadScript("modal-results.js", ctx);

    ctx._startTimelineRefresh("t1");
    expect(intervalDelay).toBe(5000);
  });

  it("does not start timer for done tasks", () => {
    let timerSet = false;
    const ctx = makeContext({
      clearInterval() {},
      setInterval() {
        timerSet = true;
        return 1;
      },
      timelineRefreshTimer: null,
      tasks: [{ id: "t1", status: "done" }],
    });
    loadScript("modal-results.js", ctx);

    ctx._startTimelineRefresh("t1");
    expect(timerSet).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// renderTimeline
// ---------------------------------------------------------------------------
describe("renderTimeline", () => {
  it("fetches spans and renders into modal-timeline-chart", async () => {
    const elements = {};
    const bodyChildren = [];
    const headChildren = [];

    function makeEl(id) {
      const el = {
        id,
        innerHTML: "",
        dataset: {},
        textContent: "",
        style: {},
        querySelectorAll() {
          return [];
        },
      };
      elements[id] = el;
      return el;
    }
    makeEl("modal-timeline-chart");

    const now = Date.now();
    const spansData = {
      spans: [
        {
          phase: "agent_turn",
          label: "implementation_1",
          started_at: new Date(now - 5000).toISOString(),
          ended_at: new Date(now - 1000).toISOString(),
        },
      ],
    };

    const ctx = makeContext({
      document: {
        getElementById(id) {
          return elements[id] || null;
        },
        createElement(tag) {
          return makeEl("dynamic-" + tag);
        },
        head: {
          appendChild(el) {
            headChildren.push(el);
            if (el.id) elements[el.id] = el;
          },
        },
        body: {
          appendChild(el) {
            bodyChildren.push(el);
            if (el.id) elements[el.id] = el;
          },
        },
      },
      _modalState: { seq: 1, abort: null },
      getOpenModalTaskId() {
        return "t1";
      },
      apiGet(url) {
        return Promise.resolve(spansData);
      },
    });

    // Load time-map.js
    const tmCode = readFileSync(join(jsDir, "build/time-map.js"), "utf8");
    vm.runInContext(tmCode, ctx, {
      filename: join(jsDir, "build/time-map.js"),
    });
    loadScript("modal-results.js", ctx);

    ctx.renderTimeline("t1");

    // Wait for async resolution
    await new Promise((r) => setTimeout(r, 50));

    const chart = elements["modal-timeline-chart"];
    expect(chart.dataset.loaded).toBe("1");
    expect(chart.innerHTML).toContain("tl-bar");
  });

  it("shows error message on fetch failure", async () => {
    const elements = {};

    function makeEl(id) {
      const el = {
        id,
        innerHTML: "",
        dataset: {},
        textContent: "",
        style: {},
        querySelectorAll() {
          return [];
        },
      };
      elements[id] = el;
      return el;
    }
    makeEl("modal-timeline-chart");

    const ctx = makeContext({
      document: {
        getElementById(id) {
          return elements[id] || null;
        },
        createElement(tag) {
          return makeEl("dynamic-" + tag);
        },
        head: { appendChild() {} },
        body: { appendChild() {} },
      },
      _modalState: { seq: 1, abort: null },
      getOpenModalTaskId() {
        return "t1";
      },
      apiGet() {
        return Promise.reject(new Error("network error"));
      },
    });

    const tmCode = readFileSync(join(jsDir, "build/time-map.js"), "utf8");
    vm.runInContext(tmCode, ctx, {
      filename: join(jsDir, "build/time-map.js"),
    });
    loadScript("modal-results.js", ctx);

    ctx.renderTimeline("t1");
    await new Promise((r) => setTimeout(r, 50));

    expect(elements["modal-timeline-chart"].innerHTML).toContain(
      "Failed to load timeline",
    );
  });

  it("returns early when chart element is missing", () => {
    const ctx = makeContext({
      document: {
        getElementById() {
          return null;
        },
      },
      _modalState: { seq: 1 },
    });
    loadScript("modal-results.js", ctx);
    // Should not throw
    ctx.renderTimeline("t1");
  });
});

// ---------------------------------------------------------------------------
// copyResultEntry / toggleResultEntryRaw
// ---------------------------------------------------------------------------
describe("copyResultEntry", () => {
  it.skip("calls copyWithFeedback when raw element exists", () => {
    const elements = {};
    let copiedText = null;
    const ctx = makeContext({
      document: {
        getElementById(id) {
          return elements[id] || null;
        },
        createElement() {
          return { style: {}, textContent: "", innerHTML: "" };
        },
      },
      navigator: { clipboard: { writeText: () => Promise.resolve() } },
      event: { currentTarget: { querySelector: () => null } },
      copyWithFeedback(text, el) {
        copiedText = text;
      },
    });
    loadScript("modal-results.js", ctx);

    elements["entry-1-raw"] = { textContent: "raw content" };
    ctx.copyResultEntry("entry-1");
    expect(copiedText).toBe("raw content");
  });

  it("does nothing when raw element is missing", () => {
    const ctx = makeContext({
      document: {
        getElementById() {
          return null;
        },
      },
      navigator: { clipboard: { writeText: () => Promise.resolve() } },
      event: { currentTarget: { querySelector: () => null } },
      copyWithFeedback() {
        throw new Error("should not be called");
      },
    });
    loadScript("modal-results.js", ctx);
    // Should not throw
    ctx.copyResultEntry("nonexistent");
  });
});

describe("toggleResultEntryRaw", () => {
  it.skip("calls toggleRenderedRaw with correct elements", () => {
    const elements = {};
    let toggleArgs = null;
    const ctx = makeContext({
      document: {
        getElementById(id) {
          return elements[id] || null;
        },
      },
      event: {
        currentTarget: {
          id: "btn",
          classList: { contains: () => false, add: () => {}, remove: () => {} },
        },
      },
      toggleRenderedRaw(rendered, raw, btn) {
        toggleArgs = { rendered, raw, btn };
      },
    });
    loadScript("modal-results.js", ctx);

    elements["entry-1-rendered"] = { id: "rendered" };
    elements["entry-1-raw"] = { id: "raw" };
    ctx.toggleResultEntryRaw("entry-1");
    expect(toggleArgs.rendered.id).toBe("rendered");
    expect(toggleArgs.raw.id).toBe("raw");
    expect(toggleArgs.btn.id).toBe("btn");
  });
});

// ---------------------------------------------------------------------------
// setLeftTab
// ---------------------------------------------------------------------------
describe("setLeftTab", () => {
  it("delegates to the tab switcher", () => {
    const elements = {};
    const ctx = makeContext({
      document: {
        getElementById(id) {
          if (!elements[id])
            elements[id] = {
              id,
              classList: {
                _c: new Set(),
                add(c) {
                  this._c.add(c);
                },
                remove(c) {
                  this._c.delete(c);
                },
                toggle(c, f) {
                  f ? this._c.add(c) : this._c.delete(c);
                },
                contains(c) {
                  return this._c.has(c);
                },
              },
            };
          return elements[id];
        },
      },
      getOpenModalTaskId() {
        return "task-abc";
      },
      history: {
        replaceState() {},
      },
    });
    loadScript("modal-results.js", ctx);
    // Should not throw
    ctx.setLeftTab("testing");
  });
});

// ---------------------------------------------------------------------------
// renderResultsFromEvents — autoSwitch branch
// ---------------------------------------------------------------------------
describe("renderResultsFromEvents additional branches", () => {
  let ctx;
  let elements;

  beforeEach(() => {
    elements = {};
    const makeEl = (id) => {
      const el = {
        id,
        innerHTML: "",
        classList: {
          _classes: new Set(["hidden"]),
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
        textContent: "",
      };
      elements[id] = el;
      return el;
    };

    ctx = makeContext({
      escapeHtml: (s) =>
        String(s ?? "")
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")
          .replace(/>/g, "&gt;"),
      renderMarkdown: (s) => `<p>${s}</p>`,
      document: {
        getElementById: (id) => {
          if (!elements[id]) makeEl(id);
          return elements[id];
        },
        createElement: () => ({
          id: "",
          textContent: "",
          style: {},
          innerHTML: "",
          setAttribute() {},
          appendChild() {},
        }),
        querySelectorAll: () => ({ forEach: () => {} }),
        head: { appendChild() {} },
        body: { appendChild() {} },
      },
      getOpenModalTaskId: () => null,
      history: { replaceState: () => {} },
    });
    loadScript("modal-results.js", ctx);
  });

  it("calls setLeftTab when autoSwitch option is true", () => {
    ctx.renderResultsFromEvents(["result data"], { autoSwitch: true });
    const tab = elements["left-tab-implementation"];
    expect(tab.classList.contains("hidden")).toBe(false);
  });

  it("hides summary section when no tabs are visible and results are empty", () => {
    // Make both tabs hidden
    elements["left-tab-implementation"] = {
      classList: {
        _classes: new Set(["hidden"]),
        add(c) {
          this._classes.add(c);
        },
        remove(c) {
          this._classes.delete(c);
        },
        contains(c) {
          return this._classes.has(c);
        },
        toggle() {},
      },
    };
    elements["left-tab-testing"] = {
      classList: {
        _classes: new Set(["hidden"]),
        add(c) {
          this._classes.add(c);
        },
        remove(c) {
          this._classes.delete(c);
        },
        contains(c) {
          return this._classes.has(c);
        },
        toggle() {},
      },
    };

    const summary = {
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
        toggle() {},
      },
    };
    elements["modal-summary-section"] = summary;

    ctx.renderResultsFromEvents([]);
    expect(summary.classList.contains("hidden")).toBe(true);
  });

  it("handles null results", () => {
    ctx.renderResultsFromEvents(null);
    const tab = elements["left-tab-implementation"];
    expect(tab.classList.contains("hidden")).toBe(true);
  });
});
