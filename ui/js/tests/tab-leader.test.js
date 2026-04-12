import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Minimal BroadcastChannel mock that routes messages between instances. */
function createBroadcastChannelMock() {
  const channels = new Map(); // name → Set<instance>

  class MockBroadcastChannel {
    constructor(name) {
      this._name = name;
      this.onmessage = null;
      this._closed = false;
      if (!channels.has(name)) channels.set(name, new Set());
      channels.get(name).add(this);
    }

    postMessage(data) {
      if (this._closed) return;
      const peers = channels.get(this._name);
      if (!peers) return;
      for (const peer of peers) {
        if (
          peer !== this &&
          !peer._closed &&
          typeof peer.onmessage === "function"
        ) {
          peer.onmessage({ data });
        }
      }
    }

    close() {
      this._closed = true;
      const peers = channels.get(this._name);
      if (peers) peers.delete(this);
    }
  }

  return { MockBroadcastChannel, channels };
}

function createContext(BroadcastChannel) {
  const timers = [];
  const beforeUnloadHandlers = [];

  // Create the context object first, then use it as its own `window`.
  const ctx = vm.createContext({
    console,
    Math,
    BroadcastChannel,
    setTimeout: function (fn, ms) {
      const id = timers.length;
      timers.push({ fn, ms, cleared: false });
      return id;
    },
    clearTimeout: function (id) {
      if (timers[id]) timers[id].cleared = true;
    },
    restartActiveStreams: vi.fn(),
  });

  // window === ctx so that window._sseIsLeader is accessible as ctx._sseIsLeader
  ctx.window = ctx;
  ctx.window.addEventListener = function (event, handler) {
    if (event === "beforeunload") beforeUnloadHandlers.push(handler);
  };
  ctx._beforeUnloadHandlers = beforeUnloadHandlers;
  ctx._timers = timers;
  return ctx;
}

function createContextNoBroadcast() {
  const ctx = vm.createContext({
    console,
    Math,
    setTimeout: vi.fn(),
    clearTimeout: vi.fn(),
  });
  ctx.window = ctx;
  ctx.window.addEventListener = vi.fn();
  return ctx;
}

function loadTabLeader(ctx) {
  const code = readFileSync(join(jsDir, "build/tab-leader.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "build/tab-leader.js") });
}

/** Flush all pending (non-cleared) timers synchronously. */
function flushTimers(ctx) {
  for (const t of ctx._timers) {
    if (!t.cleared) {
      t.cleared = true;
      t.fn();
    }
  }
}

/** Simulate tab close by firing beforeunload handlers. */
function fireBeforeUnload(ctx) {
  for (const h of ctx._beforeUnloadHandlers) h();
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("tab-leader: BroadcastChannel unavailable", () => {
  it("makes every tab a leader when BroadcastChannel is missing", () => {
    const ctx = createContextNoBroadcast();
    loadTabLeader(ctx);

    expect(ctx._sseIsLeader()).toBe(true);
    // Relay and follower registration should be no-ops (no throw).
    ctx._sseRelay("test", { x: 1 });
    ctx._sseOnFollowerEvent("test", () => {});
  });
});

describe("tab-leader: single tab becomes leader", () => {
  it("claims leadership after election timeout with no peers", () => {
    const { MockBroadcastChannel } = createBroadcastChannelMock();
    const ctx = createContext(MockBroadcastChannel);
    loadTabLeader(ctx);

    // Before timeout fires, not yet leader.
    expect(ctx._sseIsLeader()).toBe(false);

    // Fire the election timeout.
    flushTimers(ctx);
    expect(ctx._sseIsLeader()).toBe(true);
  });

  it("calls restartActiveStreams after becoming leader", () => {
    const { MockBroadcastChannel } = createBroadcastChannelMock();
    const ctx = createContext(MockBroadcastChannel);
    loadTabLeader(ctx);

    flushTimers(ctx);
    expect(ctx.restartActiveStreams).toHaveBeenCalled();
  });
});

describe("tab-leader: two tabs", () => {
  it("second tab becomes follower when first is already leader", () => {
    const { MockBroadcastChannel } = createBroadcastChannelMock();

    // Tab 1: becomes leader.
    const ctx1 = createContext(MockBroadcastChannel);
    loadTabLeader(ctx1);
    flushTimers(ctx1);
    expect(ctx1._sseIsLeader()).toBe(true);

    // Tab 2: sends who-is-leader, gets i-am-leader back synchronously.
    const ctx2 = createContext(MockBroadcastChannel);
    loadTabLeader(ctx2);
    expect(ctx2._sseIsLeader()).toBe(false);
  });

  it("leader relays events to follower", () => {
    const { MockBroadcastChannel } = createBroadcastChannelMock();

    const ctx1 = createContext(MockBroadcastChannel);
    loadTabLeader(ctx1);
    flushTimers(ctx1);

    const ctx2 = createContext(MockBroadcastChannel);
    loadTabLeader(ctx2);

    const received = [];
    ctx2._sseOnFollowerEvent("tasks-snapshot", function (data, lastEventId) {
      received.push({ data, lastEventId });
    });

    ctx1._sseRelay("tasks-snapshot", [{ id: "1" }], "42");

    expect(received).toHaveLength(1);
    expect(received[0].data).toEqual([{ id: "1" }]);
    expect(received[0].lastEventId).toBe("42");
  });

  it("follower does not relay events", () => {
    const { MockBroadcastChannel } = createBroadcastChannelMock();

    const ctx1 = createContext(MockBroadcastChannel);
    loadTabLeader(ctx1);
    flushTimers(ctx1);

    const ctx2 = createContext(MockBroadcastChannel);
    loadTabLeader(ctx2);

    const received = [];
    ctx1._sseOnFollowerEvent("tasks-snapshot", function (data) {
      received.push(data);
    });

    // Follower tries to relay — should be a no-op.
    ctx2._sseRelay("tasks-snapshot", [{ id: "1" }], "42");
    expect(received).toHaveLength(0);
  });
});

describe("tab-leader: leader handoff", () => {
  it("follower becomes leader after leader tab closes", () => {
    const { MockBroadcastChannel } = createBroadcastChannelMock();

    // Tab 1: leader.
    const ctx1 = createContext(MockBroadcastChannel);
    loadTabLeader(ctx1);
    flushTimers(ctx1);
    expect(ctx1._sseIsLeader()).toBe(true);

    // Tab 2: follower.
    const ctx2 = createContext(MockBroadcastChannel);
    loadTabLeader(ctx2);
    expect(ctx2._sseIsLeader()).toBe(false);

    // Simulate leader tab closing.
    fireBeforeUnload(ctx1);

    // ctx2 should have scheduled a re-election timer. Flush it.
    flushTimers(ctx2);
    expect(ctx2._sseIsLeader()).toBe(true);
  });
});
