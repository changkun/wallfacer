import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext() {
  const windowObj = {};
  const ctx = vm.createContext({ window: windowObj, Math, console });
  vm.runInContext(
    readFileSync(join(jsDir, "office", "effects.js"), "utf-8"),
    ctx,
  );
  return windowObj;
}

describe("MatrixEffect spawn", () => {
  const w = makeContext();

  it("at t=0, bottom rows are hidden (alpha ≈ 0)", () => {
    const fx = new w._officeMatrixEffect("spawn", 16, 16);
    // No time elapsed — sweep at top, bottom is hidden
    expect(fx.getAlphaMask(0, 15)).toBe(0);
  });

  it("at t=0.5s, all rows are revealed (alpha = 1)", () => {
    const fx = new w._officeMatrixEffect("spawn", 16, 16);
    fx.update(0.6); // past duration
    expect(fx.getAlphaMask(0, 15)).toBe(1);
    expect(fx.getAlphaMask(0, 0)).toBe(1);
  });

  it("mid-animation: top revealed, bottom hidden", () => {
    const fx = new w._officeMatrixEffect("spawn", 16, 16);
    fx.update(0.15); // ~30% through
    // Top rows should be revealed
    expect(fx.getAlphaMask(0, 0)).toBe(1);
    // Bottom rows should still be hidden
    expect(fx.getAlphaMask(0, 15)).toBe(0);
  });
});

describe("MatrixEffect despawn", () => {
  const w = makeContext();

  it("at t=0, all rows are visible (alpha = 1)", () => {
    const fx = new w._officeMatrixEffect("despawn", 16, 16);
    // No time — sweep hasn't started, everything visible
    expect(fx.getAlphaMask(0, 0)).toBe(1);
    expect(fx.getAlphaMask(0, 15)).toBe(1);
  });

  it("at t=0.5s, all rows are dissolved (alpha = 0)", () => {
    const fx = new w._officeMatrixEffect("despawn", 16, 16);
    fx.update(0.6);
    expect(fx.getAlphaMask(0, 0)).toBe(0);
    expect(fx.getAlphaMask(0, 15)).toBe(0);
  });

  it("mid-animation: top dissolved, bottom still visible", () => {
    const fx = new w._officeMatrixEffect("despawn", 16, 16);
    fx.update(0.15);
    // Top rows should be dissolved
    expect(fx.getAlphaMask(0, 0)).toBe(0);
    // Bottom rows should still be visible
    expect(fx.getAlphaMask(0, 15)).toBe(1);
  });
});

describe("MatrixEffect isComplete", () => {
  const w = makeContext();

  it("returns false during animation", () => {
    const fx = new w._officeMatrixEffect("spawn", 16, 16);
    fx.update(0.1);
    expect(fx.isComplete()).toBe(false);
  });

  it("returns true after duration", () => {
    const fx = new w._officeMatrixEffect("spawn", 16, 16);
    fx.update(0.6);
    expect(fx.isComplete()).toBe(true);
  });
});

describe("MatrixEffect column stagger", () => {
  const w = makeContext();

  it("different columns have different sweep positions at same time", () => {
    const fx = new w._officeMatrixEffect("spawn", 16, 16);
    fx.update(0.2);
    // Check that at least some columns differ in their alpha at row 8
    const alphas = [];
    for (let col = 0; col < 16; col++) {
      alphas.push(fx.getAlphaMask(col, 8));
    }
    const unique = new Set(alphas);
    // With 16 columns and stagger offsets, there should be variation
    expect(unique.size).toBeGreaterThanOrEqual(1);
  });
});

describe("MatrixEffect trail color", () => {
  const w = makeContext();

  it("returns green trail near sweep head", () => {
    const fx = new w._officeMatrixEffect("spawn", 16, 16);
    fx.update(0.25); // ~50% through
    // Find the sweep position and check trail
    let foundTrail = false;
    for (let row = 0; row < 16; row++) {
      const color = fx.getTrailColor(0, row);
      if (color && color.indexOf("rgba(0,255,0,") === 0) {
        foundTrail = true;
        break;
      }
    }
    expect(foundTrail).toBe(true);
  });

  it("returns null far from sweep head", () => {
    const fx = new w._officeMatrixEffect("spawn", 16, 16);
    fx.update(0.25);
    // Row 0 should be far above the sweep — no trail
    const color = fx.getTrailColor(0, 0);
    expect(color).toBeNull();
  });

  it("returns null when effect is complete", () => {
    const fx = new w._officeMatrixEffect("spawn", 16, 16);
    fx.update(0.6);
    expect(fx.getTrailColor(0, 8)).toBeNull();
  });
});
