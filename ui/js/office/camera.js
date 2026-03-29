(function () {
  "use strict";

  var MIN_ZOOM = 2;
  var MAX_ZOOM = 6;
  var DEFAULT_ZOOM = 3;
  var TOUCH_MIN_ZOOM = 3;

  // ---- Camera ----

  function Camera(canvasWidth, canvasHeight) {
    this.x = 0;
    this.y = 0;
    this.zoom = DEFAULT_ZOOM;
    this._canvasW = canvasWidth;
    this._canvasH = canvasHeight;
  }

  Camera.prototype.worldToScreen = function (wx, wy) {
    return {
      sx: (wx - this.x) * this.zoom,
      sy: (wy - this.y) * this.zoom,
    };
  };

  Camera.prototype.screenToWorld = function (sx, sy) {
    return {
      wx: sx / this.zoom + this.x,
      wy: sy / this.zoom + this.y,
    };
  };

  Camera.prototype.setZoom = function (level) {
    var clamped = Math.round(level);
    if (clamped < MIN_ZOOM) clamped = MIN_ZOOM;
    if (clamped > MAX_ZOOM) clamped = MAX_ZOOM;
    if (clamped === this.zoom) return;

    // Zoom toward center: keep the world point at canvas center stable.
    var centerWx = this._canvasW / 2 / this.zoom + this.x;
    var centerWy = this._canvasH / 2 / this.zoom + this.y;

    this.zoom = clamped;

    this.x = centerWx - this._canvasW / 2 / this.zoom;
    this.y = centerWy - this._canvasH / 2 / this.zoom;
  };

  Camera.prototype.pan = function (dx, dy) {
    this.x -= dx / this.zoom;
    this.y -= dy / this.zoom;
  };

  Camera.prototype.clamp = function (worldWidth, worldHeight) {
    var viewW = this._canvasW / this.zoom;
    var viewH = this._canvasH / this.zoom;

    if (viewW >= worldWidth) {
      // World fits in viewport — center it
      this.x = (worldWidth - viewW) / 2;
    } else {
      if (this.x < 0) this.x = 0;
      if (this.x > worldWidth - viewW) this.x = worldWidth - viewW;
    }
    if (viewH >= worldHeight) {
      this.y = (worldHeight - viewH) / 2;
    } else {
      if (this.y < 0) this.y = 0;
      if (this.y > worldHeight - viewH) this.y = worldHeight - viewH;
    }
  };

  Camera.prototype.resize = function (canvasWidth, canvasHeight) {
    this._canvasW = canvasWidth;
    this._canvasH = canvasHeight;
  };

  // ---- Camera follow ----

  Camera.prototype.followTarget = function (worldX, worldY) {
    this._followX = worldX - this._canvasW / 2 / this.zoom;
    this._followY = worldY - this._canvasH / 2 / this.zoom;
    this._following = true;
  };

  Camera.prototype.cancelFollow = function () {
    this._following = false;
  };

  Camera.prototype.isFollowing = function () {
    return !!this._following;
  };

  Camera.prototype.updateFollow = function () {
    if (!this._following) return;
    var lerpFactor = 0.1;
    this.x += (this._followX - this.x) * lerpFactor;
    this.y += (this._followY - this.y) * lerpFactor;
    // Stop when close enough
    var dx = Math.abs(this._followX - this.x);
    var dy = Math.abs(this._followY - this.y);
    if (dx < 0.5 && dy < 0.5) {
      this.x = this._followX;
      this.y = this._followY;
      this._following = false;
    }
  };

  // ---- Input handling ----

  function attachInputHandlers(canvas, camera, onChange) {
    var pointers = {};
    var isPanning = false;
    var lastPanX = 0;
    var lastPanY = 0;
    var isTouch = false;
    var lastPinchDist = 0;

    canvas.addEventListener("pointerdown", function (e) {
      e.preventDefault();
      canvas.setPointerCapture(e.pointerId);
      pointers[e.pointerId] = { x: e.clientX, y: e.clientY };
      if (e.pointerType === "touch") isTouch = true;

      var ids = Object.keys(pointers);
      if (ids.length === 1) {
        isPanning = true;
        lastPanX = e.clientX;
        lastPanY = e.clientY;
      } else if (ids.length === 2) {
        isPanning = false;
        lastPinchDist = pinchDistance(pointers);
      }
    });

    canvas.addEventListener("pointermove", function (e) {
      if (!pointers[e.pointerId]) return;
      pointers[e.pointerId] = { x: e.clientX, y: e.clientY };

      var ids = Object.keys(pointers);
      if (ids.length === 1 && isPanning) {
        var dx = e.clientX - lastPanX;
        var dy = e.clientY - lastPanY;
        camera.pan(dx, dy);
        camera.cancelFollow();
        lastPanX = e.clientX;
        lastPanY = e.clientY;
        if (onChange) onChange();
      } else if (ids.length === 2) {
        var dist = pinchDistance(pointers);
        if (lastPinchDist > 0) {
          var delta = dist - lastPinchDist;
          if (Math.abs(delta) > 30) {
            var dir = delta > 0 ? 1 : -1;
            var minZoom = isTouch ? TOUCH_MIN_ZOOM : MIN_ZOOM;
            var next = camera.zoom + dir;
            if (next < minZoom) next = minZoom;
            camera.setZoom(next);
            lastPinchDist = dist;
            if (onChange) onChange();
          }
        }
      }
    });

    canvas.addEventListener("pointerup", function (e) {
      delete pointers[e.pointerId];
      if (Object.keys(pointers).length === 0) {
        isPanning = false;
        isTouch = false;
      }
    });

    canvas.addEventListener("pointercancel", function (e) {
      delete pointers[e.pointerId];
      if (Object.keys(pointers).length === 0) {
        isPanning = false;
        isTouch = false;
      }
    });

    canvas.addEventListener(
      "wheel",
      function (e) {
        e.preventDefault();
        var dir = e.deltaY < 0 ? 1 : -1;
        camera.setZoom(camera.zoom + dir);
        if (onChange) onChange();
      },
      { passive: false },
    );
  }

  function pinchDistance(pointers) {
    var ids = Object.keys(pointers);
    if (ids.length < 2) return 0;
    var a = pointers[ids[0]];
    var b = pointers[ids[1]];
    var dx = a.x - b.x;
    var dy = a.y - b.y;
    return Math.sqrt(dx * dx + dy * dy);
  }

  // ---- Exports ----

  window._officeCamera = Camera;
  window._officeAttachInputHandlers = attachInputHandlers;
})();
