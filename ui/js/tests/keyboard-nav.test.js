import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const uiDir = join(__dirname, "..", "..");

function loadScript(ctx, filename) {
  loadLibDeps(filename, ctx);
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

function makeClassList() {
  const set = new Set();
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

function createElement(ownerDocument, tagName, overrides = {}) {
  const el = {
    ownerDocument,
    tagName: String(tagName || "div").toUpperCase(),
    children: [],
    parentElement: null,
    dataset: {},
    style: {},
    classList: makeClassList(),
    attributes: {},
    _listeners: {},
    innerHTML: "",
    _textContent: "",
    get textContent() {
      return this._textContent;
    },
    set textContent(v) {
      this._textContent = v;
      if (v === "") {
        for (const child of this.children) child.parentElement = null;
        this.children = [];
      }
    },
    tabIndex: undefined,
    onclick: null,
    appendChild(child) {
      if (child.parentElement) {
        const idx = child.parentElement.children.indexOf(child);
        if (idx >= 0) child.parentElement.children.splice(idx, 1);
      }
      child.parentElement = this;
      // DocumentFragment: append fragment's children instead of the fragment itself
      if (child.tagName === "FRAGMENT" && child.children) {
        for (const fc of child.children.slice()) {
          fc.parentElement = this;
          this.children.push(fc);
        }
        child.children = [];
        return child;
      }
      this.children.push(child);
      return child;
    },
    insertBefore(child, ref) {
      child.parentElement = this;
      if (!ref) {
        this.children.push(child);
        return child;
      }
      const idx = this.children.indexOf(ref);
      if (idx === -1) {
        this.children.push(child);
        return child;
      }
      const existingIdx = this.children.indexOf(child);
      if (existingIdx >= 0) this.children.splice(existingIdx, 1);
      this.children.splice(idx, 0, child);
      return child;
    },
    remove() {
      if (!this.parentElement) return;
      const idx = this.parentElement.children.indexOf(this);
      if (idx >= 0) this.parentElement.children.splice(idx, 1);
      this.parentElement = null;
    },
    addEventListener(type, handler) {
      this._listeners[type] = this._listeners[type] || [];
      this._listeners[type].push(handler);
    },
    removeEventListener(type, handler) {
      if (!this._listeners[type]) return;
      this._listeners[type] = this._listeners[type].filter(
        (fn) => fn !== handler,
      );
    },
    dispatchEvent(evt) {
      evt.target = evt.target || this;
      evt.currentTarget = this;
      (this._listeners[evt.type] || []).forEach((fn) => {
        fn.call(this, evt);
      });
      return true;
    },
    setAttribute(name, value) {
      this.attributes[name] = String(value);
      if (name === "id") this.id = String(value);
      if (name === "role") this.role = String(value);
    },
    getAttribute(name) {
      return Object.hasOwn(this.attributes, name)
        ? this.attributes[name]
        : null;
    },
    hasAttribute(name) {
      return Object.hasOwn(this.attributes, name);
    },
    removeAttribute(name) {
      delete this.attributes[name];
    },
    focus() {
      ownerDocument.activeElement = this;
    },
    blur() {
      if (ownerDocument.activeElement === this)
        ownerDocument.activeElement = null;
    },
    closest(selector) {
      let current = this;
      while (current) {
        if (
          selector === '[role="region"]' &&
          current.getAttribute("role") === "region"
        )
          return current;
        current = current.parentElement;
      }
      return null;
    },
    querySelector(selector) {
      return this.querySelectorAll(selector)[0] || null;
    },
    querySelectorAll(selector) {
      const results = [];
      function matches(node) {
        if (selector === '[role="listitem"]')
          return node.getAttribute("role") === "listitem";
        if (selector === '[role="list"]')
          return node.getAttribute("role") === "list";
        return false;
      }
      function visit(node) {
        node.children.forEach((child) => {
          if (matches(child)) results.push(child);
          visit(child);
        });
      }
      visit(this);
      return results;
    },
  };

  Object.defineProperty(el, "previousElementSibling", {
    get() {
      if (!this.parentElement) return null;
      const idx = this.parentElement.children.indexOf(this);
      return idx > 0 ? this.parentElement.children[idx - 1] : null;
    },
  });
  Object.defineProperty(el, "nextElementSibling", {
    get() {
      if (!this.parentElement) return null;
      const idx = this.parentElement.children.indexOf(this);
      return idx >= 0 && idx + 1 < this.parentElement.children.length
        ? this.parentElement.children[idx + 1]
        : null;
    },
  });

  return Object.assign(el, overrides);
}

function makeTask(id, title) {
  return {
    id,
    status: "backlog",
    kind: "",
    prompt: title + " prompt",
    execution_prompt: "",
    title,
    result: "",
    stop_reason: "",
    session_id: null,
    fresh_start: false,
    archived: false,
    is_test_run: false,
    timeout: 15,
    sandbox: "default",
    sandbox_by_activity: {},
    mount_worktrees: false,
    tags: [],
    depends_on: [],
    current_refinement: null,
    worktree_paths: {},
    position: 0,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    last_test_result: "",
    turns: 0,
  };
}

function setupContext() {
  const elements = new Map();
  const document = {
    activeElement: null,
    readyState: "complete",
    addEventListener: () => {},
    documentElement: { setAttribute: () => {} },
    createElement(tag) {
      return createElement(document, tag);
    },
    createDocumentFragment() {
      return createElement(document, "fragment");
    },
    getElementById(id) {
      return elements.get(id) || null;
    },
    querySelectorAll: () => ({ forEach: () => {} }),
  };

  const board = createElement(document, "main");
  board.setAttribute("id", "board");
  elements.set("board", board);

  ["backlog", "in_progress", "waiting", "done"].forEach((status) => {
    const region = createElement(document, "div");
    region.setAttribute("id", "col-wrapper-" + status);
    region.setAttribute("role", "region");
    board.appendChild(region);

    const list = createElement(document, "div");
    list.setAttribute("id", "col-" + status);
    list.dataset.status = status;
    list.setAttribute("role", "list");
    region.appendChild(list);
    elements.set("col-" + status, list);
    elements.set("count-" + status, createElement(document, "span"));
  });

  elements.set("max-parallel-tag", createElement(document, "span"));
  elements.set("done-stats", createElement(document, "span"));
  elements.set("archive-all-btn", createElement(document, "button"));
  elements.set(
    "board-announcer",
    Object.assign(createElement(document, "div"), {
      id: "board-announcer",
      attributes: { role: "status" },
    }),
  );

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    Promise,
    document,
    window: {
      depGraphEnabled: false,
      matchMedia: () => ({ matches: false, addEventListener: () => {} }),
    },
    localStorage: { getItem: () => null, setItem: () => {} },
    requestAnimationFrame: (cb) => cb(),
    IntersectionObserver: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
    clearInterval: () => {},
    setInterval: () => 0,
    tasks: [],
    archivedTasks: [],
    showArchived: false,
    filterQuery: "",
    maxParallelTasks: 0,
    renderMarkdown: (s) => s || "",
    highlightMatch: (s) => s || "",
    sandboxDisplayName: (s) => s || "",
    timeAgo: () => "1m ago",
    formatTimeout: () => "15m",
    getOpenModalTaskId: () => null,
    updateRefineUI: () => {},
    renderRefineHistory: () => {},
    hideDependencyGraph: () => {},
    openModal: vi.fn(() => Promise.resolve()),
    updateTaskStatus: vi.fn(() => Promise.resolve()),
    quickDoneTask: vi.fn(() => Promise.resolve()),
  });

  loadScript(ctx, "utils.js");
  loadScript(ctx, "render.js");

  return { ctx, elements, document };
}

