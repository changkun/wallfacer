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
    value: "",
    checked: false,
    style: { display: "", opacity: "" },
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Date,
    Math,
    parseInt,
    String,
    isNaN,
    JSON,
    setTimeout: vi.fn(),
    setInterval: vi.fn(),
    api: vi.fn().mockResolvedValue({}),
    showAlert: vi.fn(),
    fetchTasks: vi.fn(),
    waitForTaskDelta: vi.fn(),
    updateAutomationActiveCount: vi.fn(),
    document: {
      getElementById: (id) => elements.get(id) || null,
      addEventListener: vi.fn(),
    },
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadIdeate(ctx) {
  const code = readFileSync(join(jsDir, "ideate.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "ideate.js") });
  return ctx;
}

describe("ideate.js", () => {
  describe("updateIdeationFromTasks", () => {
    it("sets running=true when an idea-agent task is in_progress", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [["ideation-next-run", nextRunEl]],
      });
      loadIdeate(ctx);
      ctx.updateIdeationFromTasks([
        { kind: "idea-agent", status: "in_progress" },
        { kind: "task", status: "done" },
      ]);
      expect(nextRunEl.textContent).toBe("Brainstorm running\u2026");
      expect(nextRunEl.style.display).toBe("inline");
    });

    it("sets running=false when no idea-agent task is in_progress", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [["ideation-next-run", nextRunEl]],
      });
      loadIdeate(ctx);
      ctx.updateIdeationFromTasks([
        { kind: "idea-agent", status: "done" },
        { kind: "task", status: "in_progress" },
      ]);
      expect(nextRunEl.textContent).toBe("");
      expect(nextRunEl.style.display).toBe("none");
    });

    it("handles empty task array", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [["ideation-next-run", nextRunEl]],
      });
      loadIdeate(ctx);
      ctx.updateIdeationFromTasks([]);
      expect(nextRunEl.style.display).toBe("none");
    });
  });

  describe("updateNextRunDisplay", () => {
    it("shows running state", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [["ideation-next-run", nextRunEl]],
      });
      loadIdeate(ctx);
      ctx.setIdeationRunning(true);
      expect(nextRunEl.textContent).toBe("Brainstorm running\u2026");
      expect(nextRunEl.style.display).toBe("inline");
    });

    it("hides when ideation is disabled", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [["ideation-next-run", nextRunEl]],
      });
      loadIdeate(ctx);
      ctx.updateNextRunDisplay();
      expect(nextRunEl.style.display).toBe("none");
    });

    it("shows countdown when ideation enabled with interval and future next run", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-next-run", nextRunEl],
          ["ideation-toggle", makeElement()],
          ["ideation-header-toggle", makeElement()],
          ["ideation-interval", makeElement()],
          ["ideation-exploit-ratio", makeElement()],
          ["ideation-exploit-ratio-label", makeElement()],
        ],
      });
      loadIdeate(ctx);
      // Use updateIdeationConfig to set internal let variables
      ctx.updateIdeationConfig({
        ideation: true,
        ideation_interval: 30,
        ideation_next_run: new Date(Date.now() + 15 * 60000).toISOString(),
        ideation_exploit_ratio: 0.8,
      });
      expect(nextRunEl.textContent).toMatch(/^Next brainstorm in \d+m$/);
      expect(nextRunEl.style.display).toBe("inline");
    });

    it("shows hours and minutes for longer countdowns", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-next-run", nextRunEl],
          ["ideation-toggle", makeElement()],
          ["ideation-header-toggle", makeElement()],
          ["ideation-interval", makeElement()],
          ["ideation-exploit-ratio", makeElement()],
          ["ideation-exploit-ratio-label", makeElement()],
        ],
      });
      loadIdeate(ctx);
      ctx.updateIdeationConfig({
        ideation: true,
        ideation_interval: 120,
        ideation_next_run: new Date(Date.now() + 90 * 60000).toISOString(),
        ideation_exploit_ratio: 0.8,
      });
      expect(nextRunEl.textContent).toMatch(/^Next brainstorm in 1h 30m$/);
    });

    it("shows only hours when minutes are zero", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-next-run", nextRunEl],
          ["ideation-toggle", makeElement()],
          ["ideation-header-toggle", makeElement()],
          ["ideation-interval", makeElement()],
          ["ideation-exploit-ratio", makeElement()],
          ["ideation-exploit-ratio-label", makeElement()],
        ],
      });
      loadIdeate(ctx);
      ctx.updateIdeationConfig({
        ideation: true,
        ideation_interval: 120,
        ideation_next_run: new Date(Date.now() + 120 * 60000).toISOString(),
        ideation_exploit_ratio: 0.8,
      });
      expect(nextRunEl.textContent).toMatch(/^Next brainstorm in 2h$/);
    });

    it("hides when next run is in the past", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-next-run", nextRunEl],
          ["ideation-toggle", makeElement()],
          ["ideation-header-toggle", makeElement()],
          ["ideation-interval", makeElement()],
          ["ideation-exploit-ratio", makeElement()],
          ["ideation-exploit-ratio-label", makeElement()],
        ],
      });
      loadIdeate(ctx);
      ctx.updateIdeationConfig({
        ideation: true,
        ideation_interval: 30,
        ideation_next_run: new Date(Date.now() - 60000).toISOString(),
        ideation_exploit_ratio: 0.8,
      });
      expect(nextRunEl.style.display).toBe("none");
    });

    it("hides when interval is 0", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-next-run", nextRunEl],
          ["ideation-toggle", makeElement()],
          ["ideation-header-toggle", makeElement()],
          ["ideation-interval", makeElement()],
          ["ideation-exploit-ratio", makeElement()],
          ["ideation-exploit-ratio-label", makeElement()],
        ],
      });
      loadIdeate(ctx);
      ctx.updateIdeationConfig({
        ideation: true,
        ideation_interval: 0,
        ideation_next_run: new Date(Date.now() + 60000).toISOString(),
        ideation_exploit_ratio: 0.8,
      });
      expect(nextRunEl.style.display).toBe("none");
    });

    it("handles invalid date gracefully", () => {
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-next-run", nextRunEl],
          ["ideation-toggle", makeElement()],
          ["ideation-header-toggle", makeElement()],
          ["ideation-interval", makeElement()],
          ["ideation-exploit-ratio", makeElement()],
          ["ideation-exploit-ratio-label", makeElement()],
        ],
      });
      loadIdeate(ctx);
      ctx.updateIdeationConfig({
        ideation: true,
        ideation_interval: 30,
        ideation_next_run: "not-a-date",
        ideation_exploit_ratio: 0.8,
      });
      expect(nextRunEl.style.display).toBe("none");
    });

    it("handles missing element gracefully", () => {
      const ctx = makeContext({ elements: [] });
      loadIdeate(ctx);
      ctx.updateNextRunDisplay();
    });
  });

  describe("updateExploitRatioLabel", () => {
    it("updates label text with exploit/explore split", () => {
      const label = makeElement();
      const ctx = makeContext({
        elements: [["ideation-exploit-ratio-label", label]],
      });
      loadIdeate(ctx);
      ctx.updateExploitRatioLabel(80);
      expect(label.textContent).toBe("80/20");
    });

    it("handles 0 percent", () => {
      const label = makeElement();
      const ctx = makeContext({
        elements: [["ideation-exploit-ratio-label", label]],
      });
      loadIdeate(ctx);
      ctx.updateExploitRatioLabel(0);
      expect(label.textContent).toBe("0/100");
    });

    it("handles 100 percent", () => {
      const label = makeElement();
      const ctx = makeContext({
        elements: [["ideation-exploit-ratio-label", label]],
      });
      loadIdeate(ctx);
      ctx.updateExploitRatioLabel(100);
      expect(label.textContent).toBe("100/0");
    });

    it("handles string input", () => {
      const label = makeElement();
      const ctx = makeContext({
        elements: [["ideation-exploit-ratio-label", label]],
      });
      loadIdeate(ctx);
      ctx.updateExploitRatioLabel("60");
      expect(label.textContent).toBe("60/40");
    });
  });

  describe("_syncExploitRatioSlider", () => {
    it("syncs slider value and label from state via updateIdeationConfig", () => {
      const slider = makeElement();
      const label = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-exploit-ratio", slider],
          ["ideation-exploit-ratio-label", label],
          ["ideation-toggle", makeElement()],
          ["ideation-header-toggle", makeElement()],
          ["ideation-interval", makeElement()],
          ["ideation-next-run", makeElement()],
        ],
      });
      loadIdeate(ctx);
      ctx.updateIdeationConfig({
        ideation: false,
        ideation_interval: 0,
        ideation_exploit_ratio: 0.7,
      });
      expect(slider.value).toBe("70");
      expect(label.textContent).toBe("70/30");
    });
  });

  describe("_syncIdeationControls", () => {
    it("syncs all controls from state via updateIdeationConfig", () => {
      const toggle = makeElement();
      const headerToggle = makeElement();
      const sel = makeElement();
      const slider = makeElement();
      const label = makeElement();
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-toggle", toggle],
          ["ideation-header-toggle", headerToggle],
          ["ideation-interval", sel],
          ["ideation-exploit-ratio", slider],
          ["ideation-exploit-ratio-label", label],
          ["ideation-next-run", nextRunEl],
        ],
      });
      loadIdeate(ctx);
      ctx.updateIdeationConfig({
        ideation: true,
        ideation_interval: 60,
        ideation_exploit_ratio: 0.5,
      });
      expect(toggle.checked).toBe(true);
      expect(headerToggle.checked).toBe(true);
      expect(sel.value).toBe("60");
      expect(slider.value).toBe("50");
      expect(label.textContent).toBe("50/50");
    });
  });

  describe("updateIdeationConfig", () => {
    it("updates all local state and syncs controls", () => {
      const toggle = makeElement();
      const headerToggle = makeElement();
      const sel = makeElement();
      const slider = makeElement();
      const label = makeElement();
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-toggle", toggle],
          ["ideation-header-toggle", headerToggle],
          ["ideation-interval", sel],
          ["ideation-exploit-ratio", slider],
          ["ideation-exploit-ratio-label", label],
          ["ideation-next-run", nextRunEl],
        ],
      });
      loadIdeate(ctx);
      ctx.updateIdeationConfig({
        ideation: true,
        ideation_interval: 45,
        ideation_next_run: null,
        ideation_exploit_ratio: 0.6,
      });
      // Verify through DOM side effects
      expect(toggle.checked).toBe(true);
      expect(sel.value).toBe("45");
      expect(slider.value).toBe("60");
      expect(label.textContent).toBe("60/40");
    });

    it("uses defaults for missing config fields", () => {
      const toggle = makeElement();
      const slider = makeElement();
      const label = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-toggle", toggle],
          ["ideation-header-toggle", makeElement()],
          ["ideation-interval", makeElement()],
          ["ideation-exploit-ratio", slider],
          ["ideation-exploit-ratio-label", label],
          ["ideation-next-run", makeElement()],
        ],
      });
      loadIdeate(ctx);
      ctx.updateIdeationConfig({});
      expect(toggle.checked).toBe(false);
      // Default exploit ratio is 0.8 → 80
      expect(slider.value).toBe("80");
      expect(label.textContent).toBe("80/20");
    });
  });

  describe("toggleIdeation", () => {
    it("calls api and updates state on success", async () => {
      const toggle = makeElement({ checked: true });
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-toggle", toggle],
          ["ideation-header-toggle", makeElement()],
          ["ideation-interval", makeElement()],
          ["ideation-exploit-ratio", makeElement()],
          ["ideation-exploit-ratio-label", makeElement()],
          ["ideation-next-run", nextRunEl],
        ],
        api: vi.fn().mockResolvedValue({
          ideation: true,
          ideation_next_run: "2026-01-01T00:00:00Z",
        }),
      });
      loadIdeate(ctx);
      await ctx.toggleIdeation();
      expect(ctx.api).toHaveBeenCalledWith("/api/config", {
        method: "PUT",
        body: JSON.stringify({ ideation: true }),
      });
      // Verify ideation was set via the toggle staying checked
      expect(toggle.checked).toBe(true);
    });

    it("shows alert and restores toggle on error", async () => {
      const toggle = makeElement({ checked: true });
      const ctx = makeContext({
        elements: [
          ["ideation-toggle", toggle],
          ["ideation-next-run", makeElement()],
        ],
        api: vi.fn().mockRejectedValue(new Error("fail")),
      });
      loadIdeate(ctx);
      await ctx.toggleIdeation();
      expect(ctx.showAlert).toHaveBeenCalled();
      // Restores to the internal ideation state (false by default)
      expect(toggle.checked).toBe(false);
    });
  });

  describe("triggerIdeation", () => {
    it("calls waitForTaskDelta when task_id returned", async () => {
      const ctx = makeContext({
        api: vi.fn().mockResolvedValue({ task_id: "abc-123" }),
      });
      loadIdeate(ctx);
      await ctx.triggerIdeation();
      expect(ctx.api).toHaveBeenCalledWith("/api/ideate", { method: "POST" });
      expect(ctx.waitForTaskDelta).toHaveBeenCalledWith("abc-123");
    });

    it("calls fetchTasks when no task_id", async () => {
      const ctx = makeContext({
        api: vi.fn().mockResolvedValue({}),
      });
      loadIdeate(ctx);
      await ctx.triggerIdeation();
      expect(ctx.fetchTasks).toHaveBeenCalled();
    });

    it("shows alert on error", async () => {
      const ctx = makeContext({
        api: vi.fn().mockRejectedValue(new Error("boom")),
      });
      loadIdeate(ctx);
      await ctx.triggerIdeation();
      expect(ctx.showAlert).toHaveBeenCalledWith(
        "Error triggering brainstorm: boom",
      );
    });
  });

  describe("updateIdeationInterval", () => {
    it("calls api and updates controls", async () => {
      const sel = makeElement();
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-interval", sel],
          ["ideation-next-run", nextRunEl],
        ],
        api: vi.fn().mockResolvedValue({
          ideation_interval: 30,
          ideation_next_run: null,
        }),
      });
      loadIdeate(ctx);
      await ctx.updateIdeationInterval("30");
      expect(ctx.api).toHaveBeenCalledWith("/api/config", {
        method: "PUT",
        body: JSON.stringify({ ideation_interval: 30 }),
      });
      expect(sel.value).toBe("30");
    });
  });

  describe("updateIdeationExploitRatio", () => {
    it("calls api and updates slider", async () => {
      const slider = makeElement();
      const label = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-exploit-ratio", slider],
          ["ideation-exploit-ratio-label", label],
        ],
        api: vi.fn().mockResolvedValue({ ideation_exploit_ratio: 0.7 }),
      });
      loadIdeate(ctx);
      await ctx.updateIdeationExploitRatio("70");
      expect(ctx.api).toHaveBeenCalledWith("/api/config", {
        method: "PUT",
        body: JSON.stringify({ ideation_exploit_ratio: 0.7 }),
      });
      expect(slider.value).toBe("70");
      expect(label.textContent).toBe("70/30");
    });
  });

  describe("toggleIdeationHeader", () => {
    it("calls api and syncs all controls", async () => {
      const headerToggle = makeElement({ checked: true });
      const toggle = makeElement();
      const sel = makeElement();
      const slider = makeElement();
      const label = makeElement();
      const nextRunEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["ideation-header-toggle", headerToggle],
          ["ideation-toggle", toggle],
          ["ideation-interval", sel],
          ["ideation-exploit-ratio", slider],
          ["ideation-exploit-ratio-label", label],
          ["ideation-next-run", nextRunEl],
        ],
        api: vi.fn().mockResolvedValue({
          ideation: true,
          ideation_next_run: null,
        }),
      });
      loadIdeate(ctx);
      await ctx.toggleIdeationHeader();
      expect(toggle.checked).toBe(true);
      expect(headerToggle.checked).toBe(true);
      expect(ctx.updateAutomationActiveCount).toHaveBeenCalled();
    });
  });
});
