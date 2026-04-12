(function () {
  "use strict";

  // ---- State constants ----
  var SPAWN = "spawn";
  var WALK_TO_DESK = "walk_to_desk";
  var WORKING = "working";
  var SPEECH_BUBBLE = "speech_bubble";
  var IDLE = "idle";
  var WANDER = "wander";
  var DESPAWN = "despawn";

  // ---- Direction constants ----
  var DOWN = 0;
  var LEFT = 1;
  var RIGHT = 2;
  var UP = 3;

  // ---- Timing ----
  var SPAWN_DURATION = 0.5; // seconds
  var DESPAWN_DURATION = 0.5;
  var WALK_SPEED = 2; // tiles per second
  var WANDER_INTERVAL = 3; // seconds between wander attempts
  var ANIM_FRAME_DURATION = 0.25; // seconds per animation frame

  // ---- Frame counts per animation ----
  var FRAME_COUNTS = {
    idle: 1,
    walk: 6,
    typing: 6,
    reading: 2,
  };

  // ---- Character ----

  function Character(id, spriteIndex, seat) {
    this.id = id;
    this.spriteIndex = spriteIndex;
    this.seat = seat;

    // Position in tile coords (fractional for smooth movement)
    this.x = seat ? seat.x : 0;
    this.y = seat ? seat.y : 0;
    this.direction = seat ? dirFromString(seat.direction) : DOWN;

    // State
    this.state = SPAWN;
    this.dead = false;
    this.bubbleType = null; // "amber" or "red" when in SPEECH_BUBBLE
    this.bubble = null; // SpeechBubble instance

    // Timers
    this._stateTimer = 0;
    this._wanderTimer = 0;

    // Animation
    this._animFrame = 0;
    this._animTimer = 0;
    this._animType = "idle"; // idle, walk, typing, reading

    // Spawn/despawn effect
    this._effect = null;
    if (typeof window._officeMatrixEffect === "function") {
      this._effect = new window._officeMatrixEffect("spawn", 16, 32);
    }

    // Walk path
    this._path = null;
    this._pathIndex = 0;
    this._targetX = this.x;
    this._targetY = this.y;
  }

  function dirFromString(s) {
    if (s === "left") return LEFT;
    if (s === "right") return RIGHT;
    if (s === "up") return UP;
    return DOWN;
  }

  function directionBetween(fromX, fromY, toX, toY) {
    var dx = toX - fromX;
    var dy = toY - fromY;
    if (Math.abs(dx) > Math.abs(dy)) {
      return dx > 0 ? RIGHT : LEFT;
    }
    return dy > 0 ? DOWN : UP;
  }

  // ---- Update ----

  Character.prototype.update = function (dt, _tileMap) {
    if (this.dead) return;

    switch (this.state) {
      case SPAWN:
        this._stateTimer += dt;
        if (this._effect) this._effect.update(dt);
        if (this._stateTimer >= SPAWN_DURATION) {
          this._effect = null;
          this._setState(IDLE);
        }
        break;

      case IDLE:
        this._animType = "idle";
        break;

      case WANDER:
        this._animType = "walk";
        this._moveAlongPath(dt);
        if (!this._path || this._pathIndex >= this._path.length) {
          this._setState(IDLE);
        }
        break;

      case WALK_TO_DESK:
        this._animType = "walk";
        this._moveAlongPath(dt);
        if (!this._path || this._pathIndex >= this._path.length) {
          // Arrived at desk
          if (this.seat) {
            this.x = this.seat.x;
            this.y = this.seat.y;
            this.direction = dirFromString(this.seat.direction);
          }
          this._setState(WORKING);
        }
        break;

      case WORKING:
        this._animType = "typing";
        break;

      case SPEECH_BUBBLE:
        this._animType = "idle";
        if (this.bubble) this.bubble.update(dt);
        break;

      case DESPAWN:
        this._stateTimer += dt;
        if (this._effect) this._effect.update(dt);
        if (this._stateTimer >= DESPAWN_DURATION) {
          this.dead = true;
        }
        break;
    }

    // Advance animation frame
    this._animTimer += dt;
    var maxFrames = FRAME_COUNTS[this._animType] || 1;
    if (this._animTimer >= ANIM_FRAME_DURATION) {
      this._animTimer -= ANIM_FRAME_DURATION;
      this._animFrame = (this._animFrame + 1) % maxFrames;
    }
  };

  Character.prototype._setState = function (newState) {
    // Clear stale spawn/despawn effect when leaving those states
    if (this.state !== newState && newState !== DESPAWN) {
      this._effect = null;
    }
    this.state = newState;
    this._stateTimer = 0;
    this._animFrame = 0;
    this._animTimer = 0;
    this._wanderTimer = 0;
  };

  Character.prototype._tryWander = function (tileMap) {
    var target = window._officeRandomPassableTile(tileMap);
    if (!target) return;
    var path = window._officeFindPath(
      tileMap,
      Math.round(this.x),
      Math.round(this.y),
      target.x,
      target.y,
    );
    if (path && path.length > 1) {
      this._path = path;
      this._pathIndex = 1; // skip start position
      this._targetX = path[1].x;
      this._targetY = path[1].y;
      this.state = WANDER;
      this._animType = "walk";
      this._animFrame = 0;
    }
  };

  Character.prototype._moveAlongPath = function (dt) {
    if (!this._path || this._pathIndex >= this._path.length) return;

    var tx = this._targetX;
    var ty = this._targetY;
    var dx = tx - this.x;
    var dy = ty - this.y;
    var dist = Math.sqrt(dx * dx + dy * dy);
    var step = WALK_SPEED * dt;

    if (dist <= step) {
      // Reached target tile
      this.x = tx;
      this.y = ty;
      this._pathIndex++;
      if (this._pathIndex < this._path.length) {
        var next = this._path[this._pathIndex];
        this.direction = directionBetween(this.x, this.y, next.x, next.y);
        this._targetX = next.x;
        this._targetY = next.y;
      }
    } else {
      // Move toward target
      this.direction = directionBetween(this.x, this.y, tx, ty);
      this.x += (dx / dist) * step;
      this.y += (dy / dist) * step;
    }
  };

  // ---- Task status mapping ----

  Character.prototype.setTaskStatus = function (status, tileMap) {
    // Clear stale spawn effect — it should only persist while in SPAWN state
    if (this.state !== SPAWN && this._effect && this._effect.type === "spawn") {
      this._effect = null;
    }
    switch (status) {
      case "backlog":
        if (
          this.state !== IDLE &&
          this.state !== WANDER &&
          this.state !== SPAWN
        ) {
          this._setState(IDLE);
        }
        this._dismissBubble();
        break;
      case "in_progress":
        if (this.state !== WORKING && this.state !== WALK_TO_DESK) {
          this._walkToDesk(tileMap);
        }
        this._dismissBubble();
        break;
      case "committing":
        if (this.state !== WORKING && this.state !== WALK_TO_DESK) {
          this._walkToDesk(tileMap);
        }
        this._createBubble("committing");
        break;
      case "waiting":
        this._setState(SPEECH_BUBBLE);
        this.bubbleType = "amber";
        this._createBubble("waiting");
        break;
      case "failed":
        this._setState(SPEECH_BUBBLE);
        this.bubbleType = "red";
        this._createBubble("failed");
        break;
      case "done":
        if (this.state !== IDLE) {
          this._setState(IDLE);
        }
        this._dismissBubble();
        break;
      case "cancelled":
        this._setState(DESPAWN);
        if (typeof window._officeMatrixEffect === "function") {
          this._effect = new window._officeMatrixEffect("despawn", 16, 32);
        }
        break;
    }
  };

  Character.prototype._walkToDesk = function (tileMap) {
    if (!this.seat) {
      this._setState(WORKING);
      return;
    }
    if (
      Math.round(this.x) === this.seat.x &&
      Math.round(this.y) === this.seat.y
    ) {
      this._setState(WORKING);
      return;
    }
    if (tileMap) {
      var extra = new Set([this.seat.x + "," + this.seat.y]);
      var path = window._officeFindPath(
        tileMap,
        Math.round(this.x),
        Math.round(this.y),
        this.seat.x,
        this.seat.y,
        extra,
      );
      if (path && path.length > 1) {
        this._path = path;
        this._pathIndex = 1;
        this._targetX = path[1].x;
        this._targetY = path[1].y;
        this.direction = directionBetween(this.x, this.y, path[1].x, path[1].y);
      }
    }
    this.state = WALK_TO_DESK;
    this._animType = "walk";
    this._animFrame = 0;
  };

  // ---- Bubble helpers ----

  Character.prototype._createBubble = function (type) {
    if (typeof window._officeSpeechBubble === "function") {
      this.bubble = new window._officeSpeechBubble(type);
    }
  };

  Character.prototype._dismissBubble = function () {
    if (this.bubble) {
      this.bubble.dismiss();
      this.bubble = null;
    }
  };

  // ---- Draw info ----

  Character.prototype.getDrawInfo = function () {
    return {
      x: this.x,
      y: this.y,
      spriteIndex: this.spriteIndex,
      frameIndex: this._animFrame,
      direction: this.direction,
      state: this.state,
      animType: this._animType,
      effect: this._effect,
      bubble: this.bubble ? this.bubble.getDrawInfo() : null,
    };
  };

  // ---- Exports ----

  window._officeCharacterStates = {
    SPAWN: SPAWN,
    WALK_TO_DESK: WALK_TO_DESK,
    WORKING: WORKING,
    SPEECH_BUBBLE: SPEECH_BUBBLE,
    IDLE: IDLE,
    WANDER: WANDER,
    DESPAWN: DESPAWN,
  };

  window._officeCharacterDirs = {
    DOWN: DOWN,
    LEFT: LEFT,
    RIGHT: RIGHT,
    UP: UP,
  };

  window._officeCharacter = Character;
})();
