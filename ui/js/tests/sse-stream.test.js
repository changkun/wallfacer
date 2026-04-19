/**
 * Tests for lib/sse-stream.ts — managed EventSource with jittered
 * exponential backoff.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "build/lib/sse-stream.js"), "utf8");

function makeMockEventSource() {
  const instances = [];
  class MockES {
    constructor(url) {
      this.url = url;
      this.readyState = 0;
      this.onmessage = null;
      this.onerror = null;
      this._listeners = {};
      this.close = vi.fn(() => {
        this.readyState = 2;
      });
      instances.push(this);
    }
    addEventListener(name, fn) {
      this._listeners[name] = fn;
    }
    /** Simulate an error on a disconnected socket. */
    fireClose() {
      this.readyState = 2; // CLOSED
      if (this.onerror) this.onerror();
    }
  }
  MockES.CLOSED = 2;
  return { MockES, instances };
}

function makeCtx(overrides = {}) {
  const { MockES, instances } = overrides.mock || makeMockEventSource();
  const timers = [];
  const ctx = {
    Math,
    Object,
    EventSource: MockES,
    setTimeout: (fn, delay) => {
      const id = timers.length + 1;
      timers.push({ id, fn, delay });
      return id;
    },
    clearTimeout: vi.fn(),
    withAuthToken: (u) => u + "?tok=x",
  };
  vm.runInContext(code, vm.createContext(ctx));
  return { ctx, timers, instances };
}

describe("createSSEStream", () => {
  let makeRandom;
  beforeEach(() => {
    // Deterministic jitter: Math.random → 0 (lower bound of [base, 2×base]).
    makeRandom = vi.spyOn(Math, "random").mockReturnValue(0);
  });

  it("opens an EventSource through withAuthToken", () => {
    const { ctx, instances } = makeCtx();
    const handle = ctx.createSSEStream({ url: "/stream" });
    expect(instances).toHaveLength(1);
    expect(instances[0].url).toBe("/stream?tok=x");
    expect(handle).toHaveProperty("stop");
  });

  it("dispatches onMessage for default message events", () => {
    const { ctx, instances } = makeCtx();
    const onMessage = vi.fn();
    ctx.createSSEStream({ url: "/stream", onMessage });
    instances[0].onmessage({ data: "hi" });
    expect(onMessage).toHaveBeenCalledWith({ data: "hi" });
  });

  it("dispatches named listeners", () => {
    const { ctx, instances } = makeCtx();
    const onSnap = vi.fn();
    ctx.createSSEStream({
      url: "/stream",
      listeners: { snapshot: onSnap },
    });
    instances[0]._listeners.snapshot({ data: "s" });
    expect(onSnap).toHaveBeenCalledWith({ data: "s" });
  });

  it("schedules a reconnect with jitter on close, doubling the delay", () => {
    const { ctx, instances, timers } = makeCtx();
    ctx.createSSEStream({
      url: "/stream",
      initialDelay: 100,
      maxDelay: 5000,
    });
    instances[0].fireClose();
    expect(timers).toHaveLength(1);
    // Math.random() → 0, so jitter = delay * (1 + 0) = delay.
    expect(timers[0].delay).toBe(100);

    // Simulate the retry firing — it should create a new EventSource
    // and use a doubled delay next time.
    timers[0].fn();
    expect(instances).toHaveLength(2);
    instances[1].fireClose();
    expect(timers[1].delay).toBe(200);
  });

  it("caps the retry delay at maxDelay", () => {
    const { ctx, instances, timers } = makeCtx();
    ctx.createSSEStream({
      url: "/stream",
      initialDelay: 1000,
      maxDelay: 2000,
    });
    // Close, retry, close, retry, close — expect delay capped at 2000.
    instances[0].fireClose();
    timers[0].fn();
    instances[1].fireClose();
    timers[1].fn();
    instances[2].fireClose();
    expect(timers[2].delay).toBe(2000);
  });

  it("resets retry delay when a message arrives", () => {
    const { ctx, instances, timers } = makeCtx();
    ctx.createSSEStream({
      url: "/stream",
      onMessage: () => {},
      initialDelay: 100,
    });
    // Error bumps delay to 200.
    instances[0].fireClose();
    timers[0].fn();
    // New connection receives a message: delay should reset to 100.
    instances[1].onmessage({ data: "hello" });
    instances[1].fireClose();
    expect(timers[1].delay).toBe(100);
  });

  it("stop() closes the source and cancels pending retries", () => {
    const { ctx, instances, timers } = makeCtx();
    const handle = ctx.createSSEStream({ url: "/stream" });
    instances[0].fireClose();
    expect(timers).toHaveLength(1);
    handle.stop();
    expect(ctx.clearTimeout).toHaveBeenCalled();
    // A stop after close should not reconnect when the retry fires late.
    timers[0].fn();
    expect(instances).toHaveLength(1);
  });

  it("ignores errors on sockets that are still open", () => {
    const { ctx, instances, timers } = makeCtx();
    ctx.createSSEStream({ url: "/stream" });
    // Transient error without CLOSED state — should not schedule a retry.
    instances[0].readyState = 1; // OPEN
    if (instances[0].onerror) instances[0].onerror();
    expect(timers).toHaveLength(0);
  });

  it("invokes onCreate with each new source", () => {
    const { ctx, instances } = makeCtx();
    const onCreate = vi.fn();
    ctx.createSSEStream({ url: "/stream", onCreate });
    expect(onCreate).toHaveBeenCalledWith(instances[0]);
  });
});
