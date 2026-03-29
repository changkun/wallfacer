(function () {
  "use strict";

  var STORAGE_KEY = "wallfacer-office-desks";
  var Character = window._officeCharacter;

  // ---- CharacterManager ----

  function CharacterManager(tileMap, seats) {
    this._tileMap = tileMap;
    this._seats = seats || [];
    this._characters = {}; // taskId → Character
    this._deskAssignments = {}; // taskId → deskIndex
    this._nextSpriteIndex = 0;
    this._loadAssignments();
  }

  CharacterManager.prototype.setLayout = function (tileMap, seats) {
    this._tileMap = tileMap;
    this._seats = seats || [];
  };

  CharacterManager.prototype.syncTasks = function (tasks) {
    var taskIds = {};
    for (var i = 0; i < tasks.length; i++) {
      var task = tasks[i];
      taskIds[task.id] = true;

      if (!this._characters[task.id]) {
        // New task — create character
        var seat = this._assignDesk(task.id);
        var spriteIdx = this._nextSpriteIndex % 20;
        this._nextSpriteIndex++;
        this._characters[task.id] = new Character(task.id, spriteIdx, seat);
      }

      // Update status
      this._characters[task.id].setTaskStatus(task.status, this._tileMap);
    }

    // Trigger DESPAWN for removed tasks
    var ids = Object.keys(this._characters);
    for (var j = 0; j < ids.length; j++) {
      var id = ids[j];
      if (!taskIds[id]) {
        var ch = this._characters[id];
        if (ch.state !== "despawn" && !ch.dead) {
          ch.setTaskStatus("cancelled", this._tileMap);
        }
      }
    }

    // Remove dead characters
    this._removeDeadCharacters();
    this.pruneStaleAssignments(Object.keys(taskIds));
  };

  CharacterManager.prototype._assignDesk = function (taskId) {
    // Check existing assignment
    if (this._deskAssignments[taskId] !== undefined) {
      var idx = this._deskAssignments[taskId];
      if (idx < this._seats.length) {
        return this._seats[idx];
      }
    }

    // Find first unassigned seat
    var assigned = {};
    var keys = Object.keys(this._deskAssignments);
    for (var i = 0; i < keys.length; i++) {
      assigned[this._deskAssignments[keys[i]]] = true;
    }

    for (var s = 0; s < this._seats.length; s++) {
      if (!assigned[s]) {
        this._deskAssignments[taskId] = s;
        this._saveAssignments();
        return this._seats[s];
      }
    }

    // All seats taken — assign to last seat as overflow
    var overflow = this._seats.length > 0 ? this._seats.length - 1 : 0;
    this._deskAssignments[taskId] = overflow;
    this._saveAssignments();
    return this._seats[overflow] || { x: 1, y: 1, direction: "down", deskIndex: 0 };
  };

  CharacterManager.prototype.pruneStaleAssignments = function (validIds) {
    var validSet = {};
    for (var i = 0; i < validIds.length; i++) {
      validSet[validIds[i]] = true;
    }
    var keys = Object.keys(this._deskAssignments);
    var changed = false;
    for (var j = 0; j < keys.length; j++) {
      if (!validSet[keys[j]]) {
        delete this._deskAssignments[keys[j]];
        changed = true;
      }
    }
    if (changed) this._saveAssignments();
  };

  CharacterManager.prototype._removeDeadCharacters = function () {
    var ids = Object.keys(this._characters);
    for (var i = 0; i < ids.length; i++) {
      if (this._characters[ids[i]].dead) {
        delete this._characters[ids[i]];
      }
    }
  };

  CharacterManager.prototype.getDrawables = function () {
    var result = [];
    var ids = Object.keys(this._characters);
    for (var i = 0; i < ids.length; i++) {
      var ch = this._characters[ids[i]];
      if (!ch.dead) {
        result.push(ch.getDrawInfo());
      }
    }
    return result;
  };

  CharacterManager.prototype.updateAll = function (dt) {
    var ids = Object.keys(this._characters);
    for (var i = 0; i < ids.length; i++) {
      this._characters[ids[i]].update(dt, this._tileMap);
    }
    this._removeDeadCharacters();
  };

  CharacterManager.prototype.characterAt = function (worldX, worldY) {
    var ids = Object.keys(this._characters);
    for (var i = 0; i < ids.length; i++) {
      var ch = this._characters[ids[i]];
      if (ch.dead) continue;
      // Character occupies a 1×1 tile bounding box
      if (
        worldX >= ch.x &&
        worldX < ch.x + 1 &&
        worldY >= ch.y &&
        worldY < ch.y + 1
      ) {
        return ch;
      }
    }
    return null;
  };

  CharacterManager.prototype.getCharacterByTaskId = function (taskId) {
    return this._characters[taskId] || null;
  };

  // ---- localStorage persistence ----

  CharacterManager.prototype._loadAssignments = function () {
    try {
      var data = localStorage.getItem(STORAGE_KEY);
      if (data) {
        this._deskAssignments = JSON.parse(data);
      }
    } catch (e) {
      this._deskAssignments = {};
    }
  };

  CharacterManager.prototype._saveAssignments = function () {
    try {
      localStorage.setItem(
        STORAGE_KEY,
        JSON.stringify(this._deskAssignments)
      );
    } catch (e) {
      // localStorage full or unavailable — silently ignore
    }
  };

  // ---- Exports ----

  window._officeCharacterManager = CharacterManager;
})();
