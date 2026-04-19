import { describe, it, expect, vi, beforeEach } from "vitest";
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
    Math,
    Number,
    parseInt,
    String,
    JSON,
    setTimeout: vi.fn(),
    setInterval: vi.fn(),
    clearInterval: vi.fn(),
    Set,
    Array,
    escapeHtml: (s) => String(s ?? "").replace(/[&<>"']/g, (c) => c),
    api: vi.fn().mockResolvedValue({}),
    showAlert: vi.fn(),
    showToast: vi.fn(),
    fetchTasks: vi.fn(),
    Routes: {
      routines: {
        list: () => "/api/routines",
        create: () => "/api/routines",
        updateSchedule: () => "/api/routines/{id}/schedule",
        trigger: () => "/api/routines/{id}/trigger",
      },
    },
    document: {
      querySelectorAll: vi.fn().mockReturnValue([]),
    },
    encodeURIComponent,
    module: { exports: {} },
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadRoutines(ctx) {
  const code = readFileSync(join(jsDir, "routines.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "routines.js") });
  return ctx;
}

describe("routines.js", () => {
  describe("formatRoutineCountdown", () => {
    let ctx;
    beforeEach(() => {
      ctx = makeContext();
      loadRoutines(ctx);
    });

    it("returns 'paused' when disabled", () => {
      expect(ctx.formatRoutineCountdown("2030-01-01T00:00:00Z", false)).toBe(
        "paused",
      );
    });

    it("returns 're-arming…' when enabled but next-run missing", () => {
      expect(ctx.formatRoutineCountdown(null, true)).toBe("re-arming\u2026");
    });

    it("returns 'stopped' for a cancelled routine, even if still enabled", () => {
      // Regression: the previous formatter said "re-arming…" for a
      // cancelled routine that still had routine_enabled=true. The
      // engine has unregistered it, so the misleading label confused
      // users into thinking the card was still about to fire.
      expect(ctx.formatRoutineCountdown(null, true, "cancelled", false)).toBe(
        "stopped",
      );
      expect(ctx.formatRoutineCountdown(null, true, "done", false)).toBe(
        "stopped",
      );
      expect(ctx.formatRoutineCountdown(null, true, "failed", false)).toBe(
        "stopped",
      );
    });

    it("returns 'stopped (archived)' for an archived routine", () => {
      expect(
        ctx.formatRoutineCountdown(null, true, "cancelled", true),
      ).toBe("stopped (archived)");
    });

    it("still fires a normal countdown for a backlog routine", () => {
      const future = new Date(Date.now() + 60 * 1000).toISOString();
      expect(
        ctx.formatRoutineCountdown(future, true, "backlog", false),
      ).toMatch(/^in \d+s$|^in 1m \d+s$/);
    });

    it("returns 'fired just now' for past timestamps", () => {
      const past = new Date(Date.now() - 5000).toISOString();
      expect(ctx.formatRoutineCountdown(past, true)).toBe("fired just now");
    });

    it("formats minute + second countdowns", () => {
      const future = new Date(Date.now() + 125 * 1000).toISOString();
      const got = ctx.formatRoutineCountdown(future, true);
      // Accept "in 2m 5s" or off-by-one second due to tick jitter.
      expect(got).toMatch(/^in 2m \d{1,2}s$/);
    });

    it("formats hour countdowns", () => {
      const future = new Date(
        Date.now() + 2 * 3600 * 1000 + 30 * 60 * 1000,
      ).toISOString();
      const got = ctx.formatRoutineCountdown(future, true);
      expect(got).toMatch(/^in 2h \d{1,2}m$/);
    });
  });

  describe("formatRoutineLastFired", () => {
    let ctx;
    beforeEach(() => {
      ctx = makeContext();
      loadRoutines(ctx);
    });

    it("returns empty for null", () => {
      expect(ctx.formatRoutineLastFired(null)).toBe("");
    });

    it("formats seconds", () => {
      const past = new Date(Date.now() - 12 * 1000).toISOString();
      expect(ctx.formatRoutineLastFired(past)).toMatch(/^fired \d+s ago$/);
    });

    it("formats minutes", () => {
      const past = new Date(Date.now() - 5 * 60 * 1000).toISOString();
      expect(ctx.formatRoutineLastFired(past)).toMatch(/^fired \d+m ago$/);
    });
  });

  describe("currentIntervalOptions", () => {
    let ctx;
    beforeEach(() => {
      ctx = makeContext();
      loadRoutines(ctx);
    });

    it("returns the default set when current is already included", () => {
      const got = ctx.currentIntervalOptions(60);
      expect(got).toContain(60);
      expect(got).toContain(1);
    });

    it("injects an unusual current value and sorts", () => {
      const got = ctx.currentIntervalOptions(7);
      expect(got).toContain(7);
      const sorted = [...got].sort((a, b) => a - b);
      expect(got).toEqual(sorted);
    });
  });

  describe("renderRoutineFooter", () => {
    let ctx;
    beforeEach(() => {
      ctx = makeContext();
      loadRoutines(ctx);
    });

    it("renders the interval and toggle matching the task", () => {
      const html = ctx.renderRoutineFooter({
        id: "abc",
        routine_interval_seconds: 1800,
        routine_enabled: true,
        routine_next_run: null,
        routine_last_fired_at: null,
        routine_spawn_kind: "",
      });
      expect(html).toContain('<option value="30" selected');
      expect(html).toContain("checked");
      expect(html).toContain("Run now");
      expect(html).toContain('data-routine-id="abc"');
    });

    it("marks the footer paused when disabled", () => {
      const html = ctx.renderRoutineFooter({
        id: "x",
        routine_interval_seconds: 3600,
        routine_enabled: false,
        routine_next_run: null,
        routine_last_fired_at: null,
        routine_spawn_kind: "idea-agent",
      });
      expect(html).toContain("paused");
      expect(html).toContain('data-routine-enabled="0"');
      expect(html).toContain("idea-agent");
    });
  });

  describe("schedule editors", () => {
    it("PATCHes the interval on change", async () => {
      const apiMock = vi.fn().mockResolvedValue({});
      const ctx = makeContext({ api: apiMock });
      loadRoutines(ctx);
      await ctx.onRoutineIntervalChange("abc", "45");
      expect(apiMock).toHaveBeenCalledWith(
        "/api/routines/abc/schedule",
        expect.objectContaining({
          method: "PATCH",
          body: JSON.stringify({ interval_minutes: 45 }),
        }),
      );
    });

    it("PATCHes the enabled flag on toggle", async () => {
      const apiMock = vi.fn().mockResolvedValue({});
      const ctx = makeContext({ api: apiMock });
      loadRoutines(ctx);
      await ctx.onRoutineEnabledChange("xyz", false);
      expect(apiMock).toHaveBeenCalledWith(
        "/api/routines/xyz/schedule",
        expect.objectContaining({
          method: "PATCH",
          body: JSON.stringify({ enabled: false }),
        }),
      );
    });

    it("ignores non-finite intervals", async () => {
      const apiMock = vi.fn().mockResolvedValue({});
      const ctx = makeContext({ api: apiMock });
      loadRoutines(ctx);
      await ctx.onRoutineIntervalChange("abc", "NaN");
      expect(apiMock).not.toHaveBeenCalled();
    });

    it("surfaces errors via showAlert and refetches", async () => {
      const apiMock = vi.fn().mockRejectedValue(new Error("boom"));
      const alert = vi.fn();
      const fetchTasks = vi.fn();
      const ctx = makeContext({ api: apiMock, showAlert: alert, fetchTasks });
      loadRoutines(ctx);
      await ctx.onRoutineIntervalChange("id", "5");
      expect(alert).toHaveBeenCalled();
      expect(fetchTasks).toHaveBeenCalled();
    });
  });

  describe("trigger", () => {
    it("POSTs to the trigger route and shows a toast", async () => {
      const apiMock = vi.fn().mockResolvedValue({});
      const toast = vi.fn();
      const ctx = makeContext({ api: apiMock, showToast: toast });
      loadRoutines(ctx);
      await ctx.onRoutineTrigger("id1");
      expect(apiMock).toHaveBeenCalledWith(
        "/api/routines/id1/trigger",
        expect.objectContaining({ method: "POST" }),
      );
      expect(toast).toHaveBeenCalled();
    });
  });

  describe("createRoutineFromPrompt", () => {
    it("POSTs to the create route with the supplied fields", async () => {
      const apiMock = vi.fn().mockResolvedValue({ id: "new" });
      const ctx = makeContext({ api: apiMock });
      loadRoutines(ctx);
      const res = await ctx.createRoutineFromPrompt("daily triage", {
        intervalMinutes: 30,
        spawnKind: "task",
        enabled: true,
        tags: ["ops"],
      });
      expect(res.id).toBe("new");
      expect(apiMock).toHaveBeenCalledWith(
        "/api/routines",
        expect.objectContaining({ method: "POST" }),
      );
      const body = JSON.parse(apiMock.mock.calls[0][1].body);
      expect(body.interval_minutes).toBe(30);
      expect(body.spawn_kind).toBe("task");
      expect(body.tags).toEqual(["ops"]);
    });
  });
});
