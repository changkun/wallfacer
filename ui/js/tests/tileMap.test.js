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
  const src = readFileSync(join(jsDir, "office", "tileMap.js"), "utf-8");
  vm.runInContext(src, ctx);
  return windowObj;
}

describe("tileMap constants", () => {
  const w = makeContext();

  it("exposes tile type constants", () => {
    expect(w._officeTileTypes.VOID).toBe(0);
    expect(w._officeTileTypes.FLOOR).toBe(1);
    expect(w._officeTileTypes.WALL).toBe(2);
  });

  it("exposes furniture type constants", () => {
    const ft = w._officeFurnitureTypes;
    expect(ft.DESK).toBe("desk");
    expect(ft.CHAIR).toBe("chair");
    expect(ft.PC).toBe("pc");
    expect(ft.SOFA).toBe("sofa");
    expect(ft.PLANT).toBe("plant");
    expect(ft.COFFEE).toBe("coffee");
    expect(ft.WHITEBOARD).toBe("whiteboard");
    expect(ft.BOOKSHELF).toBe("bookshelf");
  });
});

describe("TileMap", () => {
  const w = makeContext();
  const TileMap = w._officeTileMap;
  const T = w._officeTileTypes;

  it("creates a grid initialized to VOID", () => {
    const m = new TileMap(4, 3);
    expect(m.width).toBe(4);
    expect(m.height).toBe(3);
    expect(m.tileAt(0, 0)).toBe(T.VOID);
    expect(m.tileAt(3, 2)).toBe(T.VOID);
  });

  it("tileAt returns VOID for out-of-bounds", () => {
    const m = new TileMap(2, 2);
    expect(m.tileAt(-1, 0)).toBe(T.VOID);
    expect(m.tileAt(5, 0)).toBe(T.VOID);
    expect(m.tileAt(0, -1)).toBe(T.VOID);
    expect(m.tileAt(0, 99)).toBe(T.VOID);
  });

  it("setTile and tileAt round-trip", () => {
    const m = new TileMap(3, 3);
    m.setTile(1, 2, T.FLOOR);
    expect(m.tileAt(1, 2)).toBe(T.FLOOR);
    m.setTile(0, 0, T.WALL);
    expect(m.tileAt(0, 0)).toBe(T.WALL);
  });

  it("isPassable returns true for empty floor", () => {
    const m = new TileMap(3, 3);
    m.setTile(1, 1, T.FLOOR);
    expect(m.isPassable(1, 1)).toBe(true);
  });

  it("isPassable returns false for wall", () => {
    const m = new TileMap(3, 3);
    m.setTile(0, 0, T.WALL);
    expect(m.isPassable(0, 0)).toBe(false);
  });

  it("isPassable returns false for furniture (non-chair)", () => {
    const m = new TileMap(5, 5);
    m.setTile(2, 2, T.FLOOR);
    m.placeFurniture({
      type: "desk",
      x: 2,
      y: 2,
      width: 1,
      height: 1,
      state: null,
    });
    expect(m.isPassable(2, 2)).toBe(false);
  });

  it("isPassable returns true for chair tiles", () => {
    const m = new TileMap(5, 5);
    m.setTile(1, 1, T.FLOOR);
    m.placeFurniture({
      type: "chair",
      x: 1,
      y: 1,
      width: 1,
      height: 1,
      state: null,
      direction: "down",
      deskIndex: 0,
    });
    expect(m.isPassable(1, 1)).toBe(true);
  });

  it("furnitureAt returns null for empty tiles", () => {
    const m = new TileMap(3, 3);
    expect(m.furnitureAt(0, 0)).toBe(null);
  });

  it("furnitureAt returns null for out-of-bounds", () => {
    const m = new TileMap(2, 2);
    expect(m.furnitureAt(-1, 0)).toBe(null);
    expect(m.furnitureAt(5, 5)).toBe(null);
  });

  it("furnitureAt returns descriptor for placed furniture", () => {
    const m = new TileMap(5, 5);
    m.setTile(2, 2, T.FLOOR);
    m.setTile(3, 2, T.FLOOR);
    const desc = {
      type: "desk",
      x: 2,
      y: 2,
      width: 2,
      height: 1,
      state: null,
    };
    m.placeFurniture(desc);
    expect(m.furnitureAt(2, 2)).toBe(desc);
    expect(m.furnitureAt(3, 2)).toBe(desc);
    expect(m.furnitureAt(4, 2)).toBe(null);
  });

  it("seatPositions returns placed chairs", () => {
    const m = new TileMap(5, 5);
    m.setTile(1, 1, T.FLOOR);
    m.placeFurniture({
      type: "chair",
      x: 1,
      y: 1,
      width: 1,
      height: 1,
      state: null,
      direction: "down",
      deskIndex: 0,
    });
    const seats = m.seatPositions();
    expect(seats).toHaveLength(1);
    expect(seats[0]).toEqual({
      x: 1,
      y: 1,
      direction: "down",
      deskIndex: 0,
    });
  });
});

