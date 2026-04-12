/**
 * Unit tests for archive/unarchive/undo behaviour in spec-mode.js.
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-mode.js"), "utf8");

function makeEl(tag, registry) {
  const _classList = new Set();
  let _id = "";
  let _textContent = "";
  let _disabled = false;
  return {
    tagName: tag,
    get id() {
      return _id;
    },
    set id(v) {
      _id = v;
      if (v && registry) registry.set(v, this);
    },
    style: {},
    dataset: {},
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
    onclick: null,
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
  const ids = [
    "sidebar-nav-board",
    "sidebar-nav-spec",
    "board",
    "spec-mode-container",
    "spec-dispatch-btn",
    "spec-summarize-btn",
    "spec-archive-btn",
    "spec-unarchive-btn",
    "spec-archived-banner",
    "spec-archive-toast",
    "spec-archive-toast-text",
  ];
  for (const id of ids) {
    const el = makeEl("DIV", registry);
    el.id = id;
  }

  const fetchMock = vi.fn(() =>
    Promise.resolve({
      ok: opts.fetchOk !== false,
      text: () => Promise.resolve(opts.fetchError || ""),
    }),
  );

  const ctx = {
    document: {
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
    fetch: fetchMock,
    withBearerHeaders: (h) => h || {},
    withAuthHeaders: (h) => h || {},
    showConfirm: vi.fn(() => Promise.resolve(opts.confirmResult !== false)),
    showAlert: vi.fn(),
    confirm: () => opts.confirmResult !== false,
    alert: vi.fn(),
    api: vi.fn(() => Promise.resolve({ dispatched: [], errors: [] })),
    Routes: { specs: {} },
    renderMarkdown: (t) => "<p>" + t + "</p>",
    setInterval: () => 42,
    clearInterval: () => {},
    setTimeout: (fn, _ms) => {
      ctx._toastTimerFn = fn;
      return 1;
    },
    clearTimeout: () => {
      ctx._toastTimerFn = null;
    },
    location: { hash: "", pathname: "/" },
    history: { replaceState: () => {} },
    Promise,
    JSON,
    console,
    storage,
    registry,
    fetchMock,
  };

  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

describe("archiveFocusedSpec", () => {
  it("no-ops when no spec is focused", async () => {
    const ctx = makeContext();
    ctx.archiveFocusedSpec();
    await new Promise((r) => setTimeout(r, 5));
    expect(ctx.fetchMock).not.toHaveBeenCalled();
  });

  it("calls POST /api/specs/archive for a leaf spec without confirmation", async () => {
    const ctx = makeContext();
    ctx._focusedSpecPath = "specs/local/leaf.md";
    ctx._focusedSpecContent = "---\nstatus: drafted\n---\n\n# L";
    // Tree has the spec as a leaf.
    ctx._specTreeData = {
      nodes: [
        {
          path: "specs/local/leaf.md",
          is_leaf: true,
          children: [],
        },
      ],
    };
    ctx.archiveFocusedSpec();
    await new Promise((r) => setTimeout(r, 10));
    expect(ctx.showConfirm).not.toHaveBeenCalled();
    expect(ctx.fetchMock).toHaveBeenCalledWith(
      "/api/specs/archive",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("confirms before archiving a non-leaf with descendants", async () => {
    const ctx = makeContext({ confirmResult: false });
    ctx._focusedSpecPath = "specs/local/parent.md";
    ctx._focusedSpecContent = "---\nstatus: drafted\n---\n\n# P";
    ctx._specTreeData = {
      nodes: [
        {
          path: "specs/local/parent.md",
          is_leaf: false,
          children: ["specs/local/parent/child.md"],
        },
        {
          path: "specs/local/parent/child.md",
          is_leaf: true,
          children: [],
        },
      ],
    };
    ctx.archiveFocusedSpec();
    await new Promise((r) => setTimeout(r, 10));
    expect(ctx.showConfirm).toHaveBeenCalled();
    expect(ctx.fetchMock).not.toHaveBeenCalled();
  });

  it("records last action so undo can reverse it", async () => {
    const ctx = makeContext();
    ctx._focusedSpecPath = "specs/local/leaf.md";
    ctx._focusedSpecContent = "---\nstatus: complete\n---\n\n# L";
    ctx._specTreeData = {
      nodes: [{ path: "specs/local/leaf.md", is_leaf: true, children: [] }],
    };
    ctx.archiveFocusedSpec();
    await new Promise((r) => setTimeout(r, 10));
    expect(ctx._lastArchiveAction).toEqual({
      action: "archive",
      path: "specs/local/leaf.md",
      prevStatus: "complete",
    });

    ctx.undoArchiveAction();
    await new Promise((r) => setTimeout(r, 10));
    expect(ctx.fetchMock).toHaveBeenCalledWith(
      "/api/specs/unarchive",
      expect.objectContaining({ method: "POST" }),
    );
    expect(ctx._lastArchiveAction).toBeNull();
  });

  it("shows alert on error response from archive endpoint", async () => {
    const ctx = makeContext({
      fetchOk: false,
      fetchError: "conflict: task live",
    });
    ctx._focusedSpecPath = "specs/local/leaf.md";
    ctx._focusedSpecContent = "---\nstatus: drafted\n---\n\n# L";
    ctx._specTreeData = {
      nodes: [{ path: "specs/local/leaf.md", is_leaf: true, children: [] }],
    };
    ctx.archiveFocusedSpec();
    await new Promise((r) => setTimeout(r, 10));
    expect(ctx.showAlert).toHaveBeenCalledWith("conflict: task live");
  });
});

describe("unarchiveFocusedSpec", () => {
  it("calls POST /api/specs/unarchive and records last action", async () => {
    const ctx = makeContext();
    ctx._focusedSpecPath = "specs/local/arch.md";
    ctx._focusedSpecContent = "---\nstatus: archived\n---\n\n# A";
    ctx.unarchiveFocusedSpec();
    await new Promise((r) => setTimeout(r, 10));
    expect(ctx.fetchMock).toHaveBeenCalledWith(
      "/api/specs/unarchive",
      expect.objectContaining({ method: "POST" }),
    );
    expect(ctx._lastArchiveAction).toEqual({
      action: "unarchive",
      path: "specs/local/arch.md",
      prevStatus: "archived",
    });
  });
});

describe("dismissArchiveToast", () => {
  it("clears last action and hides toast", () => {
    const ctx = makeContext();
    ctx._lastArchiveAction = {
      action: "archive",
      path: "x",
      prevStatus: "drafted",
    };
    ctx.dismissArchiveToast();
    expect(ctx._lastArchiveAction).toBeNull();
    const toast = ctx.registry.get("spec-archive-toast");
    expect(toast.classList.contains("hidden")).toBe(true);
  });
});
