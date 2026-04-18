import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeElement(overrides = {}) {
  return {
    id: "",
    innerHTML: "",
    textContent: "",
    style: { cssText: "", display: "" },
    disabled: false,
    scrollTop: 0,
    scrollHeight: 100,
    ...overrides,
  };
}

class MockEventSource {
  constructor(url) {
    this.url = url;
    this.readyState = 0;
    this._listeners = {};
  }
  addEventListener(type, fn) {
    if (!this._listeners[type]) this._listeners[type] = [];
    this._listeners[type].push(fn);
  }
  close() {
    this.readyState = 2;
  }
  _emit(type, data) {
    (this._listeners[type] || []).forEach((fn) => {
      fn(data);
    });
  }
}
MockEventSource.CLOSED = 2;

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const created = [];
  const ctx = {
    console,
    JSON,
    String,
    Array,
    Error,
    encodeURIComponent,
    setTimeout: vi.fn((fn) => fn()),
    EventSource: overrides.EventSource || MockEventSource,
    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: (tag) => {
        const el = makeElement({ tagName: tag });
        created.push(el);
        return el;
      },
      querySelector: () => null,
      addEventListener: vi.fn(),
    },
    api: overrides.api || vi.fn(),
    Routes: {
      images: {
        status: () => "/api/images",
        pull: () => "/api/images/pull",
        pullStream: () => "/api/images/pull/stream",
        remove: () => "/api/images",
      },
      config: {
        get: () => "/api/config",
      },
    },
    escapeHtml: (s) => String(s),
    showAlert: vi.fn(),
    showConfirm: overrides.showConfirm || vi.fn().mockResolvedValue(true),
    _created: created,
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "images.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "images.js") });
  return ctx;
}

