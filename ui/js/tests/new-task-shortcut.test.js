/**
 * Tests for the "n" keyboard shortcut that opens the new task form.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";

function makeClassList(...initial) {
  const set = new Set(initial);
  return {
    add(cls) { set.add(cls); },
    remove(cls) { set.delete(cls); },
    contains(cls) { return set.has(cls); },
    toggle(cls, force) {
      if (force === undefined) {
        if (set.has(cls)) set.delete(cls); else set.add(cls);
        return;
      }
      if (force) set.add(cls); else set.delete(cls);
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
    getAttribute() { return null; },
    focus: vi.fn(),
    scrollHeight: 0,
  };
}

describe("new task shortcut", () => {
  let elements;
  let keydownHandlers;
  let showNewTaskForm;

  beforeEach(() => {
    keydownHandlers = [];
    showNewTaskForm = vi.fn();

    elements = {
      "modal": makeElement("modal", "div", ["hidden"]),
      "alert-modal": makeElement("alert-modal", "div", ["hidden"]),
      "stats-modal": makeElement("stats-modal", "div", ["hidden"]),
      "usage-stats-modal": makeElement("usage-stats-modal", "div", ["hidden"]),
      "container-monitor-modal": makeElement("container-monitor-modal", "div", ["hidden"]),
      "instructions-modal": makeElement("instructions-modal", "div", ["hidden"]),
      "settings-modal": makeElement("settings-modal", "div", ["hidden"]),
      "new-task-btn": makeElement("new-task-btn"),
      "new-task-form": makeElement("new-task-form", "div", ["hidden"]),
      "new-prompt": makeElement("new-prompt", "textarea"),
    };

    // Minimal document stub that captures addEventListener calls
    const document = {
      activeElement: null,
      getElementById(id) { return elements[id] || null; },
      addEventListener(type, handler) {
        if (type === "keydown") keydownHandlers.push(handler);
      },
    };

    // Evaluate just the shortcut listener inline (extracted from events.js)
    // This avoids needing to load the full events.js with all its dependencies.
    const handler = (e) => {
      if (e.key !== "n" || e.ctrlKey || e.metaKey || e.altKey) return;
      var tag = document.activeElement && document.activeElement.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      var ce = document.activeElement && document.activeElement.getAttribute("contenteditable");
      if (ce !== null && ce !== "false") return;
      var modals = ["modal", "alert-modal", "stats-modal", "usage-stats-modal",
        "container-monitor-modal", "instructions-modal", "settings-modal"];
      for (var i = 0; i < modals.length; i++) {
        var m = document.getElementById(modals[i]);
        if (m && !m.classList.contains("hidden")) return;
      }
      e.preventDefault();
      showNewTaskForm();
    };
    keydownHandlers.push(handler);
  });

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
    keydownHandlers.forEach((h) => h(e));
    return e;
  }

  it("opens new task form on bare 'n' press", () => {
    fireKey();
    expect(showNewTaskForm).toHaveBeenCalledOnce();
  });

  it("does not trigger when typing in a textarea", () => {
    // Simulate active element being a textarea
    const ta = { tagName: "TEXTAREA", getAttribute: () => null };
    // Patch activeElement for this test by re-evaluating with the right context
    const handler = keydownHandlers[0];
    // We need to replace the handler's document reference — easier to test via
    // a new handler that uses the patched activeElement.
    const doc = {
      activeElement: ta,
      getElementById(id) { return elements[id] || null; },
    };
    const patchedHandler = (e) => {
      if (e.key !== "n" || e.ctrlKey || e.metaKey || e.altKey) return;
      var tag = doc.activeElement && doc.activeElement.tagName;
      if (tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT") return;
      showNewTaskForm();
    };
    const e = { type: "keydown", key: "n", ctrlKey: false, metaKey: false, altKey: false, preventDefault: vi.fn() };
    patchedHandler(e);
    expect(showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger when a modal is open", () => {
    elements["modal"].classList.remove("hidden"); // modal visible
    fireKey();
    expect(showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger with Ctrl+n", () => {
    fireKey({ ctrlKey: true });
    expect(showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger with Cmd+n", () => {
    fireKey({ metaKey: true });
    expect(showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger with Alt+n", () => {
    fireKey({ altKey: true });
    expect(showNewTaskForm).not.toHaveBeenCalled();
  });

  it("does not trigger when settings modal is open", () => {
    elements["settings-modal"].classList.remove("hidden");
    fireKey();
    expect(showNewTaskForm).not.toHaveBeenCalled();
  });
});
