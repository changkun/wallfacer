/**
 * Tests for modal-logs.js — log rendering, tab switching, and mode management.
 *
 * The streaming functions (_fetchLogs, _fetchTestLogs, startImplLogFetch) are
 * not tested here because they require live fetch/ReadableStream APIs.
 * The tab-switching and mode-setting functions are fully testable with DOM stubs.
 */
import { describe, it, expect, beforeEach } from "vitest";
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
 * Tab elements are tracked in the `elements` map so tests can assert on them.
 */
function makeLogsContext() {
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
        // Rebuild children array from <div> tags in the HTML.
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

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    AbortController: class {
      abort() {}
      signal = {};
    },
    TextDecoder: class {
      decode(v) {
        return String(v || "");
      }
    },
    fetch: () => Promise.reject(new Error("not mocked")),
    setTimeout: () => {},
    clearTimeout: () => {},
    requestAnimationFrame: (cb) => {
      cb();
    },
    NodeFilter: { SHOW_TEXT: 4 },
    window: { open: () => {} },
    document: {
      getElementById: (id) => {
        if (!elements[id]) makeEl(id);
        return elements[id];
      },
      createTreeWalker: () => ({ nextNode: () => null }),
      createElement: (tag) => {
        const el = {
          tagName: tag,
          innerHTML: "",
          style: {},
          parentNode: null,
          className: "",
          children: [],
        };
        return el;
      },
      createRange: () => ({
        createContextualFragment(html) {
          // Parse <div> elements from the HTML string to simulate a real fragment.
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
    // Runtime dependencies from other modules
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
    // Return one <div class="pretty"> per non-empty line so children can be counted.
    renderPrettyLogs: (buf) =>
      buf
        .split("\n")
        .filter((l) => l.trim())
        .map((l) => `<div class="pretty">${l}</div>`)
        .join(""),
    renderOversightInLogs: () => {},
    renderTestOversightInTestLogs: () => {},
    escapeHtml: (s) => String(s ?? ""),
    // Auth helpers used by _fetchLogs / _fetchTestLogs
    withAuthToken: (url) => url,
    withBearerHeaders: () => ({}),
    // Timeline helpers called by setRightTab
    _stopTimelineRefresh: () => {},
    _startTimelineRefresh: () => {},
    renderTimeline: () => {},
    loadFlamegraph: () => {},
    history: { replaceState: () => {} },
  });
  // setRightTab now delegates to setMainTab (main tab) and setLeftTab
  // (Impl/Testing sub-tab inside Activity). Expose spies so the setRightTab
  // suite can assert on the delegation instead of poking the old
  // right-tab-*/right-panel-* DOM that was removed. Arrays live on the ctx
  // object so closures captured when the vm script runs see the same list.
  ctx._setMainTabCalls = [];
  ctx._setLeftTabCalls = [];
  ctx.setMainTab = (tab) => {
    ctx._setMainTabCalls.push(tab);
  };
  ctx.setLeftTab = (tab) => {
    ctx._setLeftTabCalls.push(tab);
  };

  ctx.getOpenModalTaskId = function () {
    return ctx._modalState.taskId;
  };
  loadScript("modal-logs.js", ctx);
  return { ctx, elements };
}

// ---------------------------------------------------------------------------
// setRightTab
// ---------------------------------------------------------------------------
describe("setRightTab", () => {
  let ctx;
  beforeEach(() => ({ ctx } = makeLogsContext()));

  it("routes implementation to activity main tab + impl sub-tab", () => {
    ctx.setRightTab("implementation");
    expect(ctx._setMainTabCalls).toEqual(["activity"]);
    expect(ctx._setLeftTabCalls).toEqual(["implementation"]);
  });

  it("routes testing to activity main tab + testing sub-tab", () => {
    ctx.setRightTab("testing");
    expect(ctx._setMainTabCalls).toEqual(["activity"]);
    expect(ctx._setLeftTabCalls).toEqual(["testing"]);
  });

  it("routes changes to the changes main tab, no sub-tab change", () => {
    ctx.setRightTab("changes");
    expect(ctx._setMainTabCalls).toEqual(["changes"]);
    expect(ctx._setLeftTabCalls).toEqual([]);
  });

  it("routes spans to flamegraph and timeline to timeline", () => {
    ctx.setRightTab("spans");
    ctx.setRightTab("timeline");
    expect(ctx._setMainTabCalls).toEqual(["flamegraph", "timeline"]);
  });

  it("switching between Impl and Testing swaps the sub-tab", () => {
    ctx.setRightTab("implementation");
    ctx.setRightTab("testing");
    expect(ctx._setLeftTabCalls).toEqual(["implementation", "testing"]);
    // Both calls stay on the activity main tab.
    expect(ctx._setMainTabCalls).toEqual(["activity", "activity"]);
  });
});

// ---------------------------------------------------------------------------
// _updateLogsTabs
// ---------------------------------------------------------------------------
describe("_updateLogsTabs", () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it("marks the current logsMode tab as active", () => {
    vm.runInContext('logsMode = "pretty"', ctx);
    ctx._updateLogsTabs();
    expect(elements["logs-tab-pretty"].classList.contains("active")).toBe(true);
    expect(elements["logs-tab-oversight"].classList.contains("active")).toBe(
      false,
    );
    expect(elements["logs-tab-raw"].classList.contains("active")).toBe(false);
  });

  it("marks the oversight tab as active when logsMode is oversight", () => {
    vm.runInContext('logsMode = "oversight"', ctx);
    ctx._updateLogsTabs();
    expect(elements["logs-tab-oversight"].classList.contains("active")).toBe(
      true,
    );
    expect(elements["logs-tab-pretty"].classList.contains("active")).toBe(
      false,
    );
  });

  it("marks the raw tab as active when logsMode is raw", () => {
    vm.runInContext('logsMode = "raw"', ctx);
    ctx._updateLogsTabs();
    expect(elements["logs-tab-raw"].classList.contains("active")).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// _updateTestLogsTabs
// ---------------------------------------------------------------------------
describe("_updateTestLogsTabs", () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it("marks the current testLogsMode tab as active", () => {
    vm.runInContext('testLogsMode = "pretty"', ctx);
    ctx._updateTestLogsTabs();
    expect(elements["test-logs-tab-pretty"].classList.contains("active")).toBe(
      true,
    );
    expect(
      elements["test-logs-tab-oversight"].classList.contains("active"),
    ).toBe(false);
  });

  it("marks the oversight tab as active when testLogsMode is oversight", () => {
    vm.runInContext('testLogsMode = "oversight"', ctx);
    ctx._updateTestLogsTabs();
    expect(
      elements["test-logs-tab-oversight"].classList.contains("active"),
    ).toBe(true);
  });

  it("marks raw tab as active when testLogsMode is raw", () => {
    vm.runInContext('testLogsMode = "raw"', ctx);
    ctx._updateTestLogsTabs();
    expect(elements["test-logs-tab-raw"].classList.contains("active")).toBe(
      true,
    );
  });
});

// ---------------------------------------------------------------------------
// setLogsMode
// ---------------------------------------------------------------------------
describe("setLogsMode", () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it("updates logsMode to pretty and triggers renderLogs", () => {
    vm.runInContext('logsMode = "oversight"', ctx);
    vm.runInContext('rawLogBuffer = "test content"', ctx);
    ctx.setLogsMode("pretty");
    expect(vm.runInContext("logsMode", ctx)).toBe("pretty");
    // renderLogs was called: the modal-logs element should have been updated
    const logsEl = elements["modal-logs"];
    expect(logsEl.innerHTML).toContain("pretty");
  });

  it("updates logsMode to raw and strips ANSI", () => {
    vm.runInContext('rawLogBuffer = "\\x1b[31mred\\x1b[0m plain"', ctx);
    ctx.setLogsMode("raw");
    expect(vm.runInContext("logsMode", ctx)).toBe("raw");
    const logsEl = elements["modal-logs"];
    // Raw mode uses textContent (no ANSI codes)
    expect(logsEl.textContent).toContain("plain");
    expect(logsEl.textContent).not.toContain("\x1b");
  });
});

// ---------------------------------------------------------------------------
// setTestLogsMode
// ---------------------------------------------------------------------------
describe("setTestLogsMode", () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it("updates testLogsMode and triggers renderTestLogs", () => {
    vm.runInContext('testLogsMode = "oversight"', ctx);
    vm.runInContext('testRawLogBuffer = "test log"', ctx);
    ctx.setTestLogsMode("pretty");
    expect(vm.runInContext("testLogsMode", ctx)).toBe("pretty");
    const logsEl = elements["modal-test-logs"];
    expect(logsEl.innerHTML).toContain("test log");
  });

  it("updates testLogsMode to raw", () => {
    vm.runInContext('testRawLogBuffer = "raw content"', ctx);
    ctx.setTestLogsMode("raw");
    expect(vm.runInContext("testLogsMode", ctx)).toBe("raw");
    const logsEl = elements["modal-test-logs"];
    expect(logsEl.textContent).toContain("raw content");
  });
});

// ---------------------------------------------------------------------------
// renderLogs (pretty and raw modes only; oversight delegates to other module)
// ---------------------------------------------------------------------------
describe("renderLogs", () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it("renders pretty logs via renderPrettyLogs stub", () => {
    vm.runInContext('logsMode = "pretty"', ctx);
    vm.runInContext('rawLogBuffer = "my log buffer"', ctx);
    ctx.renderLogs();
    expect(elements["modal-logs"].innerHTML).toContain("my log buffer");
  });

  it("strips ANSI codes and sets textContent in raw mode", () => {
    vm.runInContext('logsMode = "raw"', ctx);
    vm.runInContext('rawLogBuffer = "\\x1b[1mBold\\x1b[0m normal"', ctx);
    ctx.renderLogs();
    const logsEl = elements["modal-logs"];
    expect(logsEl.textContent).not.toContain("\x1b");
    expect(logsEl.textContent).toContain("Bold");
    expect(logsEl.textContent).toContain("normal");
  });

  it("toggles oversight-mode class on the logs element", () => {
    vm.runInContext('logsMode = "pretty"', ctx);
    ctx.renderLogs();
    expect(elements["modal-logs"].classList.contains("oversight-mode")).toBe(
      false,
    );
  });
});

// ---------------------------------------------------------------------------
// renderTestLogs (pretty and raw modes)
// ---------------------------------------------------------------------------
describe("renderTestLogs", () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it("renders pretty test logs", () => {
    vm.runInContext('testLogsMode = "pretty"', ctx);
    vm.runInContext('testRawLogBuffer = "test output"', ctx);
    ctx.renderTestLogs();
    expect(elements["modal-test-logs"].innerHTML).toContain("test output");
  });

  it("strips ANSI codes in raw test mode", () => {
    vm.runInContext('testLogsMode = "raw"', ctx);
    vm.runInContext('testRawLogBuffer = "\\x1b[32mgreen\\x1b[0m text"', ctx);
    ctx.renderTestLogs();
    const logsEl = elements["modal-test-logs"];
    expect(logsEl.textContent).not.toContain("\x1b");
    expect(logsEl.textContent).toContain("green");
  });

  it("toggles oversight-mode class in oversight mode", () => {
    vm.runInContext('testLogsMode = "oversight"', ctx);
    ctx.renderTestLogs();
    expect(
      elements["modal-test-logs"].classList.contains("oversight-mode"),
    ).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// onLogSearchInput
// ---------------------------------------------------------------------------
describe("onLogSearchInput", () => {
  let ctx, elements;
  beforeEach(() => {
    ({ ctx, elements } = makeLogsContext());
    vm.runInContext('logsMode = "pretty"', ctx);
  });

  it("empty query renders all lines and clears count", () => {
    vm.runInContext('rawLogBuffer = "line one\\nline two\\nline three"', ctx);
    ctx.onLogSearchInput("");
    // renderPrettyLogs stub wraps in class="pretty"; with no filter the full buffer is passed
    expect(elements["modal-logs"].innerHTML).toContain("pretty");
    expect(elements["log-search-count"].textContent).toBe("");
  });

  it("non-empty query filters lines and shows match count", () => {
    // 3 lines, 2 contain 'foo'
    vm.runInContext('rawLogBuffer = "foo line\\nbar line\\nfoo baz"', ctx);
    ctx.onLogSearchInput("foo");
    expect(elements["log-search-count"].textContent).toBe("2 / 3 lines");
  });

  it("handles regex-special characters without throwing", () => {
    vm.runInContext('rawLogBuffer = "some content"', ctx);
    expect(() => ctx.onLogSearchInput("foo(bar[baz")).not.toThrow();
    // query didn't match anything → 0 / 1 lines
    expect(elements["log-search-count"].textContent).toMatch(
      /\d+ \/ \d+ lines/,
    );
  });

  it("count exactly matches filtered line count", () => {
    // 5 lines, 3 contain 'target'
    vm.runInContext(
      'rawLogBuffer = "target one\\nno match\\ntarget two\\nno match\\ntarget three"',
      ctx,
    );
    ctx.onLogSearchInput("target");
    expect(elements["log-search-count"].textContent).toBe("3 / 5 lines");
  });
});

// ---------------------------------------------------------------------------
// renderLogs — append-only path and line cap
// ---------------------------------------------------------------------------
describe("renderLogs append-only", () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it("appends incrementally and enforces MAX_LOG_LINES cap", () => {
    const logsEl =
      elements["modal-logs"] || ctx.document.getElementById("modal-logs");

    // Step 1: Feed 500 lines. First call triggers full-rebuild (cursor starts at '').
    const lines500 = Array.from({ length: 500 }, (_, i) => "line " + i).join(
      "\n",
    );
    vm.runInContext("rawLogBuffer = " + JSON.stringify(lines500), ctx);
    ctx.renderLogs();

    expect(logsEl.children.length).toBe(500);
    expect(vm.runInContext("_renderedLogLen", ctx)).toBe(lines500.length);

    // Mark the first child so we can confirm it is not re-created on the next call.
    logsEl.children[0]._sentinel = "preserved";

    // Step 2: Append 50 more lines. Second call must use the append-only path.
    const extra50 =
      "\n" + Array.from({ length: 50 }, (_, i) => "extra " + i).join("\n");
    vm.runInContext("rawLogBuffer += " + JSON.stringify(extra50), ctx);
    ctx.renderLogs();

    expect(logsEl.children.length).toBe(550);
    expect(vm.runInContext("_renderedLogLen", ctx)).toBe(
      lines500.length + extra50.length,
    );
    // The first child from step 1 must still be the same object — not re-created.
    expect(logsEl.children[0]._sentinel).toBe("preserved");

    // Step 3: Feed enough additional lines to exceed MAX_LOG_LINES (10 000).
    // We already have 550 rendered; add 9 451 more to tip over the cap.
    const overflow =
      "\n" + Array.from({ length: 9451 }, (_, i) => "overflow " + i).join("\n");
    vm.runInContext("rawLogBuffer += " + JSON.stringify(overflow), ctx);
    ctx.renderLogs();

    // Must be capped at MAX_LOG_LINES log lines + 1 truncation notice.
    expect(logsEl.children.length).toBeLessThanOrEqual(10000 + 1);
  });

  it("does a full rebuild when the mode changes then switches back", () => {
    const buf = Array.from({ length: 10 }, (_, i) => "line " + i).join("\n");
    vm.runInContext("rawLogBuffer = " + JSON.stringify(buf), ctx);
    ctx.renderLogs(); // full rebuild (initial)

    // Switch to raw mode — must trigger a rebuild.
    ctx.setLogsMode("raw");
    expect(elements["modal-logs"].textContent).toContain("line 0");

    // Switch back to pretty — must trigger another full rebuild.
    ctx.setLogsMode("pretty");
    expect(elements["modal-logs"].children.length).toBe(10);
  });

  it("does a full rebuild when the search query changes", () => {
    const buf = "match one\nskip two\nmatch three";
    vm.runInContext("rawLogBuffer = " + JSON.stringify(buf), ctx);
    ctx.renderLogs(); // full rebuild

    // Activate a filter — renders only matching lines via innerHTML.
    ctx.onLogSearchInput("match");
    expect(elements["log-search-count"].textContent).toBe("2 / 3 lines");

    // Clear the filter — full rebuild back to all lines.
    ctx.onLogSearchInput("");
    expect(elements["modal-logs"].children.length).toBe(3);
  });

  it("preserves buffer on reconnect until first chunk arrives", () => {
    // Simulate initial _fetchLogs call (retryDelay=null): buffer is cleared.
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext('logsMode = "pretty"', ctx);
    vm.runInContext('rawLogBuffer = "line1\\nline2\\nline3"', ctx);
    ctx.renderLogs();
    expect(elements["modal-logs"].children.length).toBe(3);
    const renderedLen = vm.runInContext("_renderedLogLen", ctx);
    expect(renderedLen).toBe("line1\nline2\nline3".length);

    // Simulate reconnect: _fetchLogs is called with retryDelay (non-null).
    // The buffer is preserved so existing logs stay visible during reconnect.
    ctx._fetchLogs("task-1", 2000, 0);

    // After a reconnect _fetchLogs, the buffer is preserved until the first
    // chunk of the new stream arrives — logs remain visible during the delay.
    expect(vm.runInContext("rawLogBuffer", ctx)).toBe("line1\nline2\nline3");
    expect(vm.runInContext("_renderedLogLen", ctx)).toBe(
      "line1\nline2\nline3".length,
    );
    expect(elements["modal-logs"].children.length).toBe(3);
  });

  it("clears buffer on initial fetch", () => {
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext('logsMode = "pretty"', ctx);
    vm.runInContext('rawLogBuffer = "line1\\nline2\\nline3"', ctx);
    ctx.renderLogs();
    expect(elements["modal-logs"].children.length).toBe(3);

    // Initial fetch (retryDelay=null): buffer is cleared immediately.
    ctx._fetchLogs("task-1", null, 0);

    expect(vm.runInContext("rawLogBuffer", ctx)).toBe("");
    expect(vm.runInContext("_renderedLogLen", ctx)).toBe(0);
    expect(elements["modal-logs"].innerHTML).toBe("");
  });

  it("preserves test buffer on reconnect until first chunk arrives", () => {
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext('testRawLogBuffer = "test-line1\\ntest-line2"', ctx);
    ctx.renderTestLogs();
    expect(elements["modal-test-logs"].innerHTML).toContain("test-line1");

    // Reconnect (retryDelay non-null): buffer preserved.
    ctx._fetchTestLogs("task-1", 2000, 0);

    expect(vm.runInContext("testRawLogBuffer", ctx)).toBe(
      "test-line1\ntest-line2",
    );
    expect(elements["modal-test-logs"].innerHTML).toContain("test-line1");
  });

  it("clears test buffer on initial fetch", () => {
    vm.runInContext('_modalState.taskId = "task-1"', ctx);
    vm.runInContext('testRawLogBuffer = "test-line1\\ntest-line2"', ctx);
    ctx.renderTestLogs();
    expect(elements["modal-test-logs"].innerHTML).toContain("test-line1");

    // Initial fetch (retryDelay=null): buffer cleared immediately.
    ctx._fetchTestLogs("task-1", null, 0);

    expect(vm.runInContext("testRawLogBuffer", ctx)).toBe("");
    expect(elements["modal-test-logs"].innerHTML).toBe("");
  });

  it("skips re-render when no new data has arrived", () => {
    const buf = "line a\nline b";
    vm.runInContext("rawLogBuffer = " + JSON.stringify(buf), ctx);
    ctx.renderLogs(); // full rebuild

    const lenAfterFirst = vm.runInContext("_renderedLogLen", ctx);
    const childCount = elements["modal-logs"].children.length;

    // Render again with identical buffer — append-only path, newChunk is empty.
    ctx.renderLogs();
    expect(elements["modal-logs"].children.length).toBe(childCount);
    expect(vm.runInContext("_renderedLogLen", ctx)).toBe(lenAfterFirst);
  });
});