describe("keyboard navigation on kanban cards", () => {
  let ctx;
  let elements;
  let document;

  beforeEach(() => {
    ({ ctx, elements, document } = setupContext());
    ctx.tasks = [
      makeTask("task-1", "First task"),
      makeTask("task-2", "Second task"),
      makeTask("task-3", "Third task"),
    ];
    ctx.render();
  });

  it("renders focusable listitems with aria labels", () => {
    const cards = elements.get("col-backlog").children;
    expect(cards).toHaveLength(3);
    expect(cards[0].tabIndex).toBe(0);
    expect(cards[0].getAttribute("role")).toBe("listitem");
    expect(cards[0].getAttribute("aria-label")).toContain("First task");
  });

  it("moves focus with ArrowDown", () => {
    const cards = elements.get("col-backlog").children;
    cards[0].focus();
    cards[0].dispatchEvent({
      type: "keydown",
      key: "ArrowDown",
      preventDefault: vi.fn(),
    });
    expect(document.activeElement).toBe(cards[1]);
  });

  it("opens the modal on Enter", () => {
    const card = elements.get("col-backlog").children[1];
    card.dispatchEvent({
      type: "keydown",
      key: "Enter",
      preventDefault: vi.fn(),
    });
    expect(ctx.openModal).toHaveBeenCalledWith("task-2");
  });

  it('starts a backlog task on "s"', () => {
    const card = elements.get("col-backlog").children[0];
    card.dispatchEvent({
      type: "keydown",
      key: "s",
      preventDefault: vi.fn(),
    });
    expect(ctx.updateTaskStatus).toHaveBeenCalledWith("task-1", "in_progress");
  });

  it("includes the board announcer live region markup", () => {
    const html = readFileSync(
      join(uiDir, "partials", "initial-layout.html"),
      "utf8",
    );
    expect(html).toContain('id="board-announcer"');
    expect(html).toContain('role="status"');
  });
});
