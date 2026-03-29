import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

function makeMockCtx() {
  const calls = [];
  return {
    calls,
    imageSmoothingEnabled: true,
    clearRect(...args) {
      calls.push({ op: "clearRect", args });
    },
    save() {
      calls.push({ op: "save" });
    },
    restore() {
      calls.push({ op: "restore" });
    },
    translate(...args) {
      calls.push({ op: "translate", args });
    },
    scale(...args) {
      calls.push({ op: "scale", args });
    },
    drawImage(...args) {
      calls.push({ op: "drawImage", args });
    },
    fillRect(...args) {
      calls.push({ op: "fillRect", args });
    },
    fillText(...args) {
      calls.push({ op: "fillText", args });
    },
    fillStyle: "",
    font: "",
    textAlign: "",
    textBaseline: "",
  };
}

function makeContext() {
  const windowObj = {};
  const mockCtx = makeMockCtx();
  const canvasEl = {
    width: 600,
    height: 400,
    getContext() {
      return mockCtx;
    },
  };

  class MockOffscreenCanvas {
    constructor(w, h) {
      this.width = w;
      this.height = h;
      this._draws = [];
    }
    getContext() {
      const self = this;
      return {
        imageSmoothingEnabled: true,
        fillStyle: "",
        fillRect(x, y, w, h) {
          self._draws.push({ op: "fillRect", x, y, w, h });
        },
        drawImage() {},
      };
    }
  }

  const ctx = vm.createContext({
    window: windowObj,
    OffscreenCanvas: MockOffscreenCanvas,
    requestAnimationFrame(fn) {
      return 1;
    },
    cancelAnimationFrame() {},
    Math,
    Object,
    console,
  });

  // Load dependencies first
  const tileMapSrc = readFileSync(join(jsDir, "office", "tileMap.js"), "utf-8");
  vm.runInContext(tileMapSrc, ctx);

  const rendererSrc = readFileSync(
    join(jsDir, "office", "renderer.js"),
    "utf-8",
  );
  vm.runInContext(rendererSrc, ctx);

  return { windowObj, canvasEl, mockCtx, MockOffscreenCanvas };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("OfficeRenderer", () => {
  it("can be instantiated with mock canvas", () => {
    const { windowObj, canvasEl } = makeContext();
    const spriteCache = {};
    const camera = { x: 0, y: 0, zoom: 3 };
    const renderer = new windowObj._officeRenderer(canvasEl, spriteCache, camera);
    expect(renderer).toBeDefined();
  });

  it("sets imageSmoothingEnabled to false on init", () => {
    const { windowObj, canvasEl, mockCtx } = makeContext();
    new windowObj._officeRenderer(canvasEl, {}, { x: 0, y: 0, zoom: 3 });
    expect(mockCtx.imageSmoothingEnabled).toBe(false);
  });

  it("setLayout updates internal state", () => {
    const { windowObj, canvasEl } = makeContext();
    const renderer = new windowObj._officeRenderer(
      canvasEl,
      {},
      { x: 0, y: 0, zoom: 3 },
    );

    const layout = windowObj._officeGenerateLayout(6);
    renderer.setLayout(layout.tileMap, layout.furniture, layout.seats);
    // No error thrown, internal state updated
    expect(renderer._tileMap).toBe(layout.tileMap);
    expect(renderer._furniture).toBe(layout.furniture);
  });

  it("render calls ctx.clearRect, save, scale, restore", () => {
    const { windowObj, canvasEl, mockCtx } = makeContext();
    const camera = { x: 0, y: 0, zoom: 3 };
    const renderer = new windowObj._officeRenderer(canvasEl, {}, camera);

    const layout = windowObj._officeGenerateLayout(6);
    renderer.setLayout(layout.tileMap, layout.furniture, layout.seats);

    // Manually call render (not via rAF loop)
    renderer._running = true;
    mockCtx.calls.length = 0;
    renderer.render(0);

    const ops = mockCtx.calls.map((c) => c.op);
    expect(ops).toContain("clearRect");
    expect(ops).toContain("save");
    expect(ops).toContain("scale");
    expect(ops).toContain("restore");
  });

  it("z-sorts furniture by bottom edge Y ascending", () => {
    const { windowObj, canvasEl, mockCtx } = makeContext();
    const camera = { x: 0, y: 0, zoom: 3 };
    const renderer = new windowObj._officeRenderer(canvasEl, {}, camera);

    // Create a simple tilemap
    const TileMap = windowObj._officeTileMap;
    const T = windowObj._officeTileTypes;
    const map = new TileMap(10, 10);
    for (let y = 0; y < 10; y++)
      for (let x = 0; x < 10; x++) map.setTile(x, y, T.FLOOR);

    const furniture = [
      { type: "desk", x: 1, y: 5, width: 2, height: 1, state: null },
      { type: "plant", x: 3, y: 2, width: 1, height: 1, state: null },
      { type: "sofa", x: 0, y: 8, width: 2, height: 1, state: null },
    ];

    renderer.setLayout(map, furniture, []);
    renderer._running = true;
    mockCtx.calls.length = 0;
    renderer.render(0);

    // The fillRect calls for furniture should be in Y order:
    // plant (y=2, bottom=3), desk (y=5, bottom=6), sofa (y=8, bottom=9)
    const fillRects = mockCtx.calls.filter(
      (c) => c.op === "fillRect" && c.args[1] !== 0,
    );
    // First furniture fillRect should be plant (y=2 → py=32)
    // Find the furniture draw calls (after floor drawImage)
    const drawImageIdx = mockCtx.calls.findIndex((c) => c.op === "drawImage");
    const afterFloor = mockCtx.calls.slice(drawImageIdx + 1);
    const furnitureFills = afterFloor.filter((c) => c.op === "fillRect");
    // y values should be ascending
    if (furnitureFills.length >= 3) {
      expect(furnitureFills[0].args[1]).toBeLessThanOrEqual(
        furnitureFills[1].args[1],
      );
      expect(furnitureFills[1].args[1]).toBeLessThanOrEqual(
        furnitureFills[2].args[1],
      );
    }
  });

  it("floor cache is reused on second render (no redraw)", () => {
    const { windowObj, canvasEl, mockCtx } = makeContext();
    const camera = { x: 0, y: 0, zoom: 3 };
    const renderer = new windowObj._officeRenderer(canvasEl, {}, camera);

    const layout = windowObj._officeGenerateLayout(6);
    renderer.setLayout(layout.tileMap, layout.furniture, layout.seats);
    renderer._running = true;

    // First render — builds floor cache
    renderer.render(0);
    const floorCanvas1 = renderer._floorCanvas;
    expect(floorCanvas1).toBeDefined();

    // Second render — should reuse same floor canvas
    renderer.render(16);
    expect(renderer._floorCanvas).toBe(floorCanvas1);
    expect(renderer._floorDirty).toBe(false);
  });

  it("invalidateFloorCache marks floor dirty", () => {
    const { windowObj, canvasEl } = makeContext();
    const renderer = new windowObj._officeRenderer(
      canvasEl,
      {},
      { x: 0, y: 0, zoom: 3 },
    );

    const layout = windowObj._officeGenerateLayout(6);
    renderer.setLayout(layout.tileMap, layout.furniture, layout.seats);
    renderer._running = true;
    renderer.render(0);

    expect(renderer._floorDirty).toBe(false);
    renderer.invalidateFloorCache();
    expect(renderer._floorDirty).toBe(true);
  });
});
