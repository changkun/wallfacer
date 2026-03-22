/**
 * Tests for transport.js — auth helpers and fetch wrappers.
 *
 * These are pure unit tests for the lowest-level layer: token extraction,
 * auth header injection, URL token append, and the api() fetch wrapper.
 */
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
    fetch: overrides.fetch || vi.fn(),
    document: overrides.document || {
      getElementById: () => null,
      querySelector: () => null,
      querySelectorAll: () => [],
      addEventListener: () => {},
      documentElement: { setAttribute: () => {} },
      readyState: "complete",
    },
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

function makeTokenDoc(token) {
  return {
    getElementById: () => null,
    querySelectorAll: () => [],
    querySelector: (sel) =>
      sel === 'meta[name="wallfacer-token"]' ? { content: token } : null,
    addEventListener: () => {},
    documentElement: { setAttribute: () => {} },
    readyState: "complete",
  };
}

// ---------------------------------------------------------------------------
// getWallfacerToken
// ---------------------------------------------------------------------------

describe("getWallfacerToken", () => {
  it("returns empty string when the meta tag is absent", () => {
    const ctx = loadScript(makeContext(), "transport.js");
    expect(ctx.getWallfacerToken()).toBe("");
  });

  it("returns the token content from the meta tag", () => {
    const ctx = loadScript(
      makeContext({ document: makeTokenDoc("my-token") }),
      "transport.js",
    );
    expect(ctx.getWallfacerToken()).toBe("my-token");
  });
});

// ---------------------------------------------------------------------------
// withAuthHeaders
// ---------------------------------------------------------------------------

describe("withAuthHeaders", () => {
  it("does not add Authorization for GET requests even when a token is present", () => {
    const ctx = loadScript(
      makeContext({ document: makeTokenDoc("tok") }),
      "transport.js",
    );
    const headers = ctx.withAuthHeaders({}, "GET");
    expect(headers.Authorization).toBeUndefined();
  });

  it("adds Bearer Authorization for POST requests when a token is present", () => {
    const ctx = loadScript(
      makeContext({ document: makeTokenDoc("secret") }),
      "transport.js",
    );
    const headers = ctx.withAuthHeaders({}, "POST");
    expect(headers.Authorization).toBe("Bearer secret");
  });

  it("does not add Authorization when no token is present", () => {
    const ctx = loadScript(makeContext(), "transport.js");
    const headers = ctx.withAuthHeaders({}, "POST");
    expect(headers.Authorization).toBeUndefined();
  });

  it("merges extra headers without mutating the input", () => {
    const ctx = loadScript(makeContext(), "transport.js");
    const input = { "Content-Type": "application/json" };
    const result = ctx.withAuthHeaders(input, "POST");
    expect(result["Content-Type"]).toBe("application/json");
    expect(input).toEqual({ "Content-Type": "application/json" }); // not mutated
  });
});

// ---------------------------------------------------------------------------
// withBearerHeaders
// ---------------------------------------------------------------------------

describe("withBearerHeaders", () => {
  it("adds Authorization unconditionally (used for streaming GET)", () => {
    const ctx = loadScript(
      makeContext({ document: makeTokenDoc("bearer-tok") }),
      "transport.js",
    );
    const headers = ctx.withBearerHeaders({});
    expect(headers.Authorization).toBe("Bearer bearer-tok");
  });

  it("returns headers without Authorization when no token is present", () => {
    const ctx = loadScript(makeContext(), "transport.js");
    const headers = ctx.withBearerHeaders({ "X-Custom": "yes" });
    expect(headers.Authorization).toBeUndefined();
    expect(headers["X-Custom"]).toBe("yes");
  });
});

// ---------------------------------------------------------------------------
// withAuthToken
// ---------------------------------------------------------------------------

describe("withAuthToken", () => {
  it("appends token as the first query parameter when URL has no query string", () => {
    const ctx = loadScript(
      makeContext({ document: makeTokenDoc("tok123") }),
      "transport.js",
    );
    expect(ctx.withAuthToken("/api/tasks/stream")).toBe(
      "/api/tasks/stream?token=tok123",
    );
  });

  it("appends token with & when URL already has query parameters", () => {
    const ctx = loadScript(
      makeContext({ document: makeTokenDoc("tok123") }),
      "transport.js",
    );
    expect(ctx.withAuthToken("/api/tasks/stream?foo=bar")).toBe(
      "/api/tasks/stream?foo=bar&token=tok123",
    );
  });

  it("returns the URL unchanged when no token is present", () => {
    const ctx = loadScript(makeContext(), "transport.js");
    expect(ctx.withAuthToken("/api/tasks/stream")).toBe("/api/tasks/stream");
  });

  it("URL-encodes the token", () => {
    const ctx = loadScript(
      makeContext({ document: makeTokenDoc("a b+c") }),
      "transport.js",
    );
    const url = ctx.withAuthToken("/api/stream");
    expect(url).toContain("token=a%20b%2Bc");
  });
});