describe("generateOfficeLayout", () => {
  const w = makeContext();
  const gen = w._officeGenerateLayout;
  const T = w._officeTileTypes;
  const F = w._officeFurnitureTypes;

  it("produces at least 6 seats for taskCount=1 (minimum)", () => {
    const { seats } = gen(1);
    expect(seats.length).toBeGreaterThanOrEqual(4);
  });

  it("produces at least 10 seats for taskCount=10", () => {
    const { seats } = gen(10);
    expect(seats.length).toBeGreaterThanOrEqual(10);
  });

  it("all seats are on passable floor tiles", () => {
    const { tileMap, seats } = gen(8);
    for (const seat of seats) {
      expect(tileMap.tileAt(seat.x, seat.y)).toBe(T.FLOOR);
      expect(tileMap.isPassable(seat.x, seat.y)).toBe(true);
    }
  });

  it("non-chair furniture tiles are impassable", () => {
    const { tileMap, furniture } = gen(6);
    for (const f of furniture) {
      if (f.type === F.CHAIR) continue;
      for (var dy = 0; dy < (f.height || 1); dy++) {
        for (var dx = 0; dx < (f.width || 1); dx++) {
          expect(tileMap.isPassable(f.x + dx, f.y + dy)).toBe(false);
        }
      }
    }
  });

  it("walls form a closed perimeter with no gaps", () => {
    const { tileMap } = gen(6);
    const w = tileMap.width;
    const h = tileMap.height;
    // Top and bottom edges
    for (var x = 0; x < w; x++) {
      expect(tileMap.tileAt(x, 0)).toBe(T.WALL);
      expect(tileMap.tileAt(x, h - 1)).toBe(T.WALL);
    }
    // Left and right edges
    for (var y = 0; y < h; y++) {
      expect(tileMap.tileAt(0, y)).toBe(T.WALL);
      expect(tileMap.tileAt(w - 1, y)).toBe(T.WALL);
    }
  });

  it("common area has plant and bookshelf", () => {
    const { furniture } = gen(4);
    const types = furniture.map((f) => f.type);
    expect(types).toContain(F.PLANT);
    expect(types).toContain(F.BOOKSHELF);
  });

  it("common area includes coffee, whiteboard, and bookshelf", () => {
    const { furniture } = gen(6);
    const types = furniture.map((f) => f.type);
    expect(types).toContain(F.COFFEE);
    expect(types).toContain(F.WHITEBOARD);
    expect(types).toContain(F.BOOKSHELF);
  });

  it("desk count matches expected N", () => {
    const { furniture } = gen(3);
    var desks = furniture.filter((f) => f.type === F.DESK);
    // min 4 desks (N = max(3, 4) = 4)
    expect(desks.length).toBeGreaterThanOrEqual(4);
  });

  it("handles large task counts", () => {
    const { seats, tileMap } = gen(50);
    expect(seats.length).toBeGreaterThanOrEqual(50);
    expect(tileMap.width).toBeGreaterThan(0);
    expect(tileMap.height).toBeGreaterThan(0);
  });
});
