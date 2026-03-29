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

  var CLUSTER_W = 8; // chair + desk(2) + gap(2) + desk(2) + chair
  var CLUSTER_H = 2; // two facing rows
  var CLUSTER_GAP_Y = 2; // vertical gap between clusters
  var WALL_PAD = 1; // wall thickness
  var INTERIOR_PAD = 1; // floor padding inside walls
  var COMMON_AREA_H = 3; // height of common area at bottom

  function generateOfficeLayout(taskCount) {
    var N = Math.max(taskCount, 6);
    // Each cluster has 4 desks (2 per row, 2 rows)
    var clusterCount = Math.ceil(N / 4);
    var desksPerRow = 2; // clusters per row
    var clusterRows = Math.ceil(clusterCount / desksPerRow);

    // Compute interior dimensions
    var interiorW = desksPerRow * CLUSTER_W + (desksPerRow - 1) * 2;
    var interiorH =
      clusterRows * CLUSTER_H +
      (clusterRows - 1) * CLUSTER_GAP_Y +
      1 + // gap before common area
      COMMON_AREA_H;

    // Total map size including walls and padding
    var totalW = interiorW + 2 * (WALL_PAD + INTERIOR_PAD);
    var totalH = interiorH + 2 * (WALL_PAD + INTERIOR_PAD);

    var map = new TileMap(totalW, totalH);

    // Fill walls around border
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

    var originX = WALL_PAD + INTERIOR_PAD;
    var originY = WALL_PAD + INTERIOR_PAD;

    // Place desk clusters
    var deskIndex = 0;
    for (var cr = 0; cr < clusterRows; cr++) {
      for (var cc = 0; cc < desksPerRow; cc++) {
        if (deskIndex >= N) break;
        var cx = originX + cc * (CLUSTER_W + 2);
        var cy = originY + cr * (CLUSTER_H + CLUSTER_GAP_Y);
        deskIndex = placeCluster(map, cx, cy, deskIndex, N);
      }
    }

    // Place common area at bottom
    var commonY =
      originY + clusterRows * CLUSTER_H + (clusterRows - 1) * CLUSTER_GAP_Y + 1;
    placeCommonArea(map, originX, commonY, interiorW);

    return {
      tileMap: map,
      furniture: map._furniture.slice(),
      seats: map.seatPositions(),
    };
  }

  function placeCluster(map, cx, cy, startIndex, maxDesks) {
    var idx = startIndex;
    // Top row (facing down): left pair
    if (idx < maxDesks) {
      placeDeskUnit(map, cx, cy, "down", idx);
      idx++;
    }
    // Top row (facing down): right pair
    if (idx < maxDesks) {
      placeDeskUnit(map, cx + 5, cy, "down", idx);
      idx++;
    }
    // Bottom row (facing up): left pair
    if (idx < maxDesks) {
      placeDeskUnit(map, cx, cy + 1, "up", idx);
      idx++;
    }
    // Bottom row (facing up): right pair
    if (idx < maxDesks) {
      placeDeskUnit(map, cx + 5, cy + 1, "up", idx);
      idx++;
    }
    return idx;
  }

  // Place one desk unit: chair + desk(2 tiles) + PC
  // For "down" facing: chair at x, desk at x+1..x+2, PC at inner edge
  // For "up" facing: desk at x+1..x+2, chair at x
  function placeDeskUnit(map, x, y, direction, deskIndex) {
    // Chair position
    var chairX = x;
    map.placeFurniture({
      type: CHAIR,
      x: chairX,
      y: y,
      width: 1,
      height: 1,
      state: null,
      direction: direction,
      deskIndex: deskIndex,
    });

    // Desk (2 tiles wide)
    map.placeFurniture({
      type: DESK,
      x: x + 1,
      y: y,
      width: 2,
      height: 1,
      state: null,
    });

    // PC on the desk tile closest to center gap
    map.placeFurniture({
      type: PC,
      x: x + 2,
      y: y,
      width: 1,
      height: 1,
      state: "off",
    });
  }

  function placeCommonArea(map, ox, oy, areaW) {
    // Sofa (2 tiles wide)
    map.placeFurniture({
      type: SOFA,
      x: ox,
      y: oy,
      width: 2,
      height: 1,
      state: null,
    });

    // Plant
    map.placeFurniture({
      type: PLANT,
      x: ox + 3,
      y: oy,
      width: 1,
      height: 1,
      state: null,
    });

    // Coffee machine
    map.placeFurniture({
      type: COFFEE,
      x: ox + 5,
      y: oy,
      width: 1,
      height: 1,
      state: null,
    });

    // Whiteboard (2 tiles wide) on the next row
    if (oy + 1 < map.height - WALL_PAD) {
      map.placeFurniture({
        type: WHITEBOARD,
        x: ox,
        y: oy + 1,
        width: 2,
        height: 1,
        state: null,
      });
    }

    // Bookshelf
    if (oy + 1 < map.height - WALL_PAD) {
      map.placeFurniture({
        type: BOOKSHELF,
        x: ox + 3,
        y: oy + 1,
        width: 1,
        height: 1,
        state: null,
      });
    }
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
