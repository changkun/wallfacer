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
    value: "7",
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
    },
    escapeHtml: (s) => String(s).replace(/</g, "&lt;"),
    bindModalBackdropClose: vi.fn(),
    createModalStateController: vi.fn().mockReturnValue(vi.fn()),
    loadJsonEndpoint: vi.fn((url, renderCb, setState) => {
      capturedRenderCb = renderCb;
    }),
    openModalPanel: vi.fn(),
    closeModalPanel: vi.fn(),
    createHoverRow: vi.fn().mockReturnValue({
      tagName: "TR",
      style: {},
    }),
    appendNoDataRow: vi.fn(),
    _getCapturedRenderCb: () => capturedRenderCb,
    _triggerDomReady: () => domContentLoadedCb && domContentLoadedCb(),
    ...overrides,
  };
  const vmCtx = vm.createContext(ctx);
  // Make window === global so IIFE `window.X = fn` creates globals
  vmCtx.window = vmCtx;
  return vmCtx;
}

function setupAndLoad(extraElements = []) {
  const modal = makeElement();
  const loading = makeElement();
  const error = makeElement();
  const empty = makeElement();
  const content = makeElement();
  const summary = makeElement();
  const byStatusTbody = makeElement();
  const bySubAgentTbody = makeElement();
  const periodSelect = makeElement({ value: "7" });

  const elements = [
    ["usage-stats-modal", modal],
    ["usage-stats-loading", loading],
    ["usage-stats-error", error],
    ["usage-stats-empty", empty],
    ["usage-stats-content", content],
    ["usage-stats-summary", summary],
    ["usage-stats-by-status-tbody", byStatusTbody],
    ["usage-stats-by-sub-agent-tbody", bySubAgentTbody],
    ["usage-stats-period", periodSelect],
    ...extraElements,
  ];

  const ctx = makeContext({ elements });
  const code = readFileSync(join(jsDir, "usage-stats.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "usage-stats.js") });
  ctx._triggerDomReady();

  return {
    ctx,
    modal,
    summary,
    byStatusTbody,
    bySubAgentTbody,
    periodSelect,
  };
}

describe("usage-stats.js", () => {
  describe("initialization", () => {
    it("registers DOMContentLoaded handler and binds backdrop close", () => {
      const { ctx, modal } = setupAndLoad();
      expect(ctx.bindModalBackdropClose).toHaveBeenCalledWith(
        modal,
        expect.any(Function),
      );
      expect(ctx.createModalStateController).toHaveBeenCalled();
    });

    it("binds period change to fetchStats", () => {
      const { ctx, periodSelect } = setupAndLoad();
      expect(periodSelect.addEventListener).toHaveBeenCalledWith(
        "change",
        expect.any(Function),
      );
    });
  });

  describe("showUsageStats / closeUsageStats", () => {
    it("showUsageStats opens modal and triggers fetch", () => {
      const { ctx, modal } = setupAndLoad();
      ctx.window.showUsageStats();
      expect(ctx.openModalPanel).toHaveBeenCalledWith(modal);
      expect(ctx.loadJsonEndpoint).toHaveBeenCalledWith(
        "/api/usage?days=7",
        expect.any(Function),
        expect.any(Function),
      );
    });

    it("closeUsageStats closes modal", () => {
      const { ctx, modal } = setupAndLoad();
      ctx.window.closeUsageStats();
      expect(ctx.closeModalPanel).toHaveBeenCalledWith(modal);
    });
  });

  describe("renderStats", () => {
    it("renders summary with task count and cost", () => {
      const { ctx, summary, byStatusTbody, bySubAgentTbody } = setupAndLoad();
      ctx.window.showUsageStats();
      const renderCb = ctx._getCapturedRenderCb();
      expect(renderCb).toBeTruthy();

      renderCb({
        task_count: 5,
        period_days: 7,
        total: { input_tokens: 1000, output_tokens: 500, cost_usd: 0.1234 },
        by_status: {
          done: { input_tokens: 800, output_tokens: 400, cost_usd: 0.1 },
        },
        by_sub_agent: {
          implementation: {
            input_tokens: 600,
            output_tokens: 300,
            cost_usd: 0.08,
          },
        },
      });

      expect(summary.textContent).toContain("5 tasks");
      expect(summary.textContent).toContain("last 7 days");
      expect(summary.textContent).toContain("$0.1234");
    });

    it("shows 'all time' for period_days=0", () => {
      const { ctx, summary } = setupAndLoad();
      ctx.window.showUsageStats();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        task_count: 1,
        period_days: 0,
        total: { cost_usd: 0.5 },
        by_status: {},
        by_sub_agent: {},
      });

      expect(summary.textContent).toContain("all time");
    });

    it("handles singular task count", () => {
      const { ctx, summary } = setupAndLoad();
      ctx.window.showUsageStats();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        task_count: 1,
        period_days: 30,
        total: { cost_usd: 0 },
        by_status: {},
        by_sub_agent: {},
      });

      expect(summary.textContent).toContain("1 task ");
    });

    it("calls setState empty when no data", () => {
      const { ctx } = setupAndLoad();
      ctx.window.showUsageStats();
      const renderCb = ctx._getCapturedRenderCb();
      const setState = ctx.createModalStateController.mock.results[0].value;

      renderCb({
        task_count: 0,
        total: { cost_usd: 0 },
        by_status: {},
        by_sub_agent: {},
      });

      expect(setState).toHaveBeenCalledWith("empty");
    });

    it("renders status rows with createHoverRow", () => {
      const { ctx, byStatusTbody } = setupAndLoad();
      ctx.window.showUsageStats();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        task_count: 3,
        period_days: 7,
        total: { input_tokens: 100, output_tokens: 50, cost_usd: 0.01 },
        by_status: {
          done: { input_tokens: 80, output_tokens: 40, cost_usd: 0.008 },
          failed: { input_tokens: 20, output_tokens: 10, cost_usd: 0.002 },
        },
        by_sub_agent: {},
      });

      // 2 status rows + 1 total row = 3 createHoverRow calls for status
      // plus sub_agent may call appendNoDataRow
      expect(ctx.createHoverRow).toHaveBeenCalled();
    });

    it("calls appendNoDataRow when no status data", () => {
      const { ctx, byStatusTbody } = setupAndLoad();
      ctx.window.showUsageStats();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        task_count: 1,
        period_days: 7,
        total: { cost_usd: 0.01 },
        by_status: {},
        by_sub_agent: {
          implementation: {
            input_tokens: 100,
            output_tokens: 50,
            cost_usd: 0.01,
          },
        },
      });

      expect(ctx.appendNoDataRow).toHaveBeenCalledWith(
        byStatusTbody,
        5,
        "No data",
      );
    });

    it("renders planning row in by_sub_agent table", () => {
      const { ctx } = setupAndLoad();
      ctx.window.showUsageStats();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        task_count: 0,
        period_days: 7,
        total: { input_tokens: 120, output_tokens: 40, cost_usd: 0.05 },
        by_status: {},
        by_sub_agent: {
          planning: {
            input_tokens: 120,
            output_tokens: 40,
            cost_usd: 0.05,
          },
        },
      });

      // Capture the row args that carry the planning label.
      const rowArgs = ctx.createHoverRow.mock.calls.map((c) => c[0]);
      const planningRow = rowArgs.find(
        (row) => Array.isArray(row) && row[0] && row[0].text === "Planning",
      );
      expect(planningRow).toBeDefined();
    });
  });

  describe("period selector seeding", () => {
    // Wait for one microtask turn so the fetch().then() chain runs.
    const flush = () => new Promise((r) => setTimeout(r, 0));

    it("defaults period selector from config", async () => {
      const fetchMock = vi.fn(() =>
        Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ planning_window_days: 30 }),
        }),
      );
      const ctx = makeContext({ fetch: fetchMock });
      const modal = makeElement();
      const periodSelect = makeElement({ value: "7" });
      ctx.document.getElementById = (id) => {
        switch (id) {
          case "usage-stats-modal":
            return modal;
          case "usage-stats-period":
            return periodSelect;
          default:
            return makeElement();
        }
      };
      const code = readFileSync(join(jsDir, "usage-stats.js"), "utf8");
      vm.runInContext(code, ctx, { filename: join(jsDir, "usage-stats.js") });
      ctx._triggerDomReady();

      ctx.window.showUsageStats();
      await flush();
      await flush();

      expect(fetchMock).toHaveBeenCalledWith("/api/config");
      expect(periodSelect.value).toBe("30");
    });

    it("falls back when config missing planning_window_days", async () => {
      const fetchMock = vi.fn(() =>
        Promise.resolve({
          ok: true,
          json: () => Promise.resolve({ workspaces: ["/tmp/x"] }),
        }),
      );
      const ctx = makeContext({ fetch: fetchMock });
      const modal = makeElement();
      const periodSelect = makeElement({ value: "7" });
      ctx.document.getElementById = (id) => {
        switch (id) {
          case "usage-stats-modal":
            return modal;
          case "usage-stats-period":
            return periodSelect;
          default:
            return makeElement();
        }
      };
      const code = readFileSync(join(jsDir, "usage-stats.js"), "utf8");
      vm.runInContext(code, ctx, { filename: join(jsDir, "usage-stats.js") });
      ctx._triggerDomReady();

      ctx.window.showUsageStats();
      await flush();
      await flush();

      // Value must be preserved from the HTML default when config omits
      // planning_window_days.
      expect(periodSelect.value).toBe("7");
    });

    it("period change refetches /api/usage with new days", () => {
      const { ctx, periodSelect } = setupAndLoad();
      // Simulate user selection before change event fires.
      periodSelect.value = "30";
      const changeHandler = periodSelect.addEventListener.mock.calls.find(
        (c) => c[0] === "change",
      )[1];
      ctx.loadJsonEndpoint.mockClear();
      changeHandler();

      expect(ctx.loadJsonEndpoint).toHaveBeenCalledWith(
        "/api/usage?days=30",
        expect.any(Function),
        expect.any(Function),
      );
    });
  });
});
