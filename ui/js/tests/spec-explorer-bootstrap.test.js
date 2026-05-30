/**
 * Regression tests for the first-spec bootstrap choreography gating in
 * spec-explorer.js.
 *
 * Bug: the "Your first spec was created at …" toast popped up on every page
 * refresh. The spec-tree SSE snapshot handler treated `_specTreeData == null`
 * (the state right after a fresh page load) as "the workspace was empty", so
 * the first non-empty snapshot of any session looked like an empty→non-empty
 * transition and fired the choreography. The fix gates the transition check on
 * `_seenSpecSnapshot`, so the first snapshot of a stream is only a baseline.
 */
import { describe, it, expect, beforeEach, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const code = readFileSync(join(__dirname, "..", "spec-explorer.js"), "utf8");

// Builds a vm context with just enough stubs to drive _startSpecTreeStream's
// snapshot handler and observe whether BootstrapChoreography.trigger is called.
function makeContext() {
  const triggerSpy = vi.fn();
  let snapshotListener = null;

  const ctx = {
    document: { getElementById: () => null, createElement: () => ({}) },
    localStorage: { getItem: () => null, setItem: () => {} },
    Routes: { specs: { stream: () => "/api/specs/stream" } },
    createSSEStream: (cfg) => {
      snapshotListener = cfg.listeners.snapshot;
      return { close: () => {} };
    },
    BootstrapChoreography: { trigger: triggerSpy },
    activeWorkspaces: ["/workspace/repo"],
    JSON,
    Array,
    Set,
    Promise,
    console,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);

  // Isolate the trigger decision: stub the DOM-touching helpers the handler
  // calls before the choreography check so they can't throw and short-circuit
  // the try block.
  ctx.renderSpecTree = () => {};
  ctx._syncSpecModeState = () => {};
  ctx._updateSpecPaneVisibility = () => {};

  ctx._startSpecTreeStream();
  return {
    ctx,
    triggerSpy,
    emit: (data) => snapshotListener({ data: JSON.stringify(data) }),
  };
}

const WITH_SPECS = {
  nodes: [{ path: "specs/local/foo.md", is_leaf: true }],
};
const EMPTY = { nodes: [] };

describe("spec-explorer first-spec choreography gating", () => {
  let h;
  beforeEach(() => {
    h = makeContext();
  });

  it("does NOT fire on the first snapshot when the workspace already has specs (refresh)", () => {
    h.emit(WITH_SPECS);
    expect(h.triggerSpy).not.toHaveBeenCalled();
  });

  it("does NOT fire on repeated non-empty snapshots (reconnect/refresh storms)", () => {
    h.emit(WITH_SPECS);
    h.emit(WITH_SPECS);
    h.emit(WITH_SPECS);
    expect(h.triggerSpy).not.toHaveBeenCalled();
  });

  it("DOES fire on a genuine empty→non-empty transition during the session", () => {
    h.emit(EMPTY); // baseline: workspace is empty (chat-first)
    expect(h.triggerSpy).not.toHaveBeenCalled();
    h.emit(WITH_SPECS); // user creates their first spec
    expect(h.triggerSpy).toHaveBeenCalledTimes(1);
    expect(h.triggerSpy).toHaveBeenCalledWith("specs/local/foo.md", "/workspace/repo");
  });

  it("does not re-fire once the transition has happened", () => {
    h.emit(EMPTY);
    h.emit(WITH_SPECS);
    h.emit(WITH_SPECS);
    expect(h.triggerSpy).toHaveBeenCalledTimes(1);
  });
});
