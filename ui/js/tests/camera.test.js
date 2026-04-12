import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext() {
  const windowObj = {};
  const events = {};

  const canvas = {
    setPointerCapture() {},
    addEventListener(type, fn, opts) {
      if (!events[type]) events[type] = [];
      events[type].push(fn);
    },
  };

  const ctx = vm.createContext({
    window: windowObj,
    Math,
    Object,
    console,
  });

  const src = readFileSync(join(jsDir, "office", "camera.js"), "utf-8");
  vm.runInContext(src, ctx);

  return { windowObj, canvas, events };
}

function fire(events, type, data) {
  (events[type] || []).forEach((fn) => { fn(data); });
}

// ---------------------------------------------------------------------------
// Camera transforms
// ---------------------------------------------------------------------------

describe("Camera coordinate transforms", () => {
  it("worldToScreen at zoom=3 with offset (0,0)", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    const s = cam.worldToScreen(10, 10);
    expect(s.sx).toBe(30);
    expect(s.sy).toBe(30);
  });

  it("worldToScreen with offset (5,0)", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.x = 5;
    const s = cam.worldToScreen(10, 10);
    expect(s.sx).toBe(15); // (10-5)*3
    expect(s.sy).toBe(30);
  });

  it("screenToWorld is inverse of worldToScreen", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.x = 7;
    cam.y = 3;
    cam.zoom = 4;
    const s = cam.worldToScreen(20, 15);
    const w = cam.screenToWorld(s.sx, s.sy);
    expect(w.wx).toBeCloseTo(20);
    expect(w.wy).toBeCloseTo(15);
  });
});

// ---------------------------------------------------------------------------
// setZoom
// ---------------------------------------------------------------------------

describe("setZoom", () => {
  it("clamps below minimum to 2", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.setZoom(1);
    expect(cam.zoom).toBe(2);
    cam.setZoom(0);
    expect(cam.zoom).toBe(2);
    cam.setZoom(-5);
    expect(cam.zoom).toBe(2);
  });

  it("clamps above maximum to 6", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.setZoom(7);
    expect(cam.zoom).toBe(6);
    cam.setZoom(100);
    expect(cam.zoom).toBe(6);
  });

  it("rounds to integer", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.setZoom(3.7);
    expect(cam.zoom).toBe(4);
  });

  it("zooms toward center (center world point stays stable)", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.x = 0;
    cam.y = 0;
    // Center of viewport in world coords at zoom=3: (50, 33.33)
    cam.setZoom(4);
    // After zoom to 4, center should still be at (50, 33.33)
    const centerWx = 300 / 2 / cam.zoom + cam.x;
    const centerWy = 200 / 2 / cam.zoom + cam.y;
    expect(centerWx).toBeCloseTo(50);
    expect(centerWy).toBeCloseTo(200 / 6); // 33.33
  });
});

// ---------------------------------------------------------------------------
// pan
// ---------------------------------------------------------------------------

describe("pan", () => {
  it("shifts offset correctly", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.x = 10;
    cam.y = 5;
    cam.pan(9, 6); // screen-space delta
    // pan subtracts dx/zoom from x
    expect(cam.x).toBe(7); // 10 - 9/3
    expect(cam.y).toBe(3); // 5 - 6/3
  });
});

// ---------------------------------------------------------------------------
// clamp
// ---------------------------------------------------------------------------

describe("clamp", () => {
  it("prevents negative offset", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.x = -10;
    cam.y = -5;
    cam.clamp(500, 500);
    expect(cam.x).toBe(0);
    expect(cam.y).toBe(0);
  });

  it("prevents panning beyond world bounds", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.zoom = 3;
    // viewW = 300/3 = 100, viewH = 200/3 ≈ 66.67
    // maxX = 200 - 100 = 100, maxY = 200 - 66.67 ≈ 133.33
    cam.x = 999;
    cam.y = 999;
    cam.clamp(200, 200);
    expect(cam.x).toBe(100);
    expect(cam.y).toBeCloseTo(200 - 200 / 3);
  });

  it("centers when world fits in viewport", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.zoom = 3;
    // viewW = 100 > worldWidth 50 → center at (50-100)/2 = -25
    cam.x = 10;
    cam.clamp(50, 50);
    expect(cam.x).toBe(-25);
    expect(cam.y).toBeCloseTo((50 - 200 / 3) / 2);
  });
});

