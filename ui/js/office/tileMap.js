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

  // Layout: horizontal row of workstations. Each station is 3 tiles wide.
  // The character (16x32) is drawn with feet at tile bottom, head extends up.
  // Layout rows (from top):
  //   row 0: head room (empty floor for character heads)
  //   row 1: head room 2 (for 32px character sprites)
  //   row 2: desk + monitor on desk
  //   row 3: character seat (chair behind character)
  //   row 4: floor padding
  var STATION_W = 4;
  var STATION_GAP = 1;
  var WALL_PAD = 1;
  var INTERIOR_PAD = 1;
  var COMMON_W = 3;

  function generateOfficeLayout(taskCount) {
    var N = Math.max(taskCount, 4);

    var stationsW = N * STATION_W + (N - 1) * STATION_GAP;
    var interiorW = COMMON_W + 1 + stationsW;
    var interiorH = 5; // 2 head room + desk + seat + floor

    var totalW = interiorW + 2 * (WALL_PAD + INTERIOR_PAD);
    var totalH = interiorH + 2 * (WALL_PAD + INTERIOR_PAD);

    var map = new TileMap(totalW, totalH);

    for (var y = 0; y < totalH; y++) {
      for (var x = 0; x < totalW; x++) {
        if (
          x < WALL_PAD ||
          x >= totalW - WALL_PAD ||
          y < WALL_PAD ||
          y >= totalH - WALL_PAD
        ) {
          map.setTile(x, y, WALL);
        } else {
          map.setTile(x, y, FLOOR);
        }
      }
    }

    var ox = WALL_PAD + INTERIOR_PAD;
    var oy = WALL_PAD + INTERIOR_PAD;

    // Common area on the left
    placeCommonArea(map, ox, oy + 2);

    // Workstations: desk at row 2, seat at row 3
    var stationX = ox + COMMON_W + 1;
    for (var i = 0; i < N; i++) {
      var sx = stationX + i * (STATION_W + STATION_GAP);
      placeStation(map, sx, oy, i);
    }

    return {
      tileMap: map,
      furniture: map._furniture.slice(),
      seats: map.seatPositions(),
    };
  }

  function placeStation(map, x, y, deskIndex) {
    // y+1..y+2: Desk (2x2 with legs)
    map.placeFurniture({
      type: DESK,
      x: x,
      y: y + 1,
      width: 2,
      height: 2,
      state: null,
    });
    // PC screen on top-right of desk
    map.placeFurniture({
      type: PC,
      x: x + 1,
      y: y + 1,
      width: 1,
      height: 1,
      state: "off",
    });
    // y+3: Seat position (character sits here; chair sprite rendered at y+2..y+3)
    map.placeFurniture({
      type: CHAIR,
      x: x + 2,
      y: y + 2,
      width: 1,
      height: 2,
      state: null,
      direction: "left",
      deskIndex: deskIndex,
    });
  }

  function placeCommonArea(map, ox, oy) {
    // Sofa (2x2 grey couch)
    map.placeFurniture({
      type: SOFA,
      x: ox,
      y: oy - 1,
      width: 2,
      height: 2,
      state: null,
    });
    // Bookshelf (1x2 tall)
    map.placeFurniture({
      type: BOOKSHELF,
      x: ox + 2,
      y: oy - 1,
      width: 1,
      height: 2,
      state: null,
    });
    // Small plant (1x1)
    map.placeFurniture({
      type: PLANT,
      x: ox,
      y: oy + 1,
      width: 1,
      height: 1,
      state: null,
    });
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
