(function () {
  "use strict";

  var DIRS = [
    { dx: 0, dy: -1 },
    { dx: 0, dy: 1 },
    { dx: -1, dy: 0 },
    { dx: 1, dy: 0 },
  ];

  function findPath(tileMap, startX, startY, goalX, goalY, extraPassable) {
    if (startX === goalX && startY === goalY) {
      return [{ x: startX, y: startY }];
    }

    var extra = extraPassable || null;
    var startKey = startX + "," + startY;
    var goalKey = goalX + "," + goalY;

    var queue = [{ x: startX, y: startY }];
    var visited = {};
    visited[startKey] = null; // parent pointer (null = start)

    var head = 0;
    while (head < queue.length) {
      var cur = queue[head++];

      for (var i = 0; i < DIRS.length; i++) {
        var nx = cur.x + DIRS[i].dx;
        var ny = cur.y + DIRS[i].dy;
        var nkey = nx + "," + ny;

        if (nkey in visited) continue;

        var passable = tileMap.isPassable(nx, ny) || (extra && extra.has(nkey));
        if (!passable) continue;

        visited[nkey] = cur;

        if (nx === goalX && ny === goalY) {
          return reconstructPath(visited, goalX, goalY);
        }

        queue.push({ x: nx, y: ny });
      }
    }

    return null; // no path
  }

  function reconstructPath(visited, goalX, goalY) {
    var path = [];
    var key = goalX + "," + goalY;
    var node = { x: goalX, y: goalY };

    while (node !== null) {
      path.push({ x: node.x, y: node.y });
      node = visited[node.x + "," + node.y];
    }

    path.reverse();
    return path;
  }

  function randomPassableTile(tileMap) {
    var passable = [];
    for (var y = 0; y < tileMap.height; y++) {
      for (var x = 0; x < tileMap.width; x++) {
        if (tileMap.isPassable(x, y)) {
          passable.push({ x: x, y: y });
        }
      }
    }
    if (passable.length === 0) return null;
    var idx = Math.floor(Math.random() * passable.length);
    return passable[idx];
  }

  // ---- Exports ----

  window._officeFindPath = findPath;
  window._officeRandomPassableTile = randomPassableTile;
})();
