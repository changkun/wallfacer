import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext(overrides = {}) {
  const ctx = {
    console,
    Date,
    JSON,
    setTimeout: vi.fn(),
    setInterval: vi.fn(),
    api: vi.fn().mockResolvedValue({}),
    showAlert: vi.fn(),
    fetchTasks: vi.fn(),
    waitForTaskDelta: vi.fn(),
    document: { getElementById: vi.fn(() => null) },
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadIdeate(ctx) {
  const code = readFileSync(join(jsDir, "ideate.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "ideate.js") });
  return ctx;
}

describe("ideate.js (legacy shim)", () => {
  it("stub helpers are callable and return undefined", () => {
    const ctx = makeContext();
    loadIdeate(ctx);
    expect(ctx.toggleIdeation()).toBeUndefined();
    expect(ctx.toggleIdeationHeader()).toBeUndefined();
    expect(ctx.updateIdeationInterval()).toBeUndefined();
    expect(ctx.updateIdeationExploitRatio()).toBeUndefined();
    expect(ctx.updateIdeationConfig({})).toBeUndefined();
    expect(ctx.updateIdeationFromTasks([])).toBeUndefined();
    expect(ctx.setIdeationRunning(true)).toBeUndefined();
    expect(ctx.updateNextRunDisplay()).toBeUndefined();
    expect(ctx.updateExploitRatioLabel(50)).toBeUndefined();
  });

  describe("triggerIdeation", () => {
    it("POSTs /api/ideate and waits for the returned task", async () => {
      const api = vi.fn().mockResolvedValue({ task_id: "new-task-id" });
      const waitForTaskDelta = vi.fn();
      const ctx = makeContext({ api, waitForTaskDelta });
      loadIdeate(ctx);
      await ctx.triggerIdeation();
      expect(api).toHaveBeenCalledWith(
        "/api/ideate",
        expect.objectContaining({ method: "POST" }),
      );
      expect(waitForTaskDelta).toHaveBeenCalledWith("new-task-id");
    });

    it("falls back to fetchTasks when no task id is returned", async () => {
      const api = vi.fn().mockResolvedValue({});
      const fetchTasks = vi.fn();
      const ctx = makeContext({ api, fetchTasks });
      loadIdeate(ctx);
      await ctx.triggerIdeation();
      expect(fetchTasks).toHaveBeenCalled();
    });

    it("surfaces errors via showAlert", async () => {
      const api = vi.fn().mockRejectedValue(new Error("boom"));
      const showAlert = vi.fn();
      const ctx = makeContext({ api, showAlert });
      loadIdeate(ctx);
      await ctx.triggerIdeation();
      expect(showAlert).toHaveBeenCalled();
    });
  });
});
