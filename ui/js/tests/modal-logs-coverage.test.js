/**
 * Additional coverage tests for modal-logs.js.
 *
 * Targets uncovered branches and functions not tested by modal-logs.test.js:
 * - _isCurrentModalSeq
 * - _modalApiJson
 * - _updateServerTruncationBanner (all branches)
 * - highlightLogMatches (tree-walker path)
 * - onLogSearchInput (raw mode)
 * - renderLogs: oversight mode delegation, search-bar visibility
 * - renderTestLogs: oversight delegation
 * - startLogStream / startTestLogStream (oversight pre-fetch paths)
 * - _downloadFullLog
 * - _fetchLogs / _fetchTestLogs: seq mismatch guard, modal closed guard
 */
import { describe, it, expect, vi, } from "vitest";
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

/**
 * Build a context that satisfies all runtime dependencies of modal-logs.js.
 */
function makeLogsContext(overrides = {}) {
  const elements = {};

  function makeEl(id) {
    const children = [];
    let _innerHTML = "";
    let _textContent = "";
    const el = {
      id,
      get innerHTML() {
        return _innerHTML;
      },
      set innerHTML(v) {
        _innerHTML = v;
        children.length = 0;
        if (v) {
          const matches = v.match(/<div\b[^>]*>/g) || [];
          for (const m of matches) {
            const cls = (m.match(/class="([^"]*)"/) || [])[1] || "";
            children.push({
              tagName: "div",
              className: cls,
              style: {},
              parentNode: el,
            });
          }
        }
      },
      get textContent() {
        return _textContent;
      },
      set textContent(v) {
        _textContent = v;
      },
      scrollHeight: 200,
      scrollTop: 200,
      clientHeight: 100,
      style: {},
      get children() {
        return children;
      },
      get firstChild() {
        return children[0] || null;
      },
      appendChild(child) {
        if (child && child._isFragment) {
          for (const c of child._children) {
            c.parentNode = el;
            children.push(c);
          }
        } else if (child != null) {
          child.parentNode = el;
          children.push(child);
        }
      },
      removeChild(child) {
        const idx = children.indexOf(child);
        if (idx !== -1) children.splice(idx, 1);
      },
      insertBefore(newNode, refNode) {
        const idx = children.indexOf(refNode);
        if (idx !== -1) {
          children.splice(idx, 0, newNode);
        } else {
          children.unshift(newNode);
        }
      },
      querySelector(selector) {
        if (selector.startsWith("#")) {
          const qid = selector.slice(1);
          return elements[qid] || null;
        }
        if (selector.startsWith(".")) {
          const cls = selector.slice(1);
          return (
            children.find(
              (c) => c.className && c.className.split(" ").includes(cls),
            ) || null
          );
        }
        return null;
      },
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
    };
    elements[id] = el;
    return el;
  }

  const windowOpenSpy = vi.fn();

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    Map,
    AbortController: class {
      constructor() {
        this.signal = {};
      }
      abort() {}
    },
    TextDecoder: class {
      decode(v) {
        return String(v || "");
      }
    },
    fetch: overrides.fetch || (() => Promise.reject(new Error("not mocked"))),
    setTimeout: overrides.setTimeout || (() => {}),
    clearTimeout: () => {},
    requestAnimationFrame: (cb) => {
      cb();
    },
    NodeFilter: { SHOW_TEXT: 4 },
    window: { open: windowOpenSpy },
    document: {
      getElementById: (id) => {
        if (!elements[id]) makeEl(id);
        return elements[id];
      },
      createTreeWalker: (_root, _filter) => {
        // Simulate a tree walker that returns text nodes
        const textNodes = overrides.textNodes || [];
        let idx = 0;
        return {
          nextNode: () => {
            return idx < textNodes.length ? textNodes[idx++] : null;
          },
        };
      },
      createElement: (tag) => ({
        tagName: tag,
        id: "",
        innerHTML: "",
        style: {},
        parentNode: null,
        className: "",
        children: [],
        querySelector: () => null,
        addEventListener: () => {},
      }),
      createRange: () => ({
        createContextualFragment(html) {
          const matches = html.match(/<div\b[^>]*>/g) || [];
          const frChildren = matches.map((m) => {
            const cls = (m.match(/class="([^"]*)"/) || [])[1] || "";
            return {
              tagName: "div",
              className: cls,
              style: {},
              parentNode: null,
            };
          });
          return { _isFragment: true, _children: frChildren };
        },
      }),
    },
    _modalState: { seq: 0, taskId: null, abort: null },
    tasks: [],
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
    cardOversightCache: new Map(),
    renderPrettyLogs: (buf) =>
      buf
        .split("\n")
        .filter((l) => l.trim())
        .map((l) => `<div class="pretty">${l}</div>`)
        .join(""),
    renderOversightInLogs: vi.fn(),
    renderTestOversightInTestLogs: vi.fn(),
    escapeHtml: (s) => String(s ?? ""),
    withAuthToken: (url) => url,
    withBearerHeaders: () => ({}),
    _stopTimelineRefresh: () => {},
    _startTimelineRefresh: () => {},
    renderTimeline: () => {},
    loadFlamegraph: () => {},
    history: { replaceState: () => {} },
    scheduleRender: vi.fn(),
    apiGet: overrides.apiGet || undefined,
  });

  ctx.getOpenModalTaskId = function () {
    return ctx._modalState.taskId;
  };
  loadScript("modal-logs.js", ctx);
  return { ctx, elements, windowOpenSpy };
}

