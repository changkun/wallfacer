import { describe, it, expect, } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext() {
  const windowObj = {};
  const elements = {};
  const eventListeners = {};

  function makeElement(id, tag) {
    const el = {
      id,
      tagName: tag || "div",
      style: {},
      children: [],
      classList: {
        _set: new Set(),
        add(c) {
          this._set.add(c);
        },
        remove(c) {
          this._set.delete(c);
        },
        contains(c) {
          return this._set.has(c);
        },
      },
      textContent: "",
      className: "",
      clientWidth: 800,
      clientHeight: 600,
      parentElement: null,
      setAttribute() {},
      appendChild(child) {
        this.children.push(child);
        child.parentElement = this;
      },
      addEventListener(type, fn) {
        if (!eventListeners[id]) eventListeners[id] = {};
        if (!eventListeners[id][type]) eventListeners[id][type] = [];
        eventListeners[id][type].push(fn);
      },
      getContext() {
        return {
          imageSmoothingEnabled: true,
          clearRect() {},
          save() {},
          restore() {},
          translate() {},
          scale() {},
          drawImage() {},
          fillRect() {},
          fillText() {},
          beginPath() {},
          arc() {},
          fill() {},
          fillStyle: "",
          font: "",
          textAlign: "",
          textBaseline: "",
        };
      },
    };
    elements[id] = el;
    return el;
  }

  makeElement("office-container");
  makeElement("board", "main");
  const toggleBtn = makeElement("office-toggle", "button");
  toggleBtn.classList.add("hidden");

  const document = {
    getElementById(id) {
      return elements[id] || null;
    },
    createElement(tag) {
      const el = makeElement("canvas-" + Math.random(), tag);
      return el;
    },
    addEventListener(type, fn) {
      if (!eventListeners["document"]) eventListeners["document"] = {};
      if (!eventListeners["document"][type])
        eventListeners["document"][type] = [];
      eventListeners["document"][type].push(fn);
    },
  };

  windowObj.addEventListener = function (type, fn) {
    if (!eventListeners["window"]) eventListeners["window"] = {};
    if (!eventListeners["window"][type]) eventListeners["window"][type] = [];
    eventListeners["window"][type].push(fn);
  };

  class MockOffscreenCanvas {
    constructor(w, h) {
      this.width = w;
      this.height = h;
    }
    getContext() {
      return {
        imageSmoothingEnabled: true,
        fillStyle: "",
        fillRect() {},
        drawImage() {},
      };
    }
  }

  // Globals that state.js and office.js both need
  let _taskChangeListeners = [];

  const ctx = vm.createContext({
    window: windowObj,
    document,
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
    OffscreenCanvas: MockOffscreenCanvas,
    Image: class {
      constructor() {
        this.onload = null;
        this.onerror = null;
      }
      set src(_v) {}
    },
    Set,
    JSON,
    Promise,
    performance: { now: () => Date.now() },
    location: { search: "?office=dev" },
    setTimeout: globalThis.setTimeout,
    clearTimeout: globalThis.clearTimeout,
    requestAnimationFrame() {
      return 1;
    },
    cancelAnimationFrame() {},
    Math,
    Object,
    console,
    // Simulate the global registerTaskChangeListener from state.js
    registerTaskChangeListener(fn) {
      _taskChangeListeners.push(fn);
    },
  });

  // Load office modules
  const files = [
    "office/tileMap.js",
    "office/spriteCache.js",
    "office/camera.js",
    "office/pathfinding.js",
    "office/effects.js",
    "office/bubbles.js",
    "office/character.js",
    "office/renderer.js",
    "office/characterManager.js",
    "office/interaction.js",
    "office/minimap.js",
    "office/office.js",
  ];
  for (const f of files) {
    vm.runInContext(readFileSync(join(jsDir, f), "utf-8"), ctx);
  }

  return { windowObj, elements, eventListeners, _taskChangeListeners };
}

