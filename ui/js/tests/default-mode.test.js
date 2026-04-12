/**
 * Unit tests for the default-mode-resolution spec.
 *
 * Covers:
 *  - resolveDefaultMode pure priority algorithm (saved / task count /
 *    workspaceIsNew fallbacks).
 *  - switchMode persistence gating via the `{persist: true}` opt — explicit
 *    user actions write localStorage, programmatic switches do not.
 *  - The user-facing "plan" label is persisted even though the internal
 *    mode value is "spec".
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-mode.js"), "utf8");

function makeEl() {
  const classes = new Set();
  const style = {};
  const el = {
    tagName: "DIV",
    style,
    textContent: "",
    innerHTML: "",
    classList: {
      add: (c) => classes.add(c),
      remove: (c) => classes.delete(c),
      toggle: (c, force) => (force ? classes.add(c) : classes.delete(c)),
      contains: (c) => classes.has(c),
    },
  };
  return el;
}

function makeContext(opts = {}) {
  const storage = new Map();
  if (opts.savedMode !== undefined && opts.savedMode !== null) {
    storage.set("wallfacer-mode", opts.savedMode);
  }

  const registry = new Map();
  // Pre-create DOM nodes consulted during mode switching / _applyMode.
  const ids = [
    "sidebar-nav-board",
    "sidebar-nav-spec",
    "sidebar-nav-docs",
    "board",
    "spec-mode-container",
    "docs-mode-container",
    "explorer-panel",
    "workspace-git-bar",
    "task-search",
  ];
  for (const id of ids) registry.set(id, makeEl());

  const ctx = {
    console,
    document: {
      getElementById: (id) => registry.get(id) || null,
      addEventListener: () => {},
      querySelector: () => null,
    },
    localStorage: {
      getItem: (k) => (storage.has(k) ? storage.get(k) : null),
      setItem: (k, v) => storage.set(k, v),
      removeItem: (k) => storage.delete(k),
    },
    location: { hash: "", pathname: "/" },
    history: { replaceState: () => {} },
    setInterval: () => 0,
    clearInterval: () => {},
    fetch: () => Promise.reject(new Error("stubbed")),
    Routes: { explorer: { readFile: () => "/api/explorer/file" } },
    withBearerHeaders: () => ({}),
    renderMarkdown: (s) => s || "",
    Promise,
    showConfirm: () => Promise.resolve(true),
    showAlert: () => {},
    storage,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

// ---------------------------------------------------------------------------
// resolveDefaultMode — pure priority algorithm
// ---------------------------------------------------------------------------

describe("resolveDefaultMode", () => {
  let ctx;
  function resolve(args) {
    if (!ctx) ctx = makeContext();
    return ctx.resolveDefaultMode(args);
  }

  it("TestResolve_SavedPreferenceWins — saved board sticks even with 0 tasks", () => {
    expect(
      resolve({ savedMode: "board", taskCount: 0, workspaceIsNew: false }),
    ).toBe("board");
  });

  it("TestResolve_SavedPreferenceWins — saved plan sticks even with many tasks", () => {
    expect(
      resolve({ savedMode: "plan", taskCount: 5, workspaceIsNew: false }),
    ).toBe("plan");
  });

  it("TestResolve_NoSavedFallsBackToTaskCount — any task present → board", () => {
    expect(resolve({ savedMode: null, taskCount: 5 })).toBe("board");
  });

  it("TestResolve_EmptyBoardNoSaved — no tasks and no preference → plan", () => {
    expect(resolve({ savedMode: null, taskCount: 0 })).toBe("plan");
  });

  it("TestResolve_NewWorkspaceOverridesSaved — workspaceIsNew forces plan", () => {
    expect(
      resolve({ savedMode: "board", taskCount: 5, workspaceIsNew: true }),
    ).toBe("plan");
  });

  it("TestResolve_InvalidSavedValueIgnored — 'garbage' falls through to task count", () => {
    expect(resolve({ savedMode: "garbage", taskCount: 3 })).toBe("board");
    expect(resolve({ savedMode: "garbage", taskCount: 0 })).toBe("plan");
  });
});

// ---------------------------------------------------------------------------
// Saved preference write gating — only explicit user actions persist
// ---------------------------------------------------------------------------

describe("wallfacer-mode persistence gating", () => {
  it("TestSavedMode_WrittenOnExplicitClick — switchMode with persist=true writes 'plan'", () => {
    const ctx = makeContext();
    ctx.switchMode("spec", { persist: true });
    expect(ctx.storage.get("wallfacer-mode")).toBe("plan");
  });

  it("writes 'board' when switching to board with persist=true", () => {
    const ctx = makeContext({ savedMode: "plan" });
    ctx.switchMode("board", { persist: true });
    expect(ctx.storage.get("wallfacer-mode")).toBe("board");
  });

  it("TestSavedMode_NotWrittenOnToastFollowThrough — programmatic switchMode does not persist", () => {
    // Simulate the dispatch toast's "View on Board" click, which is a
    // programmatic navigation. Saved preference must remain unchanged.
    const ctx = makeContext({ savedMode: "plan" });
    ctx.switchMode("board"); // no opts → programmatic
    expect(ctx.storage.get("wallfacer-mode")).toBe("plan");
  });

  it("does not persist for docs-mode switches even when persist is true", () => {
    // Docs is out of scope of the Plan/Board binary; the saved preference
    // is reserved for those two values only.
    const ctx = makeContext({ savedMode: "plan" });
    ctx.switchMode("docs", { persist: true });
    expect(ctx.storage.get("wallfacer-mode")).toBe("plan");
  });
});

// ---------------------------------------------------------------------------
// resolveInitialMode — one-shot boot-time resolution
// ---------------------------------------------------------------------------

describe("resolveInitialMode", () => {
  it("auto-switches to spec (plan) when no preference and no tasks exist", () => {
    const ctx = makeContext();
    expect(ctx.getCurrentMode()).toBe("board"); // provisional at boot
    ctx.resolveInitialMode(0);
    expect(ctx.getCurrentMode()).toBe("spec");
    // Auto-resolution must NOT persist — the user never picked it.
    expect(ctx.storage.has("wallfacer-mode")).toBe(false);
  });

  it("leaves board when tasks exist and no preference is saved", () => {
    const ctx = makeContext();
    ctx.resolveInitialMode(7);
    expect(ctx.getCurrentMode()).toBe("board");
  });

  it("honours saved plan preference over task count", () => {
    const ctx = makeContext({ savedMode: "plan" });
    ctx.resolveInitialMode(100);
    expect(ctx.getCurrentMode()).toBe("spec");
  });

  it("is a one-shot — a second call does not flip mode after user switch", () => {
    const ctx = makeContext();
    ctx.resolveInitialMode(0); // → spec
    ctx.switchMode("board", { persist: true });
    ctx.resolveInitialMode(0); // should be a no-op
    expect(ctx.getCurrentMode()).toBe("board");
  });

  it("markWorkspaceIsNew makes the next resolve force plan even with tasks", () => {
    const ctx = makeContext({ savedMode: "board" });
    ctx.markWorkspaceIsNew();
    ctx.resolveInitialMode(42);
    expect(ctx.getCurrentMode()).toBe("spec");
  });

  it("clearWorkspaceIsNew restores saved/task-count priority", () => {
    const ctx = makeContext({ savedMode: "board" });
    ctx.markWorkspaceIsNew();
    ctx.clearWorkspaceIsNew();
    ctx.resolveInitialMode(42);
    // With saved "board", we stay on board.
    expect(ctx.getCurrentMode()).toBe("board");
  });
});
