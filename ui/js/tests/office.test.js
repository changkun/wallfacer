import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// ---------------------------------------------------------------------------
// Helpers — mock DOM environment for office.js
// ---------------------------------------------------------------------------

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
      clientWidth: 800,
      clientHeight: 600,
      parentElement: null,
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
      const el = makeElement("office-canvas-" + Math.random(), tag);
      // Make canvas report size from parent
      Object.defineProperty(el, "clientWidth", {
        get() {
          return el.parentElement ? el.parentElement.clientWidth : 800;
        },
      });
      Object.defineProperty(el, "clientHeight", {
        get() {
          return el.parentElement ? el.parentElement.clientHeight : 600;
        },
      });
      return el;
    },
    addEventListener(type, fn) {
      if (!eventListeners["document"]) eventListeners["document"] = {};
      if (!eventListeners["document"][type])
        eventListeners["document"][type] = [];
      eventListeners["document"][type].push(fn);
    },
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

  // window needs addEventListener for resize handler
  windowObj.addEventListener = function (type, fn) {
    if (!eventListeners["window"]) eventListeners["window"] = {};
    if (!eventListeners["window"][type]) eventListeners["window"][type] = [];
    eventListeners["window"][type].push(fn);
  };

  const ctx = vm.createContext({
    window: windowObj,
    document,
    OffscreenCanvas: MockOffscreenCanvas,
    Image: class {
      constructor() {
        this.onload = null;
        this.onerror = null;
      }
      set src(v) {}
    },
    Promise,
    requestAnimationFrame() {
      return 1;
    },
    cancelAnimationFrame() {},
    Math,
    Object,
    console,
  });

  // Load all office modules in order
  const files = [
    "office/tileMap.js",
    "office/spriteCache.js",
    "office/camera.js",
    "office/renderer.js",
    "office/office.js",
  ];
  for (const f of files) {
    vm.runInContext(readFileSync(join(jsDir, f), "utf-8"), ctx);
  }

  return { windowObj, elements, eventListeners, ctx };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("office coordinator", () => {
  it("initOffice shows toggle button and creates canvas", () => {
    const { windowObj, elements, eventListeners } = makeContext();

    // Trigger DOMContentLoaded
    const dcl = eventListeners["document"]?.["DOMContentLoaded"];
    expect(dcl).toBeDefined();
    dcl.forEach((fn) => fn());

    // Toggle button should be visible
    expect(elements["office-toggle"].classList.contains("hidden")).toBe(false);
    // Canvas should be appended to container
    expect(elements["office-container"].children.length).toBe(1);
  });

  it("showOffice hides board and shows office-container", () => {
    const { windowObj, elements, eventListeners } = makeContext();
    const dcl = eventListeners["document"]?.["DOMContentLoaded"];
    dcl.forEach((fn) => fn());

    windowObj._officeShow();

    expect(elements["board"].style.display).toBe("none");
    expect(elements["office-container"].style.display).toBe("block");
    expect(windowObj._officeIsVisible()).toBe(true);
  });

  it("hideOffice restores board and hides office-container", () => {
    const { windowObj, elements, eventListeners } = makeContext();
    const dcl = eventListeners["document"]?.["DOMContentLoaded"];
    dcl.forEach((fn) => fn());

    windowObj._officeShow();
    windowObj._officeHide();

    expect(elements["board"].style.display).toBe("");
    expect(elements["office-container"].style.display).toBe("none");
    expect(windowObj._officeIsVisible()).toBe(false);
  });

  it("toggle button click alternates views", () => {
    const { windowObj, elements, eventListeners } = makeContext();
    const dcl = eventListeners["document"]?.["DOMContentLoaded"];
    dcl.forEach((fn) => fn());

    // Simulate click
    windowObj._officeToggle();
    expect(windowObj._officeIsVisible()).toBe(true);
    expect(elements["office-toggle"].textContent).toBe("Board");

    windowObj._officeToggle();
    expect(windowObj._officeIsVisible()).toBe(false);
    expect(elements["office-toggle"].textContent).toBe("Office");
  });

  it("updateLayout changes the renderer layout", () => {
    const { windowObj, eventListeners } = makeContext();
    const dcl = eventListeners["document"]?.["DOMContentLoaded"];
    dcl.forEach((fn) => fn());

    // Should not throw
    windowObj._officeUpdateLayout(10);
    windowObj._officeUpdateLayout(0);
  });
});
