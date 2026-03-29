(function () {
  "use strict";

  var _assetsAvailable = false;

  // ---- SpriteSheet (successful image load) ----

  function SpriteSheet(img, frameWidth, frameHeight) {
    this._img = img;
    this._frameW = frameWidth;
    this._frameH = frameHeight;
    this._cols = Math.floor(img.width / frameWidth);
  }

  SpriteSheet.prototype.frame = function (index) {
    var col = index % this._cols;
    var row = Math.floor(index / this._cols);
    return {
      sx: col * this._frameW,
      sy: row * this._frameH,
      sw: this._frameW,
      sh: this._frameH,
    };
  };

  Object.defineProperty(SpriteSheet.prototype, "image", {
    get: function () {
      return this._img;
    },
  });

  Object.defineProperty(SpriteSheet.prototype, "frameWidth", {
    get: function () {
      return this._frameW;
    },
  });

  Object.defineProperty(SpriteSheet.prototype, "frameHeight", {
    get: function () {
      return this._frameH;
    },
  });

  // ---- PlaceholderSheet (fallback when assets missing) ----

  function hashColor(key) {
    var h = 0;
    for (var i = 0; i < key.length; i++) {
      h = ((h << 5) - h + key.charCodeAt(i)) | 0;
    }
    var hue = Math.abs(h) % 360;
    return "hsl(" + hue + ", 50%, 60%)";
  }

  function PlaceholderSheet(key, frameWidth, frameHeight) {
    this._frameW = frameWidth;
    this._frameH = frameHeight;
    this._cols = 1;
    this._color = hashColor(key);
    this._canvas = new OffscreenCanvas(frameWidth, frameHeight);
    var ctx = this._canvas.getContext("2d");
    ctx.fillStyle = this._color;
    ctx.fillRect(0, 0, frameWidth, frameHeight);
  }

  PlaceholderSheet.prototype.frame = function () {
    return { sx: 0, sy: 0, sw: this._frameW, sh: this._frameH };
  };

  Object.defineProperty(PlaceholderSheet.prototype, "image", {
    get: function () {
      return this._canvas;
    },
  });

  Object.defineProperty(PlaceholderSheet.prototype, "frameWidth", {
    get: function () {
      return this._frameW;
    },
  });

  Object.defineProperty(PlaceholderSheet.prototype, "frameHeight", {
    get: function () {
      return this._frameH;
    },
  });

  // ---- SpriteCache ----

  function SpriteCache() {
    this._cache = {};
  }

  SpriteCache.prototype.loadSpriteSheet = function (url, frameWidth, frameHeight) {
    return new Promise(function (resolve) {
      var img = new Image();
      img.onload = function () {
        _assetsAvailable = true;
        resolve(new SpriteSheet(img, frameWidth, frameHeight));
      };
      img.onerror = function () {
        resolve(new PlaceholderSheet(url, frameWidth, frameHeight));
      };
      img.src = url;
    });
  };

  SpriteCache.prototype.getCached = function (key, zoom) {
    return this._cache[key + ":" + zoom] || null;
  };

  SpriteCache.prototype.cache = function (key, zoom, canvas) {
    this._cache[key + ":" + zoom] = canvas;
  };

  SpriteCache.prototype.invalidateZoom = function () {
    this._cache = {};
  };

  SpriteCache.prototype.rasterizeFrame = function (spriteSheet, frameIndex, zoom) {
    var rect = spriteSheet.frame(frameIndex);
    var w = rect.sw * zoom;
    var h = rect.sh * zoom;
    var oc = new OffscreenCanvas(w, h);
    var ctx = oc.getContext("2d");
    ctx.imageSmoothingEnabled = false;
    ctx.drawImage(spriteSheet.image, rect.sx, rect.sy, rect.sw, rect.sh, 0, 0, w, h);
    return oc;
  };

  // ---- LimeZu sprite definitions ----
  // Character sheets: 896×656 px, 16×16 frame grid (56 cols × 41 rows).
  // Layout from Spritesheet_animations_GUIDE.png.
  // Each direction block: down, up, left, right in that order within a row span.

  var CHARACTER_ANIMS = {
    idle: {
      // Row 0: 4 idle frames per direction (only 1 used for idle stance).
      down:  { row: 0, col: 0, frames: 1 },
      up:    { row: 0, col: 1, frames: 1 },
      left:  { row: 0, col: 2, frames: 1 },
      right: { row: 0, col: 3, frames: 1 },
    },
    walk: {
      // Rows 1–2: walk animation. 6 frames per direction.
      // Down starts at (0,1), up at (6,1), left at (12,1), right at (18,1).
      down:  { row: 1, col: 0,  frames: 6 },
      up:    { row: 1, col: 6,  frames: 6 },
      left:  { row: 1, col: 12, frames: 6 },
      right: { row: 1, col: 18, frames: 6 },
    },
    sit: {
      // Row 3: sit-down transition, ~4 frames per direction.
      down:  { row: 3, col: 0,  frames: 4 },
      up:    { row: 3, col: 4,  frames: 4 },
      left:  { row: 3, col: 8,  frames: 4 },
      right: { row: 3, col: 12, frames: 4 },
    },
    sitting_idle: {
      // Rows 5–6: sitting idle. 1 frame per direction.
      down:  { row: 5, col: 0, frames: 1 },
      up:    { row: 5, col: 1, frames: 1 },
      left:  { row: 5, col: 2, frames: 1 },
      right: { row: 5, col: 3, frames: 1 },
    },
    typing: {
      // Rows 8–10: desk/typing animations. Multiple frames with furniture overlay.
      // Using the character-only frames from the sitting-at-desk rows.
      down:  { row: 8, col: 0,  frames: 4 },
      up:    { row: 8, col: 4,  frames: 4 },
      left:  { row: 8, col: 8,  frames: 4 },
      right: { row: 8, col: 12, frames: 4 },
    },
  };

  // Furniture sheet: office_sheet.png, 256×848 px, 16px grid (16 cols × 53 rows).
  // Pixel regions for individual furniture items within the sheet.
  var FURNITURE_DEFS = {
    desk:       { sx: 0,   sy: 0,   sw: 32, sh: 16, frames: 1 },
    chair:      { sx: 0,   sy: 16,  sw: 16, sh: 16, frames: 1 },
    pc:         { sx: 32,  sy: 0,   sw: 16, sh: 16, frames: 2 },  // off, on
    sofa:       { sx: 0,   sy: 32,  sw: 32, sh: 16, frames: 1 },
    plant:      { sx: 48,  sy: 0,   sw: 16, sh: 16, frames: 1 },
    coffee:     { sx: 64,  sy: 0,   sw: 16, sh: 16, frames: 1 },
    whiteboard: { sx: 0,   sy: 48,  sw: 32, sh: 16, frames: 1 },
    bookshelf:  { sx: 64,  sy: 16,  sw: 16, sh: 32, frames: 1 },
  };

  // Tile sheet regions.
  // floor.png (240×640): 5 columns of styles, each 48px wide.
  // Using column 0 (neutral beige office floor), rows 0–2 for 3 tile variants.
  var TILE_DEFS = {
    floor: {
      sx: 0, sy: 0, sw: 48, sh: 48, // 3×3 tile pattern block
      variants: 3, // number of 16px tile variants in the block
    },
    // wall.png (512×640): auto-tile column groups.
    // Using first wall style (column 0), which provides:
    //   top-left, top, top-right, left, center, right,
    //   bottom-left, bottom, bottom-right (3×3 arrangement)
    wall: {
      sx: 0, sy: 0, sw: 48, sh: 48, // 3×3 auto-tile group
    },
  };

  // ---- Asset detection ----

  function detectAssets() {
    return new Promise(function (resolve) {
      var img = new Image();
      img.onload = function () {
        _assetsAvailable = true;
        resolve(true);
      };
      img.onerror = function () {
        _assetsAvailable = false;
        resolve(false);
      };
      img.src = "/assets/office/characters/char_00.png";
    });
  }

  // ---- Exports ----

  function assetAvailable() {
    return _assetsAvailable;
  }

  // Exposed for testing — allows resetting state between test runs.
  function _resetAssetState() {
    _assetsAvailable = false;
  }

  window._officeSpriteCache = SpriteCache;
  window._officeAssetAvailable = assetAvailable;
  window._officeResetAssetState = _resetAssetState;
  window._officeDetectAssets = detectAssets;
  window._officeCharacterAnims = CHARACTER_ANIMS;
  window._officeFurnitureDefs = FURNITURE_DEFS;
  window._officeTileDefs = TILE_DEFS;
})();
