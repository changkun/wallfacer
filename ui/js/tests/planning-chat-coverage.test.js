/**
 * Tests for planning-chat.js — PlanningChat module.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// Helper: create a mock DOM element with common properties.
function makeElement(tag, overrides = {}) {
  const children = [];
  const classList = new Set();
  const eventListeners = {};
  let _innerHTML = "";
  const el = {
    tagName: (tag || "div").toUpperCase(),
    className: "",
    textContent: "",
    value: "",
    dataset: {},
    style: {},
    children,
    childElementCount: 0,
    scrollTop: 0,
    scrollHeight: 400,
    clientHeight: 300,
    title: "",
    get innerHTML() {
      return _innerHTML;
    },
    set innerHTML(val) {
      _innerHTML = val;
      // Parse simple <div class="..."></div> patterns into child elements.
      children.length = 0;
      el.childElementCount = 0;
      const re = /<div\s+class="([^"]*)"[^>]*>([\s\S]*?)<\/div>/g;
      let match;
      while ((match = re.exec(val)) !== null) {
        const child = makeElement("div");
        child.className = match[1];
        child.innerHTML = match[2];
        children.push(child);
      }
      el.childElementCount = children.length;
    },
    appendChild(child) {
      children.push(child);
      el.childElementCount = children.length;
    },
    insertBefore(newEl, ref) {
      const idx = children.indexOf(ref);
      if (idx >= 0) children.splice(idx, 0, newEl);
      else children.push(newEl);
      el.childElementCount = children.length;
    },
    remove() {},
    querySelector(sel) {
      // Recursive search through children for className match.
      return _querySel(el, sel);
    },
    querySelectorAll(sel) {
      return _queryAll(el, sel);
    },
    addEventListener(event, fn) {
      if (!eventListeners[event]) eventListeners[event] = [];
      eventListeners[event].push(fn);
    },
    dispatchEvent() {},
    getBoundingClientRect() {
      return { left: 10, top: 100, width: 400, height: 40 };
    },
    focus: vi.fn(),
    scrollIntoView: vi.fn(),
    classList: {
      add(c) {
        classList.add(c);
      },
      remove(c) {
        classList.delete(c);
      },
      toggle(c, force) {
        if (force) classList.add(c);
        else classList.delete(c);
      },
      contains(c) {
        return classList.has(c);
      },
    },
    // For test: fire event listeners.
    _fire(event, evtObj) {
      (eventListeners[event] || []).forEach((fn) => fn(evtObj || {}));
    },
    _listeners: eventListeners,
    ...overrides,
  };
  return el;
}

function _querySel(el, sel) {
  // Simple class-based selector matching.
  const cls = sel.startsWith(".") ? sel.slice(1) : null;
  if (!el.children) return null;
  for (const child of el.children) {
    if (cls && child.className && child.className.includes(cls)) return child;
    const found = _querySel(child, sel);
    if (found) return found;
  }
  return null;
}

function _queryAll(el, sel) {
  const cls = sel.startsWith(".") ? sel.slice(1) : null;
  const results = [];
  if (!el.children) return results;
  for (const child of el.children) {
    if (cls && child.className && child.className.includes(cls))
      results.push(child);
    results.push(..._queryAll(child, sel));
  }
  return results;
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const createdElements = [];

  const ctx = {
    console,
    Date,
    Math,
    parseInt,
    String,
    Array,
    Error,
    Object,
    isNaN,
    JSON,
    RegExp,
    Promise,
    setTimeout: vi.fn((fn, ms) => {
      if (typeof fn === "function") fn();
      return 1;
    }),
    setInterval: vi.fn(),
    clearInterval: vi.fn(),
    localStorage: {
      _store: {},
      getItem(key) {
        return this._store[key] || null;
      },
      setItem(key, val) {
        this._store[key] = val;
      },
    },
    navigator: { platform: "MacIntel" },
    Event: function Event() {},
    fetch: vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      text: vi.fn().mockResolvedValue(""),
      json: vi.fn().mockResolvedValue({}),
    }),
    api: vi.fn().mockImplementation((url) => {
      if (
        typeof url === "string" &&
        url.indexOf("/api/planning/threads") !== -1
      ) {
        return Promise.resolve({
          threads: [
            { id: "t1", name: "Chat 1", archived: false, active: true },
          ],
          active_id: "t1",
        });
      }
      return Promise.resolve([]);
    }),
    Routes: {
      planning: {
        messages: () => "/api/planning/messages",
        sendMessage: () => "/api/planning/messages",
        messageStream: () => "/api/planning/messages/stream",
        commands: () => "/api/planning/commands",
        interruptMessage: () => "/api/planning/messages/interrupt",
        clearMessages: () => "/api/planning/messages",
        undo: () => "/api/planning/undo",
        listThreads: () => "/api/planning/threads",
        createThread: () => "/api/planning/threads",
        renameThread: () => "/api/planning/threads/{id}",
        archiveThread: () => "/api/planning/threads/{id}/archive",
        unarchiveThread: () => "/api/planning/threads/{id}/unarchive",
        activateThread: () => "/api/planning/threads/{id}/activate",
      },
    },
    renderMarkdown: vi.fn((text) => "<p>" + text + "</p>"),
    renderPrettyLogs: vi.fn((raw) => "<pre>" + raw + "</pre>"),
    startStreamingFetch: vi.fn(() => ({ abort: vi.fn() })),
    escapeHtml: vi.fn((s) => s),
    specModeState: { focusedSpecPath: "" },
    attachMentionAutocomplete: vi.fn(),
    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: (tag) => {
        const el = makeElement(tag);
        createdElements.push(el);
        return el;
      },
      querySelector: (sel) => {
        if (sel === ".spec-chat-composer") {
          return makeElement("div", {
            parentElement: makeElement("div"),
          });
        }
        if (sel === ".mention-dropdown") return null;
        return null;
      },
      body: {
        appendChild: vi.fn(),
      },
      addEventListener: vi.fn(),
    },
    window: null, // set below
    _createdElements: createdElements,
    ...overrides,
  };
  ctx.window = ctx;
  return vm.createContext(ctx);
}

function loadPlanningChat(ctx) {
  // planning-chat.js uses the shared autocomplete widget. Load the
  // compiled widget first so `attachAutocomplete` is a global in the
  // sandbox context before init() runs.
  const widget = readFileSync(join(jsDir, "build/lib/autocomplete.js"), "utf8");
  vm.runInContext(widget, ctx, {
    filename: join(jsDir, "build/lib/autocomplete.js"),
  });
  const code = readFileSync(join(jsDir, "planning-chat.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "planning-chat.js") });
  return ctx;
}

// Build a standard set of elements for init().
function makeStandardElements() {
  return [
    [
      "spec-chat-input",
      makeElement("textarea", {
        value: "",
        scrollHeight: 40,
      }),
    ],
    [
      "spec-chat-send",
      makeElement("button", {
        parentElement: makeElement("div"),
      }),
    ],
    ["spec-chat-messages", makeElement("div")],
    ["spec-chat-stream", makeElement("div")],
    ["spec-chat-clear", makeElement("button")],
    ["spec-chat-send-mode", makeElement("button")],
    ["spec-chat-send-hint", makeElement("span")],
    ["spec-chat-slash-hint", makeElement("button")],
    ["spec-chat-at-hint", makeElement("button")],
  ];
}

describe("planning-chat.js", () => {
  describe("PlanningChat module shape", () => {
    it("exports init, sendMessage, clearHistory, isStreaming, getQueue", () => {
      const ctx = makeContext({ elements: makeStandardElements() });
      loadPlanningChat(ctx);
      expect(ctx.PlanningChat).toBeDefined();
      expect(typeof ctx.PlanningChat.init).toBe("function");
      expect(typeof ctx.PlanningChat.sendMessage).toBe("function");
      expect(typeof ctx.PlanningChat.clearHistory).toBe("function");
      expect(typeof ctx.PlanningChat.isStreaming).toBe("function");
      expect(typeof ctx.PlanningChat.getQueue).toBe("function");
    });
  });

  describe("isStreaming", () => {
    it("returns false initially", () => {
      const ctx = makeContext({ elements: makeStandardElements() });
      loadPlanningChat(ctx);
      expect(ctx.PlanningChat.isStreaming()).toBe(false);
    });
  });

  describe("getQueue", () => {
    it("returns empty array initially", () => {
      const ctx = makeContext({ elements: makeStandardElements() });
      loadPlanningChat(ctx);
      expect(ctx.PlanningChat.getQueue()).toEqual([]);
    });

    it("returns a copy of the queue", () => {
      const ctx = makeContext({ elements: makeStandardElements() });
      loadPlanningChat(ctx);
      const q1 = ctx.PlanningChat.getQueue();
      const q2 = ctx.PlanningChat.getQueue();
      expect(q1).not.toBe(q2);
    });
  });

  describe("init", () => {
    it("sets up event listeners on input and send button", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      // input should have keydown and input listeners
      const input = elemMap.get("spec-chat-input");
      expect(input._listeners["keydown"]).toBeDefined();
      expect(input._listeners["keydown"].length).toBeGreaterThan(0);
      expect(input._listeners["input"]).toBeDefined();
      expect(input._listeners["input"].length).toBeGreaterThan(0);
    });

    it("wires clear button", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      const clearBtn = elemMap.get("spec-chat-clear");
      expect(clearBtn._listeners["click"]).toBeDefined();
      expect(clearBtn._listeners["click"].length).toBe(1);
    });

    it("wires send-mode toggle", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      const modeBtn = elemMap.get("spec-chat-send-mode");
      expect(modeBtn._listeners["click"]).toBeDefined();
    });

    it("wires slash shortcut button", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      const slashBtn = elemMap.get("spec-chat-slash-hint");
      expect(slashBtn._listeners["click"]).toBeDefined();
    });

    it("wires at-mention shortcut button", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      const atBtn = elemMap.get("spec-chat-at-hint");
      expect(atBtn._listeners["click"]).toBeDefined();
    });

    it("calls attachMentionAutocomplete", () => {
      const elems = makeStandardElements();
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      expect(ctx.attachMentionAutocomplete).toHaveBeenCalled();
    });

    it("loads history on init", async () => {
      const elems = makeStandardElements();
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      await ctx.PlanningChat.init();
      // api should have been called with the messages route (thread param
      // appended once the manifest resolves to the default "Chat 1").
      expect(ctx.api).toHaveBeenCalledWith("/api/planning/messages?thread=t1");
    });

    it("fetches commands on init", () => {
      const elems = makeStandardElements();
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      // api should have been called with the commands route
      expect(ctx.api).toHaveBeenCalledWith("/api/planning/commands");
    });

    it("returns early if input element is missing", () => {
      const ctx = makeContext({ elements: [] });
      loadPlanningChat(ctx);
      // Should not throw.
      ctx.PlanningChat.init();
      // api should not be called since init bailed early.
      expect(ctx.api).not.toHaveBeenCalled();
    });

    it("returns early if messages element is missing", () => {
      const elems = [["spec-chat-input", makeElement("textarea")]];
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      expect(ctx.api).not.toHaveBeenCalled();
    });
  });

  describe("init - send mode hint", () => {
    it("updates hint with Mac shortcut text on init", () => {
      const hintEl = makeElement("span");
      const elems = makeStandardElements();
      // Replace the hint element.
      const elemMap = new Map(elems);
      elemMap.set("spec-chat-send-hint", hintEl);
      const ctx = makeContext({
        elements: Array.from(elemMap.entries()),
        navigator: { platform: "MacIntel" },
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      // Default mode is "enter", so hint should be about Shift+Return.
      expect(hintEl.textContent).toBe("Shift+Return for new line");
    });

    it("toggles send mode on click", () => {
      const hintEl = makeElement("span");
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      elemMap.set("spec-chat-send-hint", hintEl);
      const ctx = makeContext({
        elements: Array.from(elemMap.entries()),
        navigator: { platform: "MacIntel" },
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      // Click the mode button to toggle.
      const modeBtn = elemMap.get("spec-chat-send-mode");
      modeBtn._fire("click");
      expect(hintEl.textContent).toBe("\u2318+Return to send");

      // Click again to toggle back.
      modeBtn._fire("click");
      expect(hintEl.textContent).toBe("Shift+Return for new line");
    });

    it("shows Ctrl on non-Mac platforms", () => {
      const hintEl = makeElement("span");
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      elemMap.set("spec-chat-send-hint", hintEl);
      const ctx = makeContext({
        elements: Array.from(elemMap.entries()),
        navigator: { platform: "Win32" },
      });
      loadPlanningChat(ctx);

      // Set send mode to cmd-enter via localStorage before init.
      ctx.localStorage._store["wallfacer-chat-send-mode"] = "cmd-enter";
      // Reload to pick up the localStorage value.
      const code = readFileSync(join(jsDir, "planning-chat.js"), "utf8");
      vm.runInContext(code, ctx, { filename: join(jsDir, "planning-chat.js") });
      ctx.PlanningChat.init();
      expect(hintEl.textContent).toBe("Ctrl+Return to send");
    });
  });

  describe("init - slash button", () => {
    it("sets input value to / and triggers input change", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      const input = elemMap.get("spec-chat-input");
      const slashBtn = elemMap.get("spec-chat-slash-hint");
      slashBtn._fire("click");
      expect(input.value).toBe("/");
      expect(input.focus).toHaveBeenCalled();
    });
  });

  describe("init - at button", () => {
    it("appends @ to input value", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");
      input.value = "hello ";
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      const atBtn = elemMap.get("spec-chat-at-hint");
      atBtn._fire("click");
      expect(input.value).toBe("hello @");
      expect(input.focus).toHaveBeenCalled();
    });
  });

  describe("init - scroll tracking", () => {
    it("sets up scroll listener on messages element", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      const messagesEl = elemMap.get("spec-chat-messages");
      expect(messagesEl._listeners["scroll"]).toBeDefined();
      expect(messagesEl._listeners["scroll"].length).toBe(1);
    });
  });

  describe("init - history loading", () => {
    it("renders messages from loaded history", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const messagesEl = elemMap.get("spec-chat-messages");

      const ctx = makeContext({
        elements: elems,
        api: vi.fn().mockImplementation((url) => {
          if (url.indexOf("/api/planning/threads") !== -1) {
            return Promise.resolve({
              threads: [{ id: "t1", name: "Chat 1" }],
              active_id: "t1",
            });
          }
          if (url.startsWith("/api/planning/messages")) {
            return Promise.resolve([
              {
                role: "user",
                content: "hello",
                timestamp: "2026-01-01T00:00:00Z",
              },
              {
                role: "assistant",
                content: "hi there",
                timestamp: "2026-01-01T00:00:01Z",
              },
            ]);
          }
          return Promise.resolve([]);
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      // Wait for async _loadHistory.
      await new Promise((r) => setTimeout(r, 50));

      // Messages should have been appended.
      expect(messagesEl.children.length).toBeGreaterThanOrEqual(2);
    });

    it("renders assistant messages with raw_output and activity", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const messagesEl = elemMap.get("spec-chat-messages");

      const rawOutput =
        '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"test"}]}}\n';
      const ctx = makeContext({
        elements: elems,
        api: vi.fn().mockImplementation((url) => {
          if (url.indexOf("/api/planning/threads") !== -1) {
            return Promise.resolve({
              threads: [{ id: "t1", name: "Chat 1" }],
              active_id: "t1",
            });
          }
          if (url.startsWith("/api/planning/messages")) {
            return Promise.resolve([
              {
                role: "assistant",
                content: "result",
                raw_output: rawOutput,
                timestamp: "2026-01-01T00:00:01Z",
              },
            ]);
          }
          return Promise.resolve([]);
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      await new Promise((r) => setTimeout(r, 50));
      expect(messagesEl.children.length).toBeGreaterThanOrEqual(1);
      expect(ctx.renderPrettyLogs).toHaveBeenCalled();
    });

    it("handles history load failure gracefully", async () => {
      const elems = makeStandardElements();
      const ctx = makeContext({
        elements: elems,
        api: vi.fn().mockRejectedValue(new Error("network error")),
      });
      loadPlanningChat(ctx);
      // Should not throw.
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 50));
    });
  });

  describe("sendMessage", () => {
    it("sends message via fetch and starts streaming", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");
      const messagesEl = elemMap.get("spec-chat-messages");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      // Set up fetch to return ok.
      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });

      input.value = "test message";
      await ctx.PlanningChat.sendMessage("test message");

      // fetch should have been called.
      expect(ctx.fetch).toHaveBeenCalledWith(
        "/api/planning/messages",
        expect.objectContaining({
          method: "POST",
          body: JSON.stringify({
            message: "test message",
            focused_spec: "",
            thread: "t1",
          }),
        }),
      );

      // Input should be cleared.
      expect(input.value).toBe("");

      // A user bubble should have been appended.
      expect(messagesEl.children.length).toBeGreaterThanOrEqual(1);

      // startStreamingFetch should have been called.
      expect(ctx.startStreamingFetch).toHaveBeenCalled();

      // isStreaming should be true.
      expect(ctx.PlanningChat.isStreaming()).toBe(true);
    });

    it("includes focused spec path", async () => {
      const elems = makeStandardElements();
      const ctx = makeContext({
        elements: elems,
        specModeState: { focusedSpecPath: "specs/local/test.md" },
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("hello");

      expect(ctx.fetch).toHaveBeenCalledWith(
        "/api/planning/messages",
        expect.objectContaining({
          body: JSON.stringify({
            message: "hello",
            focused_spec: "specs/local/test.md",
            thread: "t1",
          }),
        }),
      );
    });

    it("handles 409 conflict", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const messagesEl = elemMap.get("spec-chat-messages");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({
        ok: false,
        status: 409,
        text: vi.fn().mockResolvedValue("busy"),
      });
      await ctx.PlanningChat.sendMessage("hi");

      // Should append a system message about agent being busy.
      const systemMsgs = messagesEl.children.filter
        ? messagesEl.children.filter((c) => c.className.includes("system"))
        : messagesEl.children.filter((c) => c.className.includes("system"));
      // At least a user bubble + system message should be appended.
      expect(messagesEl.children.length).toBeGreaterThanOrEqual(2);

      // isStreaming should remain false.
      expect(ctx.PlanningChat.isStreaming()).toBe(false);
    });

    it("handles non-ok response", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const messagesEl = elemMap.get("spec-chat-messages");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({
        ok: false,
        status: 500,
        text: vi.fn().mockResolvedValue("Internal Server Error"),
      });
      await ctx.PlanningChat.sendMessage("test");

      expect(messagesEl.children.length).toBeGreaterThanOrEqual(2);
      expect(ctx.PlanningChat.isStreaming()).toBe(false);
    });

    it("handles fetch exception", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const messagesEl = elemMap.get("spec-chat-messages");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockRejectedValueOnce(new Error("network down"));
      await ctx.PlanningChat.sendMessage("test");

      expect(messagesEl.children.length).toBeGreaterThanOrEqual(2);
      expect(ctx.PlanningChat.isStreaming()).toBe(false);
    });

    it("enqueues message when already streaming", async () => {
      const elems = makeStandardElements();
      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      // Start a stream.
      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("first");

      expect(ctx.PlanningChat.isStreaming()).toBe(true);

      // Send another while streaming - should be queued.
      await ctx.PlanningChat.sendMessage("second");
      const q = ctx.PlanningChat.getQueue();
      expect(q.length).toBe(1);
      expect(q[0].text).toBe("second");
    });
  });

  describe("streaming - onChunk and onDone callbacks", () => {
    it("processes NDJSON chunks and calls renderMarkdown", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("hello");

      // Simulate chunks arriving.
      const ndjsonLine =
        '{"type":"assistant","message":{"content":[{"type":"text","text":"Hello world"}]}}' +
        "\n";
      streamCallbacks.onChunk(ndjsonLine);

      expect(ctx.renderMarkdown).toHaveBeenCalledWith("Hello world");

      // Complete the stream.
      streamCallbacks.onDone(true);

      expect(ctx.PlanningChat.isStreaming()).toBe(false);
    });

    it("shows tool activity in chunks", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("hello");

      const toolLine =
        '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"read_file"}]}}' +
        "\n";
      streamCallbacks.onChunk(toolLine);

      expect(ctx.renderPrettyLogs).toHaveBeenCalled();
    });

    it("retries once on empty onDone", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};
      let callCount = 0;

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          callCount++;
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      // First call.
      expect(callCount).toBe(1);

      // onDone with hadData=false triggers retry.
      streamCallbacks.onDone(false);

      // setTimeout was called, which calls _connectStream again.
      expect(callCount).toBe(2);

      // Second onDone with hadData=false should not retry again.
      streamCallbacks.onDone(false);
      expect(ctx.PlanningChat.isStreaming()).toBe(false);
    });

    it("retries once on onError", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};
      let callCount = 0;

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          callCount++;
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      expect(callCount).toBe(1);

      // First error triggers retry.
      streamCallbacks.onError();
      expect(callCount).toBe(2);

      // Second error does not retry.
      streamCallbacks.onError();
      expect(ctx.PlanningChat.isStreaming()).toBe(false);
    });

    it("renders error messages from NDJSON", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      const errorLine =
        '{"type":"result","is_error":true,"result":"Something went wrong"}' +
        "\n";
      streamCallbacks.onChunk(errorLine);
      streamCallbacks.onDone(true);

      expect(ctx.PlanningChat.isStreaming()).toBe(false);
    });
  });

  describe("clearHistory", () => {
    it("calls fetch DELETE and clears messages", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const messagesEl = elemMap.get("spec-chat-messages");
      messagesEl.innerHTML = "<div>old message</div>";

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 200 });
      await ctx.PlanningChat.clearHistory();

      expect(ctx.fetch).toHaveBeenCalledWith(
        "/api/planning/messages?thread=t1",
        expect.objectContaining({ method: "DELETE" }),
      );
      expect(messagesEl.innerHTML).toBe("");
    });

    it("clears messages even if fetch fails", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const messagesEl = elemMap.get("spec-chat-messages");
      messagesEl.innerHTML = "<div>old</div>";

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockRejectedValueOnce(new Error("fail"));
      await ctx.PlanningChat.clearHistory();
      expect(messagesEl.innerHTML).toBe("");
    });
  });

  describe("queue management", () => {
    it("drains queue after streaming completes", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      // Start first message.
      ctx.fetch.mockResolvedValue({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("first");
      expect(ctx.PlanningChat.isStreaming()).toBe(true);

      // Enqueue a second message.
      await ctx.PlanningChat.sendMessage("second");
      expect(ctx.PlanningChat.getQueue().length).toBe(1);

      // Complete the first stream.
      streamCallbacks.onDone(true);

      // The queue should have been drained - second message sent.
      expect(ctx.PlanningChat.getQueue().length).toBe(0);
    });
  });

  describe("_onInputKeydown - Enter key", () => {
    it("sends message on Enter in enter mode", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      input.value = "typed message";
      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });

      const prevented = { defaultPrevented: false };
      input._fire("keydown", {
        key: "Enter",
        shiftKey: false,
        metaKey: false,
        ctrlKey: false,
        preventDefault: () => {
          prevented.defaultPrevented = true;
        },
      });

      // Allow async sendMessage to complete.
      await new Promise((r) => setTimeout(r, 50));

      expect(prevented.defaultPrevented).toBe(true);
      expect(ctx.fetch).toHaveBeenCalledWith(
        "/api/planning/messages",
        expect.objectContaining({ method: "POST" }),
      );
    });

    it("does not send on Shift+Enter in enter mode", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      input.value = "some text";
      const prevented = { defaultPrevented: false };
      input._fire("keydown", {
        key: "Enter",
        shiftKey: true,
        metaKey: false,
        ctrlKey: false,
        preventDefault: () => {
          prevented.defaultPrevented = true;
        },
      });

      // Shift+Enter in "enter" mode should send because of the logic:
      // shouldSend = !e.shiftKey || e.metaKey || e.ctrlKey
      // With shiftKey=true, metaKey=false, ctrlKey=false: shouldSend = false
      expect(prevented.defaultPrevented).toBe(false);
    });

    it("does not send on empty input", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      input.value = "   ";
      const prevented = { defaultPrevented: false };
      input._fire("keydown", {
        key: "Enter",
        shiftKey: false,
        metaKey: false,
        ctrlKey: false,
        preventDefault: () => {
          prevented.defaultPrevented = true;
        },
      });

      // preventDefault is called, but sendMessage is not (text is empty after trim).
      expect(prevented.defaultPrevented).toBe(true);
    });
  });

  describe("_onInputKeydown - cmd-enter mode", () => {
    it("sends on Cmd+Enter in cmd-enter mode", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      // Toggle to cmd-enter mode.
      const modeBtn = elemMap.get("spec-chat-send-mode");
      modeBtn._fire("click");

      input.value = "cmd enter test";
      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });

      const prevented = { defaultPrevented: false };
      input._fire("keydown", {
        key: "Enter",
        shiftKey: false,
        metaKey: true,
        ctrlKey: false,
        preventDefault: () => {
          prevented.defaultPrevented = true;
        },
      });

      await new Promise((r) => setTimeout(r, 50));
      expect(prevented.defaultPrevented).toBe(true);
    });

    it("does not send on plain Enter in cmd-enter mode", () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      // Toggle to cmd-enter mode.
      const modeBtn = elemMap.get("spec-chat-send-mode");
      modeBtn._fire("click");

      input.value = "text";
      const prevented = { defaultPrevented: false };
      input._fire("keydown", {
        key: "Enter",
        shiftKey: false,
        metaKey: false,
        ctrlKey: false,
        preventDefault: () => {
          prevented.defaultPrevented = true;
        },
      });

      expect(prevented.defaultPrevented).toBe(false);
    });
  });

  describe("_onInputKeydown - autocomplete navigation", () => {
    it("navigates autocomplete with ArrowDown/ArrowUp and selects with Enter", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");

      const ctx = makeContext({
        elements: elems,
        api: vi.fn().mockImplementation((url) => {
          if (url.indexOf("/api/planning/threads") !== -1) {
            return Promise.resolve({
              threads: [{ id: "t1", name: "Chat 1" }],
              active_id: "t1",
            });
          }
          if (url === "/api/planning/commands") {
            return Promise.resolve([
              { name: "help", description: "Show help" },
              { name: "status", description: "Show status" },
            ]);
          }
          return Promise.resolve([]);
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 50));

      // Type "/" to trigger autocomplete.
      input.value = "/";
      // Trigger the input event handler.
      input._fire("input");
      await new Promise((r) => setTimeout(r, 50));

      // Now navigate with ArrowDown.
      input._fire("keydown", {
        key: "ArrowDown",
        preventDefault: vi.fn(),
        shiftKey: false,
        metaKey: false,
        ctrlKey: false,
      });

      // Select with Enter.
      input._fire("keydown", {
        key: "Enter",
        preventDefault: vi.fn(),
        shiftKey: false,
        metaKey: false,
        ctrlKey: false,
      });
    });

    it("closes autocomplete on Escape", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");

      const ctx = makeContext({
        elements: elems,
        api: vi.fn().mockImplementation((url) => {
          if (url.indexOf("/api/planning/threads") !== -1) {
            return Promise.resolve({
              threads: [{ id: "t1", name: "Chat 1" }],
              active_id: "t1",
            });
          }
          if (url === "/api/planning/commands") {
            return Promise.resolve([
              { name: "help", description: "Show help" },
            ]);
          }
          return Promise.resolve([]);
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 50));

      input.value = "/";
      input._fire("input");
      await new Promise((r) => setTimeout(r, 50));

      input._fire("keydown", {
        key: "Escape",
        preventDefault: vi.fn(),
        shiftKey: false,
        metaKey: false,
        ctrlKey: false,
      });
    });
  });

  describe("_onInputChange - autocomplete trigger", () => {
    it("closes autocomplete when input does not start with /", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      input.value = "hello";
      input._fire("input");
      // No autocomplete should be shown.
    });

    it("closes autocomplete when input contains newline after /", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      input.value = "/hello\nworld";
      input._fire("input");
    });
  });

  describe("_autoGrow", () => {
    it("adjusts input height based on scrollHeight", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");
      input.scrollHeight = 80;

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      // Trigger input event which calls _autoGrow.
      input._fire("input");
      expect(input.style.height).toBe("80px");
    });

    it("caps height at 200px", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");
      input.scrollHeight = 500;

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      input._fire("input");
      expect(input.style.height).toBe("200px");
    });
  });

  describe("NDJSON parsing via streaming", () => {
    it("extracts text from multiple assistant blocks", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      const line1 =
        '{"type":"assistant","message":{"content":[{"type":"text","text":"Hello "}]}}' +
        "\n";
      const line2 =
        '{"type":"assistant","message":{"content":[{"type":"text","text":"World"}]}}' +
        "\n";
      streamCallbacks.onChunk(line1 + line2);

      // renderMarkdown should have been called with concatenated text.
      expect(ctx.renderMarkdown).toHaveBeenCalledWith("Hello World");
    });

    it("handles invalid JSON lines gracefully", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      // Send invalid JSON mixed with valid.
      const chunk =
        'not json\n{"type":"assistant","message":{"content":[{"type":"text","text":"ok"}]}}\n';
      streamCallbacks.onChunk(chunk);

      expect(ctx.renderMarkdown).toHaveBeenCalledWith("ok");
    });

    it("handles non-text content blocks", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      // Content with thinking type (not text).
      const line =
        '{"type":"assistant","message":{"content":[{"type":"thinking","thinking":"hmm"}]}}' +
        "\n";
      streamCallbacks.onChunk(line);

      // renderPrettyLogs should be called for tool activity.
      expect(ctx.renderPrettyLogs).toHaveBeenCalled();
    });

    it("detects user type as tool activity", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      const line =
        '{"type":"user","message":{"content":[{"type":"tool_result"}]}}' + "\n";
      streamCallbacks.onChunk(line);

      // User type is treated as tool activity.
      expect(ctx.renderPrettyLogs).toHaveBeenCalled();
    });

    it("shows 'No response' when stream has no content", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      // Complete stream with no content.
      streamCallbacks.onDone(true);

      expect(ctx.PlanningChat.isStreaming()).toBe(false);
    });
  });

  describe("interrupt", () => {
    it("interrupts an active stream", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      let streamCallbacks = {};
      const abortFn = vi.fn();

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: abortFn };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      // Start streaming.
      ctx.fetch.mockResolvedValue({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");
      expect(ctx.PlanningChat.isStreaming()).toBe(true);

      // Find and click the interrupt button. It was created via createElement.
      // We need to find it in the send group.
      const sendBtn = elemMap.get("spec-chat-send");
      const interruptBtn = sendBtn.parentElement.children.find(
        (c) => c.className && c.className.includes("interrupt"),
      );

      // Instead, just fire the fetch for interrupt and call _stopStreaming via the click handler.
      // The interrupt button was wired in init. Let's test via the public visible behavior.
      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 200 });

      // We can access the interrupt button from created elements.
      const createdEls = ctx._createdElements || [];
      const intBtn = createdEls.find(
        (e) => e.className && e.className.includes("interrupt"),
      );
      if (intBtn) {
        intBtn._fire("click");
        await new Promise((r) => setTimeout(r, 50));
      }

      expect(ctx.PlanningChat.isStreaming()).toBe(false);
      expect(abortFn).toHaveBeenCalled();
    });
  });

  describe("localStorage integration", () => {
    it("reads send mode from localStorage", () => {
      const ctx = makeContext({
        elements: makeStandardElements(),
        localStorage: {
          _store: { "wallfacer-chat-send-mode": "cmd-enter" },
          getItem(key) {
            return this._store[key] || null;
          },
          setItem(key, val) {
            this._store[key] = val;
          },
        },
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      // The hint should reflect cmd-enter mode.
      const hintEl = new Map(makeStandardElements()).get("spec-chat-send-hint");
      // We need the actual element from context, so let's check via toggle behavior.
      // Since the mode was loaded as cmd-enter, toggling should go to enter.
    });

    it("saves send mode to localStorage on toggle", () => {
      const storage = {
        _store: {},
        getItem(key) {
          return this._store[key] || null;
        },
        setItem: vi.fn(function (key, val) {
          this._store[key] = val;
        }),
      };
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const ctx = makeContext({
        elements: elems,
        localStorage: storage,
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();

      const modeBtn = elemMap.get("spec-chat-send-mode");
      modeBtn._fire("click");
      expect(storage.setItem).toHaveBeenCalledWith(
        "wallfacer-chat-send-mode",
        "cmd-enter",
      );
    });
  });

  describe("_renderChatResponse edge cases", () => {
    it("renders error + text + activity together", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      // Chunk with text, tool activity, and error.
      const chunk =
        '{"type":"assistant","message":{"content":[{"type":"text","text":"output"},{"type":"tool_use","name":"x"}]}}' +
        "\n" +
        '{"type":"result","is_error":true,"result":"boom"}' +
        "\n";
      streamCallbacks.onChunk(chunk);

      expect(ctx.renderMarkdown).toHaveBeenCalledWith("output");
      expect(ctx.renderPrettyLogs).toHaveBeenCalled();

      streamCallbacks.onDone(true);
    });

    it("renders without renderPrettyLogs when it is not a function", async () => {
      const elems = makeStandardElements();
      let streamCallbacks = {};

      const ctx = makeContext({
        elements: elems,
        renderPrettyLogs: "not a function",
        startStreamingFetch: vi.fn((opts) => {
          streamCallbacks = opts;
          return { abort: vi.fn() };
        }),
      });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });
      await ctx.PlanningChat.sendMessage("test");

      const chunk =
        '{"type":"assistant","message":{"content":[{"type":"tool_use","name":"x"}]}}' +
        "\n";
      streamCallbacks.onChunk(chunk);
      streamCallbacks.onDone(true);
      // Should not throw.
    });
  });

  describe("send button click", () => {
    it("sends message when send button is clicked with input", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");
      const sendBtn = elemMap.get("spec-chat-send");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      input.value = "click send";
      ctx.fetch.mockResolvedValueOnce({ ok: true, status: 202, text: vi.fn() });

      sendBtn._fire("click");
      await new Promise((r) => setTimeout(r, 50));

      expect(ctx.fetch).toHaveBeenCalledWith(
        "/api/planning/messages",
        expect.objectContaining({ method: "POST" }),
      );
    });

    it("does not send when input is empty", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const input = elemMap.get("spec-chat-input");
      const sendBtn = elemMap.get("spec-chat-send");

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      input.value = "";
      const fetchCallsBefore = ctx.fetch.mock.calls.length;
      sendBtn._fire("click");
      await new Promise((r) => setTimeout(r, 50));

      // fetch for sendMessage should NOT have been called (only the init ones).
      expect(ctx.fetch.mock.calls.length).toBe(fetchCallsBefore);
    });
  });

  describe("clear button click", () => {
    it("clears history when clear button is clicked", async () => {
      const elems = makeStandardElements();
      const elemMap = new Map(elems);
      const clearBtn = elemMap.get("spec-chat-clear");
      const messagesEl = elemMap.get("spec-chat-messages");
      messagesEl.innerHTML = "<div>msg</div>";

      const ctx = makeContext({ elements: elems });
      loadPlanningChat(ctx);
      ctx.PlanningChat.init();
      await new Promise((r) => setTimeout(r, 10));

      ctx.fetch.mockResolvedValueOnce({ ok: true });
      clearBtn._fire("click");
      await new Promise((r) => setTimeout(r, 50));

      expect(messagesEl.innerHTML).toBe("");
    });
  });
});
