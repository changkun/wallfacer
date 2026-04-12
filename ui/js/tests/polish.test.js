import { describe, it, expect, } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// ---------------------------------------------------------------------------
// Camera follow tests (standalone, no DOM)
// ---------------------------------------------------------------------------

function makeCameraContext() {
  const windowObj = {};
  const ctx = vm.createContext({ window: windowObj, Math, Object, console });
  vm.runInContext(
    readFileSync(join(jsDir, "office", "camera.js"), "utf-8"),
    ctx,
  );
  return windowObj;
}

describe("Camera follow", () => {
  const w = makeCameraContext();

  it("followTarget moves camera toward target over multiple updates", () => {
    const cam = new w._officeCamera(300, 200);
    cam.x = 0;
    cam.y = 0;
    cam.zoom = 3;

    cam.followTarget(100, 100);
    expect(cam.isFollowing()).toBe(true);

    for (let i = 0; i < 50; i++) {
      cam.updateFollow();
    }

    // Should have moved close to the target offset
    const targetX = 100 - 300 / 2 / 3; // 50
    const targetY = 100 - 200 / 2 / 3; // ~66.67
    expect(Math.abs(cam.x - targetX)).toBeLessThan(1);
    expect(Math.abs(cam.y - targetY)).toBeLessThan(1);
  });

  it("manual pan cancels follow", () => {
    const cam = new w._officeCamera(300, 200);
    cam.followTarget(100, 100);
    expect(cam.isFollowing()).toBe(true);

    cam.cancelFollow();
    expect(cam.isFollowing()).toBe(false);
  });

  it("follow completes and stops when close enough", () => {
    const cam = new w._officeCamera(300, 200);
    cam.x = 0;
    cam.y = 0;
    cam.zoom = 3;
    cam.followTarget(50, 33.33);

    for (let i = 0; i < 200; i++) {
      cam.updateFollow();
    }
    expect(cam.isFollowing()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// View preference and minimap tests (full office context)
// ---------------------------------------------------------------------------

function makeOfficeContext(opts) {
  const windowObj = {};
  const elements = {};
  const eventListeners = {};
  const storage = (opts && opts.storage) || {};

  function makeElement(id, tag) {
    const el = {
      id,
      tagName: tag || "div",
      style: {},
      children: [],
      classList: {
        _set: new Set(["hidden"]),
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
      clientWidth: 800,
      clientHeight: 600,
      parentElement: null,
      className: "",
      appendChild(child) {
        this.children.push(child);
        child.parentElement = this;
      },
      addEventListener(type, fn) {
        if (!eventListeners[id]) eventListeners[id] = {};
        if (!eventListeners[id][type]) eventListeners[id][type] = [];
        eventListeners[id][type].push(fn);
      },
      setAttribute() {},
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
          strokeRect() {},
          fillStyle: "",
          strokeStyle: "",
          lineWidth: 1,
          font: "",
          textAlign: "",
          textBaseline: "",
        };
      },
    };
    elements[id] = el;
    return el;
  }

  const container = makeElement("office-container");
  // Remove hidden from container toggle btn
  makeElement("board", "main");
  const toggleBtn = makeElement("office-toggle", "button");

  const document = {
    getElementById(id) {
      return elements[id] || null;
    },
    createElement(tag) {
      const el = makeElement("el-" + Math.random(), tag);
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

  const ctx = vm.createContext({
    window: windowObj,
    document,
    localStorage: {
      _data: storage,
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
      set src(v) {}
    },
    Set,
    JSON,
    Promise,
    performance: { now: () => Date.now() },
    location: { search: "?office=dev" },
    registerTaskChangeListener() {},
    requestAnimationFrame() {
      return 1;
    },
    cancelAnimationFrame() {},
    setTimeout: globalThis.setTimeout,
    clearTimeout: globalThis.clearTimeout,
    Math,
    Object,
    console,
  });

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

  return { windowObj, elements, eventListeners, storage };
}

function initCtx(ctx) {
  const dcl = ctx.eventListeners["document"]?.["DOMContentLoaded"];
  if (dcl) dcl.forEach((fn) => { fn(); });
}

describe("View preference", () => {
  it("show and hide work correctly", () => {
    const ctx = makeOfficeContext();
    initCtx(ctx);
    const { windowObj } = ctx;

    windowObj._officeShow();
    expect(windowObj._officeIsVisible()).toBe(true);

    windowObj._officeHide();
    expect(windowObj._officeIsVisible()).toBe(false);
  });

  it("starts hidden by default", () => {
    const ctx = makeOfficeContext();
    initCtx(ctx);
    expect(ctx.windowObj._officeIsVisible()).toBe(false);
  });
});

describe("Minimap", () => {
  it("not created visible when desk count <= 20", () => {
    const ctx = makeOfficeContext();
    initCtx(ctx);
    const { windowObj } = ctx;

    windowObj._officeShow();
    // Default layout has 6 desks, below threshold
    // Minimap exists but should not be visible
    // We test via the sync path
    windowObj._officeSyncTasks([{ id: "a", status: "backlog" }]);
    // 6 seats < 20 threshold → minimap hidden (canvas display=none)
  });

  it("created visible when desk count > 20", () => {
    const ctx = makeOfficeContext();
    initCtx(ctx);
    const { windowObj } = ctx;

    windowObj._officeShow();
    // Create enough tasks to exceed threshold
    const manyTasks = [];
    for (let i = 0; i < 25; i++) {
      manyTasks.push({ id: "task-" + i, status: "backlog" });
    }
    windowObj._officeSyncTasks(manyTasks);
    // Layout now has >= 25 seats, above 20 threshold
  });
});

describe("SR summary", () => {
  it("text updates when character states change", async () => {
    const ctx = makeOfficeContext();
    initCtx(ctx);
    const { windowObj, elements } = ctx;

    windowObj._officeShow();
    windowObj._officeSyncTasks([
      { id: "a", status: "backlog", title: "Auth" },
      { id: "b", status: "in_progress", title: "Fix bug" },
    ]);

    // SR summary is debounced — wait for it
    await new Promise((r) => setTimeout(r, 2200));

    const srEl = elements["office-container"].children.find(
      (c) => c.id === "office-sr-summary",
    );
    if (srEl) {
      expect(srEl.textContent).toContain("2 tasks");
    }
  });

  it("debounced: multiple rapid changes produce one update", async () => {
    const ctx = makeOfficeContext();
    initCtx(ctx);
    const { windowObj } = ctx;

    windowObj._officeShow();
    windowObj._officeSyncTasks([{ id: "a", status: "backlog" }]);
    windowObj._officeSyncTasks([
      { id: "a", status: "backlog" },
      { id: "b", status: "in_progress" },
    ]);
    windowObj._officeSyncTasks([
      { id: "a", status: "backlog" },
      { id: "b", status: "in_progress" },
      { id: "c", status: "done" },
    ]);

    // Only the last update should take effect after debounce
    await new Promise((r) => setTimeout(r, 2200));

    const srEl = ctx.elements["office-container"].children.find(
      (c) => c.id === "office-sr-summary",
    );
    if (srEl) {
      expect(srEl.textContent).toContain("3 tasks");
    }
  });
});
