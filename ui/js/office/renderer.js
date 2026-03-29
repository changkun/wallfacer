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
    this._officeSheet = null;
    this._charSheets = [];
  }

  OfficeRenderer.prototype.loadSprites = function () {
    var self = this;
    // Load office furniture sheet
    var officeImg = new Image();
    officeImg.onload = function () {
      self._officeSheet = officeImg;
      self._floorDirty = true;
    };
    officeImg.src = "/assets/office/furniture/office_sheet.png";

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
    var pcOverlays = [];

    for (var i = 0; i < this._furniture.length; i++) {
      var f = this._furniture[i];
      // PC monitors are drawn in a separate overlay pass so they
      // always appear on top of desks regardless of Z-sort order.
      if (f.type === "pc") {
        pcOverlays.push(f);
        continue;
      }
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

    // Draw PC monitors on top of desks
    for (var p = 0; p < pcOverlays.length; p++) {
      var pc = pcOverlays[p];
      this._drawFurnitureItem(ctx, {
        x: pc.x,
        y: pc.y,
        width: pc.width || 1,
        height: pc.height || 1,
        type: pc.type,
        state: pc.state,
      });
    }
  };

  // ---- Furniture (clean programmatic rendering) ----

  // Sprite coordinates in office_sheet.png (verified via pixel inspection)
  var SPRITE_MAP = {
    desk: { sx: 64, sy: 480, sw: 32, sh: 32 }, // flat beige desk with front face
    chair: { sx: 64, sy: 128, sw: 16, sh: 32 }, // office chair facing left (1x2)
    pc: { sx: 224, sy: 192, sw: 16, sh: 16 }, // blue-screen monitor on stand
    sofa: { sx: 0, sy: 320, sw: 32, sh: 16 }, // grey 2-seat couch (2x1 sprite)
    plant: { sx: 96, sy: 112, sw: 16, sh: 16 }, // small green bush (1x1)
    bookshelf: { sx: 112, sy: 208, sw: 16, sh: 32 }, // bookshelf with books (1x2)
  };

  OfficeRenderer.prototype._drawFurnitureItem = function (ctx, f) {
    var fw = f.width * TILE;
    var fh = f.height * TILE;
    var fx = f.x * TILE;
    var fy = f.y * TILE;

    if (this._officeSheet) {
      var sprite = SPRITE_MAP[f.type];
      if (sprite) {
        // Draw sprite at natural size, bottom-aligned in tile area.
        // This handles sprites smaller than their tile footprint (e.g.
        // sofa is 32x16 sprite in a 2x2 tile area).
        ctx.drawImage(
          this._officeSheet,
          sprite.sx,
          sprite.sy,
          sprite.sw,
          sprite.sh,
          fx,
          fy + fh - sprite.sh,
          sprite.sw,
          sprite.sh,
        );
        return;
      }
    }

    // PC/monitor programmatic fallback (used when sprite sheet not loaded)
    if (f.type === "pc") {
      var isOn = f.state === "on";
      ctx.fillStyle = isOn ? "#5BA3D9" : "#434C5E";
      ctx.fillRect(fx + 2, fy + 2, fw - 4, fh - 6);
      ctx.strokeStyle = "#2E3440";
      ctx.lineWidth = 0.5;
      ctx.strokeRect(fx + 2, fy + 2, fw - 4, fh - 6);
      ctx.fillStyle = "#2E3440";
      ctx.fillRect(fx + fw / 2 - 2, fy + fh - 4, 4, 3);
      ctx.fillRect(fx + fw / 2 - 3, fy + fh - 1, 6, 1);
      return;
    }

    // Placeholder fallback
    var style = FURNITURE_STYLE[f.type];
    if (!style) style = { fill: "#888", stroke: "#666" };
    ctx.fillStyle = style.fill;
    ctx.fillRect(fx, fy, fw, fh);
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

    // Draw 16x32 sprite: feet at bottom of tile, head extends upward
    ctx.drawImage(
      sheet,
      srcCol * TILE,
      srcY,
      TILE,
      charH,
      px,
      py + TILE - charH,
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
    // Characters are 16x32 (2 tiles tall), drawn with feet at py+TILE
    var charH = 32;
    var startY = py + TILE - charH;
    for (var row = 0; row < charH; row++) {
      var alpha = effect.getAlphaMask(0, row);
      if (alpha > 0) {
        ctx.globalAlpha = alpha;
        ctx.fillStyle = CHARACTER_COLOR;
        ctx.fillRect(px, startY + row, TILE, 1);
      }
      var trail = effect.getTrailColor(0, row);
      if (trail) {
        ctx.globalAlpha = 1;
        ctx.fillStyle = trail;
        ctx.fillRect(px, startY + row, TILE, 1);
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
