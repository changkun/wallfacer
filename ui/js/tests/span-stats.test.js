import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeElement(overrides = {}) {
  return {
    textContent: "",
    innerHTML: "",
    value: "",
    style: { display: "" },
    appendChild: vi.fn(),
    addEventListener: vi.fn(),
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  let domContentLoadedCb = null;
  let capturedRenderCb = null;

  const ctx = {
    console,
    JSON,
    Object,
    String,
    Number,
    Math,
    Array,
    parseInt,
    encodeURIComponent,
    setTimeout: vi.fn(),
    window: {},
    document: {
      getElementById: (id) => elements.get(id) || null,
      addEventListener: vi.fn((type, fn) => {
        if (type === "DOMContentLoaded") domContentLoadedCb = fn;
      }),
      documentElement: { getAttribute: () => "light" },
    },
    escapeHtml: (s) => String(s),
    bindModalBackdropClose: vi.fn(),
    createModalStateController: vi.fn().mockReturnValue(vi.fn()),
    loadJsonEndpoint: vi.fn((_url, renderCb) => {
      capturedRenderCb = renderCb;
    }),
    openModalPanel: vi.fn(),
    closeModalPanel: vi.fn(),
    createHoverRow: vi.fn().mockReturnValue({
      tagName: "TR",
      style: {},
    }),
    fmtMs: (ms) => {
      if (ms === undefined || ms === null) return "\u2014";
      if (ms < 1000) return ms + "ms";
      return (ms / 1000).toFixed(1) + "s";
    },
    _getCapturedRenderCb: () => capturedRenderCb,
    _triggerDomReady: () => domContentLoadedCb && domContentLoadedCb(),
    ...overrides,
  };
  const vmCtx = vm.createContext(ctx);
  vmCtx.window = vmCtx;
  return vmCtx;
}

function setupAndLoad() {
  const modal = makeElement();
  const loading = makeElement();
  const error = makeElement();
  const empty = makeElement();
  const content = makeElement();
  const summary = makeElement();
  const tbody = makeElement();
  const throughput = makeElement();

  const elements = [
    ["span-stats-modal", modal],
    ["span-stats-loading", loading],
    ["span-stats-error", error],
    ["span-stats-empty", empty],
    ["span-stats-content", content],
    ["span-stats-summary", summary],
    ["span-stats-tbody", tbody],
    ["span-stats-throughput", throughput],
  ];

  const ctx = makeContext({ elements });
  const code = readFileSync(join(jsDir, "span-stats.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "span-stats.js") });
  ctx._triggerDomReady();

  return { ctx, modal, summary, tbody, throughput };
}

describe("span-stats.js", () => {
  describe("initialization", () => {
    it("registers DOMContentLoaded and binds modal", () => {
      const { ctx, modal } = setupAndLoad();
      expect(ctx.bindModalBackdropClose).toHaveBeenCalledWith(
        modal,
        expect.any(Function),
      );
      expect(ctx.createModalStateController).toHaveBeenCalled();
    });
  });

  describe("showSpanStats / closeSpanStats", () => {
    it("showSpanStats opens modal and fetches", () => {
      const { ctx, modal } = setupAndLoad();
      ctx.window.showSpanStats();
      expect(ctx.openModalPanel).toHaveBeenCalledWith(modal);
      expect(ctx.loadJsonEndpoint).toHaveBeenCalledWith(
        "/api/debug/spans",
        expect.any(Function),
        expect.any(Function),
      );
    });

    it("closeSpanStats closes modal", () => {
      const { ctx, modal } = setupAndLoad();
      ctx.window.closeSpanStats();
      expect(ctx.closeModalPanel).toHaveBeenCalledWith(modal);
    });
  });

  describe("renderStats", () => {
    it("renders phase rows and summary", () => {
      const { ctx, summary, tbody } = setupAndLoad();
      ctx.window.showSpanStats();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        tasks_scanned: 10,
        spans_total: 25,
        phases: {
          agent_turn: {
            count: 15,
            min_ms: 100,
            p50_ms: 500,
            p95_ms: 2000,
            p99_ms: 5000,
            max_ms: 8000,
            sum_ms: 7500,
          },
          worktree_setup: {
            count: 10,
            min_ms: 50,
            p50_ms: 200,
            p95_ms: 800,
            p99_ms: 1500,
            max_ms: 2000,
            sum_ms: 2000,
          },
        },
        throughput: {
          total_completed: 8,
          total_failed: 2,
          success_rate_pct: 80.0,
          median_execution_s: 30,
          p95_execution_s: 120,
        },
      });

      expect(summary.innerHTML).toContain("10");
      expect(summary.innerHTML).toContain("25");
      expect(summary.innerHTML).toContain("2");
      expect(ctx.createHoverRow).toHaveBeenCalled();
    });

    it("shows empty state when no phases and no throughput", () => {
      const { ctx } = setupAndLoad();
      ctx.window.showSpanStats();
      const renderCb = ctx._getCapturedRenderCb();
      const setState = ctx.createModalStateController.mock.results[0].value;

      renderCb({
        tasks_scanned: 0,
        spans_total: 0,
        phases: {},
        throughput: {},
      });

      expect(setState).toHaveBeenCalledWith("empty");
    });

    it("renders throughput tiles", () => {
      const { ctx, throughput } = setupAndLoad();
      ctx.window.showSpanStats();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        tasks_scanned: 5,
        spans_total: 10,
        phases: {
          commit: {
            count: 5,
            min_ms: 1000,
            p50_ms: 2000,
            p95_ms: 5000,
            p99_ms: 8000,
            max_ms: 10000,
            sum_ms: 15000,
          },
        },
        throughput: {
          total_completed: 4,
          total_failed: 1,
          success_rate_pct: 80.0,
          median_execution_s: 45.5,
          p95_execution_s: 180,
          daily_completions: [
            { date: "2026-03-01", count: 2 },
            { date: "2026-03-02", count: 0 },
            { date: "2026-03-03", count: 3 },
          ],
        },
      });

      expect(throughput.innerHTML).toContain("Completed");
      expect(throughput.innerHTML).toContain("4");
      expect(throughput.innerHTML).toContain("Failed");
      expect(throughput.innerHTML).toContain("80.0%");
    });

    it("handles throughput with no data", () => {
      const { ctx, throughput } = setupAndLoad();
      ctx.window.showSpanStats();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        tasks_scanned: 0,
        spans_total: 0,
        phases: {},
        throughput: {
          total_completed: 0,
          total_failed: 0,
          success_rate_pct: 0,
        },
      });

      // With 0 completed and 0 failed but throughput obj exists, it still renders tiles
      expect(throughput.innerHTML).toContain("\u2014");
    });

    it("clears throughput when null", () => {
      const { ctx, throughput } = setupAndLoad();
      ctx.window.showSpanStats();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        tasks_scanned: 1,
        spans_total: 1,
        phases: {
          agent_turn: {
            count: 1,
            min_ms: 100,
            p50_ms: 100,
            p95_ms: 100,
            p99_ms: 100,
            max_ms: 100,
            sum_ms: 100,
          },
        },
        throughput: null,
      });

      expect(throughput.innerHTML).toBe("");
    });
  });
});
