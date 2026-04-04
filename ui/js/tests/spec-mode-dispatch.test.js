/**
 * Unit tests for dispatchFocusedSpec() in spec-mode.js.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-mode.js"), "utf8");

function makeEl(tag, registry) {
  const _classList = new Set();
  const _style = {};
  const _dataset = {};
  let _id = "";
  let _textContent = "";
  let _disabled = false;
  let _onclick = null;

  return {
    tagName: tag,
    get id() {
      return _id;
    },
    set id(v) {
      _id = v;
      if (v && registry) registry.set(v, this);
    },
    style: _style,
    dataset: _dataset,
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
    get onclick() {
      return _onclick;
    },
    set onclick(v) {
      _onclick = v;
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
  };
}

function makeContext(opts = {}) {
  const registry = new Map();
  const storage = new Map();

  // Pre-create elements.
  const boardTab = makeEl("BUTTON", registry);
  boardTab.id = "sidebar-nav-board";
  boardTab.classList.add("active");

  const specTab = makeEl("BUTTON", registry);
  specTab.id = "sidebar-nav-spec";

  const board = makeEl("MAIN", registry);
  board.id = "board";

  const specContainer = makeEl("DIV", registry);
  specContainer.id = "spec-mode-container";
  specContainer.style.display = "none";

  const dispatchBtn = makeEl("BUTTON", registry);
  dispatchBtn.id = "spec-dispatch-btn";
  dispatchBtn.textContent = "Dispatch";

  const fetchCalls = [];
  const confirmResult =
    opts.confirmResult !== undefined ? opts.confirmResult : true;

  const ctx = {
    document: {
      registry,
      getElementById(id) {
        return registry.get(id) || null;
      },
      addEventListener() {},
    },
    localStorage: {
      getItem(k) {
        return storage.get(k) ?? null;
      },
      setItem(k, v) {
        storage.set(k, v);
      },
    },
    fetch: vi.fn(() => Promise.reject(new Error("stubbed"))),
    Routes: {
      explorer: { readFile: () => "/api/explorer/file" },
      specs: { dispatch: () => "/api/specs/dispatch" },
    },
    withBearerHeaders: () => ({}),
    withAuthHeaders: (h) => h || {},
    api: vi.fn(() =>
      opts.apiResponse
        ? Promise.resolve(opts.apiResponse)
        : Promise.resolve({ dispatched: [], errors: [] }),
    ),
    renderMarkdown: (text) => "<p>" + text + "</p>",
    setInterval: () => 42,
    clearInterval: () => {},
    location: { hash: "", pathname: "/" },
    history: { replaceState: () => {} },
    confirm: vi.fn(() => confirmResult),
    alert: vi.fn(),
    showConfirm: vi.fn(() => Promise.resolve(confirmResult)),
    showAlert: vi.fn(),
    Promise,
    console,
    storage,
    fetchCalls,
    registry,
    dispatchBtn,
  };

  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

describe("dispatchFocusedSpec", () => {
  it("does nothing when no spec is focused", async () => {
    const ctx = makeContext();
    // _focusedSpecPath is null by default.
    ctx.dispatchFocusedSpec();
    expect(ctx.showConfirm).not.toHaveBeenCalled();
    expect(ctx.api).not.toHaveBeenCalled();
  });

  it("does nothing when user cancels confirmation", async () => {
    const ctx = makeContext({ confirmResult: false });
    // Set a focused spec path.
    ctx._focusedSpecPath = "specs/local/test.md";
    ctx.dispatchFocusedSpec();

    await new Promise((r) => setTimeout(r, 10));

    expect(ctx.showConfirm).toHaveBeenCalledWith(
      "Dispatch this spec to the task board?",
    );
    expect(ctx.api).not.toHaveBeenCalled();
  });

  it("calls the dispatch API with the focused spec path", async () => {
    const ctx = makeContext({
      apiResponse: {
        dispatched: [{ spec_path: "specs/local/test.md", task_id: "abc-123" }],
        errors: [],
      },
    });
    ctx._focusedSpecPath = "specs/local/test.md";
    ctx.dispatchFocusedSpec();

    await new Promise((r) => setTimeout(r, 10));

    expect(ctx.api).toHaveBeenCalledWith("/api/specs/dispatch", {
      method: "POST",
      body: JSON.stringify({ paths: ["specs/local/test.md"], run: false }),
    });
  });

  it("disables the button during dispatch", async () => {
    const ctx = makeContext();
    ctx._focusedSpecPath = "specs/local/test.md";
    ctx.dispatchFocusedSpec();

    // Wait for showConfirm + API to resolve.
    await new Promise((r) => setTimeout(r, 10));

    // Button should be re-enabled and hidden after success.
    const btn = ctx.document.getElementById("spec-dispatch-btn");
    expect(btn.disabled).toBe(false);
    expect(btn.classList.contains("hidden")).toBe(true);
  });

  it("shows an alert on API error", async () => {
    const ctx = makeContext();
    ctx.api = vi.fn(() => Promise.reject(new Error("validation failed")));
    ctx._focusedSpecPath = "specs/local/test.md";
    ctx.dispatchFocusedSpec();

    await new Promise((r) => setTimeout(r, 10));

    expect(ctx.showAlert).toHaveBeenCalledWith(
      "Dispatch failed: validation failed",
    );
    // Button should be re-enabled after error.
    const btn = ctx.document.getElementById("spec-dispatch-btn");
    expect(btn.disabled).toBe(false);
    expect(btn.textContent).toBe("Dispatch");
  });
});
