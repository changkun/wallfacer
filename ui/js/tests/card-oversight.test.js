/**
 * Tests for buildPhaseListHTML (oversight-shared.js).
 *
 * The card-level oversight accordion was removed from the board overview;
 * oversight is only accessible from the task detail modal.
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

// ---------------------------------------------------------------------------
// Minimal DOM element stub usable inside a vm context.
// ---------------------------------------------------------------------------
function makeEl(tag) {
  const el = {
    tagName: (tag || "div").toLowerCase(),
    _html: "",
    _text: "",
    _listeners: {},
    _queries: {},
    dataset: {},
    open: false,
    className: "",
    style: {},
    onclick: null,
    classList: {
      _set: new Set(),
      add(c) {
        this._set.add(c);
      },
      remove(c) {
        this._set.delete(c);
      },
      toggle(c, f) {
        if (f !== undefined) {
          f ? this._set.add(c) : this._set.delete(c);
        } else {
          this._set.has(c) ? this._set.delete(c) : this._set.add(c);
        }
      },
      contains(c) {
        return this._set.has(c);
      },
    },
    get innerHTML() {
      return this._html;
    },
    set innerHTML(v) {
      this._html = String(v ?? "");
    },
    get textContent() {
      return this._text;
    },
    set textContent(v) {
      this._text = String(v ?? "");
    },
    addEventListener(evt, fn) {
      if (!this._listeners[evt]) this._listeners[evt] = [];
      this._listeners[evt].push(fn);
    },
    querySelector(sel) {
      return this._queries[sel] || null;
    },
    // Test helper: register a child element for a given CSS selector.
    _setQuery(sel, child) {
      this._queries[sel] = child;
    },
    // Test helper: fire all listeners for an event type.
    _fire(evt) {
      (this._listeners[evt] || []).forEach((fn) => {
        fn();
      });
    },
  };
  return el;
}

// ---------------------------------------------------------------------------
// Minimal task object with all fields that updateCard reads.
// ---------------------------------------------------------------------------
function makeTask(overrides) {
  return Object.assign(
    {
      id: "test-id",
      status: "done",
      kind: "",
      prompt: "Test task prompt",
      execution_prompt: "",
      title: "Test Task",
      result: "Completed successfully",
      stop_reason: "",
      session_id: null,
      fresh_start: false,
      archived: false,
      is_test_run: false,
      timeout: 15,
      sandbox: "default",
      sandbox_by_activity: {},
      mount_worktrees: false,
      tags: [],
      depends_on: [],
      current_refinement: null,
      worktree_paths: {},
      position: 0,
      created_at: new Date().toISOString(),
      updated_at: new Date().toISOString(),
      last_test_result: "",
      turns: 2,
    },
    overrides,
  );
}

// ---------------------------------------------------------------------------
// Build a vm context that satisfies all render.js runtime dependencies.
// ---------------------------------------------------------------------------
function makeRenderContext({ fetchImpl } = {}) {
  const bodyEl = makeEl("div");
  const summaryEl = makeEl("summary");
  const detailsEl = makeEl("details");
  detailsEl._setQuery(".card-oversight-body", bodyEl);
  detailsEl._setQuery(".card-oversight-summary", summaryEl);

  const cardEl = makeEl("div");
  // querySelector on card returns the detailsEl when the innerHTML contains
  // the oversight block (set by updateCard for done/failed tasks).
  cardEl.querySelector = function (sel) {
    if (sel === ".card-oversight" && this._html.includes("card-oversight")) {
      return detailsEl;
    }
    if (sel === "[data-diff]") return null;
    return null;
  };

  const defaultFetch = () =>
    Promise.resolve({
      json: () =>
        Promise.resolve({
          status: "ready",
          phase_count: 2,
          phases: [
            { title: "Phase Alpha", summary: "Initial setup" },
            { title: "Phase Beta", summary: "Implementation work" },
          ],
        }),
    });

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    Promise,
    fetch: fetchImpl || defaultFetch,
    document: {
      createElement: () => cardEl,
      getElementById: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: "complete",
      addEventListener: () => {},
    },
    window: {
      depGraphEnabled: false,
      matchMedia: () => ({ matches: false, addEventListener: () => {} }),
    },
    localStorage: { getItem: () => null, setItem: () => {} },
    IntersectionObserver: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
    clearInterval: () => {},
    setInterval: () => 0,
    requestAnimationFrame: (cb) => {
      if (cb) cb();
    },
    // Stubs for functions from other modules consumed by render.js
    escapeHtml: (s) =>
      String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
    timeAgo: () => "1h ago",
    formatTimeout: () => "15m",
    sandboxDisplayName: (s) => s || "default",
    renderMarkdown: (s) => s || "",
    highlightMatch: (s) => s || "",
    taskDisplayPrompt: (t) => (t && t.prompt) || "",
    openModal: () => Promise.resolve(),
    tasks: [],
    filterQuery: "",
    maxParallelTasks: 0,
  });

  loadScript("state.js", ctx);
  loadScript("build/oversight-shared.js", ctx);
  loadScript("render.js", ctx);

  return { ctx, cardEl, detailsEl, bodyEl, summaryEl };
}

// ---------------------------------------------------------------------------
// buildPhaseListHTML (oversight-shared.js)
// ---------------------------------------------------------------------------
describe("buildPhaseListHTML", () => {
  let ctx;

  beforeEach(() => {
    ctx = vm.createContext({
      console,
      Math,
      Date,
      escapeHtml: (s) =>
        String(s ?? "")
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")
          .replace(/>/g, "&gt;")
          .replace(/"/g, "&quot;"),
    });
    loadScript("build/oversight-shared.js", ctx);
  });

  it("returns oversight-empty div for null phases", () => {
    expect(ctx.buildPhaseListHTML(null)).toContain("oversight-empty");
  });

  it("returns oversight-empty div for empty array", () => {
    expect(ctx.buildPhaseListHTML([])).toContain("oversight-empty");
  });

  it("renders phase title and summary", () => {
    const html = ctx.buildPhaseListHTML([
      { title: "Setup", summary: "Did setup" },
    ]);
    expect(html).toContain("Setup");
    expect(html).toContain("Did setup");
    expect(html).toContain("Phase 1");
  });

  it("renders multiple phases with correct numbers", () => {
    const html = ctx.buildPhaseListHTML([
      { title: "Alpha", summary: "First" },
      { title: "Beta", summary: "Second" },
    ]);
    expect(html).toContain("Phase 1");
    expect(html).toContain("Phase 2");
    expect(html).toContain("Alpha");
    expect(html).toContain("Beta");
  });

  it("escapes HTML in title and summary", () => {
    const html = ctx.buildPhaseListHTML([
      { title: "<b>X</b>", summary: "<em>y</em>" },
    ]);
    expect(html).not.toContain("<b>");
    expect(html).toContain("&lt;b&gt;");
    expect(html).not.toContain("<em>");
  });
});

// ---------------------------------------------------------------------------
// Card oversight no longer shown in board overview (only in modal)
// ---------------------------------------------------------------------------
describe("board card — no card-oversight element", () => {
  it("does not inject card-oversight for a done task", () => {
    const { ctx, cardEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: "done" }));
    expect(cardEl.innerHTML).not.toContain("card-oversight");
  });

  it("does not inject card-oversight for a failed task", () => {
    const { ctx, cardEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: "failed" }));
    expect(cardEl.innerHTML).not.toContain("card-oversight");
  });
});
