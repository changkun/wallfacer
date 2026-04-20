import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext(overrides = {}) {
  const listeners = {};
  const elementListeners = {};
  const elements = new Map(overrides.elements || []);

  function makeEl(id) {
    const el = {
      id,
      classList: {
        _set: new Set(["hidden"]),
        contains: function (c) {
          return this._set.has(c);
        },
        add: function (c) {
          this._set.add(c);
        },
        remove: function (c) {
          this._set.delete(c);
        },
      },
      style: { height: "" },
      scrollHeight: 100,
      value: "",
      tagName: "DIV",
      addEventListener: vi.fn((type, fn) => {
        if (!elementListeners[id]) elementListeners[id] = {};
        if (!elementListeners[id][type]) elementListeners[id][type] = [];
        elementListeners[id][type].push(fn);
      }),
      getAttribute: () => null,
    };
    return el;
  }

  // Pre-create expected elements
  const modal = elements.get("modal") || makeEl("modal");
  const alertModal = elements.get("alert-modal") || makeEl("alert-modal");
  const newPrompt = elements.get("new-prompt") || makeEl("new-prompt");
  elements.set("modal", modal);
  elements.set("alert-modal", alertModal);
  elements.set("new-prompt", newPrompt);

  const ctx = {
    console: { error: vi.fn(), log: vi.fn(), warn: vi.fn() },
    setTimeout: vi.fn(),
    localStorage: { setItem: vi.fn(), getItem: vi.fn() },
    document: {
      getElementById: (id) => elements.get(id) || null,
      addEventListener: vi.fn((type, fn) => {
        if (!listeners[type]) listeners[type] = [];
        listeners[type].push(fn);
      }),
      activeElement: { tagName: "BODY", getAttribute: () => null },
    },
    // Functions called during init and by event handlers
    closeModal: vi.fn(),
    closeAlert: vi.fn(),
    closeStatsModal: vi.fn(),
    closeUsageStats: vi.fn(),
    closeContainerMonitor: vi.fn(),
    closeInstructionsEditor: vi.fn(),
    closeSettings: vi.fn(),
    closeKeyboardShortcuts: vi.fn(),
    closeExplorerPreview: vi.fn(),
    closeFirstVisibleModal: vi.fn().mockReturnValue(true),
    showNewTaskForm: vi.fn(),
    openKeyboardShortcuts: vi.fn(),
    toggleExplorer: vi.fn(),
    switchMode: vi.fn(),
    getCurrentMode: vi.fn().mockReturnValue("board"),
    toggleSpecChat: vi.fn(),
    dispatchFocusedSpec: vi.fn(),
    breakDownFocusedSpec: vi.fn(),
    createTask: vi.fn(),
    hideNewTaskForm: vi.fn(),
    initSortable: vi.fn(),
    initTrashBin: vi.fn(),
    loadMaxParallel: vi.fn(),
    loadOversightInterval: vi.fn(),
    loadAutoPush: vi.fn(),
    fetchConfig: vi.fn(),
    _listeners: listeners,
    _elementListeners: elementListeners,
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "events.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "events.js") });
  return ctx;
}

function getDocListener(ctx, type, index = 0) {
  const calls = ctx.document.addEventListener.mock.calls.filter(
    (c) => c[0] === type,
  );
  return calls[index] ? calls[index][1] : null;
}

function getElListener(ctx, elId, type, index = 0) {
  const arr = (ctx._elementListeners[elId] || {})[type] || [];
  return arr[index] || null;
}

describe("events.js", () => {
  describe("initialization", () => {
    it("calls init functions on load", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx.initSortable).toHaveBeenCalled();
      expect(ctx.initTrashBin).toHaveBeenCalled();
      expect(ctx.loadMaxParallel).toHaveBeenCalled();
      expect(ctx.loadOversightInterval).toHaveBeenCalled();
      expect(ctx.loadAutoPush).toHaveBeenCalled();
      expect(ctx.fetchConfig).toHaveBeenCalled();
    });

    it("handles initSortable error gracefully", () => {
      const ctx = makeContext({
        initSortable: vi.fn(() => {
          throw new Error("sortable fail");
        }),
      });
      loadScript(ctx); // should not throw
      expect(ctx.console.error).toHaveBeenCalledWith(
        "sortable init:",
        expect.any(Error),
      );
    });

    it("handles initTrashBin error gracefully", () => {
      const ctx = makeContext({
        initTrashBin: vi.fn(() => {
          throw new Error("trash fail");
        }),
      });
      loadScript(ctx);
      expect(ctx.console.error).toHaveBeenCalledWith(
        "trash bin init:",
        expect.any(Error),
      );
    });
  });

  describe("modal close on backdrop click", () => {
    it("closes modal when clicking the modal backdrop", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const handler = getElListener(ctx, "modal", "click");
      expect(handler).toBeTruthy();
      const modalEl = ctx.document.getElementById("modal");
      handler({ target: modalEl });
      expect(ctx.closeModal).toHaveBeenCalled();
    });

    it("does not close modal when clicking inside", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const handler = getElListener(ctx, "modal", "click");
      handler({ target: { id: "some-child" } });
      expect(ctx.closeModal).not.toHaveBeenCalled();
    });
  });

  describe("alert modal close on backdrop click", () => {
    it("closes alert when clicking the alert backdrop", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const handler = getElListener(ctx, "alert-modal", "click");
      expect(handler).toBeTruthy();
      const alertEl = ctx.document.getElementById("alert-modal");
      handler({ target: alertEl });
      expect(ctx.closeAlert).toHaveBeenCalled();
    });
  });

  describe("escape key handling", () => {
    it("calls closeFirstVisibleModal on Escape", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const keydownHandler = getDocListener(ctx, "keydown", 0);
      expect(keydownHandler).toBeTruthy();
      keydownHandler({ key: "Escape" });
      expect(ctx.closeFirstVisibleModal).toHaveBeenCalled();
    });
  });

  describe("global keyboard shortcuts", () => {
    function makeShortcutEvent(key, extra = {}) {
      return {
        key,
        ctrlKey: false,
        metaKey: false,
        altKey: false,
        preventDefault: vi.fn(),
        ...extra,
      };
    }

    it("opens new task form on 'n'", () => {
      const ctx = makeContext();
      loadScript(ctx);
      // The second keydown listener is the shortcut handler
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("n"));
      expect(ctx.showNewTaskForm).toHaveBeenCalled();
    });

    it("opens keyboard shortcuts on '?'", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("?"));
      expect(ctx.openKeyboardShortcuts).toHaveBeenCalled();
    });

    it("toggles explorer on 'e'", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("e"));
      expect(ctx.toggleExplorer).toHaveBeenCalled();
    });

    it("switches to plan (spec) mode on 'p' from board", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("p"));
      expect(ctx.switchMode).toHaveBeenCalledWith("spec", { persist: true });
    });

    it("reverses to board on 'p' from plan (spec) mode", () => {
      const ctx = makeContext({
        getCurrentMode: vi.fn().mockReturnValue("spec"),
      });
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("p"));
      expect(ctx.switchMode).toHaveBeenCalledWith("board", { persist: true });
    });

    it("does not switch mode on 's' (binding removed)", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("s"));
      expect(ctx.switchMode).not.toHaveBeenCalled();
    });

    it("ignores shortcuts with modifier keys", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("n", { ctrlKey: true }));
      expect(ctx.showNewTaskForm).not.toHaveBeenCalled();
    });

    it("ignores shortcuts when focused on input", () => {
      const ctx = makeContext();
      ctx.document.activeElement = {
        tagName: "INPUT",
        getAttribute: () => null,
      };
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("n"));
      expect(ctx.showNewTaskForm).not.toHaveBeenCalled();
    });

    it("ignores shortcuts when focused on textarea", () => {
      const ctx = makeContext();
      ctx.document.activeElement = {
        tagName: "TEXTAREA",
        getAttribute: () => null,
      };
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("n"));
      expect(ctx.showNewTaskForm).not.toHaveBeenCalled();
    });

    it("ignores shortcuts when a modal is visible", () => {
      const ctx = makeContext();
      const modal = ctx.document.getElementById("modal");
      modal.classList._set.delete("hidden"); // make modal visible
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("n"));
      expect(ctx.showNewTaskForm).not.toHaveBeenCalled();
    });

    it("toggles spec chat on 'c' in spec mode", () => {
      const ctx = makeContext({
        getCurrentMode: vi.fn().mockReturnValue("spec"),
        getLayoutState: vi.fn().mockReturnValue("three-pane"),
      });
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("c"));
      expect(ctx.toggleSpecChat).toHaveBeenCalled();
    });

    it("TestLayout_CIsNoOpInChatFirst — 'c' is a no-op in chat-first layout", () => {
      const ctx = makeContext({
        getCurrentMode: vi.fn().mockReturnValue("spec"),
        getLayoutState: vi.fn().mockReturnValue("chat-first"),
      });
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("c"));
      expect(ctx.toggleSpecChat).not.toHaveBeenCalled();
    });

    it("dispatches focused spec on 'd' in spec mode", () => {
      const ctx = makeContext({
        getCurrentMode: vi.fn().mockReturnValue("spec"),
      });
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("d"));
      expect(ctx.dispatchFocusedSpec).toHaveBeenCalled();
    });

    it("breaks down focused spec on 'b' in spec mode", () => {
      const ctx = makeContext({
        getCurrentMode: vi.fn().mockReturnValue("spec"),
      });
      loadScript(ctx);
      const handler = getDocListener(ctx, "keydown", 1);
      handler(makeShortcutEvent("b"));
      expect(ctx.breakDownFocusedSpec).toHaveBeenCalled();
    });
  });

  describe("new-prompt textarea", () => {
    // Ctrl/Cmd+Enter submit and Escape cancel handlers used to live in
    // events.js but duplicated the composer-scoped handler in tasks.js,
    // causing cmd+Enter to create two tasks per press. They were removed
    // (see ui/js/tests/composer-submit-shortcut.test.js for the regression
    // guard). The behavior is exercised by tasks.test.js + tasks-coverage.

    it("auto-grows and saves draft on input", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const handler = getElListener(ctx, "new-prompt", "input");
      const target = {
        style: { height: "" },
        scrollHeight: 200,
        value: "draft text",
      };
      handler({ target });
      expect(target.style.height).toBe("200px");
      expect(ctx.localStorage.setItem).toHaveBeenCalledWith(
        "wallfacer-new-task-draft",
        "draft text",
      );
    });
  });
});
