(function () {
  "use strict";

  var T = window._officeTileTypes;
  var TILE = 16; // pixels per tile at 1x zoom

  // Placeholder colors (used when sprite sheets haven't loaded yet).
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
  var CHARACTER_COLOR = "#4A90D9";

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

    // Loaded sprite sheet images (set by loadSprites)
    this._officeSheet = null;
    this._floorSheet = null;
    this._wallSheet = null;
    this._charSheets = []; // char_00..char_19
  }

  // ---- Sprite loading ----

  OfficeRenderer.prototype.loadSprites = function () {
    var self = this;

    // Load office furniture sheet
    var officeImg = new Image();
    officeImg.onload = function () {
      self._officeSheet = officeImg;
      self._floorDirty = true;
    };
    officeImg.src = "/assets/office/furniture/office_sheet.png";

    // Load floor tile sheet
    var floorImg = new Image();
    floorImg.onload = function () {
      self._floorSheet = floorImg;
      self._floorDirty = true;
    };
    floorImg.src = "/assets/office/tiles/floor.png";

    // Load wall tile sheet
    var wallImg = new Image();
    wallImg.onload = function () {
      self._wallSheet = wallImg;
      self._floorDirty = true;
    };
    wallImg.src = "/assets/office/tiles/wall.png";

    // Load character sheets
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

    ctx.clearRect(0, 0, this._canvas.width, this._canvas.height);

    if (!this._tileMap) {
      this._rafId = requestAnimationFrame(this._boundRender);
      return;
    }

    ctx.save();
    ctx.translate(-cam.x * zoom, -cam.y * zoom);
    ctx.scale(zoom, zoom);

    this._drawFloor(ctx);
    this._drawScene(ctx);
    this._drawBubbles(ctx);
    this._drawSelection(ctx);

    ctx.restore();

    this._rafId = requestAnimationFrame(this._boundRender);
  };

  // ---- Floor + walls ----

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
            this._drawFloorTile(fctx, x, y);
          } else if (tile === T.WALL) {
            this._drawWallTile(fctx, x, y);
          }
        }
      }
      this._floorDirty = false;
    }
    ctx.drawImage(this._floorCanvas, 0, 0);
  };

  OfficeRenderer.prototype._drawFloorTile = function (ctx, x, y) {
    if (this._floorSheet) {
      // Pick a tile variant based on position for visual variety
      // floor.png: 240×640, first column style is 48px wide (3 tiles × 16px)
      // Use row 0 (neutral style), pick 1 of 3 variants by (x+y) % 3
      var variant = (x + y) % 3;
      ctx.drawImage(
        this._floorSheet,
        variant * TILE, 0, TILE, TILE,
        x * TILE, y * TILE, TILE, TILE,
      );
    } else {
      ctx.fillStyle = FLOOR_COLOR;
      ctx.fillRect(x * TILE, y * TILE, TILE, TILE);
    }
  };

  OfficeRenderer.prototype._drawWallTile = function (ctx, x, y) {
    if (this._wallSheet) {
      // wall.png: auto-tile set. Use a simple approach:
      // Detect which edges are exposed (adjacent to non-wall) and pick
      // the appropriate sub-tile from the first wall style.
      // For now, use center fill tile at (16, 16) in the sheet.
      var map = this._tileMap;
      var hasTop = y > 0 && map.tileAt(x, y - 1) === T.WALL;
      var hasBot = y < map.height - 1 && map.tileAt(x, y + 1) === T.WALL;
      var hasLeft = x > 0 && map.tileAt(x - 1, y) === T.WALL;
      var hasRight = x < map.width - 1 && map.tileAt(x + 1, y) === T.WALL;

      // Simple auto-tile: 3×3 grid at (0,0) in wall sheet
      // [TL, T, TR] = row 0, [L, C, R] = row 1, [BL, B, BR] = row 2
      var srcCol = 1; // center
      var srcRow = 1;
      if (!hasTop && !hasLeft) { srcCol = 0; srcRow = 0; }
      else if (!hasTop && !hasRight) { srcCol = 2; srcRow = 0; }
      else if (!hasBot && !hasLeft) { srcCol = 0; srcRow = 2; }
      else if (!hasBot && !hasRight) { srcCol = 2; srcRow = 2; }
      else if (!hasTop) { srcCol = 1; srcRow = 0; }
      else if (!hasBot) { srcCol = 1; srcRow = 2; }
      else if (!hasLeft) { srcCol = 0; srcRow = 1; }
      else if (!hasRight) { srcCol = 2; srcRow = 1; }

      ctx.drawImage(
        this._wallSheet,
        srcCol * TILE, srcRow * TILE, TILE, TILE,
        x * TILE, y * TILE, TILE, TILE,
      );
    } else {
      ctx.fillStyle = WALL_COLOR;
      ctx.fillRect(x * TILE, y * TILE, TILE, TILE);
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

    drawables.sort(function (a, b) {
      return (a.y + a.height) - (b.y + b.height);
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

  // ---- Furniture ----

  OfficeRenderer.prototype._drawFurnitureItem = function (ctx, f) {
    var fw = f.width * TILE;
    var fh = f.height * TILE;
    var fx = f.x * TILE;
    var fy = f.y * TILE;

    if (this._officeSheet) {
      var defs = window._officeFurnitureDefs;
      var def = defs && defs[f.type];
      if (def) {
        // For multi-frame items (e.g. PC on/off), pick frame based on state
        var frameOffset = 0;
        if (f.type === "pc" && f.state === "on" && def.frames > 1) {
          frameOffset = def.sw; // next frame is adjacent in the sheet
        }
        ctx.drawImage(
          this._officeSheet,
          def.sx + frameOffset, def.sy, def.sw, def.sh,
          fx, fy, fw, fh,
        );
        return;
      }
    }

    // Placeholder fallback
    ctx.fillStyle = FURNITURE_COLORS[f.type] || "#999";
    ctx.fillRect(fx, fy, fw, fh);
    ctx.fillStyle = "#FFF";
    ctx.font = "3px sans-serif";
    ctx.textAlign = "center";
    ctx.textBaseline = "middle";
    ctx.fillText(f.type.charAt(0).toUpperCase(), fx + fw / 2, fy + fh / 2);
  };

  // ---- Characters ----

  OfficeRenderer.prototype._drawCharacter = function (ctx, info) {
    var px = info.x * TILE;
    var py = info.y * TILE;
    var effect = info.effect;

    // Get the character sprite sheet
    var sheet = this._charSheets[info.spriteIndex % this._charSheets.length];
    var anims = window._officeCharacterAnims;

    if (effect && !effect.isComplete()) {
      this._drawCharacterWithEffect(ctx, info, px, py, effect, sheet);
      return;
    }

    if (sheet && anims) {
      this._drawCharacterSprite(ctx, info, px, py, sheet, anims);
    } else {
      this._drawCharacterPlaceholder(ctx, info, px, py);
    }
  };

  OfficeRenderer.prototype._drawCharacterSprite = function (ctx, info, px, py, sheet, anims) {
    // Map animType + direction to sheet coordinates
    var animName = info.animType || "idle";
    var dirs = ["down", "up", "left", "right"];
    var dirName = dirs[info.direction] || "down";

    var animDef = anims[animName];
    if (!animDef) animDef = anims.idle;
    var dirDef = animDef[dirName];
    if (!dirDef) dirDef = animDef.down || { row: 0, col: 0, frames: 1 };

    var frameIdx = info.frameIndex % dirDef.frames;
    var srcCol = dirDef.col + frameIdx;
    var srcRow = dirDef.row;

    ctx.drawImage(
      sheet,
      srcCol * TILE, srcRow * TILE, TILE, TILE,
      px, py, TILE, TILE,
    );
  };

  OfficeRenderer.prototype._drawCharacterPlaceholder = function (ctx, info, px, py) {
    ctx.fillStyle = CHARACTER_COLOR;
    ctx.beginPath();
    ctx.arc(px + TILE / 2, py + TILE / 2, TILE / 2 - 1, 0, Math.PI * 2);
    ctx.fill();

    ctx.fillStyle = "#FFF";
    ctx.beginPath();
    var dotX = px + TILE / 2;
    var dotY = py + TILE / 2;
    var off = TILE / 3;
    if (info.direction === 0) dotY += off;
    else if (info.direction === 1) dotX -= off;
    else if (info.direction === 2) dotX += off;
    else if (info.direction === 3) dotY -= off;
    ctx.arc(dotX, dotY, 1.5, 0, Math.PI * 2);
    ctx.fill();
  };

  OfficeRenderer.prototype._drawCharacterWithEffect = function (ctx, info, px, py, effect, sheet) {
    for (var row = 0; row < TILE; row++) {
      var alpha = effect.getAlphaMask(0, row);
      if (alpha > 0) {
        ctx.globalAlpha = alpha;
        if (sheet) {
          // Draw one scanline of the character sprite
          ctx.drawImage(sheet, 0, 0 + row, TILE, 1, px, py + row, TILE, 1);
        } else {
          ctx.fillStyle = CHARACTER_COLOR;
          ctx.fillRect(px, py + row, TILE, 1);
        }
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

    // Find the integer zoom that fits the office
    var zoomX = canvasW / worldW;
    var zoomY = canvasH / worldH;
    var fitZoom = Math.floor(Math.min(zoomX, zoomY));
    if (fitZoom < 2) fitZoom = 2;
    if (fitZoom > 6) fitZoom = 6;

    this._camera.zoom = fitZoom;

    // Center: camera (x,y) is the top-left world coordinate visible
    var viewW = canvasW / fitZoom;
    var viewH = canvasH / fitZoom;
    this._camera.x = (worldW - viewW) / 2;
    this._camera.y = (worldH - viewH) / 2;
  };

  // ---- Exports ----

  window._officeRenderer = OfficeRenderer;
  window._officeTileSize = TILE;
})();
