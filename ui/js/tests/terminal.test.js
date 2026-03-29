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

function makeDom() {
  // Minimal DOM for terminal tab bar tests.
  const elements = {};

  const tabList = {
    _children: [],
    get children() {
      return this._children;
    },
    appendChild(child) {
      this._children.push(child);
    },
    querySelector(sel) {
      const match = sel.match(/\[data-session-id="([^"]+)"\]/);
      if (match) {
        return this._children.find(
          (c) => c._attrs && c._attrs["data-session-id"] === match[1],
        );
      }
      return null;
    },
    querySelectorAll(sel) {
      if (sel === ".terminal-tab") return [...this._children];
      return [];
    },
  };

  const tabBar = { hidden: true };
  const addBtn = { _listeners: {}, addEventListener(ev, fn) { this._listeners[ev] = fn; } };
  const canvas = {
    classList: { contains: () => false },
  };
  const panel = {
    classList: { contains: () => false },
  };

  elements["terminal-tab-list"] = tabList;
  elements["terminal-tab-bar"] = tabBar;
  elements["terminal-tab-add"] = addBtn;
  elements["terminal-canvas"] = canvas;
  elements["status-bar-panel"] = panel;

  return {
    getElementById: (id) => elements[id] || null,
    querySelector: () => null,
    querySelectorAll: () => [],
    addEventListener: () => {},
    documentElement: { setAttribute: () => {} },
    readyState: "complete",
    createElement(tag) {
      const el = {
        _tag: tag,
        _attrs: {},
        _children: [],
        _listeners: {},
        className: "",
        textContent: "",
        innerHTML: "",
        setAttribute(k, v) { this._attrs[k] = v; },
        getAttribute(k) { return this._attrs[k] || null; },
        appendChild(child) { this._children.push(child); },
        addEventListener(ev, fn) { this._listeners[ev] = fn; },
        querySelector(sel) {
          return this._children.find((c) => c.className === sel.replace(".", "")) || null;
        },
        remove() {
          // Remove from parent tabList.
          const idx = tabList._children.indexOf(this);
          if (idx >= 0) tabList._children.splice(idx, 1);
        },
      };
      return el;
    },
    _elements: elements,
    _tabList: tabList,
    _tabBar: tabBar,
    _addBtn: addBtn,
  };
}

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
        if (id === "terminal-canvas")
          return { classList: { contains: () => false } };
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

// ---------------------------------------------------------------------------
// tab bar
// ---------------------------------------------------------------------------

describe("tab bar", () => {
  function makeTabContext() {
    const dom = makeDom();
    return loadTerminal(makeContext({ document: dom }));
  }

  it("addTerminalTab creates element with correct attributes", () => {
    const ctx = makeTabContext();
    ctx.addTerminalTab("sess-1");
    const tabList = ctx.document._tabList;
    expect(tabList._children.length).toBe(1);
    const tab = tabList._children[0];
    expect(tab._attrs["data-session-id"]).toBe("sess-1");
    expect(tab._attrs["aria-selected"]).toBe("false");
    const label = tab._children.find(
      (c) => c.className === "terminal-tab__label",
    );
    expect(label).toBeTruthy();
    expect(label.textContent).toBe("Shell 1");
  });

  it("addTerminalTab uses custom label when provided", () => {
    const ctx = makeTabContext();
    ctx.addTerminalTab("sess-1", "My Shell");
    const tab = ctx.document._tabList._children[0];
    const label = tab._children.find(
      (c) => c.className === "terminal-tab__label",
    );
    expect(label.textContent).toBe("My Shell");
  });

  it("addTerminalTab increments counter for default labels", () => {
    const ctx = makeTabContext();
    ctx.addTerminalTab("sess-1");
    ctx.addTerminalTab("sess-2");
    const labels = ctx.document._tabList._children.map(
      (t) =>
        t._children.find((c) => c.className === "terminal-tab__label")
          .textContent,
    );
    expect(labels).toEqual(["Shell 1", "Shell 2"]);
  });

  it("removeTerminalTab removes the element", () => {
    const ctx = makeTabContext();
    ctx.addTerminalTab("sess-1");
    ctx.addTerminalTab("sess-2");
    ctx.removeTerminalTab("sess-1");
    expect(ctx.document._tabList._children.length).toBe(1);
    expect(ctx.document._tabList._children[0]._attrs["data-session-id"]).toBe(
      "sess-2",
    );
  });

  it("activateTerminalTab sets aria-selected correctly", () => {
    const ctx = makeTabContext();
    ctx.addTerminalTab("sess-1");
    ctx.addTerminalTab("sess-2");
    ctx.activateTerminalTab("sess-2");
    const tabs = ctx.document._tabList._children;
    expect(tabs[0]._attrs["aria-selected"]).toBe("false");
    expect(tabs[1]._attrs["aria-selected"]).toBe("true");
  });

  it("tab bar hidden when no tabs, shown when tabs exist", () => {
    const ctx = makeTabContext();
    expect(ctx.document._tabBar.hidden).toBe(true);
    ctx.addTerminalTab("sess-1");
    expect(ctx.document._tabBar.hidden).toBe(false);
    ctx.removeTerminalTab("sess-1");
    expect(ctx.document._tabBar.hidden).toBe(true);
  });

  it("renameTerminalTab updates label text", () => {
    const ctx = makeTabContext();
    ctx.addTerminalTab("sess-1", "Old");
    ctx.renameTerminalTab("sess-1", "New");
    const tab = ctx.document._tabList._children[0];
    const label = tab._children.find(
      (c) => c.className === "terminal-tab__label",
    );
    expect(label.textContent).toBe("New");
  });

  it("tab click fires callback with session ID", () => {
    const ctx = makeTabContext();
    const clicked = [];
    ctx.setTabClickHandler((id) => clicked.push(id));
    ctx.addTerminalTab("sess-1");
    const tab = ctx.document._tabList._children[0];
    tab._listeners.click();
    expect(clicked).toEqual(["sess-1"]);
  });

  it("close button fires callback with session ID", () => {
    const ctx = makeTabContext();
    const closed = [];
    ctx.setTabCloseHandler((id) => closed.push(id));
    ctx.addTerminalTab("sess-1");
    const tab = ctx.document._tabList._children[0];
    const closeBtn = tab._children.find(
      (c) => c.className === "terminal-tab__close",
    );
    closeBtn._listeners.click({ stopPropagation: () => {} });
    expect(closed).toEqual(["sess-1"]);
  });

  it("add button fires callback", () => {
    const ctx = makeTabContext();
    let addCalled = false;
    ctx.setTabAddHandler(() => {
      addCalled = true;
    });
    ctx.initTerminal();
    // Simulate click on + button.
    const addBtn = ctx.document._addBtn;
    if (addBtn._listeners.click) addBtn._listeners.click();
    expect(addCalled).toBe(true);
  });
});