// ---------------------------------------------------------------------------
// _isCurrentModalSeq
// ---------------------------------------------------------------------------
describe("_isCurrentModalSeq", () => {
  it("returns true when seq is not a number", () => {
    const { ctx } = makeLogsContext();
    expect(ctx._isCurrentModalSeq(undefined)).toBe(true);
    expect(ctx._isCurrentModalSeq("abc")).toBe(true);
    expect(ctx._isCurrentModalSeq(null)).toBe(true);
  });

  it("returns true when seq matches _modalState.seq", () => {
    const { ctx } = makeLogsContext();
    vm.runInContext("_modalState.seq = 5", ctx);
    expect(ctx._isCurrentModalSeq(5)).toBe(true);
  });

  it("returns false when seq does not match _modalState.seq", () => {
    const { ctx } = makeLogsContext();
    vm.runInContext("_modalState.seq = 5", ctx);
    expect(ctx._isCurrentModalSeq(3)).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// _modalApiJson
// ---------------------------------------------------------------------------
describe("_modalApiJson", () => {
  it("uses apiGet when available", async () => {
    const apiGetFn = vi.fn().mockResolvedValue({ status: "ready" });
    const { ctx } = makeLogsContext({ apiGet: apiGetFn });
    const result = await ctx._modalApiJson("/api/test", null);
    expect(apiGetFn).toHaveBeenCalledWith("/api/test", { signal: null });
    expect(result).toEqual({ status: "ready" });
  });

  it("falls back to fetch when apiGet is not a function", async () => {
    const fetchFn = vi.fn().mockResolvedValue({
      json: () => Promise.resolve({ ok: true }),
    });
    const { ctx } = makeLogsContext({ fetch: fetchFn, apiGet: undefined });
    // Remove apiGet so the fallback is used
    vm.runInContext("apiGet = undefined", ctx);
    const result = await ctx._modalApiJson("/api/test", null);
    expect(fetchFn).toHaveBeenCalledWith("/api/test", { signal: null });
    expect(result).toEqual({ ok: true });
  });
});

// ---------------------------------------------------------------------------
// _updateServerTruncationBanner
// ---------------------------------------------------------------------------
describe("_updateServerTruncationBanner", () => {
  it("hides banner in oversight mode", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "oversight"', ctx);
    // Create a pre-existing banner
    const section = elements["modal-logs-section"];
    if (!section) ctx.document.getElementById("modal-logs-section");
    const banner = { id: "server-truncation-notice", style: {} };
    elements["server-truncation-notice"] = banner;
    const origGetById = ctx.document.getElementById;
    ctx.document.getElementById = (id) => {
      if (id === "server-truncation-notice") return banner;
      return origGetById(id);
    };
    ctx._updateServerTruncationBanner();
    expect(banner.style.display).toBe("none");
  });

  it("hides banner when no truncation sentinel in buffer", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "pretty"', ctx);
    vm.runInContext('rawLogBuffer = "normal content"', ctx);
    const banner = { id: "server-truncation-notice", style: {} };
    elements["server-truncation-notice"] = banner;
    const origGetById = ctx.document.getElementById;
    ctx.document.getElementById = (id) => {
      if (id === "server-truncation-notice") return banner;
      return origGetById(id);
    };
    ctx._updateServerTruncationBanner();
    expect(banner.style.display).toBe("none");
  });

  it("shows existing banner when truncation sentinel is present", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "pretty"', ctx);
    vm.runInContext('rawLogBuffer = \'{"subtype":"truncation_notice"}\'', ctx);
    const banner = {
      id: "server-truncation-notice",
      style: { display: "none" },
    };
    elements["server-truncation-notice"] = banner;
    const origGetById = ctx.document.getElementById;
    ctx.document.getElementById = (id) => {
      if (id === "server-truncation-notice") return banner;
      return origGetById(id);
    };
    ctx._updateServerTruncationBanner();
    expect(banner.style.display).toBe("");
  });

  it("creates banner when truncation sentinel is present and no existing banner", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "pretty"', ctx);
    vm.runInContext('rawLogBuffer = \'{"subtype":"truncation_notice"}\'', ctx);
    // Ensure no existing banner
    const origGetById = ctx.document.getElementById;
    ctx.document.getElementById = (id) => {
      if (id === "server-truncation-notice") return null;
      return origGetById(id);
    };
    // The section and logsEl should exist
    const section = origGetById("modal-logs-section");
    const logsEl = origGetById("modal-logs");
    // insertBefore should be called on section
    const insertSpy = vi.spyOn(section, "insertBefore");
    ctx._updateServerTruncationBanner();
    expect(insertSpy).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// renderLogs — oversight mode
// ---------------------------------------------------------------------------
describe("renderLogs — oversight mode", () => {
  it("delegates to renderOversightInLogs and invalidates render cursor", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "oversight"', ctx);
    vm.runInContext('rawLogBuffer = "some data"', ctx);
    vm.runInContext("_renderedLogLen = 100", ctx);
    ctx.renderLogs();
    expect(ctx.renderOversightInLogs).toHaveBeenCalled();
    expect(vm.runInContext("_renderedLogMode", ctx)).toBe("oversight");
    expect(vm.runInContext("_renderedLogLen", ctx)).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// renderLogs — search bar visibility
// ---------------------------------------------------------------------------
describe("renderLogs — search bar visibility", () => {
  it("hides search bar in oversight mode", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "oversight"', ctx);
    ctx.renderLogs();
    const searchBar = elements["log-search-bar"];
    expect(searchBar.style.display).toBe("none");
  });

  it("shows search bar in pretty mode", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "pretty"', ctx);
    vm.runInContext('rawLogBuffer = "data"', ctx);
    ctx.renderLogs();
    const searchBar = elements["log-search-bar"];
    expect(searchBar.style.display).toBe("flex");
  });
});

// ---------------------------------------------------------------------------
// renderLogs — raw mode with search query
// ---------------------------------------------------------------------------
describe("renderLogs — raw mode search", () => {
  it("filters lines and shows count in raw mode", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "raw"', ctx);
    vm.runInContext('rawLogBuffer = "foo line\\nbar line\\nfoo baz"', ctx);
    vm.runInContext('logSearchQuery = "foo"', ctx);
    ctx.renderLogs();
    const logsEl = elements["modal-logs"];
    expect(logsEl.textContent).toContain("foo");
    expect(logsEl.textContent).not.toContain("bar");
    expect(elements["log-search-count"].textContent).toBe("2 / 3 lines");
  });

  it("shows all lines in raw mode without search", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "raw"', ctx);
    vm.runInContext('rawLogBuffer = "line1\\nline2"', ctx);
    vm.runInContext('logSearchQuery = ""', ctx);
    ctx.renderLogs();
    const logsEl = elements["modal-logs"];
    expect(logsEl.textContent).toContain("line1");
    expect(logsEl.textContent).toContain("line2");
    expect(elements["log-search-count"].textContent).toBe("");
  });
});

// ---------------------------------------------------------------------------
// renderTestLogs — oversight mode
// ---------------------------------------------------------------------------
describe("renderTestLogs — oversight mode", () => {
  it("delegates to renderTestOversightInTestLogs", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('testLogsMode = "oversight"', ctx);
    ctx.renderTestLogs();
    expect(ctx.renderTestOversightInTestLogs).toHaveBeenCalled();
    expect(
      elements["modal-test-logs"].classList.contains("oversight-mode"),
    ).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// highlightLogMatches
// ---------------------------------------------------------------------------
describe("highlightLogMatches", () => {
  it("does nothing when query is empty", () => {
    const { ctx } = makeLogsContext();
    // Should not throw
    expect(() => ctx.highlightLogMatches("")).not.toThrow();
  });

  it("wraps matching text in mark tags", () => {
    // Create a text node that the tree walker will return
    let replacedWith = null;
    const textNode = {
      textContent: "hello world hello",
      parentNode: {
        replaceChild: (newNode, _oldNode) => {
          replacedWith = newNode;
        },
      },
    };
    const { ctx } = makeLogsContext({ textNodes: [textNode] });
    ctx.highlightLogMatches("hello");
    expect(replacedWith).not.toBeNull();
    expect(replacedWith.innerHTML).toContain("<mark");
    expect(replacedWith.innerHTML).toContain("hello");
  });

  it("does not replace nodes that do not match", () => {
    let replaceCount = 0;
    const textNode = {
      textContent: "no match here",
      parentNode: {
        replaceChild: () => {
          replaceCount++;
        },
      },
    };
    const { ctx } = makeLogsContext({ textNodes: [textNode] });
    ctx.highlightLogMatches("xyz");
    expect(replaceCount).toBe(0);
  });
});

// ---------------------------------------------------------------------------
// _downloadFullLog
// ---------------------------------------------------------------------------
describe("_downloadFullLog", () => {
  it("opens log URL in new window when task is open", () => {
    const { ctx, windowOpenSpy } = makeLogsContext();
    vm.runInContext('_modalState.taskId = "task-123"', ctx);
    ctx._downloadFullLog();
    expect(windowOpenSpy).toHaveBeenCalledWith(
      "/api/tasks/task-123/logs?raw=true",
    );
  });

  it("does nothing when no task is open", () => {
    const { ctx, windowOpenSpy } = makeLogsContext();
    vm.runInContext("_modalState.taskId = null", ctx);
    ctx._downloadFullLog();
    expect(windowOpenSpy).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// _fetchLogs — guard conditions
// ---------------------------------------------------------------------------
describe("_fetchLogs — guard conditions", () => {
  it("bails out when modal is closed (different task)", () => {
    const fetchFn = vi.fn();
    const { ctx } = makeLogsContext({ fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-A"', ctx);
    // Call _fetchLogs for a different task
    ctx._fetchLogs("task-B", null, 0);
    expect(fetchFn).not.toHaveBeenCalled();
  });

  it("bails out when seq does not match", () => {
    const fetchFn = vi.fn();
    const { ctx } = makeLogsContext({ fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-A"', ctx);
    vm.runInContext("_modalState.seq = 5", ctx);
    // Call with wrong seq
    ctx._fetchLogs("task-A", null, 3);
    expect(fetchFn).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// _fetchTestLogs — guard conditions
// ---------------------------------------------------------------------------
describe("_fetchTestLogs — guard conditions", () => {
  it("bails out when modal is closed (different task)", () => {
    const fetchFn = vi.fn();
    const { ctx } = makeLogsContext({ fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-A"', ctx);
    ctx._fetchTestLogs("task-B", null, 0);
    expect(fetchFn).not.toHaveBeenCalled();
  });

  it("bails out when seq does not match", () => {
    const fetchFn = vi.fn();
    const { ctx } = makeLogsContext({ fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-A"', ctx);
    vm.runInContext("_modalState.seq = 5", ctx);
    ctx._fetchTestLogs("task-A", null, 3);
    expect(fetchFn).not.toHaveBeenCalled();
  });

  it("uses phase=test URL for completed tasks", () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      body: {
        getReader: () => ({
          read: () => Promise.resolve({ done: true }),
        }),
      },
    });
    const { ctx } = makeLogsContext({ fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-A"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    // Task is done (not running)
    vm.runInContext('tasks = [{ id: "task-A", status: "done" }]', ctx);
    ctx._fetchTestLogs("task-A", null, 0);
    expect(fetchFn).toHaveBeenCalled();
    const url = fetchFn.mock.calls[0][0];
    expect(url).toContain("phase=test");
  });

  it("uses raw=true URL for running tasks", () => {
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      body: {
        getReader: () => ({
          read: () => Promise.resolve({ done: true }),
        }),
      },
    });
    const { ctx } = makeLogsContext({ fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-A"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    vm.runInContext('tasks = [{ id: "task-A", status: "in_progress" }]', ctx);
    ctx._fetchTestLogs("task-A", null, 0);
    expect(fetchFn).toHaveBeenCalled();
    const url = fetchFn.mock.calls[0][0];
    expect(url).toContain("raw=true");
  });
});

// ---------------------------------------------------------------------------
// startLogStream — oversight pre-fetch
// ---------------------------------------------------------------------------
describe("startLogStream", () => {
  it("switches to oversight mode when oversight data is ready", async () => {
    const apiGetFn = vi
      .fn()
      .mockResolvedValue({ status: "ready", phase_count: 3, phases: ["a"] });
    const fetchFn = vi.fn().mockRejectedValue(new Error("not needed"));
    const { ctx } = makeLogsContext({ apiGet: apiGetFn, fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    vm.runInContext("_modalState.abort = { signal: {} }", ctx);
    ctx.startLogStream("task-1", 0);
    // Wait for the async oversight fetch to settle
    await new Promise((r) => setTimeout(r, 10));
    expect(vm.runInContext("logsMode", ctx)).toBe("oversight");
    expect(vm.runInContext("oversightFetching", ctx)).toBe(false);
  });

  it("stays in pretty mode when oversight is not ready", async () => {
    const apiGetFn = vi.fn().mockResolvedValue({ status: "pending" });
    const fetchFn = vi.fn().mockRejectedValue(new Error("not needed"));
    const { ctx } = makeLogsContext({ apiGet: apiGetFn, fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    ctx.startLogStream("task-1", 0);
    await new Promise((r) => setTimeout(r, 10));
    expect(vm.runInContext("logsMode", ctx)).toBe("pretty");
  });

  it("handles oversight fetch error gracefully", async () => {
    const apiGetFn = vi.fn().mockRejectedValue(new Error("network error"));
    const fetchFn = vi.fn().mockRejectedValue(new Error("not needed"));
    const { ctx } = makeLogsContext({ apiGet: apiGetFn, fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    ctx.startLogStream("task-1", 0);
    await new Promise((r) => setTimeout(r, 10));
    expect(vm.runInContext("oversightFetching", ctx)).toBe(false);
    expect(vm.runInContext("logsMode", ctx)).toBe("pretty");
  });

  it("ignores oversight result if task changed", async () => {
    const apiGetFn = vi
      .fn()
      .mockImplementation(
        () =>
          new Promise((resolve) =>
            setTimeout(
              () => resolve({ status: "ready", phase_count: 2, phases: [] }),
              5,
            ),
          ),
      );
    const fetchFn = vi.fn().mockRejectedValue(new Error("not needed"));
    const { ctx } = makeLogsContext({ apiGet: apiGetFn, fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    ctx.startLogStream("task-1", 0);
    // Change open task before the promise resolves
    vm.runInContext('_modalState.taskId = "task-2"', ctx);
    await new Promise((r) => setTimeout(r, 20));
    // logsMode should stay pretty since the task changed
    expect(vm.runInContext("logsMode", ctx)).toBe("pretty");
  });
});

// ---------------------------------------------------------------------------
// startTestLogStream — oversight pre-fetch
// ---------------------------------------------------------------------------
describe("startTestLogStream", () => {
  it("switches to oversight mode when test oversight is ready", async () => {
    const apiGetFn = vi.fn().mockResolvedValue({ status: "ready" });
    const fetchFn = vi.fn().mockRejectedValue(new Error("not needed"));
    const { ctx } = makeLogsContext({ apiGet: apiGetFn, fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    vm.runInContext("_modalState.abort = { signal: {} }", ctx);
    ctx.startTestLogStream("task-1", 0);
    await new Promise((r) => setTimeout(r, 10));
    expect(vm.runInContext("testLogsMode", ctx)).toBe("oversight");
    expect(vm.runInContext("testOversightFetching", ctx)).toBe(false);
  });

  it("stays in pretty mode when test oversight is not ready", async () => {
    const apiGetFn = vi.fn().mockResolvedValue({ status: "pending" });
    const fetchFn = vi.fn().mockRejectedValue(new Error("not needed"));
    const { ctx } = makeLogsContext({ apiGet: apiGetFn, fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    ctx.startTestLogStream("task-1", 0);
    await new Promise((r) => setTimeout(r, 10));
    expect(vm.runInContext("testLogsMode", ctx)).toBe("pretty");
  });

  it("handles test oversight fetch AbortError gracefully", async () => {
    const abortErr = new Error("aborted");
    abortErr.name = "AbortError";
    const apiGetFn = vi.fn().mockRejectedValue(abortErr);
    const fetchFn = vi.fn().mockRejectedValue(new Error("not needed"));
    const { ctx } = makeLogsContext({ apiGet: apiGetFn, fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    ctx.startTestLogStream("task-1", 0);
    await new Promise((r) => setTimeout(r, 10));
    // oversightFetching stays true because AbortError does not clear it
    // (the abort branch returns early without setting oversightFetching = false)
    expect(vm.runInContext("testLogsMode", ctx)).toBe("pretty");
  });
});

// ---------------------------------------------------------------------------
// startImplLogFetch — oversight pre-fetch + fetch impl logs
// ---------------------------------------------------------------------------
describe("startImplLogFetch", () => {
  it("switches to oversight mode when oversight is ready", async () => {
    const apiGetFn = vi
      .fn()
      .mockResolvedValue({ status: "ready", phase_count: 2, phases: ["a"] });
    const fetchFn = vi.fn().mockResolvedValue({
      ok: true,
      body: {
        getReader: () => ({
          read: () => Promise.resolve({ done: true }),
        }),
      },
    });
    const { ctx } = makeLogsContext({ apiGet: apiGetFn, fetch: fetchFn });
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    vm.runInContext("_modalState.abort = { signal: {} }", ctx);
    ctx.startImplLogFetch("task-1", 0);
    await new Promise((r) => setTimeout(r, 10));
    expect(vm.runInContext("logsMode", ctx)).toBe("oversight");
  });

  it("resets render cursor and buffer", () => {
    const apiGetFn = vi.fn().mockResolvedValue({ status: "pending" });
    const fetchFn = vi.fn().mockResolvedValue({
      ok: false,
      body: null,
    });
    const { ctx, elements } = makeLogsContext({
      apiGet: apiGetFn,
      fetch: fetchFn,
    });
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext("_modalState.seq = 0", ctx);
    vm.runInContext('rawLogBuffer = "old data"', ctx);
    vm.runInContext("_renderedLogLen = 50", ctx);
    ctx.startImplLogFetch("task-1", 0);
    expect(vm.runInContext("rawLogBuffer", ctx)).toBe("");
    expect(vm.runInContext("_renderedLogLen", ctx)).toBe(0);
    expect(elements["modal-logs"].innerHTML).toBe("");
  });
});

// ---------------------------------------------------------------------------
// renderLogs — buffer reset triggers full rebuild
// ---------------------------------------------------------------------------
describe("renderLogs — buffer shorter than renderedLen triggers rebuild", () => {
  it("rebuilds when buffer is shorter than last rendered length", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "pretty"', ctx);
    vm.runInContext('rawLogBuffer = "long buffer content"', ctx);
    ctx.renderLogs();
    const firstLen = vm.runInContext("_renderedLogLen", ctx);
    expect(firstLen).toBeGreaterThan(0);

    // Simulate buffer reset (shorter than before)
    vm.runInContext('rawLogBuffer = "short"', ctx);
    ctx.renderLogs();
    expect(vm.runInContext("_renderedLogLen", ctx)).toBe(5);
  });
});

// ---------------------------------------------------------------------------
// onLogSearchInput — raw mode
// ---------------------------------------------------------------------------
describe("onLogSearchInput — raw mode", () => {
  it("filters in raw mode and shows count", () => {
    const { ctx, elements } = makeLogsContext();
    vm.runInContext('logsMode = "raw"', ctx);
    vm.runInContext('rawLogBuffer = "alpha\\nbeta\\nalpha again"', ctx);
    ctx.onLogSearchInput("alpha");
    expect(elements["log-search-count"].textContent).toBe("2 / 3 lines");
    expect(elements["modal-logs"].textContent).toContain("alpha");
    expect(elements["modal-logs"].textContent).not.toContain("beta");
  });
});
