(function () {
  "use strict";

  var VIEW_PREF_KEY = "wallfacer-office-view";
  var SR_DEBOUNCE_MS = 2000;

  var _visible = false;
  var _renderer = null;
  var _camera = null;
  var _spriteCache = null;
  var _canvas = null;
  var _currentLayout = null;
  var _characterManager = null;
  var _minimap = null;
  var _pendingTasks = null; // buffered task list when office is hidden
  var _lastUpdateTime = 0;
  var _srSummaryEl = null;
  var _srDebounceTimer = null;

  function initOffice() {
    var container = document.getElementById("office-container");
    if (!container) return;

    _canvas = document.createElement("canvas");
    _canvas.id = "office-canvas";
    _canvas.style.width = "100%";
    _canvas.style.height = "100%";
    _canvas.style.display = "block";
    _canvas.style.touchAction = "none"; // prevent browser pan/zoom
    _canvas.setAttribute("role", "img");
    _canvas.setAttribute(
      "aria-label",
      "Pixel office view showing task agent characters",
    );
    container.appendChild(_canvas);

    // Screen-reader summary
    _srSummaryEl = document.createElement("div");
    _srSummaryEl.id = "office-sr-summary";
    _srSummaryEl.className = "sr-only";
    _srSummaryEl.setAttribute("aria-live", "polite");
    container.appendChild(_srSummaryEl);

    // Size canvas to container
    resizeCanvas();

    _spriteCache = new window._officeSpriteCache();
    _camera = new window._officeCamera(_canvas.width, _canvas.height);
    _renderer = new window._officeRenderer(_canvas, _spriteCache, _camera);
    _renderer.loadSprites(); // load LimeZu sprite sheets
    _characterManager = new window._officeCharacterManager(null, []);
    _renderer.setCharacterManager(_characterManager);

    // Interaction layer
    if (window._officeInteraction) {
      var interaction = new window._officeInteraction(
        _canvas,
        _camera,
        _characterManager,
      );
      _renderer.setInteraction(interaction);
    }

    // Minimap
    if (window._officeMinimap) {
      _minimap = new window._officeMinimap(container, _camera, function () {
        if (_currentLayout) {
          _camera.clamp(
            _currentLayout.tileMap.width * window._officeTileSize,
            _currentLayout.tileMap.height * window._officeTileSize,
          );
        }
      });
    }

    // Attach pan/zoom input
    window._officeAttachInputHandlers(_canvas, _camera, function () {
      if (_currentLayout) {
        _camera.clamp(
          _currentLayout.tileMap.width * window._officeTileSize,
          _currentLayout.tileMap.height * window._officeTileSize,
        );
      }
    });

    // Generate initial empty layout
    updateLayout(0);

    // Detect assets and show status bar office button
    var devMode =
      typeof location !== "undefined" &&
      location.search &&
      location.search.indexOf("office=dev") !== -1;

    if (devMode) {
      _showOfficeBtn();
    } else if (typeof window._officeDetectAssets === "function") {
      window._officeDetectAssets().then(function (available) {
        if (available) _showOfficeBtn();
      });
    }

    function _showOfficeBtn() {
      var btn = document.getElementById("status-bar-office-btn");
      if (btn) btn.classList.remove("hidden");
    }

    // Register for task state changes from SSE
    if (typeof registerTaskChangeListener === "function") {
      registerTaskChangeListener(officeSync);
    }

    // Handle window resize
    window.addEventListener("resize", function () {
      resizeCanvas();
      if (_camera) _camera.resize(_canvas.width, _canvas.height);
      if (_renderer) {
        _renderer.invalidateFloorCache();
        if (_visible) _renderer.fitToViewport();
      }
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
        _currentLayout.seats,
      );
    }
    if (_characterManager) {
      _characterManager.setLayout(_currentLayout.tileMap, _currentLayout.seats);
    }
  }

  function officeSync(taskList) {
    if (!_characterManager) return;

    // Filter to non-archived tasks
    var activeTasks = [];
    for (var i = 0; i < taskList.length; i++) {
      if (!taskList[i].archived) {
        activeTasks.push(taskList[i]);
      }
    }

    if (!_visible) {
      // Buffer for when office becomes visible
      _pendingTasks = activeTasks;
      return;
    }

    _applySync(activeTasks);
  }

  function _applySync(activeTasks) {
    // Expand layout if needed
    if (_currentLayout && activeTasks.length > _currentLayout.seats.length) {
      updateLayout(activeTasks.length);
    }
    _characterManager.syncTasks(activeTasks);

    // Update minimap visibility
    if (_minimap && _currentLayout) {
      _minimap.updateVisibility(_currentLayout.seats.length);
      _minimap.setLayout(_currentLayout.tileMap, _currentLayout.furniture);
    }

    // Update SR summary (debounced)
    _scheduleSRUpdate(activeTasks);
  }

  function syncTasks(taskList) {
    officeSync(taskList);
  }

  function showOffice() {
    _visible = true;

    // Resize canvas now that container is visible
    resizeCanvas();
    if (_camera) _camera.resize(_canvas.width, _canvas.height);
    if (_renderer) {
      _renderer.invalidateFloorCache();
      _renderer.fitToViewport();
      _renderer.start();
    }

    // Apply buffered task updates
    if (_pendingTasks) {
      _applySync(_pendingTasks);
      _pendingTasks = null;
    }

    // Start character update loop
    _lastUpdateTime = performance.now();
    _startUpdateLoop();
  }

  function hideOffice() {
    _visible = false;

    if (_renderer) _renderer.stop();
    _stopUpdateLoop();
  }

  function isOfficeVisible() {
    return _visible;
  }

  var _updateRafId = null;
  function _startUpdateLoop() {
    if (_updateRafId !== null) return;
    _lastUpdateTime = performance.now();
    function tick(now) {
      var dt = (now - _lastUpdateTime) / 1000;
      _lastUpdateTime = now;
      if (dt > 0.1) dt = 0.1; // cap dt to avoid large jumps
      if (_characterManager) _characterManager.updateAll(dt);
      if (_camera && _camera.updateFollow) _camera.updateFollow();
      if (_minimap && _characterManager) {
        _minimap.update(now, _characterManager.getDrawables());
      }
      _updateRafId = requestAnimationFrame(tick);
    }
    _updateRafId = requestAnimationFrame(tick);
  }
  function _stopUpdateLoop() {
    if (_updateRafId !== null) {
      cancelAnimationFrame(_updateRafId);
      _updateRafId = null;
    }
  }

  function _scheduleSRUpdate(activeTasks) {
    if (!_srSummaryEl) return;
    if (_srDebounceTimer) clearTimeout(_srDebounceTimer);
    _srDebounceTimer = setTimeout(function () {
      _updateSRSummary(activeTasks);
    }, SR_DEBOUNCE_MS);
  }

  function _updateSRSummary(activeTasks) {
    if (!_srSummaryEl) return;
    if (!activeTasks || activeTasks.length === 0) {
      _srSummaryEl.textContent = "No tasks in office view.";
      return;
    }
    var parts = [];
    for (var i = 0; i < activeTasks.length; i++) {
      var t = activeTasks[i];
      var name = (t.title || t.prompt || t.id).substring(0, 25);
      var status = (t.status || "unknown").replace(/_/g, " ");
      parts.push('"' + name + '" ' + status);
    }
    _srSummaryEl.textContent =
      activeTasks.length + " tasks: " + parts.join(", ");
  }

  document.addEventListener("DOMContentLoaded", initOffice);

  // ---- Exports ----

  window._officeInit = initOffice;
  window._officeShow = showOffice;
  window._officeHide = hideOffice;
  window._officeIsVisible = isOfficeVisible;
  window._officeUpdateLayout = updateLayout;
  window._officeSyncTasks = syncTasks;
  window._officeGetCharacterManager = function () {
    return _characterManager;
  };
})();
