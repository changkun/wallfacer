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
    get id() { return _id; },
    set id(v) { _id = v; if (v) registry.set(v, el); },
    disabled: false,
    value: "",
    style: {},
    parentElement: null,
    classList: {
      add(c) { _classList.add(c); },
      remove(c) { _classList.delete(c); },
      toggle(c, force) { if (force) _classList.add(c); else _classList.delete(c); },
      contains(c) { return _classList.has(c); },
    },
    get className() { return [..._classList].join(" "); },
    set className(v) { _classList.clear(); v.split(/\s+/).filter(Boolean).forEach(c => _classList.add(c)); },
    get innerHTML() { return _innerHTML; },
    set innerHTML(v) { _innerHTML = v; },
    get textContent() { return _textContent; },
    set textContent(v) { _textContent = v; _innerHTML = v; },
    get children() { return _children; },
    appendChild(child) {
      child.parentElement = el;
      _children.push(child);
    },
    remove() {},
    addEventListener(type, fn) {
      if (!_listeners[type]) _listeners[type] = [];
      _listeners[type].push(fn);
    },
    dispatchEvent(e) {
      const fns = _listeners[e.type] || [];
      fns.forEach(fn => fn(e));
    },
    querySelector(sel) {
      // Very basic: match class selector against children.
      for (const child of _children) {
        if (sel.startsWith(".") && child.classList.contains(sel.slice(1))) return child;
        const found = child.querySelector ? child.querySelector(sel) : null;
        if (found) return found;
      }
      return null;
    },
    querySelectorAll(sel) {
      const result = [];
      for (const child of _children) {
        if (sel.startsWith(".") && child.classList.contains(sel.slice(1))) result.push(child);
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
    "spec-chat-stream", "spec-chat-messages", "spec-chat-input", "spec-chat-send",
  ];
  for (const id of ids) {
    const el = makeEl("DIV", registry);
    el.id = id;
  }
  // Set up parent for input (autocomplete appends to it).
  const inputEl = registry.get("spec-chat-input");
  const inputWrapper = makeEl("DIV", registry);
  inputEl.parentElement = inputWrapper;
  inputWrapper.appendChild = function(child) { child.parentElement = inputWrapper; };

  let fetchResult = { status: 200, ok: true, json: () => Promise.resolve([]), text: () => Promise.resolve("") };
  let apiResult = [];

  const ctx = {
    document: {
      getElementById(id) { return registry.get(id) || null; },
      createElement(tag) { return makeEl(tag, registry); },
      addEventListener() {},
      querySelector() { return null; },
    },
    window: {},
    console,
    setTimeout,
    clearTimeout,
    setInterval: () => 0,
    clearInterval: () => {},
    Promise,
    JSON,
    Event: class Event { constructor(type) { this.type = type; } },
    EventSource: class EventSource {
      constructor() { this.onmessage = null; this.onerror = null; }
      addEventListener() {}
      close() {}
    },
    fetch: () => Promise.resolve(fetchResult),
    Routes: {
      planning: {
        messages: () => "/api/planning/messages",
        sendMessage: () => "/api/planning/messages",
        messageStream: () => "/api/planning/messages/stream",
        commands: () => "/api/planning/commands",
      },
    },
    api: () => Promise.resolve(apiResult),
    renderMarkdown: (text) => "<p>" + text + "</p>",
    specModeState: { focusedSpecPath: "" },
    _setFetchResult(r) { fetchResult = r; },
    _setApiResult(r) { apiResult = r; ctx.api = () => Promise.resolve(r); },
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

  it("module exposes init, sendMessage, isStreaming", () => {
    expect(typeof ctx.PlanningChat.init).toBe("function");
    expect(typeof ctx.PlanningChat.sendMessage).toBe("function");
    expect(typeof ctx.PlanningChat.isStreaming).toBe("function");
  });

  it("isStreaming returns false initially", () => {
    expect(ctx.PlanningChat.isStreaming()).toBe(false);
  });

  it("init loads history via api call", async () => {
    let apiCalled = false;
    ctx.api = () => { apiCalled = true; return Promise.resolve([]); };
    ctx.PlanningChat.init();
    await new Promise(r => ctx.setTimeout(r, 10));
    expect(apiCalled).toBe(true);
  });

  it("sendMessage posts to server", async () => {
    let postBody = null;
    ctx.fetch = (url, opts) => {
      if (opts && opts.method === "POST") {
        postBody = JSON.parse(opts.body);
      }
      return Promise.resolve({ status: 202, ok: true, json: () => Promise.resolve({}) });
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
      return Promise.resolve({ status: 202, ok: true, json: () => Promise.resolve({}) });
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
        { name: "summarize", description: "Summarize spec", usage: "/summarize [words]" },
      ]);
    };
    ctx.PlanningChat.init();

    // Simulate typing / in the input — trigger _onInputChange.
    const input = ctx.document.getElementById("spec-chat-input");
    input.value = "/sum";
    input.dispatchEvent(new ctx.Event("input"));

    await new Promise(r => ctx.setTimeout(r, 50));
    expect(commandsFetched).toBe(true);
  });
});
