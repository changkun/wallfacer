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
// planning-chat.js uses attachAutocomplete from the shared widget
// (ui/js/lib/autocomplete.ts, compiled to build/lib/autocomplete.js by
// `make ui-ts`). Load the widget first so the global is available.
const autocompleteCode = readFileSync(
  join(jsDir, "build/lib/autocomplete.js"),
  "utf8",
);
const chatCode = readFileSync(join(jsDir, "planning-chat.js"), "utf8");

// Minimal DOM element factory.
function makeEl(tag, registry) {
  const _classList = new Set();
  const _children = [];
  const _listeners = {};
  const _attrs = {};
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
    setAttribute(name, value) {
      _attrs[name] = String(value);
    },
    getAttribute(name) {
      return Object.prototype.hasOwnProperty.call(_attrs, name)
        ? _attrs[name]
        : null;
    },
    hasAttribute(name) {
      return Object.prototype.hasOwnProperty.call(_attrs, name);
    },
    removeAttribute(name) {
      delete _attrs[name];
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
    remove() {
      const parent = el.parentElement;
      if (!parent || !parent.children) return;
      const idx = parent.children.indexOf(el);
      if (idx >= 0) parent.children.splice(idx, 1);
      el.parentElement = null;
    },
    addEventListener(type, fn) {
      if (!_listeners[type]) _listeners[type] = [];
      _listeners[type].push(fn);
    },
    dispatchEvent(e) {
      const fns = _listeners[e.type] || [];
      fns.forEach((fn) => fn(e));
    },
    click() {
      const fns = _listeners["click"] || [];
      fns.forEach((fn) => fn({ type: "click" }));
    },
    focus() {},
    querySelector(sel) {
      // Match class selector against children, recursing into descendants.
      const want = sel.startsWith(".") ? sel.slice(1) : null;
      if (!want) return null;
      for (const child of _children) {
        if (child.classList && child.classList.contains(want)) return child;
        const found = child.querySelector ? child.querySelector(sel) : null;
        if (found) return found;
      }
      return null;
    },
    querySelectorAll(sel) {
      // Supports ".class" and ".class[attr]" selectors. Walks direct
      // children only (sufficient for the planning-chat message list).
      const result = [];
      const m = sel.match(/^\.([\w-]+)(?:\[([\w-]+)\])?$/);
      if (!m) return result;
      const cls = m[1];
      const attr = m[2];
      for (const child of _children) {
        if (!child.classList || !child.classList.contains(cls)) continue;
        if (attr && !child.hasAttribute(attr)) continue;
        result.push(child);
      }
      return result;
    },
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

  // The shared autocomplete widget appends its dropdown to document.body.
  // Supply a minimal body so fetchItems-driven renders don't crash.
  const body = makeEl("BODY", registry);

  const ctx = {
    document: {
      body,
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
    window: { addEventListener() {} },
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
        undo: () => "/api/planning/undo",
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
  vm.runInContext(autocompleteCode, ctx);
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

  // ---- per-message undo button ----

  function messagesEl() {
    return ctx.document.getElementById("spec-chat-messages");
  }

  function bubbleFor(children, round) {
    return children.find(
      (b) =>
        b.classList.contains("planning-chat-bubble--assistant") &&
        b.getAttribute("data-round") === String(round),
    );
  }

  async function loadWithHistory(history) {
    ctx._setApiResult(history);
    ctx.PlanningChat.init();
    // Yield for the awaited _loadHistory inside init.
    await new Promise((r) => ctx.setTimeout(r, 10));
  }

  it("user bubble never renders an undo button", async () => {
    await loadWithHistory([
      {
        role: "user",
        content: "do a thing",
        timestamp: "2026-04-12T10:00:00Z",
      },
    ]);
    const kids = messagesEl().children;
    const userBubble = kids.find((b) =>
      b.classList.contains("planning-chat-bubble--user"),
    );
    expect(userBubble).toBeTruthy();
    expect(userBubble.querySelector(".planning-chat-bubble__undo")).toBeNull();
  });

  it("assistant bubble without plan_round renders no undo button", async () => {
    await loadWithHistory([
      {
        role: "assistant",
        content: "noop response",
        timestamp: "2026-04-12T10:00:00Z",
        plan_round: 0,
      },
    ]);
    const kids = messagesEl().children;
    const ab = kids.find((b) =>
      b.classList.contains("planning-chat-bubble--assistant"),
    );
    expect(ab).toBeTruthy();
    expect(ab.getAttribute("data-round")).toBeNull();
    expect(ab.querySelector(".planning-chat-bubble__undo")).toBeNull();
  });

  it("only the latest-round assistant bubble has its undo button enabled", async () => {
    await loadWithHistory([
      { role: "user", content: "a", timestamp: "t", plan_round: 0 },
      {
        role: "assistant",
        content: "A1",
        timestamp: "t",
        plan_round: 1,
      },
      { role: "user", content: "b", timestamp: "t", plan_round: 0 },
      {
        role: "assistant",
        content: "A2",
        timestamp: "t",
        plan_round: 2,
      },
      { role: "user", content: "c", timestamp: "t", plan_round: 0 },
      {
        role: "assistant",
        content: "A3",
        timestamp: "t",
        plan_round: 3,
      },
    ]);
    const kids = messagesEl().children;
    const r1 = bubbleFor(kids, 1);
    const r2 = bubbleFor(kids, 2);
    const r3 = bubbleFor(kids, 3);
    expect(r1 && r2 && r3).toBeTruthy();
    expect(r1.querySelector(".planning-chat-bubble__undo").disabled).toBe(true);
    expect(r2.querySelector(".planning-chat-bubble__undo").disabled).toBe(true);
    expect(r3.querySelector(".planning-chat-bubble__undo").disabled).toBe(
      false,
    );
  });

  it("successful undo dims the bubble, strips its button, appends a system message", async () => {
    await loadWithHistory([
      {
        role: "assistant",
        content: "drafted foo",
        timestamp: "t",
        plan_round: 1,
      },
    ]);
    ctx._setFetchResult({
      status: 200,
      ok: true,
      json: () =>
        Promise.resolve({
          round: 1,
          summary: "drafted foo",
          files_reverted: ["specs/foo.md"],
          workspace: "/ws",
        }),
    });
    const kids = messagesEl().children;
    const ab = bubbleFor(kids, 1);
    const btn = ab.querySelector(".planning-chat-bubble__undo");
    const originalLength = kids.length;

    btn.click();
    await new Promise((r) => ctx.setTimeout(r, 10));

    // Bubble is dimmed, no longer keyed by round, button gone.
    expect(ab.classList.contains("planning-chat-bubble--reverted")).toBe(true);
    expect(ab.getAttribute("data-round")).toBeNull();
    expect(ab.querySelector(".planning-chat-bubble__undo")).toBeNull();
    // Original bubble still in the tree.
    expect(kids.indexOf(ab)).toBeGreaterThanOrEqual(0);
    // A new system bubble was appended with the expected content.
    const sys = kids[kids.length - 1];
    expect(sys.classList.contains("planning-chat-system--undo")).toBe(true);
    expect(sys.textContent).toContain("Undid round 1");
    expect(sys.textContent).toContain("drafted foo");
    // Net count: same original bubbles + 1 appended system = +1.
    expect(kids.length).toBe(originalLength + 1);
  });

  it("409 'not at HEAD' conflict appends a warning without reverting", async () => {
    await loadWithHistory([
      {
        role: "assistant",
        content: "drafted foo",
        timestamp: "t",
        plan_round: 1,
      },
    ]);
    ctx._setFetchResult({
      status: 409,
      ok: false,
      json: () =>
        Promise.resolve({
          error:
            "latest planning commit is not at HEAD; new commits have been added since — resolve manually",
        }),
    });
    const kids = messagesEl().children;
    const ab = bubbleFor(kids, 1);
    const btn = ab.querySelector(".planning-chat-bubble__undo");

    btn.click();
    await new Promise((r) => ctx.setTimeout(r, 10));

    // Bubble is NOT reverted; it keeps its round and its button.
    expect(ab.classList.contains("planning-chat-bubble--reverted")).toBe(false);
    expect(ab.getAttribute("data-round")).toBe("1");
    expect(ab.querySelector(".planning-chat-bubble__undo")).toBeTruthy();
    // Button is re-enabled (it's still the latest round).
    expect(ab.querySelector(".planning-chat-bubble__undo").disabled).toBe(
      false,
    );
    // A warning system bubble was appended.
    const sys = kids[kids.length - 1];
    expect(sys.classList.contains("planning-chat-system")).toBe(true);
    expect(sys.textContent).toContain("unrelated commits");
  });

  it("409 stash-pop conflict appends a stash-specific warning", async () => {
    await loadWithHistory([
      {
        role: "assistant",
        content: "drafted foo",
        timestamp: "t",
        plan_round: 1,
      },
    ]);
    ctx._setFetchResult({
      status: 409,
      ok: false,
      json: () =>
        Promise.resolve({
          error:
            "stash pop conflict after undo; stash retained for manual resolution",
        }),
    });
    const kids = messagesEl().children;
    const ab = bubbleFor(kids, 1);
    const btn = ab.querySelector(".planning-chat-bubble__undo");

    btn.click();
    await new Promise((r) => ctx.setTimeout(r, 10));

    const sys = kids[kids.length - 1];
    expect(sys.textContent).toContain("stash list");
    expect(ab.classList.contains("planning-chat-bubble--reverted")).toBe(false);
  });
});
