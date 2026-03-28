/**
 * Tests for global keyboard shortcuts: "n" (new task) and "?" (shortcuts help).
 */
import { describe, it, expect, vi, beforeEach } from "vitest";

function makeClassList(...initial) {
  const set = new Set(initial);
  return {
    add(cls) {
      set.add(cls);
    },
    remove(cls) {
      set.delete(cls);
    },
    contains(cls) {
      return set.has(cls);
    },
    toggle(cls, force) {
      if (force === undefined) {
        if (set.has(cls)) set.delete(cls);
        else set.add(cls);
        return;
      }
      if (force) set.add(cls);
      else set.delete(cls);
    },
  };
}

function makeElement(id, tag, classes) {
  return {
    id,
    tagName: (tag || "div").toUpperCase(),
    classList: makeClassList(...(classes || [])),
    style: {},
    value: "",
    dataset: {},
    _listeners: {},
    addEventListener(type, handler) {
      this._listeners[type] = this._listeners[type] || [];
      this._listeners[type].push(handler);
    },
    getAttribute() {
      return null;
    },
    focus: vi.fn(),
    scrollHeight: 0,
  };
}

function setup() {
  const showNewTaskForm = vi.fn();
  const openKeyboardShortcuts = vi.fn();

  const elements = {
    modal: makeElement("modal", "div", ["hidden"]),
    "alert-modal": makeElement("alert-modal", "div", ["hidden"]),
    "stats-modal": makeElement("stats-modal", "div", ["hidden"]),
    "usage-stats-modal": makeElement("usage-stats-modal", "div", ["hidden"]),
    "container-monitor-modal": makeElement("container-monitor-modal", "div", [
      "hidden",
    ]),
    "instructions-modal": makeElement("instructions-modal", "div", ["hidden"]),
    "settings-modal": makeElement("settings-modal", "div", ["hidden"]),
    "keyboard-shortcuts-modal": makeElement("keyboard-shortcuts-modal", "div", [
      "hidden",
    ]),
    "new-task-btn": makeElement("new-task-btn"),
    "new-task-form": makeElement("new-task-form", "div", ["hidden"]),
    "new-prompt": makeElement("new-prompt", "textarea"),
  };

  const doc = {
    activeElement: null,
    getElementById(id) {
      return elements[id] || null;
    },
  };

  // Mirror the combined handler from events.js
  const handler = (e) => {
    if (e.ctrlKey || e.metaKey || e.altKey) return;
    if (e.key !== "n" && e.key !== "?") return;
    var tag = doc.activeElement && doc.activeElement.tagName;
    if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
    var ce =
      doc.activeElement && doc.activeElement.getAttribute("contenteditable");
    if (ce !== null && ce !== "false") return;
    var modals = [
      "modal",
      "alert-modal",
      "stats-modal",
      "usage-stats-modal",
      "container-monitor-modal",
      "instructions-modal",
      "settings-modal",
      "keyboard-shortcuts-modal",
    ];
    for (var i = 0; i < modals.length; i++) {
      var m = doc.getElementById(modals[i]);
      if (m && !m.classList.contains("hidden")) return;
    }
    e.preventDefault();
    if (e.key === "n") showNewTaskForm();
    if (e.key === "?") openKeyboardShortcuts();
  };

  function fireKey(overrides) {
    const e = {
      type: "keydown",
      key: "n",
      ctrlKey: false,
      metaKey: false,
      altKey: false,
      preventDefault: vi.fn(),
      ...overrides,
    };
    handler(e);
    return e;
  }

  return { elements, doc, showNewTaskForm, openKeyboardShortcuts, fireKey };
}

describe("new task shortcut (n)", () => {
  let env;
  beforeEach(() => {
    env = setup();
  });

  it("opens new task form on bare 'n' press", () => {
    env.fireKey();
    expect(env.showNewTaskForm).toHaveBeenCalledOnce();
    expect(env.openKeyboardShortcuts).not.toHaveBeenCalled();
  });

  it("does not trigger when typing in a textarea", () => {
    env.doc.activeElement = { tagName: "TEXTAREA", getAttribute: () => null };
    env.fireKey();
    expect(env.showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger when typing in an input", () => {
    env.doc.activeElement = { tagName: "INPUT", getAttribute: () => null };
    env.fireKey();
    expect(env.showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger when a modal is open", () => {
    env.elements["modal"].classList.remove("hidden");
    env.fireKey();
    expect(env.showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger with Ctrl+n", () => {
    env.fireKey({ ctrlKey: true });
    expect(env.showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger with Cmd+n", () => {
    env.fireKey({ metaKey: true });
    expect(env.showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger with Alt+n", () => {
    env.fireKey({ altKey: true });
    expect(env.showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger when settings modal is open", () => {
    env.elements["settings-modal"].classList.remove("hidden");
    env.fireKey();
    expect(env.showNewTaskForm).not.toHaveBeenCalled();
  });
});

describe("keyboard shortcuts help (?)", () => {
  let env;
  beforeEach(() => {
    env = setup();
  });

  it("opens shortcuts help on '?' press", () => {
    env.fireKey({ key: "?" });
    expect(env.openKeyboardShortcuts).toHaveBeenCalledOnce();
    expect(env.showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger when typing in an input", () => {
    env.doc.activeElement = { tagName: "INPUT", getAttribute: () => null };
    env.fireKey({ key: "?" });
    expect(env.openKeyboardShortcuts).not.toHaveBeenCalled();
  });

  it("does not trigger when a modal is open", () => {
    env.elements["modal"].classList.remove("hidden");
    env.fireKey({ key: "?" });
    expect(env.openKeyboardShortcuts).not.toHaveBeenCalled();
  });

  it("does not trigger when shortcuts modal is already open", () => {
    env.elements["keyboard-shortcuts-modal"].classList.remove("hidden");
    env.fireKey({ key: "?" });
    expect(env.openKeyboardShortcuts).not.toHaveBeenCalled();
  });

  it("does not trigger with modifier keys", () => {
    env.fireKey({ key: "?", ctrlKey: true });
    env.fireKey({ key: "?", metaKey: true });
    env.fireKey({ key: "?", altKey: true });
    expect(env.openKeyboardShortcuts).not.toHaveBeenCalled();
  });
});
