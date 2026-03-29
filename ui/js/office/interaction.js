(function () {
  "use strict";

  var PAN_THRESHOLD = 5; // px movement before treating as pan
  var DOUBLE_TAP_MS = 300;
  var LONG_PRESS_MS = 500;
  var TILE = 16;

  // ---- OfficeInteraction ----

  function OfficeInteraction(canvas, camera, characterManager) {
    this._canvas = canvas;
    this._camera = camera;
    this._mgr = characterManager;
    this._selectedId = null;

    // Pointer tracking
    this._downX = 0;
    this._downY = 0;
    this._downTime = 0;
    this._moved = false;
    this._lastTapTime = 0;
    this._lastTapId = null;
    this._longPressTimer = null;

    // Tooltip element
    this._tooltip = null;

    this._attachListeners(canvas);
  }

  OfficeInteraction.prototype.getSelectedId = function () {
    return this._selectedId;
  };

  OfficeInteraction.prototype.clearSelection = function () {
    this._selectedId = null;
    this._hideTooltip();
  };

  // ---- Input ----

  OfficeInteraction.prototype._attachListeners = function (canvas) {
    var self = this;

    canvas.addEventListener("pointerdown", function (e) {
      self._downX = e.clientX;
      self._downY = e.clientY;
      self._downTime = Date.now();
      self._moved = false;

      // Start long-press timer (touch only)
      if (e.pointerType === "touch") {
        self._clearLongPress();
        self._longPressTimer = setTimeout(function () {
          self._onLongPress(e);
        }, LONG_PRESS_MS);
      }
    });

    canvas.addEventListener("pointermove", function (e) {
      // Check if this is a pan (not a click)
      var dx = e.clientX - self._downX;
      var dy = e.clientY - self._downY;
      if (Math.abs(dx) > PAN_THRESHOLD || Math.abs(dy) > PAN_THRESHOLD) {
        self._moved = true;
        self._clearLongPress();
      }

      // Hover tooltip (desktop only)
      if (e.pointerType === "mouse") {
        self._onHover(e);
      }
    });

    canvas.addEventListener("pointerup", function (e) {
      self._clearLongPress();
      if (self._moved) return; // was a pan

      var now = Date.now();
      var hit = self._hitTest(e.clientX, e.clientY);

      // Check for double-tap
      if (
        hit &&
        hit.id === self._lastTapId &&
        now - self._lastTapTime < DOUBLE_TAP_MS
      ) {
        self._onDoubleTap(hit);
        self._lastTapTime = 0;
        self._lastTapId = null;
        return;
      }

      self._lastTapTime = now;
      self._lastTapId = hit ? hit.id : null;

      // Single tap
      self._onTap(hit, e);
    });

    canvas.addEventListener("pointerleave", function () {
      self._hideTooltip();
    });

    // Keyboard
    document.addEventListener("keydown", function (e) {
      if (!window._officeIsVisible || !window._officeIsVisible()) return;

      if (e.key === "Escape") {
        self.clearSelection();
      } else if (e.key === "Tab" && !e.ctrlKey && !e.metaKey) {
        e.preventDefault();
        self._cycleSelection(e.shiftKey ? -1 : 1);
      } else if (e.key === "Enter" && self._selectedId) {
        self._openTaskModal(self._selectedId);
      }
    });
  };

  // ---- Hit testing ----

  OfficeInteraction.prototype._hitTest = function (clientX, clientY) {
    var rect = this._canvas.getBoundingClientRect();
    var sx = clientX - rect.left;
    var sy = clientY - rect.top;
    var world = this._camera.screenToWorld(sx, sy);

    // Check bubble hit first (bubbles float above characters)
    var bubbleChar = this._hitBubble(world.wx, world.wy);
    if (bubbleChar) {
      return { id: bubbleChar.id, type: "bubble", character: bubbleChar };
    }

    // Check character hit
    var ch = this._mgr.characterAt(world.wx / TILE, world.wy / TILE);
    if (ch) {
      return { id: ch.id, type: "character", character: ch };
    }

    return null;
  };

  OfficeInteraction.prototype._hitBubble = function (wx, wy) {
    // Check all characters with bubbles
    var drawables = this._mgr.getDrawables();
    var bubbleH = window._officeBubbleHeight || 13;
    for (var i = 0; i < drawables.length; i++) {
      var d = drawables[i];
      if (!d.bubble || !d.bubble.visible) continue;
      // Bubble is centered above character
      var bx = d.x * TILE - 11 / 2 + 8;
      var by = d.y * TILE - bubbleH - 2;
      if (wx >= bx && wx <= bx + 11 && wy >= by && wy <= by + bubbleH) {
        return this._mgr.getCharacterByTaskId(this._findTaskIdByDrawInfo(d));
      }
    }
    return null;
  };

  OfficeInteraction.prototype._findTaskIdByDrawInfo = function (drawInfo) {
    // Walk all characters to find matching position
    var drawables = this._mgr.getDrawables();
    for (var i = 0; i < drawables.length; i++) {
      if (drawables[i] === drawInfo) {
        // Look up by iterating characters
        break;
      }
    }
    // Fallback: use characterAt
    var ch = this._mgr.characterAt(drawInfo.x, drawInfo.y);
    return ch ? ch.id : null;
  };

  // ---- Actions ----

  OfficeInteraction.prototype._onTap = function (hit, e) {
    if (hit) {
      this._selectedId = hit.id;
      this._showTooltipForCharacter(hit.character, e.clientX, e.clientY);

      if (hit.type === "bubble") {
        this._openTaskModal(hit.id);
      }
    } else {
      this.clearSelection();
    }
  };

  OfficeInteraction.prototype._onDoubleTap = function (hit) {
    if (hit) {
      this._openTaskModal(hit.id);
    }
  };

  OfficeInteraction.prototype._onLongPress = function (e) {
    var hit = this._hitTest(e.clientX, e.clientY);
    if (hit) {
      this._showTooltipForCharacter(hit.character, e.clientX, e.clientY);
    }
  };

  OfficeInteraction.prototype._onHover = function (e) {
    var rect = this._canvas.getBoundingClientRect();
    var sx = e.clientX - rect.left;
    var sy = e.clientY - rect.top;
    var world = this._camera.screenToWorld(sx, sy);
    var ch = this._mgr.characterAt(world.wx / TILE, world.wy / TILE);

    if (ch) {
      this._showTooltipForCharacter(ch, e.clientX, e.clientY);
    } else {
      this._hideTooltip();
    }
  };

  OfficeInteraction.prototype._openTaskModal = function (taskId) {
    if (typeof openModal === "function") {
      openModal(taskId);
    }
  };

  OfficeInteraction.prototype._cycleSelection = function (dir) {
    var drawables = this._mgr.getDrawables();
    if (drawables.length === 0) return;

    // Build ordered list of task IDs (by desk index/position)
    var ids = [];
    for (var i = 0; i < drawables.length; i++) {
      var ch = this._mgr.characterAt(drawables[i].x, drawables[i].y);
      if (ch) ids.push(ch.id);
    }
    if (ids.length === 0) return;

    var currentIdx = ids.indexOf(this._selectedId);
    var nextIdx;
    if (currentIdx === -1) {
      nextIdx = dir > 0 ? 0 : ids.length - 1;
    } else {
      nextIdx = (currentIdx + dir + ids.length) % ids.length;
    }
    this._selectedId = ids[nextIdx];
  };

  // ---- Tooltip ----

  OfficeInteraction.prototype._showTooltipForCharacter = function (
    ch,
    clientX,
    clientY,
  ) {
    if (!this._tooltip) {
      this._tooltip = document.createElement("div");
      this._tooltip.id = "office-tooltip";
      this._tooltip.style.cssText =
        "position:fixed;background:rgba(0,0,0,0.85);color:#fff;font-size:11px;" +
        "padding:4px 8px;border-radius:4px;pointer-events:none;z-index:999;" +
        "max-width:200px;white-space:nowrap;overflow:hidden;text-overflow:ellipsis;";
      document.body.appendChild(this._tooltip);
    }

    // Find task title from global state
    var title = ch.id;
    if (typeof findTaskById === "function") {
      var task = findTaskById(ch.id);
      if (task) {
        title = (task.title || task.prompt || ch.id).substring(0, 30);
      }
    }

    this._tooltip.textContent = title;
    this._tooltip.style.display = "block";
    this._tooltip.style.left = clientX + 12 + "px";
    this._tooltip.style.top = clientY - 20 + "px";
  };

  OfficeInteraction.prototype._hideTooltip = function () {
    if (this._tooltip) {
      this._tooltip.style.display = "none";
    }
  };

  OfficeInteraction.prototype._clearLongPress = function () {
    if (this._longPressTimer) {
      clearTimeout(this._longPressTimer);
      this._longPressTimer = null;
    }
  };

  // ---- Exports ----

  window._officeInteraction = OfficeInteraction;
})();
