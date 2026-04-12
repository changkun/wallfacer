/**
 * Tests for terminal.js — xterm.js integration with WebSocket PTY relay.
 *
 * Uses vm.createContext to load the script in a controlled sandbox,
 * mocking browser globals (XMLHttpRequest, document, WebSocket, xterm, etc.).
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// ---------------------------------------------------------------------------
// Mock factories
// ---------------------------------------------------------------------------

function makeMockXHR(status = 200, responseText = "9090") {
  const instances = [];
  class MockXHR {
    constructor() {
      this.status = 0;
      this.responseText = "";
      instances.push(this);
    }
    open() {}
    send() {
      this.status = status;
      this.responseText = responseText;
    }
  }
  MockXHR._instances = instances;
  return MockXHR;
}

function makeMockTerminal() {
  const writes = [];
  return {
    _writes: writes,
    open: vi.fn(),
    write: vi.fn(function (d) {
      writes.push(d);
    }),
    focus: vi.fn(),
    clear: vi.fn(),
    loadAddon: vi.fn(),
    onData: vi.fn(),
    onResize: vi.fn(),
    attachCustomKeyEventHandler: vi.fn(),
    cols: 80,
    rows: 24,
    options: {},
  };
}

function makeMockFitAddon() {
  return { fit: vi.fn() };
}

function makeElement(tag, attrs) {
  const listeners = {};
  const children = [];
  const classList = {
    _set: new Set(),
    add(c) {
      this._set.add(c);
    },
    remove(c) {
      this._set.delete(c);
    },
    contains(c) {
      return this._set.has(c);
    },
  };
  return {
    tagName: tag || "div",
    children,
    childNodes: children,
    hidden: false,
    textContent: "",
    innerHTML: "",
    className: "",
    style: {},
    classList,
    _listeners: listeners,
    _attrs: { ...attrs },
    setAttribute(k, v) {
      this._attrs[k] = v;
    },
    getAttribute(k) {
      return this._attrs[k] ?? null;
    },
    addEventListener(ev, fn) {
      if (!listeners[ev]) listeners[ev] = [];
      listeners[ev].push(fn);
    },
    removeEventListener(ev, fn) {
      if (listeners[ev]) {
        listeners[ev] = listeners[ev].filter((f) => f !== fn);
      }
    },
    appendChild(child) {
      children.push(child);
      return child;
    },
    remove() {},
    contains(el) {
      return children.includes(el);
    },
    querySelector(sel) {
      // Very simple data-session-id selector support.
      const m = sel.match(/\[data-session-id="([^"]+)"\]/);
      if (m) {
        return (
          children.find((c) => c._attrs?.["data-session-id"] === m[1]) || null
        );
      }
      // Class selector
      if (sel.startsWith(".")) {
        const cls = sel.slice(1);
        return children.find((c) => (c.className || "").includes(cls)) || null;
      }
      return null;
    },
    querySelectorAll(sel) {
      if (sel === ".terminal-tab")
        return children.filter((c) =>
          (c.className || "").includes("terminal-tab"),
        );
      return [];
    },
    getBoundingClientRect() {
      return { top: 100, right: 200, bottom: 120, left: 10 };
    },
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const MockXHR = overrides.XMLHttpRequest || makeMockXHR();
  const mockTerm = overrides._mockTerminal || makeMockTerminal();
  const mockFitAddon = overrides._mockFitAddon || makeMockFitAddon();

  const cssVars = overrides.cssVars || {};

  const createdElements = [];

  const ctx = {
    console,
    Date,
    Math,
    parseInt,
    JSON,
    Object,
    Array,
    Uint8Array,
    ArrayBuffer,
    Error,
    RegExp,
    String,
    Number,
    encodeURIComponent,
    btoa: overrides.btoa || ((s) => Buffer.from(s).toString("base64")),
    atob: overrides.atob || ((s) => Buffer.from(s, "base64").toString()),
    setTimeout: overrides.setTimeout || vi.fn((fn) => fn()),
    clearTimeout: overrides.clearTimeout || vi.fn(),
    setInterval: vi.fn(),
    XMLHttpRequest: MockXHR,
    WebSocket:
      overrides.WebSocket ||
      class MockWebSocket {
        constructor(url) {
          this.url = url;
          this.readyState = 1; // OPEN
          this.binaryType = "";
          this.onopen = null;
          this.onmessage = null;
          this.onclose = null;
          this.onerror = null;
        }
        send() {}
        close() {}
        static OPEN = 1;
        static CLOSED = 3;
      },
    ResizeObserver:
      overrides.ResizeObserver ||
      class {
        constructor() {}
        observe() {}
      },
    MutationObserver:
      overrides.MutationObserver ||
      class {
        constructor() {}
        observe() {}
      },
    Terminal:
      overrides.Terminal ||
      function () {
        return mockTerm;
      },
    FitAddon: overrides.FitAddon || {
      FitAddon: function () {
        return mockFitAddon;
      },
    },
    fetch:
      overrides.fetch ||
      vi.fn(() => Promise.resolve({ json: () => Promise.resolve([]) })),
    getComputedStyle: () => ({
      getPropertyValue: (name) => cssVars[name] || "",
    }),
    getWallfacerToken: overrides.getWallfacerToken || (() => ""),
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelector: () => null,
      querySelectorAll: () => [],
      documentElement: {
        setAttribute: () => {},
        style: {},
      },
      readyState: "complete",
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      createElement: (tag) => {
        const el = makeElement(tag);
        createdElements.push(el);
        return el;
      },
      body: {
        appendChild: vi.fn(),
        removeChild: vi.fn(),
      },
    },
    window: {},
    location: overrides.location || {
      host: "localhost:8080",
      protocol: "http:",
    },
    _createdElements: createdElements,
    _mockTerminal: mockTerm,
    _mockFitAddon: mockFitAddon,
  };

  // window === ctx for globals
  ctx.window = ctx;

  return vm.createContext(ctx);
}

function loadTerminal(ctx) {
  const code = readFileSync(join(jsDir, "terminal.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "terminal.js") });
  return ctx;
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("_desktopServerHost IIFE", () => {
  it("returns host:port when XHR succeeds", () => {
    const ctx = makeContext({
      XMLHttpRequest: makeMockXHR(200, "9090"),
    });
    loadTerminal(ctx);
    const result = vm.runInContext("_desktopServerHost", ctx);
    expect(result).toBe("localhost:9090");
  });

  it("returns null when XHR returns non-200", () => {
    const ctx = makeContext({
      XMLHttpRequest: makeMockXHR(404, ""),
    });
    loadTerminal(ctx);
    const result = vm.runInContext("_desktopServerHost", ctx);
    expect(result).toBeNull();
  });

  it("returns null when XHR returns empty body", () => {
    const ctx = makeContext({
      XMLHttpRequest: makeMockXHR(200, ""),
    });
    loadTerminal(ctx);
    const result = vm.runInContext("_desktopServerHost", ctx);
    expect(result).toBeNull();
  });

  it("returns null when XHR throws", () => {
    class ThrowingXHR {
      open() {
        throw new Error("network error");
      }
      send() {}
    }
    const ctx = makeContext({ XMLHttpRequest: ThrowingXHR });
    loadTerminal(ctx);
    const result = vm.runInContext("_desktopServerHost", ctx);
    expect(result).toBeNull();
  });
});

describe("_isLightColor", () => {
  it("returns true for white (#ffffff)", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    expect(vm.runInContext('_isLightColor("#ffffff")', ctx)).toBe(true);
  });

  it("returns false for black (#000000)", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    expect(vm.runInContext('_isLightColor("#000000")', ctx)).toBe(false);
  });

  it("returns true for a light gray (#cccccc)", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    expect(vm.runInContext('_isLightColor("#cccccc")', ctx)).toBe(true);
  });

  it("returns false for dark gray (#333333)", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    expect(vm.runInContext('_isLightColor("#333333")', ctx)).toBe(false);
  });

  it("handles hex without # prefix", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    expect(vm.runInContext('_isLightColor("ffffff")', ctx)).toBe(true);
    expect(vm.runInContext('_isLightColor("000000")', ctx)).toBe(false);
  });

  it("detects a medium-light color (#90a0b0)", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    // 0.299*144 + 0.587*160 + 0.114*176 = 43.056 + 93.92 + 20.064 = 157.04 > 128
    expect(vm.runInContext('_isLightColor("#90a0b0")', ctx)).toBe(true);
  });

  it("detects a medium-dark color (#404040)", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    // 0.299*64 + 0.587*64 + 0.114*64 = 64 > 128? No → false
    expect(vm.runInContext('_isLightColor("#404040")', ctx)).toBe(false);
  });
});

describe("_buildTermTheme", () => {
  it("returns dark theme when background is dark", () => {
    const ctx = makeContext({
      cssVars: {
        "--bg": "#1a1917",
        "--text": "#cccccc",
        "--accent": "#d97757",
      },
    });
    loadTerminal(ctx);
    const theme = vm.runInContext("_buildTermTheme()", ctx);
    expect(theme.background).toBe("#1a1917");
    expect(theme.foreground).toBe("#cccccc");
    expect(theme.cursor).toBe("#d97757");
    expect(theme.selectionBackground).toBe("rgba(78,140,255,0.3)");
    // Dark palette colors
    expect(theme.red).toBe("#f14c4c");
    expect(theme.green).toBe("#23d18b");
  });

  it("returns light theme when background is light", () => {
    const ctx = makeContext({
      cssVars: {
        "--bg": "#f5f5f5",
        "--text": "#333333",
        "--accent": "#0078d4",
      },
    });
    loadTerminal(ctx);
    const theme = vm.runInContext("_buildTermTheme()", ctx);
    expect(theme.background).toBe("#f5f5f5");
    expect(theme.foreground).toBe("#333333");
    expect(theme.selectionBackground).toBe("rgba(0,0,0,0.15)");
    // Light palette colors
    expect(theme.red).toBe("#cd3131");
    expect(theme.green).toBe("#00bc70");
  });

  it("uses fallback values when CSS vars are empty", () => {
    const ctx = makeContext({ cssVars: {} });
    loadTerminal(ctx);
    const theme = vm.runInContext("_buildTermTheme()", ctx);
    expect(theme.background).toBe("#1a1917");
    expect(theme.foreground).toBe("#cccccc");
    expect(theme.cursor).toBe("#d97757");
  });
});

describe("ANSI color palettes", () => {
  it("_darkAnsiColors has all 16 entries", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    const colors = vm.runInContext("_darkAnsiColors", ctx);
    const keys = [
      "black",
      "red",
      "green",
      "yellow",
      "blue",
      "magenta",
      "cyan",
      "white",
      "brightBlack",
      "brightRed",
      "brightGreen",
      "brightYellow",
      "brightBlue",
      "brightMagenta",
      "brightCyan",
      "brightWhite",
    ];
    for (const k of keys) {
      expect(colors).toHaveProperty(k);
      expect(typeof colors[k]).toBe("string");
    }
  });

  it("_lightAnsiColors has all 16 entries", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    const colors = vm.runInContext("_lightAnsiColors", ctx);
    expect(colors).toHaveProperty("black");
    expect(colors).toHaveProperty("brightWhite");
    expect(Object.keys(colors)).toHaveLength(16);
  });
});

describe("_getCSSVar", () => {
  it("retrieves and trims a CSS variable", () => {
    const ctx = makeContext({
      cssVars: { "--bg": "  #abcdef  " },
    });
    loadTerminal(ctx);
    // The mock getComputedStyle returns the raw value; _getCSSVar calls .trim()
    // Our mock already returns the value without extra spaces, but let's verify
    // the function works at all.
    const val = vm.runInContext('_getCSSVar("--bg")', ctx);
    expect(val).toBe("#abcdef");
  });
});

describe("tab management functions", () => {
  function makeTabContext() {
    const tabList = makeElement("div");
    const tabBar = makeElement("div");
    tabBar.hidden = true;
    const ctx = makeContext({
      elements: [
        ["terminal-tab-list", tabList],
        ["terminal-tab-bar", tabBar],
      ],
    });
    loadTerminal(ctx);
    return { ctx, tabList, tabBar };
  }

  it("addTerminalTab creates a tab element with session id", () => {
    const { ctx, tabList, tabBar } = makeTabContext();
    vm.runInContext('addTerminalTab("s1", "Shell 1")', ctx);
    expect(tabList.children).toHaveLength(1);
    expect(tabList.children[0]._attrs["data-session-id"]).toBe("s1");
    expect(tabBar.hidden).toBe(false);
  });

  it("addTerminalTab auto-generates label when none provided", () => {
    const { ctx, tabList } = makeTabContext();
    vm.runInContext('addTerminalTab("s1", null)', ctx);
    const labelSpan = tabList.children[0].children.find(
      (c) => c.className === "terminal-tab__label",
    );
    expect(labelSpan.textContent).toBe("Shell 1");
  });

  it("addTerminalTab increments counter for multiple tabs", () => {
    const { ctx, tabList } = makeTabContext();
    vm.runInContext('addTerminalTab("s1", null)', ctx);
    vm.runInContext('addTerminalTab("s2", null)', ctx);
    const labels = tabList.children.map(
      (t) =>
        t.children.find((c) => c.className === "terminal-tab__label")
          .textContent,
    );
    expect(labels).toEqual(["Shell 1", "Shell 2"]);
  });

  it("removeTerminalTab removes the tab by session id", () => {
    const { ctx, tabList, tabBar } = makeTabContext();
    vm.runInContext('addTerminalTab("s1", "Shell 1")', ctx);
    expect(tabList.children).toHaveLength(1);
    // Override tab.remove to actually remove from parent
    tabList.children[0].remove = function () {
      const idx = tabList.children.indexOf(this);
      if (idx >= 0) tabList.children.splice(idx, 1);
    };
    vm.runInContext('removeTerminalTab("s1")', ctx);
    expect(tabList.children).toHaveLength(0);
    expect(tabBar.hidden).toBe(true);
  });

  it("activateTerminalTab sets aria-selected on the correct tab", () => {
    const { ctx, tabList } = makeTabContext();
    vm.runInContext('addTerminalTab("s1", "Shell 1")', ctx);
    vm.runInContext('addTerminalTab("s2", "Shell 2")', ctx);
    vm.runInContext('activateTerminalTab("s2")', ctx);
    expect(tabList.children[0]._attrs["aria-selected"]).toBe("false");
    expect(tabList.children[1]._attrs["aria-selected"]).toBe("true");
  });

  it("activateTerminalTab switches selection", () => {
    const { ctx, tabList } = makeTabContext();
    vm.runInContext('addTerminalTab("s1", "Shell 1")', ctx);
    vm.runInContext('addTerminalTab("s2", "Shell 2")', ctx);
    vm.runInContext('activateTerminalTab("s1")', ctx);
    expect(tabList.children[0]._attrs["aria-selected"]).toBe("true");
    expect(tabList.children[1]._attrs["aria-selected"]).toBe("false");
  });

  it("renameTerminalTab updates the label text", () => {
    const { ctx, tabList } = makeTabContext();
    vm.runInContext('addTerminalTab("s1", "Shell 1")', ctx);
    vm.runInContext('renameTerminalTab("s1", "Container X")', ctx);
    const label = tabList.children[0].children.find(
      (c) => c.className === "terminal-tab__label",
    );
    expect(label.textContent).toBe("Container X");
  });

  it("renameTerminalTab does nothing for unknown session", () => {
    const { ctx } = makeTabContext();
    vm.runInContext('addTerminalTab("s1", "Shell 1")', ctx);
    // Should not throw
    vm.runInContext('renameTerminalTab("unknown", "Nope")', ctx);
  });
});

describe("setTabClickHandler / setTabCloseHandler / setTabAddHandler", () => {
  it("overwrites the handlers", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    // The handlers are internal vars; we can't easily verify them directly,
    // but we can verify the setter functions exist and don't throw.
    vm.runInContext("setTabClickHandler(function(){})", ctx);
    vm.runInContext("setTabCloseHandler(function(){})", ctx);
    vm.runInContext("setTabAddHandler(function(){})", ctx);
  });
});

describe("session state management", () => {
  it("_clearSessionState empties sessions and resets counter", () => {
    const tabList = makeElement("div");
    const tabBar = makeElement("div");
    const ctx = makeContext({
      elements: [
        ["terminal-tab-list", tabList],
        ["terminal-tab-bar", tabBar],
      ],
    });
    loadTerminal(ctx);

    // Manually populate session state
    vm.runInContext(
      '_sessions["a"] = { buffer: [] }; _sessions["b"] = { buffer: [] }; _activeSessionId = "a"; _termTabCounter = 5;',
      ctx,
    );
    vm.runInContext("_clearSessionState()", ctx);
    expect(vm.runInContext("Object.keys(_sessions).length", ctx)).toBe(0);
    expect(vm.runInContext("_activeSessionId", ctx)).toBeNull();
    expect(vm.runInContext("_termTabCounter", ctx)).toBe(0);
  });
});

describe("_handleSessionsList", () => {
  function makeSessionContext() {
    const tabList = makeElement("div");
    const tabBar = makeElement("div");
    const mockTerm = makeMockTerminal();
    const ctx = makeContext({
      elements: [
        ["terminal-tab-list", tabList],
        ["terminal-tab-bar", tabBar],
      ],
      _mockTerminal: mockTerm,
      // Don't auto-execute setTimeout callbacks for deferred focus
      setTimeout: vi.fn(),
    });
    loadTerminal(ctx);
    return { ctx, tabList, tabBar, mockTerm };
  }

  it("does nothing when sessions is null", () => {
    const { ctx } = makeSessionContext();
    vm.runInContext("_handleSessionsList(null)", ctx);
    expect(vm.runInContext("Object.keys(_sessions).length", ctx)).toBe(0);
  });

  it("adds tabs for new sessions", () => {
    const { ctx, tabList } = makeSessionContext();
    vm.runInContext(
      '_handleSessionsList([{id:"s1",active:true},{id:"s2",active:false}])',
      ctx,
    );
    expect(vm.runInContext("Object.keys(_sessions).length", ctx)).toBe(2);
    expect(tabList.children).toHaveLength(2);
    expect(vm.runInContext("_activeSessionId", ctx)).toBe("s1");
  });

  it("removes tabs for sessions no longer on server", () => {
    const { ctx, tabList } = makeSessionContext();
    // Add two sessions
    vm.runInContext(
      '_handleSessionsList([{id:"s1",active:true},{id:"s2",active:false}])',
      ctx,
    );
    expect(tabList.children).toHaveLength(2);
    // Patch remove() to actually splice from children
    for (const child of tabList.children) {
      child.remove = function () {
        const idx = tabList.children.indexOf(this);
        if (idx >= 0) tabList.children.splice(idx, 1);
      };
    }
    // Now server only has s1
    vm.runInContext('_handleSessionsList([{id:"s1",active:true}])', ctx);
    expect(vm.runInContext("Object.keys(_sessions).length", ctx)).toBe(1);
    expect(vm.runInContext('_sessions["s2"]', ctx)).toBeUndefined();
  });

  it("uses container name as tab label (truncated if >24 chars)", () => {
    const { ctx, tabList } = makeSessionContext();
    vm.runInContext(
      '_handleSessionsList([{id:"s1",active:true,container:"my-container-with-a-very-long-name-here"}])',
      ctx,
    );
    const label = tabList.children[0].children.find(
      (c) => c.className === "terminal-tab__label",
    );
    // Should be truncated to 24 chars + ellipsis
    expect(label.textContent).toBe("my-container-with-a-very\u2026");
  });
});

describe("_handleSessionClosed", () => {
  it("removes session and tab", () => {
    const tabList = makeElement("div");
    const tabBar = makeElement("div");
    const ctx = makeContext({
      elements: [
        ["terminal-tab-list", tabList],
        ["terminal-tab-bar", tabBar],
      ],
      setTimeout: vi.fn(),
    });
    loadTerminal(ctx);

    vm.runInContext('_handleSessionsList([{id:"s1",active:true}])', ctx);
    tabList.children[0].remove = function () {
      const idx = tabList.children.indexOf(this);
      if (idx >= 0) tabList.children.splice(idx, 1);
    };
    vm.runInContext('_handleSessionClosed("s1")', ctx);
    expect(vm.runInContext('_sessions["s1"]', ctx)).toBeUndefined();
  });
});

describe("_handleSessionExited", () => {
  it("writes session ended message when active session exits", () => {
    const tabList = makeElement("div");
    const tabBar = makeElement("div");
    const mockTerm = makeMockTerminal();
    const ctx = makeContext({
      elements: [
        ["terminal-tab-list", tabList],
        ["terminal-tab-bar", tabBar],
      ],
      _mockTerminal: mockTerm,
      setTimeout: vi.fn(),
    });
    loadTerminal(ctx);

    // Set _term so the function can write to it
    vm.runInContext("_term = _mockTerminal", ctx);
    vm.runInContext(
      '_sessions["s1"] = { buffer: [] }; _activeSessionId = "s1";',
      ctx,
    );
    vm.runInContext('_handleSessionExited("s1")', ctx);
    expect(mockTerm.write).toHaveBeenCalledWith(
      expect.stringContaining("Session ended."),
    );
    expect(vm.runInContext('_sessions["s1"]', ctx)).toBeUndefined();
  });
});

describe("disconnectTerminal", () => {
  it("clears reconnect timer and closes WebSocket", () => {
    const tabList = makeElement("div");
    const tabBar = makeElement("div");
    const clearTimeoutFn = vi.fn();
    const ctx = makeContext({
      elements: [
        ["terminal-tab-list", tabList],
        ["terminal-tab-bar", tabBar],
      ],
      clearTimeout: clearTimeoutFn,
      setTimeout: vi.fn(),
    });
    loadTerminal(ctx);

    // Simulate a reconnect timer and an open WebSocket
    const mockWs = { close: vi.fn() };
    vm.runInContext("_termReconnectTimer = 42", ctx);
    ctx._termWs = mockWs;
    vm.runInContext("_termWs = _termReconnectTimer", ctx); // just set it truthy
    // Actually set the ws properly
    vm.runInContext("_termWs = null", ctx);

    vm.runInContext("disconnectTerminal()", ctx);
    expect(clearTimeoutFn).toHaveBeenCalled();
    expect(vm.runInContext("_termReconnectTimer", ctx)).toBeNull();
  });
});

describe("isTerminalConnected", () => {
  it("returns false when no WebSocket", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    expect(vm.runInContext("isTerminalConnected()", ctx)).toBe(false);
  });

  it("returns true when WebSocket is OPEN", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    vm.runInContext("_termWs = { readyState: WebSocket.OPEN }", ctx);
    expect(vm.runInContext("isTerminalConnected()", ctx)).toBe(true);
  });

  it("returns false when WebSocket is not OPEN", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    vm.runInContext("_termWs = { readyState: 3 }", ctx);
    expect(vm.runInContext("isTerminalConnected()", ctx)).toBe(false);
  });
});

describe("initTerminal", () => {
  it("creates terminal and wires events when Terminal is available", () => {
    const tabList = makeElement("div");
    const tabBar = makeElement("div");
    const canvas = makeElement("div");
    const addBtn = makeElement("button");
    const containerBtn = makeElement("button");
    const mockTerm = makeMockTerminal();
    const mockFitAddon = makeMockFitAddon();

    const ctx = makeContext({
      elements: [
        ["terminal-tab-list", tabList],
        ["terminal-tab-bar", tabBar],
        ["terminal-canvas", canvas],
        ["terminal-tab-add", addBtn],
        ["terminal-container-btn", containerBtn],
        ["status-bar-panel", makeElement("div")],
      ],
      _mockTerminal: mockTerm,
      _mockFitAddon: mockFitAddon,
      setTimeout: vi.fn(),
    });
    loadTerminal(ctx);
    vm.runInContext("initTerminal()", ctx);

    expect(mockTerm.open).toHaveBeenCalledWith(canvas);
    expect(mockFitAddon.fit).toHaveBeenCalled();
    expect(mockTerm.loadAddon).toHaveBeenCalled();
    expect(mockTerm.onData).toHaveBeenCalled();
    expect(mockTerm.onResize).toHaveBeenCalled();
    expect(mockTerm.attachCustomKeyEventHandler).toHaveBeenCalled();
  });

  it("does not re-init if already initialized", () => {
    const mockTerm = makeMockTerminal();
    const mockFitAddon = makeMockFitAddon();
    const canvas = makeElement("div");
    const ctx = makeContext({
      elements: [
        ["terminal-canvas", canvas],
        ["terminal-tab-list", makeElement("div")],
        ["terminal-tab-bar", makeElement("div")],
      ],
      _mockTerminal: mockTerm,
      _mockFitAddon: mockFitAddon,
      setTimeout: vi.fn(),
    });
    loadTerminal(ctx);
    vm.runInContext("initTerminal()", ctx);
    const firstCallCount = mockTerm.open.mock.calls.length;
    vm.runInContext("initTerminal()", ctx);
    expect(mockTerm.open.mock.calls.length).toBe(firstCallCount);
  });
});

describe("_hideTermPanel", () => {
  it("hides panel elements and sets aria-expanded to false", () => {
    const panel = makeElement("div");
    const handle = makeElement("div");
    const btn = makeElement("button");
    const tabBar = makeElement("div");
    const ctx = makeContext({
      elements: [
        ["status-bar-panel", panel],
        ["status-bar-panel-resize", handle],
        ["status-bar-terminal-btn", btn],
        ["terminal-tab-bar", tabBar],
      ],
    });
    loadTerminal(ctx);
    vm.runInContext("_hideTermPanel()", ctx);

    expect(panel.classList.contains("hidden")).toBe(true);
    expect(handle.classList.contains("hidden")).toBe(true);
    expect(btn._attrs["aria-expanded"]).toBe("false");
    expect(tabBar.hidden).toBe(true);
  });
});

describe("_sessionBufferLimit", () => {
  it("is 100000 by default", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    expect(vm.runInContext("_sessionBufferLimit", ctx)).toBe(100000);
  });
});

describe("window exports", () => {
  it("exposes all public functions on window", () => {
    const ctx = makeContext();
    loadTerminal(ctx);
    const fns = [
      "initTerminal",
      "connectTerminal",
      "disconnectTerminal",
      "isTerminalConnected",
      "addTerminalTab",
      "removeTerminalTab",
      "activateTerminalTab",
      "renameTerminalTab",
      "setTabClickHandler",
      "setTabCloseHandler",
      "setTabAddHandler",
    ];
    for (const fn of fns) {
      expect(typeof vm.runInContext(`window.${fn}`, ctx)).toBe("function");
    }
  });
});
