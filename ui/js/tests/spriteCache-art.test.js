import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext() {
  const windowObj = {};
  const imageInstances = [];

  class MockImage {
    constructor() {
      this.width = 0;
      this.height = 0;
      this.onload = null;
      this.onerror = null;
      this._src = "";
      imageInstances.push(this);
    }
    get src() {
      return this._src;
    }
    set src(val) {
      this._src = val;
    }
  }

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
    Image: MockImage,
    OffscreenCanvas: MockOffscreenCanvas,
    Promise,
    Math,
    Object,
    console,
  });

  vm.runInContext(
    readFileSync(join(jsDir, "office", "spriteCache.js"), "utf-8"),
    ctx,
  );

  return { windowObj, imageInstances };
}

// ---------------------------------------------------------------------------
// CHARACTER_ANIMS
// ---------------------------------------------------------------------------

describe("CHARACTER_ANIMS", () => {
  const { windowObj } = makeContext();
  const anims = windowObj._officeCharacterAnims;

  it("defines walk with 4 directions", () => {
    expect(anims.walk.down).toBeDefined();
    expect(anims.walk.up).toBeDefined();
    expect(anims.walk.left).toBeDefined();
    expect(anims.walk.right).toBeDefined();
  });

  it("walk has 6 frames per direction", () => {
    expect(anims.walk.down.frames).toBe(6);
    expect(anims.walk.up.frames).toBe(6);
    expect(anims.walk.left.frames).toBe(6);
    expect(anims.walk.right.frames).toBe(6);
  });

  it("typing defines frames for 4 directions", () => {
    expect(anims.typing.down).toBeDefined();
    expect(anims.typing.up).toBeDefined();
    expect(anims.typing.left).toBeDefined();
    expect(anims.typing.right).toBeDefined();
    expect(anims.typing.down.megaRow).toBeDefined();
  });

  it("idle defines 1 frame per direction", () => {
    expect(anims.idle.down.frames).toBe(1);
    expect(anims.idle.up.frames).toBe(1);
  });

  it("all animation entries have row, col, frames", () => {
    const names = Object.keys(anims);
    for (const name of names) {
      const dirs = Object.keys(anims[name]);
      for (const dir of dirs) {
        const entry = anims[name][dir];
        expect(typeof entry.megaRow).toBe("number");
        expect(typeof entry.col).toBe("number");
        expect(typeof entry.frames).toBe("number");
        expect(entry.frames).toBeGreaterThan(0);
      }
    }
  });
});

// ---------------------------------------------------------------------------
// FURNITURE_DEFS
// ---------------------------------------------------------------------------

describe("FURNITURE_DEFS", () => {
  const { windowObj } = makeContext();
  const defs = windowObj._officeFurnitureDefs;

  it("PC has >= 1 frame", () => {
    expect(defs.pc.frames).toBeGreaterThanOrEqual(1);
  });

  it("DESK has sx, sy, sw, sh within 256×848 bounds", () => {
    expect(defs.desk.sx).toBeGreaterThanOrEqual(0);
    expect(defs.desk.sy).toBeGreaterThanOrEqual(0);
    expect(defs.desk.sx + defs.desk.sw).toBeLessThanOrEqual(256);
    expect(defs.desk.sy + defs.desk.sh).toBeLessThanOrEqual(848);
  });

  it("all furniture defs have valid regions", () => {
    const types = Object.keys(defs);
    expect(types.length).toBeGreaterThanOrEqual(5);
    for (const type of types) {
      const def = defs[type];
      expect(typeof def.sx).toBe("number");
      expect(typeof def.sy).toBe("number");
      expect(typeof def.sw).toBe("number");
      expect(typeof def.sh).toBe("number");
      expect(typeof def.frames).toBe("number");
      expect(def.sw).toBeGreaterThan(0);
      expect(def.sh).toBeGreaterThan(0);
    }
  });
});

// ---------------------------------------------------------------------------
// TILE_DEFS
// ---------------------------------------------------------------------------

describe("TILE_DEFS", () => {
  const { windowObj } = makeContext();
  const defs = windowObj._officeTileDefs;

  it("floor and wall tile defs are defined", () => {
    expect(defs.floor).toBeDefined();
    expect(defs.wall).toBeDefined();
  });

  it("floor has valid region", () => {
    expect(defs.floor.sw).toBeGreaterThan(0);
    expect(defs.floor.sh).toBeGreaterThan(0);
  });
});

// ---------------------------------------------------------------------------
// detectAssets
// ---------------------------------------------------------------------------

describe("detectAssets", () => {
  it("returns true on successful image load", async () => {
    const { windowObj, imageInstances } = makeContext();
    windowObj._officeResetAssetState();

    const p = windowObj._officeDetectAssets();
    const img = imageInstances[imageInstances.length - 1];
    img.width = 896;
    img.height = 656;
    img.onload();

    const result = await p;
    expect(result).toBe(true);
    expect(windowObj._officeAssetAvailable()).toBe(true);
  });

  it("returns false on image load error", async () => {
    const { windowObj, imageInstances } = makeContext();
    windowObj._officeResetAssetState();

    const p = windowObj._officeDetectAssets();
    const img = imageInstances[imageInstances.length - 1];
    img.onerror();

    const result = await p;
    expect(result).toBe(false);
    expect(windowObj._officeAssetAvailable()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// Placeholder mode
// ---------------------------------------------------------------------------

describe("placeholder mode", () => {
  it("all sprites render without errors when assets missing", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    // Load a sheet that fails
    const p = cache.loadSpriteSheet("/missing.png", 16, 16);
    imageInstances[0].onerror();
    const sheet = await p;

    // Should not throw
    expect(sheet.frame(0)).toBeDefined();
    expect(sheet.image).toBeDefined();
    expect(sheet.frameWidth).toBe(16);
    expect(sheet.frameHeight).toBe(16);
  });
});
