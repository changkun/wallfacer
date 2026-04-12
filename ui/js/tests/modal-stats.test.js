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

function setupAndLoad(extraCtxOverrides) {
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
  const planningSection = makeElement();
  const planningTbody = makeElement();
  const planningPeriod = makeElement({ value: "30" });

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
    ["stats-planning-section", planningSection],
    ["stats-planning-tbody", planningTbody],
    ["stats-planning-period", planningPeriod],
  ];

  const ctx = makeContext({ elements, ...(extraCtxOverrides || {}) });
  const code = readFileSync(join(jsDir, "modal-stats.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "modal-stats.js") });
  ctx._triggerDomReady();

  return {
    ctx,
    modal,
    summary,
    canvas,
    byWorkspaceSection,
    planningSection,
    planningTbody,
    planningPeriod,
  };
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
      // Default planning window is 30 days until /api/config overrides it.
      expect(ctx.loadJsonEndpoint).toHaveBeenCalledWith(
        "/api/stats?days=30",
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

    it("renders planning rows per group", () => {
      const { ctx, planningSection } = setupAndLoad();
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
        planning: {
          abc123: {
            label: "repoA",
            paths: ["/repo/a"],
            usage: { input_tokens: 10, output_tokens: 5, cost_usd: 0.01 },
            round_count: 2,
            timeline: [
              { timestamp: "2026-04-01T00:00:00Z", cost_usd: 0.01, tokens: 15 },
            ],
          },
          def456: {
            label: "repoB",
            paths: ["/repo/b"],
            usage: { input_tokens: 100, output_tokens: 50, cost_usd: 0.5 },
            round_count: 5,
            timeline: [
              { timestamp: "2026-04-01T00:00:00Z", cost_usd: 0.5, tokens: 150 },
            ],
          },
        },
      });

      expect(planningSection.style.display).toBe("");
      const calls = ctx.appendRowsToTbody.mock.calls.filter(
        (c) => c[0] === "stats-planning-tbody",
      );
      expect(calls.length).toBe(1);
      const rows = calls[0][1];
      expect(rows.length).toBe(2);
      // Both group labels end up in the rendered rows.
      const joined = JSON.stringify(rows);
      expect(joined).toContain("repoA");
      expect(joined).toContain("repoB");
    });

    it("hides planning section when empty", () => {
      const { ctx, planningSection } = setupAndLoad();
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
        planning: {},
      });

      expect(planningSection.style.display).toBe("none");
    });

    it("renders sparkline SVG per group", () => {
      const { ctx } = setupAndLoad();
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
        planning: {
          k1: {
            label: "group",
            paths: [],
            usage: { input_tokens: 0, output_tokens: 0, cost_usd: 0.03 },
            round_count: 3,
            timeline: [
              { timestamp: "2026-04-01T00:00:00Z", cost_usd: 0.01, tokens: 10 },
              { timestamp: "2026-04-01T00:01:00Z", cost_usd: 0.02, tokens: 20 },
              { timestamp: "2026-04-01T00:02:00Z", cost_usd: 0.03, tokens: 30 },
            ],
          },
        },
      });

      const calls = ctx.appendRowsToTbody.mock.calls.filter(
        (c) => c[0] === "stats-planning-tbody",
      );
      const rows = calls[0][1];
      // Sparkline lives in the last column's html.
      const svgCell = rows[0][rows[0].length - 1].html;
      expect(svgCell).toContain("<svg");
      expect(svgCell).toContain("<polyline");
      // Three points, each "x,y" token separated by spaces in the polyline.
      const pointsAttr = /points="([^"]+)"/.exec(svgCell);
      expect(pointsAttr).not.toBeNull();
      expect(pointsAttr[1].trim().split(/\s+/).length).toBe(3);
    });

    it("escapes HTML in group labels", () => {
      // Override escapeHtml so the test can verify it was invoked on the label
      // (the production helper ships via the HTML runtime; the test harness
      // stub by default passes strings through).
      const escapeHtmlSpy = vi.fn((s) => {
        const str = String(s);
        return str
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")
          .replace(/>/g, "&gt;");
      });
      const { ctx } = setupAndLoad({ escapeHtml: escapeHtmlSpy });
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
        planning: {
          k1: {
            label: "<script>alert(1)</script>",
            paths: [],
            usage: { input_tokens: 0, output_tokens: 0, cost_usd: 0 },
            round_count: 0,
            timeline: [],
          },
        },
      });

      const calls = ctx.appendRowsToTbody.mock.calls.filter(
        (c) => c[0] === "stats-planning-tbody",
      );
      const labelHtml = calls[0][1][0][0].html;
      // The rendered label must not contain a literal <script> tag.
      expect(labelHtml).not.toContain("<script>");
      expect(labelHtml).toContain("&lt;script&gt;");
      expect(escapeHtmlSpy).toHaveBeenCalledWith("<script>alert(1)</script>");
    });

    it("reloads stats on period change", () => {
      const { ctx, planningPeriod } = setupAndLoad();
      ctx.window.openStatsModal();
      // Clear the initial fetchAndRender call from open.
      ctx.loadJsonEndpoint.mockClear();

      // Simulate the user choosing "Last 7 days" on the selector.
      const changeHandler = planningPeriod.addEventListener.mock.calls.find(
        (c) => c[0] === "change",
      )[1];
      planningPeriod.value = "7";
      changeHandler();

      expect(ctx.loadJsonEndpoint).toHaveBeenCalledWith(
        "/api/stats?days=7",
        expect.any(Function),
        expect.any(Function),
      );
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
