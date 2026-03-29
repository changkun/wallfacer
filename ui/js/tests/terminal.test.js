/**
 * Tests for terminal.js — xterm.js integration and WebSocket management.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext(overrides = {}) {
  const mockTermInstance = {
    loadAddon: vi.fn(),
    open: vi.fn(),
    focus: vi.fn(),
    write: vi.fn(),
    clear: vi.fn(),
    onData: vi.fn(),
    onResize: vi.fn(),
    cols: 80,
    rows: 24,
    options: {},
  };

  const mockFitInstance = {
    fit: vi.fn(),
  };

  const mockWs = {
    readyState: 1, // OPEN
    send: vi.fn(),
    close: vi.fn(),
    binaryType: "",
    onopen: null,
    onmessage: null,
    onclose: null,
    onerror: null,
  };

  const ctx = {
    console,
    document: overrides.document || {
      getElementById: (id) => {
        if (id === "status-bar-panel")
          return { classList: { contains: () => false } };
        return null;
      },
      querySelector: () => null,
      querySelectorAll: () => [],
      addEventListener: () => {},
      documentElement: {
        setAttribute: () => {},
      },
      readyState: "complete",
    },
    getComputedStyle: () => ({
      getPropertyValue: (name) => {
        if (name === "--bg-card") return "#1e1e1e";
        if (name === "--text") return "#cccccc";
        if (name === "--accent") return "#4e8cff";
        return "";
      },
    }),
    location: { protocol: "http:", host: "localhost:8080" },
    ResizeObserver: vi.fn().mockImplementation(() => ({ observe: vi.fn() })),
    Terminal: vi.fn().mockReturnValue(mockTermInstance),
    FitAddon: { FitAddon: vi.fn().mockReturnValue(mockFitInstance) },
    WebSocket: vi.fn().mockReturnValue(mockWs),
    setTimeout: vi.fn().mockReturnValue(42),
    clearTimeout: vi.fn(),
    JSON,
    btoa: (s) => Buffer.from(s).toString("base64"),
    getWallfacerToken: () => "test-token",
    _mockTermInstance: mockTermInstance,
    _mockFitInstance: mockFitInstance,
    _mockWs: mockWs,
    ...overrides,
  };
  // WebSocket constants.
  ctx.WebSocket.OPEN = 1;
  ctx.WebSocket.CLOSED = 3;
  ctx.window = ctx; // terminal.js exports to window
  return vm.createContext(ctx);
}

function loadTerminal(ctx) {
  const code = readFileSync(join(jsDir, "terminal.js"), "utf8");
  vm.runInContext(code, ctx, { filename: "terminal.js" });
  return ctx;
}

// ---------------------------------------------------------------------------
// initTerminal
// ---------------------------------------------------------------------------

describe("initTerminal", () => {
  it("creates xterm instance with theme from CSS vars", () => {
    const ctx = loadTerminal(makeContext());
    ctx.initTerminal();
    expect(ctx.Terminal).toHaveBeenCalledOnce();
    const args = ctx.Terminal.mock.calls[0][0];
    expect(args.theme.background).toBe("#1a1917");
    expect(args.theme.foreground).toBe("#cccccc");
    expect(args.theme.cursor).toBe("#4e8cff");
  });

  it("loads FitAddon and opens terminal in panel", () => {
    const ctx = loadTerminal(makeContext());
    ctx.initTerminal();
    expect(ctx.FitAddon.FitAddon).toHaveBeenCalledOnce();
    expect(ctx._mockTermInstance.loadAddon).toHaveBeenCalledWith(
      ctx._mockFitInstance,
    );
    expect(ctx._mockTermInstance.open).toHaveBeenCalled();
  });

  it("is idempotent — second call is a no-op", () => {
    const ctx = loadTerminal(makeContext());
    ctx.initTerminal();
    ctx.initTerminal();
    expect(ctx.Terminal).toHaveBeenCalledOnce();
  });
});

// ---------------------------------------------------------------------------
// connectTerminal
// ---------------------------------------------------------------------------

describe("connectTerminal", () => {
  it("builds correct WebSocket URL with token and dimensions", () => {
    const ctx = loadTerminal(makeContext());
    ctx.initTerminal();
    ctx.connectTerminal();
    expect(ctx.WebSocket).toHaveBeenCalledOnce();
    const url = ctx.WebSocket.mock.calls[0][0];
    expect(url).toContain("ws://localhost:8080/api/terminal/ws");
    expect(url).toContain("cols=80");
    expect(url).toContain("rows=24");
    expect(url).toContain("token=test-token");
  });

  it("re-fits and focuses when already connected", () => {
    const ctx = loadTerminal(makeContext());
    ctx.initTerminal();
    ctx.connectTerminal();
    // Simulate open state.
    ctx._mockWs.readyState = 1;
    ctx.connectTerminal(); // second call
    expect(ctx._mockFitInstance.fit).toHaveBeenCalled();
    expect(ctx._mockTermInstance.focus).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// disconnectTerminal
// ---------------------------------------------------------------------------

describe("disconnectTerminal", () => {
  it("closes WebSocket with code 1000", () => {
    const ctx = loadTerminal(makeContext());
    ctx.initTerminal();
    ctx.connectTerminal();
    ctx.disconnectTerminal();
    expect(ctx._mockWs.close).toHaveBeenCalledWith(1000);
  });

  it("clears reconnection timer", () => {
    const ctx = loadTerminal(makeContext());
    ctx.initTerminal();
    ctx.connectTerminal();
    // Trigger a non-1000 close to start reconnection.
    const onclose = ctx._mockWs.onclose;
    if (onclose) onclose({ code: 1006 });
    ctx.disconnectTerminal();
    expect(ctx.clearTimeout).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// isTerminalConnected
// ---------------------------------------------------------------------------

describe("isTerminalConnected", () => {
  it("returns false before connecting", () => {
    const ctx = loadTerminal(makeContext());
    expect(ctx.isTerminalConnected()).toBe(false);
  });

  it("returns true after connecting when ws is open", () => {
    const ctx = loadTerminal(makeContext());
    ctx.initTerminal();
    ctx.connectTerminal();
    ctx._mockWs.readyState = 1;
    expect(ctx.isTerminalConnected()).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// reconnection
// ---------------------------------------------------------------------------

describe("reconnection", () => {
  it("schedules reconnect on non-1000 close with exponential backoff", () => {
    const ctx = loadTerminal(makeContext());
    ctx.initTerminal();
    ctx.connectTerminal();
    const onclose = ctx._mockWs.onclose;
    expect(onclose).toBeTruthy();

    // First close: 1s delay.
    onclose({ code: 1006 });
    expect(ctx.setTimeout).toHaveBeenCalledTimes(1);
    const firstDelay = ctx.setTimeout.mock.calls[0][1];
    expect(firstDelay).toBe(1000);
  });
});