function initContext(ctx) {
  const dcl = ctx.eventListeners["document"]?.["DOMContentLoaded"];
  if (dcl) dcl.forEach((fn) => { fn(); });
}

describe("office SSE integration", () => {
  it("task change listener is registered on init", () => {
    const ctx = makeContext();
    initContext(ctx);
    expect(ctx._taskChangeListeners.length).toBe(1);
  });

  it("snapshot with 3 tasks creates 3 characters", () => {
    const ctx = makeContext();
    initContext(ctx);
    const { windowObj } = ctx;

    // Show office first
    windowObj._officeShow();

    // Simulate SSE snapshot via the registered listener
    ctx._taskChangeListeners[0]([
      { id: "a", status: "backlog" },
      { id: "b", status: "in_progress" },
      { id: "c", status: "done" },
    ]);

    const mgr = windowObj._officeGetCharacterManager();
    expect(mgr.getCharacterByTaskId("a")).not.toBeNull();
    expect(mgr.getCharacterByTaskId("b")).not.toBeNull();
    expect(mgr.getCharacterByTaskId("c")).not.toBeNull();
    expect(mgr.getDrawables().length).toBe(3);
  });

  it("task update with status change updates character state", () => {
    const ctx = makeContext();
    initContext(ctx);
    const { windowObj } = ctx;
    windowObj._officeShow();

    ctx._taskChangeListeners[0]([{ id: "a", status: "backlog" }]);
    const mgr = windowObj._officeGetCharacterManager();
    const ch = mgr.getCharacterByTaskId("a");
    // Get past spawn
    ch.update(0.6, null);

    // Status change
    ctx._taskChangeListeners[0]([{ id: "a", status: "in_progress" }]);
    expect(ch.state === "walk_to_desk" || ch.state === "working").toBe(true);
  });

  it("task deleted triggers DESPAWN", () => {
    const ctx = makeContext();
    initContext(ctx);
    const { windowObj } = ctx;
    windowObj._officeShow();

    ctx._taskChangeListeners[0]([{ id: "a", status: "backlog" }]);
    const mgr = windowObj._officeGetCharacterManager();

    // Remove task
    ctx._taskChangeListeners[0]([]);
    const ch = mgr.getCharacterByTaskId("a");
    if (ch) {
      expect(ch.state).toBe("despawn");
    }
  });

  it("new task via update spawns new character", () => {
    const ctx = makeContext();
    initContext(ctx);
    const { windowObj } = ctx;
    windowObj._officeShow();

    ctx._taskChangeListeners[0]([{ id: "a", status: "backlog" }]);
    ctx._taskChangeListeners[0]([
      { id: "a", status: "backlog" },
      { id: "b", status: "in_progress" },
    ]);

    const mgr = windowObj._officeGetCharacterManager();
    expect(mgr.getCharacterByTaskId("b")).not.toBeNull();
  });

  it("when office is hidden, sync is deferred", () => {
    const ctx = makeContext();
    initContext(ctx);
    const { windowObj } = ctx;
    // Office starts hidden

    ctx._taskChangeListeners[0]([
      { id: "a", status: "backlog" },
      { id: "b", status: "backlog" },
    ]);

    const mgr = windowObj._officeGetCharacterManager();
    // Characters should NOT be created while hidden
    expect(mgr.getDrawables().length).toBe(0);

    // Now show office — buffered tasks applied
    windowObj._officeShow();
    expect(mgr.getDrawables().length).toBe(2);
  });

  it("archived tasks are filtered out", () => {
    const ctx = makeContext();
    initContext(ctx);
    const { windowObj } = ctx;
    windowObj._officeShow();

    ctx._taskChangeListeners[0]([
      { id: "a", status: "done", archived: true },
      { id: "b", status: "backlog", archived: false },
    ]);

    const mgr = windowObj._officeGetCharacterManager();
    expect(mgr.getCharacterByTaskId("a")).toBeNull();
    expect(mgr.getCharacterByTaskId("b")).not.toBeNull();
  });
});
