(function () {
  "use strict";

  // ---- Tile type constants ----
  var VOID = 0;
  var FLOOR = 1;
  var WALL = 2;

  // ---- Furniture type constants ----
  var DESK = "desk";
  var CHAIR = "chair";
  var PC = "pc";
  var SOFA = "sofa";
  var PLANT = "plant";
  var COFFEE = "coffee";
  var WHITEBOARD = "whiteboard";
  var BOOKSHELF = "bookshelf";

  // ---- TileMap ----

  function TileMap(width, height) {
    this.width = width;
    this.height = height;
    this._grid = [];
    for (var y = 0; y < height; y++) {
      var row = [];
      for (var x = 0; x < width; x++) {
        row.push(VOID);
      }
      this._grid.push(row);
    }
    this._furniture = [];
    this._furnitureGrid = [];
    for (var fy = 0; fy < height; fy++) {
      var frow = [];
      for (var fx = 0; fx < width; fx++) {
        frow.push(null);
      }
      this._furnitureGrid.push(frow);
    }
    this._seats = [];
  }

  TileMap.prototype.tileAt = function (x, y) {
    if (x < 0 || x >= this.width || y < 0 || y >= this.height) return VOID;
    return this._grid[y][x];
  };

  TileMap.prototype.setTile = function (x, y, type) {
    if (x < 0 || x >= this.width || y < 0 || y >= this.height) return;
    this._grid[y][x] = type;
  };

  TileMap.prototype.isPassable = function (x, y) {
    if (this.tileAt(x, y) !== FLOOR) return false;
    var f = this._furnitureGrid[y] && this._furnitureGrid[y][x];
    if (f && f.type !== CHAIR) return false;
    return true;
  };

  TileMap.prototype.furnitureAt = function (x, y) {
    if (x < 0 || x >= this.width || y < 0 || y >= this.height) return null;
    return this._furnitureGrid[y][x];
  };

  TileMap.prototype.placeFurniture = function (desc) {
    this._furniture.push(desc);
    var w = desc.width || 1;
    var h = desc.height || 1;
    for (var dy = 0; dy < h; dy++) {
      for (var dx = 0; dx < w; dx++) {
        var fx = desc.x + dx;
        var fy = desc.y + dy;
        if (fx >= 0 && fx < this.width && fy >= 0 && fy < this.height) {
          this._furnitureGrid[fy][fx] = desc;
        }
      }
    }
    if (desc.type === CHAIR && desc.deskIndex !== undefined) {
      this._seats.push({
        x: desc.x,
        y: desc.y,
        direction: desc.direction || "down",
        deskIndex: desc.deskIndex,
      });
    }
  };

  TileMap.prototype.seatPositions = function () {
    return this._seats.slice();
  };

  // ---- Layout algorithm ----

  // A desk cluster is 4 desks arranged in 2 facing rows:
  //
  //   C D D . D D C      (row 0: chair, desk, desk, gap, desk, desk, chair)
  //   . P .   . P .      (row 1: PCs on desks)  -- PCs share desk tiles
  //   . P .   . P .      (row 2: PCs on desks)
  //   C D D . D D C      (row 3: chair, desk, desk, gap, desk, desk, chair)
  //
  // Simplified: each desk-pair occupies a horizontal band.
  // Top row faces down, bottom row faces up.
  //
  // Actual layout per cluster (width=8, height=2):
  //   Row 0: chair(facing down) desk desk gap desk desk chair(facing down)
  //   Row 1: chair(facing up)   desk desk gap desk desk chair(facing up)
  //
  // PCs are placed on the desk tiles closest to the gap.

  // Layout: single horizontal row of workstations with common area on left.
  // Each workstation = desk(2w) + PC(1w on desk) + chair(1w) = 3 tiles wide.
  // Workstations are arranged in a single row facing down.
  var STATION_W = 3; // chair + desk(2) 
  var STATION_GAP = 1; // gap between stations
  var WALL_PAD = 1;
  var INTERIOR_PAD = 1;
  var COMMON_W = 4; // common area width on left (sofa, plant, etc.)

  function generateOfficeLayout(taskCount) {
    var N = Math.max(taskCount, 4);

    // Interior: common area on left + stations in a row
    var stationsW = N * STATION_W + (N - 1) * STATION_GAP;
    var interiorW = COMMON_W + 1 + stationsW; // +1 gap between common and stations
    var interiorH = 5; // head room + desk(2h) + chair + floor below

    var totalW = interiorW + 2 * (WALL_PAD + INTERIOR_PAD);
    var totalH = interiorH + 2 * (WALL_PAD + INTERIOR_PAD);

    var map = new TileMap(totalW, totalH);

    // Fill walls and floor
    for (var y = 0; y < totalH; y++) {
      for (var x = 0; x < totalW; x++) {
        if (x < WALL_PAD || x >= totalW - WALL_PAD ||
            y < WALL_PAD || y >= totalH - WALL_PAD) {
          map.setTile(x, y, WALL);
        } else {
          map.setTile(x, y, FLOOR);
        }
      }
    }

    var ox = WALL_PAD + INTERIOR_PAD;
    var oy = WALL_PAD + INTERIOR_PAD;

    // Common area on the left (shifted down 1 for character head room)
    placeCommonArea(map, ox, oy + 1);

    // Workstations in a row, starting after common area
    var stationX = ox + COMMON_W + 1;
    for (var i = 0; i < N; i++) {
      var sx = stationX + i * (STATION_W + STATION_GAP);
      placeStation(map, sx, oy + 1, i);
    }

    return {
      tileMap: map,
      furniture: map._furniture.slice(),
      seats: map.seatPositions(),
    };
  }

  function placeStation(map, x, y, deskIndex) {
    // Row layout (top to bottom):
    // y+0: PC/monitor (1x1) — visually behind the desk
    // y+1: Desk (2x1) — single row desk
    // y+2: Chair (facing up toward desk)
    map.placeFurniture({ type: PC, x: x, y: y, width: 1, height: 1, state: "off" });
    map.placeFurniture({ type: DESK, x: x, y: y + 1, width: 2, height: 1, state: null });
    map.placeFurniture({
      type: CHAIR, x: x, y: y + 2, width: 1, height: 1, state: null,
      direction: "up", deskIndex: deskIndex,
    });
  }

  function placeCommonArea(map, ox, oy) {
    // Plant (1x2 tall potted plant)
    map.placeFurniture({ type: PLANT, x: ox, y: oy, width: 1, height: 2, state: null });
    // Bookshelf (1x2 tall)
    map.placeFurniture({ type: BOOKSHELF, x: ox + 1, y: oy, width: 1, height: 2, state: null });
    // Whiteboard (2x1 wide)
    map.placeFurniture({ type: WHITEBOARD, x: ox + 2, y: oy, width: 2, height: 1, state: null });
    // Coffee machine (1x1)
    map.placeFurniture({ type: COFFEE, x: ox + 2, y: oy + 1, width: 1, height: 1, state: null });
  }

  // ---- Exports ----

  window._officeTileTypes = { VOID: VOID, FLOOR: FLOOR, WALL: WALL };
  window._officeFurnitureTypes = {
    DESK: DESK,
    CHAIR: CHAIR,
    PC: PC,
    SOFA: SOFA,
    PLANT: PLANT,
    COFFEE: COFFEE,
    WHITEBOARD: WHITEBOARD,
    BOOKSHELF: BOOKSHELF,
  };
  window._officeTileMap = TileMap;
  window._officeGenerateLayout = generateOfficeLayout;
})();
