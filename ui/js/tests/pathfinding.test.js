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

  const tileMapSrc = readFileSync(join(jsDir, "office", "tileMap.js"), "utf-8");
  vm.runInContext(tileMapSrc, ctx);

  const pfSrc = readFileSync(join(jsDir, "office", "pathfinding.js"), "utf-8");
  vm.runInContext(pfSrc, ctx);

  return windowObj;
}

/** Build a simple open grid with walls around the border. */
function makeGrid(w, width, height) {
  const TileMap = w._officeTileMap;
  const T = w._officeTileTypes;
  const map = new TileMap(width, height);
  for (let y = 0; y < height; y++) {
    for (let x = 0; x < width; x++) {
      if (x === 0 || x === width - 1 || y === 0 || y === height - 1) {
        map.setTile(x, y, T.WALL);
      } else {
        map.setTile(x, y, T.FLOOR);
      }
    }
  }
  return map;
}

describe("findPath", () => {
  const w = makeContext();

  it("finds straight path on open 5x5 grid", () => {
    const map = makeGrid(w, 5, 5);
    const path = w._officeFindPath(map, 1, 1, 3, 1);
    expect(path).not.toBeNull();
    expect(path.length).toBe(3);
    expect(path[0]).toEqual({ x: 1, y: 1 });
    expect(path[2]).toEqual({ x: 3, y: 1 });
  });

  it("routes around an obstacle", () => {
    const map = makeGrid(w, 5, 5);
    // Block (2,1) with furniture
    map.placeFurniture({
      type: "desk",
      x: 2,
      y: 1,
      width: 1,
      height: 1,
      state: null,
    });

    const path = w._officeFindPath(map, 1, 1, 3, 1);
    expect(path).not.toBeNull();
    // Path must go around: cannot go through (2,1)
    for (const step of path) {
      expect(step.x === 2 && step.y === 1).toBe(false);
    }
    expect(path[0]).toEqual({ x: 1, y: 1 });
    expect(path[path.length - 1]).toEqual({ x: 3, y: 1 });
  });

  it("returns null when fully blocked", () => {
    const map = makeGrid(w, 5, 5);
    // Block entire row y=2 (interior is x=1..3)
    for (let x = 1; x <= 3; x++) {
      map.placeFurniture({
        type: "desk",
        x,
        y: 2,
        width: 1,
        height: 1,
        state: null,
      });
    }
    const path = w._officeFindPath(map, 1, 1, 1, 3);
    expect(path).toBeNull();
  });

  it("extraPassable allows routing through a blocked tile", () => {
    const map = makeGrid(w, 5, 5);
    map.placeFurniture({
      type: "desk",
      x: 2,
      y: 1,
      width: 1,
      height: 1,
      state: null,
    });

    // Without extra: routes around
    const pathAround = w._officeFindPath(map, 1, 1, 3, 1);
    expect(pathAround.length).toBeGreaterThan(3);

    // With extra: can go through (2,1)
    const extra = new Set(["2,1"]);
    const pathThrough = w._officeFindPath(map, 1, 1, 3, 1, extra);
    expect(pathThrough).not.toBeNull();
    expect(pathThrough.length).toBe(3);
  });

  it("includes both start and goal positions", () => {
    const map = makeGrid(w, 5, 5);
    const path = w._officeFindPath(map, 1, 1, 3, 3);
    expect(path[0]).toEqual({ x: 1, y: 1 });
    expect(path[path.length - 1]).toEqual({ x: 3, y: 3 });
  });

  it("has no diagonal moves", () => {
    const map = makeGrid(w, 7, 7);
    const path = w._officeFindPath(map, 1, 1, 5, 5);
    expect(path).not.toBeNull();
    for (let i = 1; i < path.length; i++) {
      const dx = Math.abs(path[i].x - path[i - 1].x);
      const dy = Math.abs(path[i].y - path[i - 1].y);
      expect(dx + dy).toBe(1);
    }
  });

  it("returns single-step path when start equals goal", () => {
    const map = makeGrid(w, 5, 5);
    const path = w._officeFindPath(map, 2, 2, 2, 2);
    expect(path).toEqual([{ x: 2, y: 2 }]);
  });
});

describe("randomPassableTile", () => {
  const w = makeContext();

  it("returns a passable tile", () => {
    const map = makeGrid(w, 5, 5);
    const tile = w._officeRandomPassableTile(map);
    expect(tile).not.toBeNull();
    expect(map.isPassable(tile.x, tile.y)).toBe(true);
  });

  it("returns null on fully blocked map", () => {
    const TileMap = w._officeTileMap;
    const T = w._officeTileTypes;
    const map = new TileMap(3, 3);
    for (let y = 0; y < 3; y++)
      for (let x = 0; x < 3; x++) map.setTile(x, y, T.WALL);
    const tile = w._officeRandomPassableTile(map);
    expect(tile).toBeNull();
  });
});
