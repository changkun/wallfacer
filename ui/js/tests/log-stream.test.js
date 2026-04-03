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
    Promise,
    TextDecoder: overrides.TextDecoder || class {
      decode(chunk) { return new TextDecoder().decode(chunk); }
    },
    AbortController: overrides.AbortController || class {
      constructor() {
        this.signal = { addEventListener: vi.fn() };
        this.abort = vi.fn();
      }
    },
    fetch: overrides.fetch || vi.fn(),
    withAuthToken: (url) => url + "?auth=1",
    withBearerHeaders: () => ({ Authorization: "Bearer tok" }),
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "lib/log-stream.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "lib/log-stream.js") });
  return ctx;
}

describe("lib/log-stream.js", () => {
  describe("startStreamingFetch", () => {
    it("returns an object with abort function", () => {
      const ctx = makeContext({
        fetch: vi.fn().mockResolvedValue({
          ok: true,
          body: { getReader: () => ({ read: vi.fn().mockResolvedValue({ done: true }) }) },
        }),
      });
      loadScript(ctx);
      const handle = ctx.startStreamingFetch({
        url: "/test",
        onChunk: vi.fn(),
      });
      expect(handle).toHaveProperty("abort");
      expect(typeof handle.abort).toBe("function");
    });

    it("calls fetch with auth token and bearer headers", async () => {
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        body: {
          getReader: () => ({
            read: vi.fn().mockResolvedValue({ done: true }),
          }),
        },
      });
      const ctx = makeContext({ fetch: fetchMock });
      loadScript(ctx);
      ctx.startStreamingFetch({
        url: "/api/logs",
        onChunk: vi.fn(),
      });
      await new Promise((r) => setTimeout(r, 10));
      expect(fetchMock).toHaveBeenCalledWith("/api/logs?auth=1", {
        signal: expect.anything(),
        headers: { Authorization: "Bearer tok" },
      });
    });

    it("calls onError when response is not ok", async () => {
      const fetchMock = vi.fn().mockResolvedValue({ ok: false, body: null });
      const onError = vi.fn();
      const ctx = makeContext({ fetch: fetchMock });
      loadScript(ctx);
      ctx.startStreamingFetch({
        url: "/test",
        onChunk: vi.fn(),
        onError,
      });
      await new Promise((r) => setTimeout(r, 10));
      expect(onError).toHaveBeenCalled();
    });

    it("calls onDone when stream completes", async () => {
      const onDone = vi.fn();
      const onChunk = vi.fn();
      const reader = {
        read: vi.fn()
          .mockResolvedValueOnce({
            done: false,
            value: new TextEncoder().encode("hello"),
          })
          .mockResolvedValueOnce({ done: true }),
      };
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        body: { getReader: () => reader },
      });
      const ctx = makeContext({ fetch: fetchMock });
      loadScript(ctx);
      ctx.startStreamingFetch({
        url: "/test",
        onChunk,
        onDone,
      });
      await new Promise((r) => setTimeout(r, 50));
      expect(onChunk).toHaveBeenCalled();
      expect(onDone).toHaveBeenCalledWith(true);
    });

    it("calls onFirstChunk once before first chunk", async () => {
      const onFirstChunk = vi.fn();
      const onChunk = vi.fn();
      const reader = {
        read: vi.fn()
          .mockResolvedValueOnce({
            done: false,
            value: new TextEncoder().encode("a"),
          })
          .mockResolvedValueOnce({
            done: false,
            value: new TextEncoder().encode("b"),
          })
          .mockResolvedValueOnce({ done: true }),
      };
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        body: { getReader: () => reader },
      });
      const ctx = makeContext({ fetch: fetchMock });
      loadScript(ctx);
      ctx.startStreamingFetch({
        url: "/test",
        onChunk,
        onFirstChunk,
      });
      await new Promise((r) => setTimeout(r, 50));
      expect(onFirstChunk).toHaveBeenCalledTimes(1);
      expect(onChunk).toHaveBeenCalledTimes(2);
    });

    it("stops reading when isStale returns true", async () => {
      const onChunk = vi.fn();
      let stale = false;
      const reader = {
        read: vi.fn()
          .mockResolvedValueOnce({
            done: false,
            value: new TextEncoder().encode("first"),
          })
          .mockImplementation(() => {
            stale = true;
            return Promise.resolve({
              done: false,
              value: new TextEncoder().encode("second"),
            });
          }),
      };
      const fetchMock = vi.fn().mockResolvedValue({
        ok: true,
        body: { getReader: () => reader },
      });
      const ctx = makeContext({ fetch: fetchMock });
      loadScript(ctx);
      ctx.startStreamingFetch({
        url: "/test",
        onChunk,
        isStale: () => stale,
      });
      await new Promise((r) => setTimeout(r, 50));
      // Should have processed first chunk but stopped after stale
      expect(onChunk).toHaveBeenCalledTimes(1);
    });

    it("does not call onError on AbortError", async () => {
      const onError = vi.fn();
      const err = new Error("aborted");
      err.name = "AbortError";
      const fetchMock = vi.fn().mockRejectedValue(err);
      const ctx = makeContext({ fetch: fetchMock });
      loadScript(ctx);
      ctx.startStreamingFetch({
        url: "/test",
        onChunk: vi.fn(),
        onError,
      });
      await new Promise((r) => setTimeout(r, 10));
      expect(onError).not.toHaveBeenCalled();
    });

    it("calls onError on non-abort fetch errors", async () => {
      const onError = vi.fn();
      const fetchMock = vi.fn().mockRejectedValue(new Error("network"));
      const ctx = makeContext({ fetch: fetchMock });
      loadScript(ctx);
      ctx.startStreamingFetch({
        url: "/test",
        onChunk: vi.fn(),
        onError,
      });
      await new Promise((r) => setTimeout(r, 10));
      expect(onError).toHaveBeenCalled();
    });

    it("merges external abort signal", () => {
      const externalSignal = { addEventListener: vi.fn() };
      const ctx = makeContext({
        fetch: vi.fn().mockResolvedValue({ ok: false, body: null }),
      });
      loadScript(ctx);
      ctx.startStreamingFetch({
        url: "/test",
        onChunk: vi.fn(),
        signal: externalSignal,
      });
      expect(externalSignal.addEventListener).toHaveBeenCalledWith(
        "abort",
        expect.any(Function),
      );
    });
  });
});
