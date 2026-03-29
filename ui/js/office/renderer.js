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

  var CHARACTER_COLOR = "#4A90D9";

  function OfficeRenderer(canvas, spriteCache, camera) {
    this._canvas = canvas;
    this._ctx = canvas.getContext("2d");
    this._ctx.imageSmoothingEnabled = false;
    this._spriteCache = spriteCache;
    this._camera = camera;
    this._characterManager = null;

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

    // Collect drawables (furniture + characters), z-sort, and draw
    this._drawScene(ctx);

    // Overlay pass: speech bubbles (always on top)
    this._drawBubbles(ctx);

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

  OfficeRenderer.prototype.setCharacterManager = function (mgr) {
    this._characterManager = mgr;
  };

  OfficeRenderer.prototype._drawScene = function (ctx) {
    var drawables = this._drawables;
    drawables.length = 0;

    // Add furniture
    for (var i = 0; i < this._furniture.length; i++) {
      var f = this._furniture[i];
      drawables.push({
        _isChar: false,
        x: f.x,
        y: f.y,
        width: f.width || 1,
        height: f.height || 1,
        type: f.type,
      });
    }

    // Add characters
    if (this._characterManager) {
      var chars = this._characterManager.getDrawables();
      for (var c = 0; c < chars.length; c++) {
        var ch = chars[c];
        drawables.push({
          _isChar: true,
          x: ch.x,
          y: ch.y,
          width: 1,
          height: 1,
          _charInfo: ch,
        });
      }
    }

    // Z-sort by bottom edge (y + height) ascending
    drawables.sort(function (a, b) {
      var aBottom = a.y + a.height;
      var bBottom = b.y + b.height;
      return aBottom - bBottom;
    });

    for (var d = 0; d < drawables.length; d++) {
      var item = drawables[d];
      if (item._isChar) {
        this._drawCharacter(ctx, item._charInfo);
      } else {
        this._drawFurnitureItem(ctx, item);
      }
    }
  };

  OfficeRenderer.prototype._drawFurnitureItem = function (ctx, f) {
    var fw = f.width * TILE;
    var fh = f.height * TILE;
    var fx = f.x * TILE;
    var fy = f.y * TILE;

    var color = FURNITURE_COLORS[f.type] || "#999";
    ctx.fillStyle = color;
    ctx.fillRect(fx, fy, fw, fh);

    ctx.fillStyle = "#FFF";
    ctx.font = "3px sans-serif";
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    var label = f.type.charAt(0).toUpperCase();
    ctx.fillText(label, fx + fw / 2, fy + fh / 2);
  };

  OfficeRenderer.prototype._drawCharacter = function (ctx, info) {
    var px = info.x * TILE;
    var py = info.y * TILE;
    var effect = info.effect;

    if (effect && !effect.isComplete()) {
      // Draw with matrix effect: per-row alpha + green trail
      for (var row = 0; row < TILE; row++) {
        var alpha = effect.getAlphaMask(0, row);
        if (alpha > 0) {
          ctx.globalAlpha = alpha;
          ctx.fillStyle = CHARACTER_COLOR;
          ctx.fillRect(px, py + row, TILE, 1);
        }
        var trail = effect.getTrailColor(0, row);
        if (trail) {
          ctx.globalAlpha = 1;
          ctx.fillStyle = trail;
          ctx.fillRect(px, py + row, TILE, 1);
        }
      }
      ctx.globalAlpha = 1;
    } else {
      // Normal rendering: placeholder colored circle
      ctx.fillStyle = CHARACTER_COLOR;
      ctx.beginPath();
      ctx.arc(px + TILE / 2, py + TILE / 2, TILE / 2 - 1, 0, Math.PI * 2);
      ctx.fill();

      // Direction indicator: small dot
      ctx.fillStyle = "#FFF";
      ctx.beginPath();
      var dotX = px + TILE / 2;
      var dotY = py + TILE / 2;
      var off = TILE / 3;
      if (info.direction === 0) dotY += off; // down
      else if (info.direction === 1) dotX -= off; // left
      else if (info.direction === 2) dotX += off; // right
      else if (info.direction === 3) dotY -= off; // up
      ctx.arc(dotX, dotY, 1.5, 0, Math.PI * 2);
      ctx.fill();
    }
  };

  OfficeRenderer.prototype._drawBubbles = function (ctx) {
    if (!this._characterManager) return;
    var drawBubble = window._officeDrawBubble;
    if (!drawBubble) return;

    var chars = this._characterManager.getDrawables();
    for (var i = 0; i < chars.length; i++) {
      var ch = chars[i];
      if (ch.bubble && ch.bubble.visible) {
        drawBubble(ctx, ch.x * TILE, ch.y * TILE, ch.bubble, this._camera.zoom);
      }
    }
  };

  // ---- Exports ----

  window._officeRenderer = OfficeRenderer;
  window._officeTileSize = TILE;
})();
