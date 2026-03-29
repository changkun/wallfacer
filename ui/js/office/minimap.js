(function () {
  "use strict";

  var MM_WIDTH = 150;
  var MM_HEIGHT = 100;
  var DESK_THRESHOLD = 20;
  var UPDATE_INTERVAL = 500; // ms
  var TILE = 16;

  var T = null; // lazy-loaded tile types

  // ---- Minimap ----

  function Minimap(container, camera, onPan) {
    this._camera = camera;
    this._onPan = onPan;
    this._canvas = document.createElement("canvas");
    this._canvas.width = MM_WIDTH;
    this._canvas.height = MM_HEIGHT;
    this._canvas.style.cssText =
      "position:absolute;bottom:8px;right:8px;border:1px solid rgba(255,255,255,0.3);" +
      "border-radius:4px;cursor:crosshair;z-index:10;display:none;" +
      "background:rgba(0,0,0,0.5);";
    container.appendChild(this._canvas);
    this._ctx = this._canvas.getContext("2d");

    this._tileMap = null;
    this._furniture = [];
    this._characters = []; // draw info array
    this._scaleX = 1;
    this._scaleY = 1;
    this._visible = false;
    this._lastDraw = 0;

    var self = this;
    this._canvas.addEventListener("pointerdown", function (e) {
      e.preventDefault();
      e.stopPropagation();
      self._handleClick(e);
    });
  }

  Minimap.prototype.setLayout = function (tileMap, furniture) {
    this._tileMap = tileMap;
    this._furniture = furniture;
    if (tileMap) {
      this._scaleX = MM_WIDTH / (tileMap.width * TILE);
      this._scaleY = MM_HEIGHT / (tileMap.height * TILE);
    }
  };

  Minimap.prototype.updateVisibility = function (deskCount) {
    var shouldShow = deskCount > DESK_THRESHOLD;
    this._visible = shouldShow;
    this._canvas.style.display = shouldShow ? "block" : "none";
  };

  Minimap.prototype.update = function (timestamp, characters) {
    if (!this._visible || !this._tileMap) return;
    if (timestamp - this._lastDraw < UPDATE_INTERVAL) return;
    this._lastDraw = timestamp;
    this._characters = characters || [];
    this._draw();
  };

  Minimap.prototype._draw = function () {
    if (!T) T = window._officeTileTypes;
    var ctx = this._ctx;
    var map = this._tileMap;
    var sx = this._scaleX;
    var sy = this._scaleY;

    ctx.clearRect(0, 0, MM_WIDTH, MM_HEIGHT);

    // Floor tiles as light dots
    for (var y = 0; y < map.height; y++) {
      for (var x = 0; x < map.width; x++) {
        var tile = map.tileAt(x, y);
        if (tile === T.FLOOR) {
          ctx.fillStyle = "rgba(200,190,170,0.4)";
          ctx.fillRect(x * TILE * sx, y * TILE * sy, TILE * sx, TILE * sy);
        } else if (tile === T.WALL) {
          ctx.fillStyle = "rgba(100,100,100,0.6)";
          ctx.fillRect(x * TILE * sx, y * TILE * sy, TILE * sx, TILE * sy);
        }
      }
    }

    // Furniture as dark dots
    for (var i = 0; i < this._furniture.length; i++) {
      var f = this._furniture[i];
      ctx.fillStyle = "rgba(80,60,40,0.6)";
      ctx.fillRect(
        f.x * TILE * sx,
        f.y * TILE * sy,
        (f.width || 1) * TILE * sx,
        (f.height || 1) * TILE * sy,
      );
    }

    // Characters as colored dots
    for (var c = 0; c < this._characters.length; c++) {
      var ch = this._characters[c];
      ctx.fillStyle = "#4A90D9";
      ctx.fillRect(ch.x * TILE * sx, ch.y * TILE * sy, TILE * sx, TILE * sy);
    }

    // Viewport rectangle
    var cam = this._camera;
    var vx = cam.x * sx;
    var vy = cam.y * sy;
    var vw = (cam._canvasW / cam.zoom) * sx;
    var vh = (cam._canvasH / cam.zoom) * sy;
    ctx.strokeStyle = "rgba(255,255,255,0.7)";
    ctx.lineWidth = 1;
    ctx.strokeRect(vx, vy, vw, vh);
  };

  Minimap.prototype._handleClick = function (e) {
    var rect = this._canvas.getBoundingClientRect();
    var mx = e.clientX - rect.left;
    var my = e.clientY - rect.top;

    // Convert minimap coords to world coords
    var worldX = mx / this._scaleX;
    var worldY = my / this._scaleY;

    if (this._camera) {
      this._camera.followTarget(worldX, worldY);
    }
    if (this._onPan) this._onPan();
  };

  Minimap.prototype.isVisible = function () {
    return this._visible;
  };

  // ---- Exports ----

  window._officeMinimap = Minimap;
  window._officeMinimapThreshold = DESK_THRESHOLD;
})();
