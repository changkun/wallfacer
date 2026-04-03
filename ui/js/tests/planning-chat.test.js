/**
 * Unit tests for the planning chat module.
 * Uses vm sandboxing to match the project's test pattern (no jsdom).
 */
import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const chatCode = readFileSync(join(jsDir, "planning-chat.js"), "utf8");

// Minimal DOM element factory.
function makeEl(tag, registry) {
  const _classList = new Set();
  const _children = [];
  const _listeners = {};
  let _innerHTML = "";
  let _textContent = "";
  let _id = "";

  const el = {
    tagName: tag,
    get id() {
      return _id;
    },
    set id(v) {
      _id = v;
      if (v) registry.set(v, el);
    },
    disabled: false,
    value: "",
    style: {},
    dataset: {},
    parentElement: null,
    classList: {
      add(c) {
        _classList.add(c);
      },
      remove(c) {
        _classList.delete(c);
      },
      toggle(c, force) {
        if (force) _classList.add(c);
        else _classList.delete(c);
      },
      contains(c) {
        return _classList.has(c);
      },
    },
    get className() {
      return [..._classList].join(" ");
    },
    set className(v) {
      _classList.clear();
      v.split(/\s+/)
        .filter(Boolean)
        .forEach((c) => _classList.add(c));
    },
    get innerHTML() {
      return _innerHTML;
    },
    set innerHTML(v) {
      _innerHTML = v;
    },
    get textContent() {
      return _textContent;
    },
    set textContent(v) {
      _textContent = v;
      _innerHTML = v;
    },
    get children() {
      return _children;
    },
    appendChild(child) {
      child.parentElement = el;
      _children.push(child);
    },
    insertBefore(newChild, refChild) {
      newChild.parentElement = el;
      const idx = _children.indexOf(refChild);
      if (idx >= 0) _children.splice(idx, 0, newChild);
      else _children.push(newChild);
    },
    remove() {},
    addEventListener(type, fn) {
      if (!_listeners[type]) _listeners[type] = [];
      _listeners[type].push(fn);
    },
    dispatchEvent(e) {
      const fns = _listeners[e.type] || [];
      fns.forEach((fn) => fn(e));
    },
    querySelector(sel) {
      // Very basic: match class selector against children.
      for (const child of _children) {
        if (sel.startsWith(".") && child.classList.contains(sel.slice(1)))
          return child;
        const found = child.querySelector ? child.querySelector(sel) : null;
        if (found) return found;
      }
      return null;
    },
    querySelectorAll(sel) {
      const result = [];
      for (const child of _children) {
        if (sel.startsWith(".") && child.classList.contains(sel.slice(1)))
          result.push(child);
      }
      return result;
    },
    focus() {},
  };
  return el;
}

function makeContext() {
  const registry = new Map();
  const ids = [
    "spec-chat-stream",
    "spec-chat-messages",
    "spec-chat-input",
    "spec-chat-send",
    "spec-chat-send-mode",
    "spec-chat-send-hint",
    "spec-chat-slash-hint",
    "spec-chat-at-hint",
  ];
  for (const id of ids) {
    const el = makeEl("DIV", registry);
    el.id = id;
  }
  // Set up parent hierarchy for the composer.
  const inputEl = registry.get("spec-chat-input");
  const sendBtn = registry.get("spec-chat-send");
  const sendGroup = makeEl("DIV", registry);
  sendGroup.className = "spec-chat-send-group";
  sendBtn.parentElement = sendGroup;
  const composer = makeEl("DIV", registry);
  composer.className = "spec-chat-composer";
  inputEl.parentElement = composer;
  const streamEl = registry.get("spec-chat-stream");
  composer.parentElement = streamEl;

  let fetchResult = {
    status: 200,
    ok: true,
    json: () => Promise.resolve([]),
    text: () => Promise.resolve(""),
  };
  let apiResult = [];

  const ctx = {
    document: {
      getElementById(id) {
        return registry.get(id) || null;
      },
      createElement(tag) {
        return makeEl(tag, registry);
      },
      addEventListener() {},
      querySelector(sel) {
        if (sel === ".spec-chat-composer") return composer;
        return null;
      },
    },
    window: {},
    navigator: { platform: "MacIntel" },
    localStorage: {
      _data: {},
      getItem(k) {
        return this._data[k] || null;
      },
      setItem(k, v) {
        this._data[k] = v;
      },
      removeItem(k) {
        delete this._data[k];
      },
    },
    console,
    setTimeout,
    clearTimeout,
    setInterval: () => 0,
    clearInterval: () => {},
    Promise,
    JSON,
    Event: class Event {
      constructor(type) {
        this.type = type;
      }
    },
    fetch: () => Promise.resolve(fetchResult),
    startStreamingFetch: (opts) => {
      // Don't auto-complete — tests control completion explicitly.
      ctx._lastStreamOpts = opts;
      return {
        abort: () => {
          if (opts.onDone) opts.onDone(false);
        },
      };
    },
    renderPrettyLogs: (raw) => "<pre>" + raw + "</pre>",
    escapeHtml: (s) => s,
    withAuthToken: (url) => url,
    withBearerHeaders: () => ({}),
    Routes: {
      planning: {
        messages: () => "/api/planning/messages",
        sendMessage: () => "/api/planning/messages",
        messageStream: () => "/api/planning/messages/stream",
        commands: () => "/api/planning/commands",
        interruptMessage: () => "/api/planning/messages/interrupt",
      },
    },
    api: () => Promise.resolve(apiResult),
    renderMarkdown: (text) => "<p>" + text + "</p>",
    specModeState: { focusedSpecPath: "" },
    _setFetchResult(r) {
      fetchResult = r;
    },
    _setApiResult(r) {
      apiResult = r;
      ctx.api = () => Promise.resolve(r);
    },
  };

  vm.createContext(ctx);
  vm.runInContext(chatCode, ctx);
  return ctx;
}

