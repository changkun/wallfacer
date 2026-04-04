/**
 * Unit tests for dispatchSelectedSpecs() in spec-explorer.js.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-explorer.js"), "utf8");

function makeEl(tag, registry) {
  const _classList = new Set();
  const _style = {};
  const _listeners = {};
  let _id = "";
  let _textContent = "";
  let _disabled = false;

  const el = {
    tagName: tag,
    get id() {
      return _id;
    },
    set id(v) {
      _id = v;
      if (v) registry.set(v, el);
    },
    style: _style,
    innerHTML: "",
    className: "",
    get textContent() {
      return _textContent;
    },
    set textContent(v) {
      _textContent = v;
    },
    get disabled() {
      return _disabled;
    },
    set disabled(v) {
      _disabled = v;
    },
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
    addEventListener(type, fn) {
      if (!_listeners[type]) _listeners[type] = [];
      _listeners[type].push(fn);
    },
    querySelectorAll() {
      return [];
    },
    getAttribute(name) {
      return el["_attr_" + name] || null;
    },
    remove() {},
    get nextSibling() {
      return null;
    },
    insertBefore() {},
  };
  return el;
}

function makeContext(opts = {}) {
  const registry = new Map();
  const storage = new Map();

  const ids = [
    "explorer-tree",
    "spec-status-filter",
    "spec-dispatch-bar",
    "spec-dispatch-selected-btn",
  ];
  for (const id of ids) {
    const el = makeEl("DIV", registry);
    el.id = id;
  }

  const confirmResult =
    opts.confirmResult !== undefined ? opts.confirmResult : true;

  const ctx = {
    document: {
      getElementById(id) {
        return registry.get(id) || null;
      },
      createElement(tag) {
        return makeEl(tag, registry);
      },
      querySelectorAll() {
        return [];
      },
    },
    localStorage: {
      getItem(k) {
        return storage.get(k) ?? null;
      },
      setItem(k, v) {
        storage.set(k, v);
      },
    },
    fetch: opts.fetch || (() => Promise.reject(new Error("stubbed"))),
    Routes: {
      specs: {
        tree: () => "/api/specs/tree",
        dispatch: () => "/api/specs/dispatch",
      },
    },
    withBearerHeaders: () => ({}),
    withAuthHeaders: (h) => h || {},
    api: vi.fn(() =>
      opts.apiResponse
        ? Promise.resolve(opts.apiResponse)
        : Promise.resolve({ dispatched: [], errors: [] }),
    ),
    escapeHtml: (s) => String(s).replace(/</g, "&lt;").replace(/>/g, "&gt;"),
    focusSpec: () => {},
    getFocusedSpecPath: () => null,
    activeWorkspaces: ["/workspace/repo"],
    _loadExplorerRoots: () => {},
    _startExplorerRefreshPoll: () => {},
    _stopExplorerRefreshPoll: () => {},
    _hideMinimap: () => {},
    renderMinimap: () => {},
    setInterval: () => 42,
    clearInterval: () => {},
    confirm: vi.fn(() => confirmResult),
    alert: vi.fn(),
    Math,
    JSON,
    Array,
    Set,
    console,
    registry,
    storage,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

describe("dispatchSelectedSpecs", () => {
  it("does nothing when no specs are selected", () => {
    const ctx = makeContext();
    ctx.dispatchSelectedSpecs();
    expect(ctx.confirm).not.toHaveBeenCalled();
    expect(ctx.api).not.toHaveBeenCalled();
  });

  it("does nothing when user cancels confirmation", () => {
    const ctx = makeContext({ confirmResult: false });
    ctx._selectedSpecPaths.add("specs/local/a.md");
    ctx.dispatchSelectedSpecs();
    expect(ctx.confirm).toHaveBeenCalledWith(
      "Dispatch 1 specs to the task board?",
    );
    expect(ctx.api).not.toHaveBeenCalled();
  });

  it("sends all selected paths to the dispatch API", async () => {
    const ctx = makeContext({
      apiResponse: {
        dispatched: [
          { spec_path: "specs/local/a.md", task_id: "id-1" },
          { spec_path: "specs/local/b.md", task_id: "id-2" },
        ],
        errors: [],
      },
    });
    ctx._selectedSpecPaths.add("specs/local/a.md");
    ctx._selectedSpecPaths.add("specs/local/b.md");
    ctx.dispatchSelectedSpecs();

    expect(ctx.api).toHaveBeenCalledWith("/api/specs/dispatch", {
      method: "POST",
      body: expect.any(String),
    });

    const body = JSON.parse(ctx.api.mock.calls[0][1].body);
    expect(body.paths).toEqual(
      expect.arrayContaining(["specs/local/a.md", "specs/local/b.md"]),
    );
    expect(body.run).toBe(false);
  });

  it("clears selection on success", async () => {
    const ctx = makeContext({
      apiResponse: {
        dispatched: [{ spec_path: "specs/local/a.md", task_id: "id-1" }],
        errors: [],
      },
    });
    ctx._selectedSpecPaths.add("specs/local/a.md");
    ctx.dispatchSelectedSpecs();

    await new Promise((r) => setTimeout(r, 10));

    expect(ctx._selectedSpecPaths.size).toBe(0);
  });

  it("shows alert on API error", async () => {
    const ctx = makeContext();
    ctx.api = vi.fn(() => Promise.reject(new Error("server error")));
    ctx._selectedSpecPaths.add("specs/local/a.md");
    ctx.dispatchSelectedSpecs();

    await new Promise((r) => setTimeout(r, 10));

    expect(ctx.alert).toHaveBeenCalledWith("Dispatch failed: server error");
  });

  it("shows partial success details", async () => {
    const ctx = makeContext({
      apiResponse: {
        dispatched: [{ spec_path: "specs/local/a.md", task_id: "id-1" }],
        errors: [{ spec_path: "specs/local/b.md", error: "not validated" }],
      },
    });
    ctx._selectedSpecPaths.add("specs/local/a.md");
    ctx._selectedSpecPaths.add("specs/local/b.md");
    ctx.dispatchSelectedSpecs();

    await new Promise((r) => setTimeout(r, 10));

    expect(ctx.alert).toHaveBeenCalled();
    const msg = ctx.alert.mock.calls[0][0];
    expect(msg).toContain("Dispatched 1");
    expect(msg).toContain("1 failed");
    expect(msg).toContain("not validated");
  });
});
