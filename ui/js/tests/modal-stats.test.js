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
    width: 0,
    height: 0,
    getContext: vi.fn().mockReturnValue({
      clearRect: vi.fn(),
      fillRect: vi.fn(),
      fillText: vi.fn(),
      fillStyle: "",
      font: "",
      textAlign: "",
    }),
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
    loadJsonEndpoint: vi.fn((url, renderCb) => {
      capturedRenderCb = renderCb;
    }),
    openModalPanel: vi.fn(),
    closeModalPanel: vi.fn(),
    createHoverRow: vi.fn().mockReturnValue({ tagName: "TR", style: {} }),
    appendRowsToTbody: vi.fn(),
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
  const content = makeElement();
  const summary = makeElement();
  const byStatusTbody = makeElement();
  const byActivityTbody = makeElement();
  const byWorkspaceSection = makeElement();
  const byWorkspaceTbody = makeElement();
  const canvas = makeElement();
  const topTasksTbody = makeElement();

  const elements = [
    ["stats-modal", modal],
    ["stats-loading", loading],
    ["stats-error", error],
    ["stats-content", content],
    ["stats-summary", summary],
    ["stats-by-status-tbody", byStatusTbody],
    ["stats-by-activity-tbody", byActivityTbody],
    ["stats-by-workspace-section", byWorkspaceSection],
    ["stats-by-workspace-tbody", byWorkspaceTbody],
    ["stats-daily-chart", canvas],
    ["stats-top-tasks-tbody", topTasksTbody],
  ];

  const ctx = makeContext({ elements });
  const code = readFileSync(join(jsDir, "modal-stats.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "modal-stats.js") });
  ctx._triggerDomReady();

  return { ctx, modal, summary, canvas, byWorkspaceSection };
}

describe("modal-stats.js", () => {
  describe("initialization", () => {
    it("registers DOMContentLoaded and binds backdrop", () => {
      const { ctx, modal } = setupAndLoad();
      expect(ctx.bindModalBackdropClose).toHaveBeenCalledWith(
        modal,
        expect.any(Function),
      );
    });
  });

  describe("openStatsModal / closeStatsModal", () => {
    it("opens modal and triggers fetch", () => {
      const { ctx, modal } = setupAndLoad();
      ctx.window.openStatsModal();
      expect(ctx.openModalPanel).toHaveBeenCalledWith(modal);
      expect(ctx.loadJsonEndpoint).toHaveBeenCalledWith(
        "/api/stats",
        expect.any(Function),
        expect.any(Function),
      );
    });

    it("closes modal", () => {
      const { ctx, modal } = setupAndLoad();
      ctx.window.closeStatsModal();
      expect(ctx.closeModalPanel).toHaveBeenCalledWith(modal);
    });
  });

  describe("renderStats", () => {
    it("renders summary with cost and tokens", () => {
      const { ctx, summary } = setupAndLoad();
      ctx.window.openStatsModal();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        total_cost_usd: 1.5678,
        total_input_tokens: 50000,
        total_output_tokens: 10000,
        total_cache_tokens: 5000,
        by_status: {},
        by_activity: {},
        by_workspace: {},
        daily_usage: [],
        top_tasks: [],
      });

      expect(summary.innerHTML).toContain("$1.5678");
      expect(summary.innerHTML).toContain("50,000");
      expect(summary.innerHTML).toContain("10,000");
    });

    it("renders by-status and by-activity tables", () => {
      const { ctx } = setupAndLoad();
      ctx.window.openStatsModal();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        total_cost_usd: 0.5,
        total_input_tokens: 1000,
        total_output_tokens: 500,
        total_cache_tokens: 0,
        by_status: {
          done: { input_tokens: 800, output_tokens: 400, cost_usd: 0.4 },
          failed: { input_tokens: 200, output_tokens: 100, cost_usd: 0.1 },
        },
        by_activity: {
          implementation: {
            input_tokens: 600,
            output_tokens: 300,
            cost_usd: 0.3,
          },
          test: { input_tokens: 400, output_tokens: 200, cost_usd: 0.2 },
        },
        by_workspace: {},
        daily_usage: [],
        top_tasks: [],
      });

      expect(ctx.appendRowsToTbody).toHaveBeenCalled();
    });

    it("draws daily chart on canvas", () => {
      const { ctx, canvas } = setupAndLoad();
      ctx.window.openStatsModal();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        total_cost_usd: 1.0,
        total_input_tokens: 100,
        total_output_tokens: 50,
        total_cache_tokens: 0,
        by_status: {},
        by_activity: {},
        by_workspace: {},
        daily_usage: [
          { date: "2026-03-01", cost_usd: 0.5 },
          { date: "2026-03-02", cost_usd: 0.3 },
          { date: "2026-03-03", cost_usd: 0.2 },
        ],
        top_tasks: [],
      });

      const canvasCtx = canvas.getContext("2d");
      expect(canvasCtx.clearRect).toHaveBeenCalled();
    });

    it("hides workspace section when no workspace data", () => {
      const { ctx, byWorkspaceSection } = setupAndLoad();
      ctx.window.openStatsModal();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        total_cost_usd: 0,
        total_input_tokens: 0,
        total_output_tokens: 0,
        total_cache_tokens: 0,
        by_status: {},
        by_activity: {},
        by_workspace: {},
        daily_usage: [],
        top_tasks: [],
      });

      expect(byWorkspaceSection.style.display).toBe("none");
    });

    it("shows workspace section with data", () => {
      const { ctx, byWorkspaceSection } = setupAndLoad();
      ctx.window.openStatsModal();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        total_cost_usd: 1.0,
        total_input_tokens: 100,
        total_output_tokens: 50,
        total_cache_tokens: 0,
        by_status: {},
        by_activity: {},
        by_workspace: {
          "/home/project": {
            count: 5,
            input_tokens: 100,
            output_tokens: 50,
            cost_usd: 1.0,
          },
        },
        daily_usage: [],
        top_tasks: [],
      });

      expect(byWorkspaceSection.style.display).toBe("");
    });

    it("renders top tasks with links", () => {
      const { ctx } = setupAndLoad();
      ctx.window.openStatsModal();
      const renderCb = ctx._getCapturedRenderCb();

      renderCb({
        total_cost_usd: 2.0,
        total_input_tokens: 500,
        total_output_tokens: 200,
        total_cache_tokens: 0,
        by_status: {},
        by_activity: {},
        by_workspace: {},
        daily_usage: [],
        top_tasks: [
          { id: "task-1", title: "Fix bug", status: "done", cost_usd: 1.5 },
          {
            id: "task-2",
            title: "Add feature",
            status: "failed",
            cost_usd: 0.5,
          },
        ],
      });

      expect(ctx.appendRowsToTbody).toHaveBeenCalled();
    });
  });
});
