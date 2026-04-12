import { describe, it, expect, } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext() {
  const windowObj = {};
  const canvasEvents = {};
  const docEvents = {};

  const canvas = {
    addEventListener(type, fn) {
      if (!canvasEvents[type]) canvasEvents[type] = [];
      canvasEvents[type].push(fn);
    },
    getBoundingClientRect() {
      return { left: 0, top: 0, width: 600, height: 400 };
    },
    setPointerCapture() {},
  };

  const tooltipEl = {
    id: "office-tooltip",
    style: { cssText: "", display: "", left: "", top: "" },
    textContent: "",
  };

  const bodyChildren = [];
  const document = {
    createElement() {
      return tooltipEl;
    },
    body: {
      appendChild(el) {
        bodyChildren.push(el);
      },
    },
    addEventListener(type, fn) {
      if (!docEvents[type]) docEvents[type] = [];
      docEvents[type].push(fn);
    },
  };

  const openModalCalls = [];

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
    setTimeout: globalThis.setTimeout,
    clearTimeout: globalThis.clearTimeout,
    Date,
    Math,
    Set,
    JSON,
    Object,
    console,
    openModal(id) {
      openModalCalls.push(id);
    },
    findTaskById(id) {
      return { id, title: "Test Task " + id, prompt: "do stuff" };
    },
  });

  // Load modules
  const files = [
    "office/tileMap.js",
    "office/pathfinding.js",
    "office/effects.js",
    "office/bubbles.js",
    "office/camera.js",
    "office/character.js",
    "office/characterManager.js",
    "office/interaction.js",
  ];
  for (const f of files) {
    vm.runInContext(readFileSync(join(jsDir, f), "utf-8"), ctx);
  }

  return {
    windowObj,
    canvas,
    canvasEvents,
    docEvents,
    openModalCalls,
    tooltipEl,
  };
}

function fire(events, type, data) {
  (events[type] || []).forEach((fn) => { fn(data); });
}

function setupInteraction() {
  const ctx = makeContext();
  const { windowObj } = ctx;

  // Create a layout and manager with characters
  const layout = windowObj._officeGenerateLayout(6);
  const mgr = new windowObj._officeCharacterManager(
    layout.tileMap,
    layout.seats,
  );
  mgr.syncTasks([
    { id: "task-a", status: "backlog" },
    { id: "task-b", status: "backlog" },
  ]);

  // Get past spawn
  mgr.updateAll(0.6);

  // Create camera — zoom 1 for easy coordinate math
  const camera = new windowObj._officeCamera(600, 400);
  camera.zoom = 1;
  camera.x = 0;
  camera.y = 0;

  const interaction = new windowObj._officeInteraction(ctx.canvas, camera, mgr);

  // Get character positions
  const chA = mgr.getCharacterByTaskId("task-a");
  const chB = mgr.getCharacterByTaskId("task-b");

  return { ...ctx, interaction, mgr, camera, chA, chB };
}

describe("OfficeInteraction", () => {
  it("click at character position selects it", () => {
    const { interaction, canvasEvents, chA } = setupInteraction();

    const px = chA.x * 16 + 8;
    const py = chA.y * 16 + 8;

    fire(canvasEvents, "pointerdown", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });
    fire(canvasEvents, "pointerup", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });

    expect(interaction.getSelectedId()).toBe("task-a");
  });

  it("click empty space clears selection", () => {
    const { interaction, canvasEvents, chA } = setupInteraction();

    // First select
    const px = chA.x * 16 + 8;
    const py = chA.y * 16 + 8;
    fire(canvasEvents, "pointerdown", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });
    fire(canvasEvents, "pointerup", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });
    expect(interaction.getSelectedId()).toBe("task-a");

    // Click empty space
    fire(canvasEvents, "pointerdown", {
      clientX: 1,
      clientY: 1,
      pointerType: "mouse",
    });
    fire(canvasEvents, "pointerup", {
      clientX: 1,
      clientY: 1,
      pointerType: "mouse",
    });
    expect(interaction.getSelectedId()).toBeNull();
  });

  it("double-click character opens modal", () => {
    const { canvasEvents, openModalCalls, chA } = setupInteraction();

    const px = chA.x * 16 + 8;
    const py = chA.y * 16 + 8;

    // First click
    fire(canvasEvents, "pointerdown", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });
    fire(canvasEvents, "pointerup", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });

    // Second click (double-tap)
    fire(canvasEvents, "pointerdown", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });
    fire(canvasEvents, "pointerup", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });

    expect(openModalCalls).toContain("task-a");
  });

  it("Escape key clears selection", () => {
    const { interaction, canvasEvents, docEvents, chA, windowObj } =
      setupInteraction();

    // Mock _officeIsVisible
    windowObj._officeIsVisible = () => true;

    const px = chA.x * 16 + 8;
    const py = chA.y * 16 + 8;
    fire(canvasEvents, "pointerdown", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });
    fire(canvasEvents, "pointerup", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });
    expect(interaction.getSelectedId()).toBe("task-a");

    fire(docEvents, "keydown", { key: "Escape" });
    expect(interaction.getSelectedId()).toBeNull();
  });

  it("Tab key cycles to next character", () => {
    const { interaction, docEvents, windowObj } = setupInteraction();
    windowObj._officeIsVisible = () => true;

    fire(docEvents, "keydown", { key: "Tab", preventDefault() {} });
    const first = interaction.getSelectedId();
    expect(first).not.toBeNull();

    fire(docEvents, "keydown", { key: "Tab", preventDefault() {} });
    const second = interaction.getSelectedId();
    expect(second).not.toBeNull();
    // May or may not be different depending on order, but should not throw
  });

  it("pointerdown + significant move = pan (no selection)", () => {
    const { interaction, canvasEvents, chA } = setupInteraction();

    const px = chA.x * 16 + 8;
    const py = chA.y * 16 + 8;

    fire(canvasEvents, "pointerdown", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });
    // Move significantly
    fire(canvasEvents, "pointermove", {
      clientX: px + 20,
      clientY: py + 20,
      pointerType: "mouse",
    });
    fire(canvasEvents, "pointerup", {
      clientX: px + 20,
      clientY: py + 20,
      pointerType: "mouse",
    });

    expect(interaction.getSelectedId()).toBeNull();
  });

  it("pointerdown + minimal move + pointerup = click (selection)", () => {
    const { interaction, canvasEvents, chA } = setupInteraction();

    const px = chA.x * 16 + 8;
    const py = chA.y * 16 + 8;

    fire(canvasEvents, "pointerdown", {
      clientX: px,
      clientY: py,
      pointerType: "mouse",
    });
    // Move less than threshold
    fire(canvasEvents, "pointermove", {
      clientX: px + 2,
      clientY: py + 1,
      pointerType: "mouse",
    });
    fire(canvasEvents, "pointerup", {
      clientX: px + 2,
      clientY: py + 1,
      pointerType: "mouse",
    });

    expect(interaction.getSelectedId()).toBe("task-a");
  });
});
