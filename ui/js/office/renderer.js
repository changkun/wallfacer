(function () {
  "use strict";

  var T = window._officeTileTypes;
  var TILE = 16; // pixels per tile at 1x zoom

  // Placeholder colors for furniture types when using PlaceholderSheet.
  var FURNITURE_COLORS = {
    desk: "#8B7355",
    chair: "#5C4033",
    pc: "#2C3E50",
    sofa: "#6A5ACD",
    plant: "#2E8B57",
    coffee: "#8B4513",
    whiteboard: "#E8E8E8",
    bookshelf: "#A0522D",
  };

  var FLOOR_COLOR = "#E8DCC8";
  var WALL_COLOR = "#6B6B6B";

  // ---- OfficeRenderer ----

  function OfficeRenderer(canvas, spriteCache, camera) {
    this._canvas = canvas;
    this._ctx = canvas.getContext("2d");
    this._ctx.imageSmoothingEnabled = false;
    this._spriteCache = spriteCache;
    this._camera = camera;

    this._tileMap = null;
    this._furniture = [];
    this._seats = [];
    this._drawables = []; // reusable array for z-sort

    this._floorCanvas = null;
    this._floorDirty = true;

    this._rafId = null;
    this._running = false;
    this._boundRender = this.render.bind(this);
  }

  OfficeRenderer.prototype.setLayout = function (tileMap, furniture, seats) {
    this._tileMap = tileMap;
    this._furniture = furniture;
    this._seats = seats;
    this._floorDirty = true;
  };

  OfficeRenderer.prototype.invalidateFloorCache = function () {
    this._floorDirty = true;
  };

  OfficeRenderer.prototype.start = function () {
    if (this._running) return;
    this._running = true;
    this._rafId = requestAnimationFrame(this._boundRender);
  };

  OfficeRenderer.prototype.stop = function () {
    this._running = false;
    if (this._rafId !== null) {
      cancelAnimationFrame(this._rafId);
      this._rafId = null;
    }
  };

  OfficeRenderer.prototype.render = function (timestamp) {
    if (!this._running) return;

    var ctx = this._ctx;
    var cam = this._camera;
    var zoom = cam.zoom;

    // Clear
    ctx.clearRect(0, 0, this._canvas.width, this._canvas.height);

    if (!this._tileMap) {
      this._rafId = requestAnimationFrame(this._boundRender);
      return;
    }

    ctx.save();
    ctx.translate(-cam.x * zoom, -cam.y * zoom);
    ctx.scale(zoom, zoom);

    // Draw floor layer (cached)
    this._drawFloor(ctx);

    // Collect drawables, z-sort, and draw
    this._drawFurniture(ctx);

    ctx.restore();

    this._rafId = requestAnimationFrame(this._boundRender);
  };

  OfficeRenderer.prototype._drawFloor = function (ctx) {
    var map = this._tileMap;
    if (this._floorDirty || !this._floorCanvas) {
      var w = map.width * TILE;
      var h = map.height * TILE;
      this._floorCanvas = new OffscreenCanvas(w, h);
      var fctx = this._floorCanvas.getContext("2d");
      fctx.imageSmoothingEnabled = false;

      for (var y = 0; y < map.height; y++) {
        for (var x = 0; x < map.width; x++) {
          var tile = map.tileAt(x, y);
          if (tile === T.FLOOR) {
            fctx.fillStyle = FLOOR_COLOR;
            fctx.fillRect(x * TILE, y * TILE, TILE, TILE);
          } else if (tile === T.WALL) {
            fctx.fillStyle = WALL_COLOR;
            fctx.fillRect(x * TILE, y * TILE, TILE, TILE);
          }
          // VOID tiles are left transparent.
        }
      }
      this._floorDirty = false;
    }
    ctx.drawImage(this._floorCanvas, 0, 0);
  };

  OfficeRenderer.prototype._drawFurniture = function (ctx) {
    var drawables = this._drawables;
    drawables.length = 0;

    for (var i = 0; i < this._furniture.length; i++) {
      drawables.push(this._furniture[i]);
    }

    // Z-sort by bottom edge (y + height) ascending
    drawables.sort(function (a, b) {
      var aBottom = a.y + (a.height || 1);
      var bBottom = b.y + (b.height || 1);
      return aBottom - bBottom;
    });

    for (var d = 0; d < drawables.length; d++) {
      var f = drawables[d];
      var fw = (f.width || 1) * TILE;
      var fh = (f.height || 1) * TILE;
      var fx = f.x * TILE;
      var fy = f.y * TILE;

      // Placeholder rendering: colored rectangle with type label
      var color = FURNITURE_COLORS[f.type] || "#999";
      ctx.fillStyle = color;
      ctx.fillRect(fx, fy, fw, fh);

      // Tiny label for identification
      ctx.fillStyle = "#FFF";
      ctx.font = "3px sans-serif";
      ctx.textAlign = "center";
      ctx.textBaseline = "middle";
      var label = f.type.charAt(0).toUpperCase();
      ctx.fillText(label, fx + fw / 2, fy + fh / 2);
    }
  };

  // ---- Exports ----

  window._officeRenderer = OfficeRenderer;
  window._officeTileSize = TILE;
})();
