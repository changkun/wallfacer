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
})();