// ---------------------------------------------------------------------------
// resize
// ---------------------------------------------------------------------------

describe("resize", () => {
  it("updates canvas dimensions", () => {
    const { windowObj } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    cam.resize(600, 400);
    // Verify by checking worldToScreen still works with new dims
    // and clamp uses new viewport size
    cam.x = 999;
    cam.clamp(100, 100);
    // viewW = 600/3 = 200 > 100 → centers at (100-200)/2 = -50
    expect(cam.x).toBe(-50);
  });
});

// ---------------------------------------------------------------------------
// Input handlers
// ---------------------------------------------------------------------------

describe("attachInputHandlers", () => {
  it("wheel zoom changes zoom level", () => {
    const { windowObj, canvas, events } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    let changed = false;
    windowObj._officeAttachInputHandlers(canvas, cam, () => {
      changed = true;
    });

    expect(cam.zoom).toBe(3);
    fire(events, "wheel", { deltaY: -100, preventDefault() {} });
    expect(cam.zoom).toBe(4);
    expect(changed).toBe(true);

    fire(events, "wheel", { deltaY: 100, preventDefault() {} });
    expect(cam.zoom).toBe(3);
  });

  it("pointer drag pans the camera", () => {
    const { windowObj, canvas, events } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    windowObj._officeAttachInputHandlers(canvas, cam);

    cam.x = 0;
    cam.y = 0;

    fire(events, "pointerdown", {
      pointerId: 1,
      pointerType: "mouse",
      clientX: 100,
      clientY: 100,
      preventDefault() {},
    });
    fire(events, "pointermove", {
      pointerId: 1,
      clientX: 130,
      clientY: 115,
    });
    // pan(30, 15) → x -= 30/3 = -10, y -= 15/3 = -5
    expect(cam.x).toBeCloseTo(-10);
    expect(cam.y).toBeCloseTo(-5);

    fire(events, "pointerup", { pointerId: 1 });
  });

  it("pinch zoom simulated with two pointers", () => {
    const { windowObj, canvas, events } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    windowObj._officeAttachInputHandlers(canvas, cam);

    expect(cam.zoom).toBe(3);

    // First finger down
    fire(events, "pointerdown", {
      pointerId: 1,
      pointerType: "touch",
      clientX: 100,
      clientY: 100,
      preventDefault() {},
    });
    // Second finger down — initial pinch distance = sqrt(100^2 + 0^2) = 100
    fire(events, "pointerdown", {
      pointerId: 2,
      pointerType: "touch",
      clientX: 200,
      clientY: 100,
      preventDefault() {},
    });

    // Spread fingers apart by >30px to trigger zoom in
    fire(events, "pointermove", {
      pointerId: 2,
      clientX: 240,
      clientY: 100,
    });
    // Distance is now 140, delta=40 > 30 → zoom in
    expect(cam.zoom).toBe(4);
  });

  it("touch devices enforce minimum zoom of 3", () => {
    const { windowObj, canvas, events } = makeContext();
    const cam = new windowObj._officeCamera(300, 200);
    windowObj._officeAttachInputHandlers(canvas, cam);

    // Start at zoom 3, try to pinch to zoom out
    expect(cam.zoom).toBe(3);

    fire(events, "pointerdown", {
      pointerId: 1,
      pointerType: "touch",
      clientX: 100,
      clientY: 100,
      preventDefault() {},
    });
    fire(events, "pointerdown", {
      pointerId: 2,
      pointerType: "touch",
      clientX: 200,
      clientY: 100,
      preventDefault() {},
    });

    // Pinch fingers closer (distance decreases by >30px)
    fire(events, "pointermove", {
      pointerId: 2,
      clientX: 160,
      clientY: 100,
    });
    // Distance went from 100 to 60, delta=-40 → zoom out attempt
    // But touch min is 3, so zoom stays at 3
    expect(cam.zoom).toBe(3);
  });
});
