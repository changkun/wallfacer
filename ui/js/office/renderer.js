(function () {
  "use strict";

  var T = window._officeTileTypes;
  var TILE = 16; // pixels per tile at 1x zoom

  // ---- Colors for programmatic rendering ----
  var FLOOR_COLOR = "#D4C9A8";
  var FLOOR_COLOR_ALT = "#CFC29E";
  var WALL_COLOR_TOP = "#5B5E6B";
  var WALL_COLOR_FACE = "#787B8A";
  var VOID_COLOR = "#3A3D4A";
  var CHARACTER_COLOR = "#4A90D9";

  var FURNITURE_STYLE = {
    desk: { fill: "#B8956A", stroke: "#8B7355", label: "" },
    chair: { fill: "#6B5B4F", stroke: "#4A3C32", label: "" },
    pc: { fill: "#3B4252", stroke: "#2E3440", label: "" },
    sofa: { fill: "#7B68AE", stroke: "#5B4E8A", label: "" },
    plant: { fill: "#4CAF50", stroke: "#388E3C", label: "" },
    coffee: { fill: "#795548", stroke: "#5D4037", label: "" },
    whiteboard: { fill: "#ECEFF4", stroke: "#D8DEE9", label: "" },
    bookshelf: { fill: "#A0522D", stroke: "#8B4513", label: "" },
  };

  // ---- OfficeRenderer ----

  function OfficeRenderer(canvas, spriteCache, camera) {
    this._canvas = canvas;
    this._ctx = canvas.getContext("2d");
    this._ctx.imageSmoothingEnabled = false;
    this._spriteCache = spriteCache;
    this._camera = camera;
    this._characterManager = null;
    this._interaction = null;

    this._tileMap = null;
    this._furniture = [];
    this._seats = [];
    this._drawables = [];

    this._floorCanvas = null;
    this._floorDirty = true;

    this._rafId = null;
    this._running = false;
    this._boundRender = this.render.bind(this);

    // Loaded sprite sheet images
    this._charSheets = [];
  }

  OfficeRenderer.prototype.loadSprites = function () {
    var self = this;
    for (var i = 0; i < 20; i++) {
      (function (idx) {
        var img = new Image();
        img.onload = function () {
          self._charSheets[idx] = img;
        };
        var num = idx < 10 ? "0" + idx : "" + idx;
        img.src = "/assets/office/characters/char_" + num + ".png";
      })(i);
    }
  };

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

  OfficeRenderer.prototype.setCharacterManager = function (mgr) {
    this._characterManager = mgr;
  };

  OfficeRenderer.prototype.setInteraction = function (interaction) {
    this._interaction = interaction;
  };

  // ---- Render loop ----

  OfficeRenderer.prototype.render = function (timestamp) {
    if (!this._running) return;

    var ctx = this._ctx;
    var cam = this._camera;
    var zoom = cam.zoom;

    // Background
    ctx.fillStyle = FLOOR_COLOR;
    ctx.fillRect(0, 0, this._canvas.width, this._canvas.height);

    if (!this._tileMap) {
      this._rafId = requestAnimationFrame(this._boundRender);
      return;
    }

    ctx.save();
    ctx.translate(Math.round(-cam.x * zoom), Math.round(-cam.y * zoom));
    ctx.scale(zoom, zoom);

    this._drawFloor(ctx);
    this._drawScene(ctx);
    this._drawBubbles(ctx);
    this._drawSelection(ctx);

    ctx.restore();
    this._rafId = requestAnimationFrame(this._boundRender);
  };

  // ---- Floor + walls (cached to offscreen canvas) ----

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
            // Checkerboard for subtle texture
            fctx.fillStyle = (x + y) % 2 === 0 ? FLOOR_COLOR : FLOOR_COLOR_ALT;
            fctx.fillRect(x * TILE, y * TILE, TILE, TILE);
            // Subtle grid line
            fctx.strokeStyle = "rgba(0,0,0,0.05)";
            fctx.lineWidth = 0.5;
            fctx.strokeRect(x * TILE + 0.5, y * TILE + 0.5, TILE - 1, TILE - 1);
          } else if (tile === T.WALL) {
            this._drawWallTile(fctx, map, x, y);
          }
        }
      }
      this._floorDirty = false;
    }
    ctx.drawImage(this._floorCanvas, 0, 0);
  };

  OfficeRenderer.prototype._drawWallTile = function (fctx, map, x, y) {
    var px = x * TILE;
    var py = y * TILE;

    // Check if this is a top-edge wall (floor below) or side/bottom wall
    var floorBelow = y < map.height - 1 && map.tileAt(x, y + 1) !== T.WALL;
    var floorAbove = y > 0 && map.tileAt(x, y - 1) !== T.WALL;

    if (floorAbove) {
      // Top face of wall (darker, like looking at the top edge)
      fctx.fillStyle = WALL_COLOR_TOP;
      fctx.fillRect(px, py, TILE, TILE);
      // Bottom edge highlight
      fctx.fillStyle = "rgba(255,255,255,0.1)";
      fctx.fillRect(px, py + TILE - 2, TILE, 2);
    } else if (floorBelow) {
      // Front face of wall (lighter)
      fctx.fillStyle = WALL_COLOR_FACE;
      fctx.fillRect(px, py, TILE, TILE);
      // Top edge shadow
      fctx.fillStyle = "rgba(0,0,0,0.15)";
      fctx.fillRect(px, py, TILE, 2);
    } else {
      // Interior or side wall
      fctx.fillStyle = WALL_COLOR_TOP;
      fctx.fillRect(px, py, TILE, TILE);
    }
  };

  // ---- Scene (furniture + characters, z-sorted) ----

  OfficeRenderer.prototype._drawScene = function (ctx) {
    var drawables = this._drawables;
    drawables.length = 0;

    for (var i = 0; i < this._furniture.length; i++) {
      var f = this._furniture[i];
      drawables.push({
        _isChar: false,
        x: f.x,
        y: f.y,
        width: f.width || 1,
        height: f.height || 1,
        type: f.type,
        state: f.state,
      });
    }

    if (this._characterManager) {
      var chars = this._characterManager.getDrawables();
      for (var c = 0; c < chars.length; c++) {
        drawables.push({
          _isChar: true,
          x: chars[c].x,
          y: chars[c].y,
          width: 1,
          height: 1,
          _charInfo: chars[c],
        });
      }
    }

    drawables.sort(function (a, b) {
      return a.y + a.height - (b.y + b.height);
    });

    for (var d = 0; d < drawables.length; d++) {
      if (drawables[d]._isChar) {
        this._drawCharacter(ctx, drawables[d]._charInfo);
      } else {
        this._drawFurnitureItem(ctx, drawables[d]);
      }
    }
  };

  // ---- Furniture (clean programmatic rendering) ----

  OfficeRenderer.prototype._drawFurnitureItem = function (ctx, f) {
    var fw = f.width * TILE;
    var fh = f.height * TILE;
    var fx = f.x * TILE;
    var fy = f.y * TILE;
    var style = FURNITURE_STYLE[f.type];
    if (!style) style = { fill: "#888", stroke: "#666" };

    // Rounded rectangle body
    var r = 1.5;
    ctx.fillStyle = style.fill;
    ctx.strokeStyle = style.stroke;
    ctx.lineWidth = 0.5;
    _roundRect(ctx, fx + 0.5, fy + 0.5, fw - 1, fh - 1, r);
    ctx.fill();
    ctx.stroke();

    // Type-specific details
    if (f.type === "desk") {
      // Wood grain lines
      ctx.strokeStyle = "rgba(0,0,0,0.12)";
      ctx.lineWidth = 0.3;
      for (var i = 3; i < fw - 2; i += 4) {
        ctx.beginPath();
        ctx.moveTo(fx + i, fy + 2);
        ctx.lineTo(fx + i, fy + fh - 2);
        ctx.stroke();
      }
    } else if (f.type === "pc") {
      // Screen
      var isOn = f.state === "on";
      ctx.fillStyle = isOn ? "#88C0D0" : "#434C5E";
      ctx.fillRect(fx + 2, fy + 1, fw - 4, fh - 5);
      // Screen glow when on
      if (isOn) {
        ctx.fillStyle = "rgba(136,192,208,0.3)";
        ctx.fillRect(fx, fy + fh - 3, fw, 3);
      }
      // Stand
      ctx.fillStyle = style.stroke;
      ctx.fillRect(fx + fw / 2 - 1, fy + fh - 3, 2, 2);
      ctx.fillRect(fx + fw / 2 - 2, fy + fh - 1, 4, 1);
    } else if (f.type === "chair") {
      // Seat circle
      ctx.fillStyle = style.fill;
      ctx.beginPath();
      ctx.arc(fx + fw / 2, fy + fh / 2, fw / 2 - 1.5, 0, Math.PI * 2);
      ctx.fill();
      ctx.strokeStyle = style.stroke;
      ctx.lineWidth = 0.5;
      ctx.stroke();
    } else if (f.type === "plant") {
      // Pot
      ctx.fillStyle = "#8B4513";
      ctx.fillRect(fx + 3, fy + fh - 5, fw - 6, 5);
      // Leaves
      ctx.fillStyle = "#4CAF50";
      ctx.beginPath();
      ctx.arc(fx + fw / 2, fy + fh / 2 - 2, fw / 2 - 2, 0, Math.PI * 2);
      ctx.fill();
      ctx.fillStyle = "#66BB6A";
      ctx.beginPath();
      ctx.arc(fx + fw / 2 - 1, fy + fh / 2 - 3, fw / 3, 0, Math.PI * 2);
      ctx.fill();
    } else if (f.type === "coffee") {
      // Machine body
      ctx.fillStyle = "#5D4037";
      ctx.fillRect(fx + 2, fy + 2, fw - 4, fh - 2);
      // Red indicator
      ctx.fillStyle = "#E53935";
      ctx.fillRect(fx + 3, fy + 3, 2, 2);
    } else if (f.type === "whiteboard") {
      // White surface
      ctx.fillStyle = "#FFFFFF";
      ctx.fillRect(fx + 1, fy + 1, fw - 2, fh - 4);
      // Border
      ctx.strokeStyle = "#B0BEC5";
      ctx.lineWidth = 0.5;
      ctx.strokeRect(fx + 1, fy + 1, fw - 2, fh - 4);
      // Marker scribbles
      ctx.strokeStyle = "rgba(41,98,255,0.3)";
      ctx.lineWidth = 0.5;
      ctx.beginPath();
      ctx.moveTo(fx + 3, fy + 4);
      ctx.lineTo(fx + fw - 4, fy + 4);
      ctx.moveTo(fx + 3, fy + 6);
      ctx.lineTo(fx + fw / 2, fy + 6);
      ctx.stroke();
    } else if (f.type === "bookshelf") {
      // Shelves
      for (var s = 0; s < 3; s++) {
        var sy = fy + 2 + s * (fh / 3);
        ctx.fillStyle = "#6D4C41";
        ctx.fillRect(fx + 1, sy + fh / 3 - 2, fw - 2, 1);
        // Books
        var colors = ["#E53935", "#1E88E5", "#43A047", "#FB8C00", "#8E24AA"];
        for (var b = 0; b < 3; b++) {
          ctx.fillStyle = colors[(s * 3 + b) % colors.length];
          ctx.fillRect(fx + 2 + b * 4, sy, 3, fh / 3 - 3);
        }
      }
    } else if (f.type === "sofa") {
      // Cushion
      ctx.fillStyle = "#9575CD";
      _roundRect(ctx, fx + 1, fy + 2, fw - 2, fh - 4, 2);
      ctx.fill();
      // Armrests
      ctx.fillStyle = "#7E57C2";
      ctx.fillRect(fx, fy + 1, 2, fh - 2);
      ctx.fillRect(fx + fw - 2, fy + 1, 2, fh - 2);
    }
  };

  function _roundRect(ctx, x, y, w, h, r) {
    ctx.beginPath();
    ctx.moveTo(x + r, y);
    ctx.lineTo(x + w - r, y);
    ctx.arcTo(x + w, y, x + w, y + r, r);
    ctx.lineTo(x + w, y + h - r);
    ctx.arcTo(x + w, y + h, x + w - r, y + h, r);
    ctx.lineTo(x + r, y + h);
    ctx.arcTo(x, y + h, x, y + h - r, r);
    ctx.lineTo(x, y + r);
    ctx.arcTo(x, y, x + r, y, r);
    ctx.closePath();
  }

  // ---- Characters ----

  OfficeRenderer.prototype._drawCharacter = function (ctx, info) {
    var px = info.x * TILE;
    var py = info.y * TILE;
    var effect = info.effect;

    if (effect && !effect.isComplete()) {
      this._drawCharacterWithEffect(ctx, info, px, py, effect);
      return;
    }

    var sheet = this._charSheets[info.spriteIndex % 20];
    var anims = window._officeCharacterAnims;

    if (sheet && anims) {
      this._drawCharacterSprite(ctx, info, px, py, sheet, anims);
    } else {
      this._drawCharacterPlaceholder(ctx, info, px, py);
    }
  };

  OfficeRenderer.prototype._drawCharacterSprite = function (
    ctx,
    info,
    px,
    py,
    sheet,
    anims,
  ) {
    var animName = info.animType || "idle";
    var dirs = ["down", "up", "left", "right"];
    var dirName = dirs[info.direction] || "down";

    var animDef = anims[animName] || anims.idle;
    var dirDef = animDef[dirName] ||
      animDef.down || { megaRow: 0, col: 0, frames: 1 };

    var frameIdx = info.frameIndex % dirDef.frames;
    var srcCol = dirDef.col + frameIdx;
    var charH = anims._frameH || 32;
    var srcY = (dirDef.megaRow || 0) * charH;

    // Draw 16x32 sprite, offset up by 16px so feet align with tile
    ctx.drawImage(
      sheet,
      srcCol * TILE,
      srcY,
      TILE,
      charH,
      px,
      py - TILE,
      TILE,
      charH,
    );
  };

  OfficeRenderer.prototype._drawCharacterPlaceholder = function (
    ctx,
    info,
    px,
    py,
  ) {
    // Body
    ctx.fillStyle = CHARACTER_COLOR;
    ctx.beginPath();
    ctx.arc(px + TILE / 2, py + TILE / 2, TILE / 2 - 1, 0, Math.PI * 2);
    ctx.fill();
    // Eye dot (direction indicator)
    ctx.fillStyle = "#FFF";
    ctx.beginPath();
    var dx = px + TILE / 2,
      dy = py + TILE / 2,
      off = TILE / 3;
    if (info.direction === 0) dy += off;
    else if (info.direction === 1) dx -= off;
    else if (info.direction === 2) dx += off;
    else if (info.direction === 3) dy -= off;
    ctx.arc(dx, dy, 1.5, 0, Math.PI * 2);
    ctx.fill();
  };

  OfficeRenderer.prototype._drawCharacterWithEffect = function (
    ctx,
    info,
    px,
    py,
    effect,
  ) {
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
  };

  // ---- Overlays ----

  OfficeRenderer.prototype._drawSelection = function (ctx) {
    if (!this._interaction || !this._characterManager) return;
    var selId = this._interaction.getSelectedId();
    if (!selId) return;
    var ch = this._characterManager.getCharacterByTaskId(selId);
    if (!ch || ch.dead) return;
    ctx.strokeStyle = "#FFF";
    ctx.lineWidth = 1 / this._camera.zoom;
    ctx.strokeRect(ch.x * TILE - 1, ch.y * TILE - 1, TILE + 2, TILE + 2);
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

  // ---- Auto-fit ----

  OfficeRenderer.prototype.fitToViewport = function () {
    if (!this._tileMap || !this._camera) return;
    var worldW = this._tileMap.width * TILE;
    var worldH = this._tileMap.height * TILE;
    var canvasW = this._canvas.width;
    var canvasH = this._canvas.height;
    if (canvasW === 0 || canvasH === 0) return;

    // Use fractional zoom to fill the panel. Fit to height so the office
    // fills the panel vertically, then center horizontally.
    var fitZoom = canvasH / worldH;
    if (fitZoom < 1) fitZoom = 1;

    this._camera.zoom = fitZoom;
    var viewW = canvasW / fitZoom;
    this._camera.x = (worldW - viewW) / 2;
    this._camera.y = 0;
  };

  // ---- Exports ----

  window._officeRenderer = OfficeRenderer;
  window._officeTileSize = TILE;
})();