describe("images.js", () => {
  describe("_pullPhaseText", () => {
    it("returns resolving text", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._pullPhaseText("resolving", 0)).toBe("Resolving image\u2026");
    });

    it("returns copying text with layer count", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._pullPhaseText("copying", 5)).toBe(
        "Downloading layers (5 copied)\u2026",
      );
    });

    it("returns manifest text", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._pullPhaseText("manifest", 0)).toBe("Writing manifest\u2026");
    });

    it("returns done text", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._pullPhaseText("done", 0)).toBe("Pull complete");
    });

    it("returns error text", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._pullPhaseText("error", 0)).toBe("Error");
    });

    it("returns phase as-is for unknown phase", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._pullPhaseText("custom_phase", 0)).toBe("custom_phase");
    });
  });

  describe("loadImageStatus", () => {
    it("renders cached image status", async () => {
      const container = makeElement();
      const children = [];
      container.appendChild = (el) => children.push(el);
      const ctx = makeContext({
        elements: [["sandbox-images-list", container]],
        api: vi.fn().mockResolvedValue({
          images: [
            {
              sandbox: "claude",
              cached: true,
              image: "claude:latest",
              size: "2GB",
            },
          ],
        }),
      });
      loadScript(ctx);
      await ctx.loadImageStatus();
      // container.innerHTML is set to "" then children are appended
      expect(ctx.api).toHaveBeenCalledWith("/api/images");
      expect(children.length).toBeGreaterThan(0);
    });

    it("renders missing image status", async () => {
      const container = makeElement();
      const children = [];
      container.appendChild = (el) => children.push(el);
      const ctx = makeContext({
        elements: [["sandbox-images-list", container]],
        api: vi.fn().mockResolvedValue({
          images: [{ sandbox: "codex", cached: false, image: "codex:latest" }],
        }),
      });
      loadScript(ctx);
      await ctx.loadImageStatus();
      expect(children.length).toBeGreaterThan(0);
    });

    it("shows error on API failure", async () => {
      const container = makeElement();
      const ctx = makeContext({
        elements: [["sandbox-images-list", container]],
        api: vi.fn().mockRejectedValue(new Error("fetch failed")),
      });
      loadScript(ctx);
      await ctx.loadImageStatus();
      expect(container.innerHTML).toContain("Failed to load");
    });

    it("does nothing when container element is missing", async () => {
      const ctx = makeContext({ elements: [] });
      loadScript(ctx);
      await ctx.loadImageStatus(); // should not throw
    });
  });

  describe("deleteSandboxImage", () => {
    it("calls API and reloads on confirm", async () => {
      const container = makeElement();
      container.appendChild = vi.fn();
      const apiMock = vi
        .fn()
        .mockResolvedValueOnce({}) // delete
        .mockResolvedValueOnce({ images: [] }); // reload
      const ctx = makeContext({
        elements: [["sandbox-images-list", container]],
        api: apiMock,
        showConfirm: vi.fn().mockResolvedValue(true),
      });
      loadScript(ctx);
      await ctx.deleteSandboxImage("claude");
      expect(apiMock).toHaveBeenCalledWith("/api/images", {
        method: "DELETE",
        body: JSON.stringify({ sandbox: "claude" }),
      });
    });

    it("does nothing when user cancels confirm", async () => {
      const apiMock = vi.fn();
      const ctx = makeContext({
        api: apiMock,
        showConfirm: vi.fn().mockResolvedValue(false),
      });
      loadScript(ctx);
      await ctx.deleteSandboxImage("claude");
      expect(apiMock).not.toHaveBeenCalled();
    });

    it("shows alert on API error", async () => {
      const ctx = makeContext({
        api: vi.fn().mockRejectedValue(new Error("nope")),
        showConfirm: vi.fn().mockResolvedValue(true),
      });
      loadScript(ctx);
      await ctx.deleteSandboxImage("claude");
      expect(ctx.showAlert).toHaveBeenCalledWith(
        "Failed to remove image: nope",
      );
    });
  });

  describe("pullSandboxImage", () => {
    it("starts pull and connects stream", async () => {
      const btn = makeElement({ id: "pull-btn-claude" });
      const progress = makeElement({ id: "pull-progress-claude" });
      const summary = makeElement({ id: "pull-summary-claude" });
      const ctx = makeContext({
        elements: [
          ["pull-btn-claude", btn],
          ["pull-progress-claude", progress],
          ["pull-summary-claude", summary],
        ],
        api: vi.fn().mockResolvedValue({ pull_id: "pull-123" }),
      });
      loadScript(ctx);
      await ctx.pullSandboxImage("claude");
      expect(ctx.api).toHaveBeenCalledWith("/api/images/pull", {
        method: "POST",
        body: JSON.stringify({ sandbox: "claude" }),
      });
      expect(btn.disabled).toBe(true);
    });

    it("shows error when no pull_id returned", async () => {
      const btn = makeElement({ id: "pull-btn-claude" });
      const progress = makeElement({ id: "pull-progress-claude" });
      const ctx = makeContext({
        elements: [
          ["pull-btn-claude", btn],
          ["pull-progress-claude", progress],
        ],
        api: vi.fn().mockResolvedValue({}),
      });
      loadScript(ctx);
      await ctx.pullSandboxImage("claude");
      expect(progress.textContent).toContain("Error");
      expect(btn.disabled).toBe(false);
      expect(btn.textContent).toBe("Retry");
    });

    it("does nothing when btn or progress missing", async () => {
      const ctx = makeContext({ elements: [] });
      loadScript(ctx);
      await ctx.pullSandboxImage("claude"); // should not throw
    });
  });

  describe("updateHostModeBanner", () => {
    function makeBannerContext(cfg) {
      const banner = makeElement({ hidden: true });
      const ctx = makeContext({
        api: vi.fn(async (route) => {
          if (route === "/api/config") return cfg;
          return null;
        }),
      });
      ctx.document.querySelector = (sel) =>
        sel === "[data-js-host-mode-banner]" ? banner : null;
      return { ctx, banner };
    }

    it("shows banner when host_mode is true", async () => {
      const { ctx, banner } = makeBannerContext({ host_mode: true });
      loadScript(ctx);
      await ctx.updateHostModeBanner();
      // Allow the microtask queue to drain so the .then() resolves.
      await Promise.resolve();
      expect(banner.hidden).toBe(false);
    });

    it("hides banner when host_mode is false", async () => {
      const { ctx, banner } = makeBannerContext({ host_mode: false });
      banner.hidden = false; // pretend it was shown, assert we hide it
      loadScript(ctx);
      await ctx.updateHostModeBanner();
      await Promise.resolve();
      expect(banner.hidden).toBe(true);
    });

    it("is a no-op when banner is absent", async () => {
      const ctx = makeContext();
      ctx.document.querySelector = () => null;
      loadScript(ctx);
      // Should not throw even though no banner exists.
      await ctx.updateHostModeBanner();
    });
  });
});
