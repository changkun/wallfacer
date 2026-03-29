(function () {
  "use strict";

  var DURATION = 0.5; // seconds
  var TRAIL_LENGTH = 4; // rows of green trail behind sweep head

  // ---- MatrixEffect ----

  function MatrixEffect(type, spriteWidth, spriteHeight) {
    this.type = type; // "spawn" or "despawn"
    this._w = spriteWidth;
    this._h = spriteHeight;
    this._timer = 0;
    this._complete = false;

    // Pre-compute per-column stagger offsets (0–0.2s random offset).
    this._colOffsets = [];
    for (var c = 0; c < spriteWidth; c++) {
      this._colOffsets.push(hashStagger(c) * 0.2);
    }
  }

  function hashStagger(col) {
    // Simple deterministic hash for column stagger (0..1 range).
    return ((col * 2654435761) >>> 0) / 4294967296;
  }

  MatrixEffect.prototype.update = function (dt) {
    if (this._complete) return;
    this._timer += dt;
    if (this._timer >= DURATION) {
      this._timer = DURATION;
      this._complete = true;
    }
  };

  MatrixEffect.prototype.isComplete = function () {
    return this._complete;
  };

  MatrixEffect.prototype.getAlphaMask = function (col, row) {
    if (this._complete) {
      return this.type === "spawn" ? 1 : 0;
    }

    var offset = col < this._colOffsets.length ? this._colOffsets[col] : 0;
    var adjustedTime = Math.max(0, this._timer - offset);
    var effectiveDuration = DURATION - offset;
    if (effectiveDuration <= 0) effectiveDuration = 0.01;
    var progress = adjustedTime / effectiveDuration;
    if (progress > 1) progress = 1;

    // Sweep position in row space (0 = top, _h = bottom).
    var sweepRow = progress * this._h;

    if (this.type === "spawn") {
      // Pixels above sweep are revealed, below are hidden.
      return row <= sweepRow ? 1 : 0;
    } else {
      // Despawn: pixels above sweep are hidden, below are still visible.
      return row < sweepRow ? 0 : 1;
    }
  };

  MatrixEffect.prototype.getTrailColor = function (col, row) {
    if (this._complete) return null;

    var offset = col < this._colOffsets.length ? this._colOffsets[col] : 0;
    var adjustedTime = Math.max(0, this._timer - offset);
    var effectiveDuration = DURATION - offset;
    if (effectiveDuration <= 0) effectiveDuration = 0.01;
    var progress = adjustedTime / effectiveDuration;
    if (progress > 1) progress = 1;

    var sweepRow = progress * this._h;

    // Trail is TRAIL_LENGTH rows behind the sweep head.
    var dist;
    if (this.type === "spawn") {
      dist = sweepRow - row;
    } else {
      dist = row - sweepRow;
    }

    if (dist >= 0 && dist < TRAIL_LENGTH) {
      var alpha = 1 - dist / TRAIL_LENGTH;
      return "rgba(0,255,0," + alpha.toFixed(2) + ")";
    }

    return null;
  };

  // ---- Exports ----

  window._officeMatrixEffect = MatrixEffect;
})();