describe("PlanningChat", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
  });

  it("module exposes init, sendMessage, isStreaming, getQueue", () => {
    expect(typeof ctx.PlanningChat.init).toBe("function");
    expect(typeof ctx.PlanningChat.sendMessage).toBe("function");
    expect(typeof ctx.PlanningChat.isStreaming).toBe("function");
    expect(typeof ctx.PlanningChat.getQueue).toBe("function");
  });

  it("isStreaming returns false initially", () => {
    expect(ctx.PlanningChat.isStreaming()).toBe(false);
  });

  it("init loads history via api call", async () => {
    let apiCalled = false;
    ctx.api = () => {
      apiCalled = true;
      return Promise.resolve([]);
    };
    ctx.PlanningChat.init();
    await new Promise((r) => ctx.setTimeout(r, 10));
    expect(apiCalled).toBe(true);
  });

  it("sendMessage posts to server", async () => {
    let postBody = null;
    ctx.fetch = (url, opts) => {
      if (opts && opts.method === "POST") {
        postBody = JSON.parse(opts.body);
      }
      return Promise.resolve({
        status: 202,
        ok: true,
        json: () => Promise.resolve({}),
      });
    };
    ctx.PlanningChat.init();
    await ctx.PlanningChat.sendMessage("hello world");
    expect(postBody).not.toBeNull();
    expect(postBody.message).toBe("hello world");
  });

  it("sendMessage includes focused_spec from specModeState", async () => {
    let postBody = null;
    ctx.specModeState.focusedSpecPath = "specs/local/foo.md";
    ctx.fetch = (url, opts) => {
      if (opts && opts.method === "POST") {
        postBody = JSON.parse(opts.body);
      }
      return Promise.resolve({
        status: 202,
        ok: true,
        json: () => Promise.resolve({}),
      });
    };
    ctx.PlanningChat.init();
    await ctx.PlanningChat.sendMessage("summarize");
    expect(postBody.focused_spec).toBe("specs/local/foo.md");
  });

  it("commands list fetched for autocomplete", async () => {
    let commandsFetched = false;
    ctx.api = (url) => {
      if (url.includes("commands")) commandsFetched = true;
      return Promise.resolve([
        {
          name: "summarize",
          description: "Summarize spec",
          usage: "/summarize [words]",
        },
      ]);
    };
    ctx.PlanningChat.init();

    // Simulate typing / in the input — trigger _onInputChange.
    const input = ctx.document.getElementById("spec-chat-input");
    input.value = "/sum";
    input.dispatchEvent(new ctx.Event("input"));

    await new Promise((r) => ctx.setTimeout(r, 50));
    expect(commandsFetched).toBe(true);
  });

  it("queues messages while streaming", async () => {
    // Start a streaming session.
    ctx.fetch = () => Promise.resolve({ status: 202, ok: true });
    ctx.PlanningChat.init();
    await ctx.PlanningChat.sendMessage("first");
    // Now streaming is true — next message should be queued.
    await ctx.PlanningChat.sendMessage("second");
    await ctx.PlanningChat.sendMessage("third");
    const q = ctx.PlanningChat.getQueue();
    expect(q.length).toBe(2);
    expect(q[0].text).toBe("second");
    expect(q[1].text).toBe("third");
  });

  it("getQueue returns empty array initially", () => {
    ctx.PlanningChat.init();
    expect(ctx.PlanningChat.getQueue()).toEqual([]);
  });
});
