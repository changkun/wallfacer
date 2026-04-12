(function () {
  "use strict";

  var BUBBLE_WAITING = "waiting";
  var BUBBLE_FAILED = "failed";
  var BUBBLE_COMMITTING = "committing";

  var FADE_DURATION = 0.2;

  var FRAME_COUNTS = {
    waiting: 3,
    failed: 1,
    committing: 4,
  };

  var ANIM_SPEEDS = {
    waiting: 0.4, // seconds per frame
    failed: 0, // static
    committing: 0.2,
  };

  // Pulse for failed bubble (scale oscillation)
  var PULSE_SPEED = 4; // Hz
  var PULSE_AMOUNT = 0.1; // 10% scale variation

  // ---- SpeechBubble ----

  function SpeechBubble(type) {
    this.type = type;
    this._frame = 0;
    this._animTimer = 0;
    this._pulseTimer = 0;
    this._alpha = 1;
    this._fading = false;
    this._fadeTimer = 0;
    this.visible = true;
  }

  SpeechBubble.prototype.update = function (dt) {
    if (!this.visible) return;

    // Fade out
    if (this._fading) {
      this._fadeTimer += dt;
      this._alpha = Math.max(0, 1 - this._fadeTimer / FADE_DURATION);
      if (this._alpha <= 0) {
        this.visible = false;
      }
      return;
    }

    // Animation
    var speed = ANIM_SPEEDS[this.type] || 0;
    if (speed > 0) {
      this._animTimer += dt;
      if (this._animTimer >= speed) {
        this._animTimer -= speed;
        var maxFrames = FRAME_COUNTS[this.type] || 1;
        this._frame = (this._frame + 1) % maxFrames;
      }
    }

    // Pulse for failed
    if (this.type === BUBBLE_FAILED) {
      this._pulseTimer += dt;
    }
  };

  SpeechBubble.prototype.getDrawInfo = function () {
    return {
      type: this.type,
      frameIndex: this._frame,
      visible: this.visible,
      alpha: this._alpha,
      pulseScale:
        this.type === BUBBLE_FAILED
          ? 1 +
            Math.sin(this._pulseTimer * PULSE_SPEED * Math.PI * 2) *
              PULSE_AMOUNT
          : 1,
    };
  };

  SpeechBubble.prototype.dismiss = function () {
    if (this._fading) return;
    this._fading = true;
    this._fadeTimer = 0;
  };

  // ---- Bubble rendering constants ----
  var BUBBLE_W = 11;
  var BUBBLE_H = 13;
  var POINTER_H = 3;

  var BUBBLE_COLORS = {
    waiting: "#F59E0B",
    failed: "#EF4444",
    committing: "#22C55E",
  };

  // ---- Draw bubble programmatically ----

  function drawBubble(ctx, x, y, bubbleInfo, _zoom) {
    if (!bubbleInfo.visible) return;

    var bw = BUBBLE_W;
    var bh = BUBBLE_H - POINTER_H; // body height without pointer
    var color = BUBBLE_COLORS[bubbleInfo.type] || "#999";
    var scale = bubbleInfo.pulseScale || 1;

    ctx.save();
    ctx.globalAlpha = bubbleInfo.alpha;

    // Position above character (in world coordinates, pre-zoom)
    var bx = x - bw / 2 + 8; // center on 16px tile
    var by = y - BUBBLE_H - 2;

    if (scale !== 1) {
      var cx = bx + bw / 2;
      var cy = by + bh / 2;
      ctx.translate(cx, cy);
      ctx.scale(scale, scale);
      ctx.translate(-cx, -cy);
    }

    // Body: rounded rect
    ctx.fillStyle = color;
    roundRect(ctx, bx, by, bw, bh, 2);
    ctx.fill();

    // Pointer triangle
    ctx.beginPath();
    ctx.moveTo(bx + bw / 2 - 2, by + bh);
    ctx.lineTo(bx + bw / 2, by + bh + POINTER_H);
    ctx.lineTo(bx + bw / 2 + 2, by + bh);
    ctx.closePath();
    ctx.fill();

    // Symbol
    ctx.fillStyle = "#FFF";
    drawSymbol(ctx, bx, by, bw, bh, bubbleInfo);

    ctx.restore();
  }

  function roundRect(ctx, x, y, w, h, r) {
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

  function drawSymbol(ctx, bx, by, bw, bh, info) {
    var cx = bx + bw / 2;
    var cy = by + bh / 2;

    if (info.type === BUBBLE_WAITING) {
      // "..." — three dots, frame controls which dots are visible
      var dotCount = info.frameIndex + 1; // 1, 2, or 3 dots
      for (var i = 0; i < dotCount; i++) {
        ctx.fillRect(cx - 3 + i * 3, cy, 1, 1);
      }
    } else if (info.type === BUBBLE_FAILED) {
      // "!" — vertical line + dot
      ctx.fillRect(cx, cy - 3, 1, 4);
      ctx.fillRect(cx, cy + 2, 1, 1);
    } else if (info.type === BUBBLE_COMMITTING) {
      // Spinner: simple rotating line
      var angle = (info.frameIndex / 4) * Math.PI * 2;
      var r = 3;
      var ex = cx + Math.cos(angle) * r;
      var ey = cy + Math.sin(angle) * r;
      ctx.beginPath();
      ctx.moveTo(cx, cy);
      ctx.lineTo(ex, ey);
      ctx.strokeStyle = "#FFF";
      ctx.lineWidth = 1;
      ctx.stroke();
    }
  }

  // ---- Exports ----

  window._officeBubbleTypes = {
    WAITING: BUBBLE_WAITING,
    FAILED: BUBBLE_FAILED,
    COMMITTING: BUBBLE_COMMITTING,
  };
  window._officeSpeechBubble = SpeechBubble;
  window._officeDrawBubble = drawBubble;
  window._officeBubbleHeight = BUBBLE_H;
})();
