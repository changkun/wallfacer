(function () {
  "use strict";

  var _visible = false;
  var _renderer = null;
  var _camera = null;
  var _spriteCache = null;
  var _canvas = null;
  var _currentLayout = null;
  var _characterManager = null;

  function initOffice() {
    var container = document.getElementById("office-container");
    if (!container) return;

    _canvas = document.createElement("canvas");
    _canvas.id = "office-canvas";
    _canvas.style.width = "100%";
    _canvas.style.height = "100%";
    _canvas.style.display = "block";
    _canvas.style.touchAction = "none"; // prevent browser pan/zoom
    container.appendChild(_canvas);

    // Size canvas to container
    resizeCanvas();

    _spriteCache = new window._officeSpriteCache();
    _camera = new window._officeCamera(_canvas.width, _canvas.height);
    _renderer = new window._officeRenderer(_canvas, _spriteCache, _camera);
    _characterManager = new window._officeCharacterManager(null, []);
    _renderer.setCharacterManager(_characterManager);

    // Attach pan/zoom input
    window._officeAttachInputHandlers(_canvas, _camera, function () {
      if (_currentLayout) {
        _camera.clamp(
          _currentLayout.tileMap.width * window._officeTileSize,
          _currentLayout.tileMap.height * window._officeTileSize
        );
      }
    });

    // Generate initial empty layout
    updateLayout(0);

    // Show toggle button
    var btn = document.getElementById("office-toggle");
    if (btn) {
      btn.classList.remove("hidden");
      btn.addEventListener("click", toggleOffice);
    }

    // Handle window resize
    window.addEventListener("resize", function () {
      resizeCanvas();
      if (_camera) _camera.resize(_canvas.width, _canvas.height);
      if (_renderer) _renderer.invalidateFloorCache();
    });
  }

  function resizeCanvas() {
    if (!_canvas) return;
    var container = _canvas.parentElement;
    if (!container) return;
    _canvas.width = container.clientWidth;
    _canvas.height = container.clientHeight;
  }

  function updateLayout(taskCount) {
    _currentLayout = window._officeGenerateLayout(taskCount);
    if (_renderer) {
      _renderer.setLayout(
        _currentLayout.tileMap,
        _currentLayout.furniture,
        _currentLayout.seats
      );
    }
    if (_characterManager) {
      _characterManager.setLayout(
        _currentLayout.tileMap,
        _currentLayout.seats
      );
    }
  }

  function syncTasks(tasks) {
    if (!_characterManager) return;
    // Ensure layout has enough seats
    if (_currentLayout && tasks.length > _currentLayout.seats.length) {
      updateLayout(tasks.length);
    }
    _characterManager.syncTasks(tasks);
  }

  function showOffice() {
    var board = document.getElementById("board");
    var container = document.getElementById("office-container");
    if (board) board.style.display = "none";
    if (container) container.style.display = "block";
    _visible = true;

    // Resize canvas now that container is visible
    resizeCanvas();
    if (_camera) _camera.resize(_canvas.width, _canvas.height);
    if (_renderer) {
      _renderer.invalidateFloorCache();
      _renderer.start();
    }

    var btn = document.getElementById("office-toggle");
    if (btn) btn.textContent = "Board";
  }

  function hideOffice() {
    var board = document.getElementById("board");
    var container = document.getElementById("office-container");
    if (board) board.style.display = "";
    if (container) container.style.display = "none";
    _visible = false;

    if (_renderer) _renderer.stop();

    var btn = document.getElementById("office-toggle");
    if (btn) btn.textContent = "Office";
  }

  function toggleOffice() {
    if (_visible) {
      hideOffice();
    } else {
      showOffice();
    }
  }

  function isOfficeVisible() {
    return _visible;
  }

  document.addEventListener("DOMContentLoaded", initOffice);

  // ---- Exports ----

  window._officeInit = initOffice;
  window._officeShow = showOffice;
  window._officeHide = hideOffice;
  window._officeIsVisible = isOfficeVisible;
  window._officeUpdateLayout = updateLayout;
  window._officeToggle = toggleOffice;
  window._officeSyncTasks = syncTasks;
  window._officeGetCharacterManager = function () {
    return _characterManager;
  };
})();
