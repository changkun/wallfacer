import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// ---------------------------------------------------------------------------
// Helpers — mock browser APIs needed by spriteCache.js
// ---------------------------------------------------------------------------

function makeContext() {
  const windowObj = {};

  // Track Image instances so tests can trigger onload/onerror.
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
      // Trigger load/error asynchronously via microtask so handlers are set.
    }
  }

  // Minimal OffscreenCanvas mock.
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
        drawImage(...args) {
          self._draws.push({ op: "drawImage", args });
        },
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

  const src = readFileSync(join(jsDir, "office", "spriteCache.js"), "utf-8");
  vm.runInContext(src, ctx);

  return { windowObj, imageInstances, MockOffscreenCanvas };
}

// ---------------------------------------------------------------------------
// SpriteSheet.frame()
// ---------------------------------------------------------------------------

describe("SpriteSheet frame calculation", () => {
  it("frame(0) returns correct source rect for first frame", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    const p = cache.loadSpriteSheet("/test.png", 16, 16);

    // Simulate successful load with a 64x32 image (4 cols × 2 rows).
    const img = imageInstances[0];
    img.width = 64;
    img.height = 32;
    img.onload();

    const sheet = await p;
    expect(sheet.frame(0)).toEqual({ sx: 0, sy: 0, sw: 16, sh: 16 });
  });

  it("frame(n) computes correct row/col from sheet dimensions", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    const p = cache.loadSpriteSheet("/test.png", 16, 16);
    const img = imageInstances[0];
    img.width = 64;
    img.height = 32;
    img.onload();

    const sheet = await p;
    // Frame 5 → col=1 (5%4=1), row=1 (5/4=1)
    expect(sheet.frame(5)).toEqual({ sx: 16, sy: 16, sw: 16, sh: 16 });
    // Frame 3 → col=3, row=0
    expect(sheet.frame(3)).toEqual({ sx: 48, sy: 0, sw: 16, sh: 16 });
    // Frame 7 → col=3, row=1
    expect(sheet.frame(7)).toEqual({ sx: 48, sy: 16, sw: 16, sh: 16 });
  });

  it("exposes frameWidth and frameHeight", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    const p = cache.loadSpriteSheet("/test.png", 16, 24);
    const img = imageInstances[0];
    img.width = 64;
    img.height = 48;
    img.onload();

    const sheet = await p;
    expect(sheet.frameWidth).toBe(16);
    expect(sheet.frameHeight).toBe(24);
  });
});

// ---------------------------------------------------------------------------
// Cache operations
// ---------------------------------------------------------------------------

describe("SpriteCache cache operations", () => {
  it("getCached returns null for uncached keys", () => {
    const { windowObj } = makeContext();
    const cache = new windowObj._officeSpriteCache();
    expect(cache.getCached("foo", 3)).toBe(null);
  });

  it("cache + getCached round-trips correctly", () => {
    const { windowObj } = makeContext();
    const cache = new windowObj._officeSpriteCache();
    const canvas = { mock: true };
    cache.cache("char_00:walk:0", 3, canvas);
    expect(cache.getCached("char_00:walk:0", 3)).toBe(canvas);
  });

  it("different zoom levels are separate cache entries", () => {
    const { windowObj } = makeContext();
    const cache = new windowObj._officeSpriteCache();
    const c3 = { zoom: 3 };
    const c4 = { zoom: 4 };
    cache.cache("key", 3, c3);
    cache.cache("key", 4, c4);
    expect(cache.getCached("key", 3)).toBe(c3);
    expect(cache.getCached("key", 4)).toBe(c4);
  });

  it("invalidateZoom clears all entries", () => {
    const { windowObj } = makeContext();
    const cache = new windowObj._officeSpriteCache();
    cache.cache("a", 3, { a: 1 });
    cache.cache("b", 4, { b: 1 });
    cache.invalidateZoom();
    expect(cache.getCached("a", 3)).toBe(null);
    expect(cache.getCached("b", 4)).toBe(null);
  });
});

// ---------------------------------------------------------------------------
// Placeholder fallback
// ---------------------------------------------------------------------------

describe("PlaceholderSheet fallback", () => {
  it("returns PlaceholderSheet when image load fails", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    const p = cache.loadSpriteSheet("/missing.png", 16, 16);
    imageInstances[0].onerror();

    const sheet = await p;
    // PlaceholderSheet always returns the same frame rect.
    expect(sheet.frame(0)).toEqual({ sx: 0, sy: 0, sw: 16, sh: 16 });
    expect(sheet.frame(99)).toEqual({ sx: 0, sy: 0, sw: 16, sh: 16 });
    expect(sheet.frameWidth).toBe(16);
    expect(sheet.frameHeight).toBe(16);
  });

  it("placeholder image is an OffscreenCanvas with fill", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    const p = cache.loadSpriteSheet("/missing.png", 16, 16);
    imageInstances[0].onerror();

    const sheet = await p;
    const img = sheet.image;
    expect(img.width).toBe(16);
    expect(img.height).toBe(16);
    expect(img._draws.length).toBeGreaterThan(0);
    expect(img._draws[0].op).toBe("fillRect");
  });
});

// ---------------------------------------------------------------------------
// assetAvailable
// ---------------------------------------------------------------------------

describe("assetAvailable", () => {
  it("returns false when no PNGs loaded", () => {
    const { windowObj } = makeContext();
    expect(windowObj._officeAssetAvailable()).toBe(false);
  });

  it("returns true after successful load", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    const p = cache.loadSpriteSheet("/char_00.png", 16, 16);
    const img = imageInstances[0];
    img.width = 64;
    img.height = 32;
    img.onload();
    await p;

    expect(windowObj._officeAssetAvailable()).toBe(true);
  });

  it("stays false after only failed loads", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    const p = cache.loadSpriteSheet("/missing.png", 16, 16);
    imageInstances[0].onerror();
    await p;

    expect(windowObj._officeAssetAvailable()).toBe(false);
  });

  it("resetAssetState resets to false", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    const p = cache.loadSpriteSheet("/ok.png", 16, 16);
    const img = imageInstances[0];
    img.width = 32;
    img.height = 16;
    img.onload();
    await p;

    expect(windowObj._officeAssetAvailable()).toBe(true);
    windowObj._officeResetAssetState();
    expect(windowObj._officeAssetAvailable()).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// rasterizeFrame
// ---------------------------------------------------------------------------

describe("rasterizeFrame", () => {
  it("creates OffscreenCanvas at zoom scale", async () => {
    const { windowObj, imageInstances } = makeContext();
    const cache = new windowObj._officeSpriteCache();

    const p = cache.loadSpriteSheet("/test.png", 16, 16);
    const img = imageInstances[0];
    img.width = 64;
    img.height = 32;
    img.onload();

    const sheet = await p;
    const oc = cache.rasterizeFrame(sheet, 0, 3);
    expect(oc.width).toBe(48); // 16 * 3
    expect(oc.height).toBe(48);
    expect(oc._draws[0].op).toBe("drawImage");
  });
});