// ---------------------------------------------------------------------------
// api() fetch wrapper
// ---------------------------------------------------------------------------

describe("api()", () => {
  it("adds bearer auth to non-GET requests when a token meta tag is present", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue({ ok: true, status: 204, text: async () => "" });
    const ctx = loadScript(
      makeContext({ fetch, document: makeTokenDoc("secret-token") }),
      "transport.js",
    );

    await ctx.api("/api/tasks", { method: "POST", body: "{}" });

    expect(fetch).toHaveBeenCalledWith(
      "/api/tasks",
      expect.objectContaining({
        headers: expect.objectContaining({
          Authorization: "Bearer secret-token",
        }),
      }),
    );
  });

  it("does not add Authorization to GET requests", async () => {
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({}),
      text: async () => "",
    });
    const ctx = loadScript(
      makeContext({ fetch, document: makeTokenDoc("secret") }),
      "transport.js",
    );

    await ctx.api("/api/tasks", { method: "GET" });

    const callHeaders = fetch.mock.calls[0][1].headers;
    expect(callHeaders.Authorization).toBeUndefined();
  });

  it("throws on non-2xx responses", async () => {
    const fetch = vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      text: async () => "Internal Server Error",
    });
    const ctx = loadScript(makeContext({ fetch }), "transport.js");

    await expect(ctx.api("/api/tasks")).rejects.toThrow(
      "Internal Server Error",
    );
  });

  it("returns null for 204 No Content", async () => {
    const fetch = vi
      .fn()
      .mockResolvedValue({ ok: true, status: 204, text: async () => "" });
    const ctx = loadScript(makeContext({ fetch }), "transport.js");

    const result = await ctx.api("/api/tasks/1/cancel", { method: "POST" });
    expect(result).toBeNull();
  });

  it("parses and returns JSON on success", async () => {
    const payload = { id: "abc", status: "done" };
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => payload,
      text: async () => "",
    });
    const ctx = loadScript(makeContext({ fetch }), "transport.js");

    const result = await ctx.api("/api/tasks/abc");
    expect(result).toEqual(payload);
  });
});

// ---------------------------------------------------------------------------
// SSE URL token injection — integration seam between transport and task-stream
// ---------------------------------------------------------------------------

describe("SSE stream token injection (via startTasksStream)", () => {
  it("appends the token to the task stream EventSource URL", () => {
    const instances = [];
    class MockEventSource {
      constructor(url) {
        this.url = url;
        this.readyState = 1;
        this.listeners = {};
        instances.push(this);
      }
      addEventListener(type, handler) {
        this.listeners[type] = handler;
      }
      close() {}
    }

    const ctx = vm.createContext({
      console,
      Date,
      Math,
      setTimeout: vi.fn(),
      clearTimeout,
      EventSource: MockEventSource,
      document: makeTokenDoc("secret-token"),
      Routes: { tasks: { stream: () => "/api/tasks/stream" } },
      scheduleRender: vi.fn(),
      invalidateDiffBehindCounts: vi.fn(),
      announceBoardStatus: vi.fn(),
      getTaskAccessibleTitle: vi.fn(() => "Task"),
      formatTaskStatusLabel: vi.fn(() => "done"),
      location: { hash: "" },
      localStorage: { getItem: vi.fn(), setItem: vi.fn() },
      _sseIsLeader: () => true,
      _sseRelay: vi.fn(),
      _sseOnFollowerEvent: vi.fn(),
    });

    const code = readFileSync(join(jsDir, "state.js"), "utf8");
    vm.runInContext(code, ctx, { filename: join(jsDir, "state.js") });
    loadScript(ctx, "transport.js");
    loadScript(ctx, "task-stream.js");
    loadScript(ctx, "api.js");

    vm.runInContext('activeWorkspaces = ["/Users/test/repo"];', ctx);
    ctx.startTasksStream();

    expect(instances[0].url).toBe("/api/tasks/stream?token=secret-token");
  });
});
