// Analytics mode controller. The three analytics views (Usage, Tokens & cost,
// Execution timing) used to be separate floating modals. They now live as
// tab panels inside the #analytics-mode-container mode page.
//
// Strategy: the three original modal partials remain the single source of
// truth for their markup. On first entry to analytics mode, we reparent
// their root nodes into #analytics-panels and add a body class that
// neutralises the modal chrome via CSS. Tabs show/hide among the three.
(function () {
  var TABS = ["usage", "analytics", "timing"];
  var MODAL_IDS = {
    usage: "usage-stats-modal",
    analytics: "stats-modal",
    timing: "span-stats-modal",
  };
  var LOADERS = {
    usage: function () {
      if (typeof window.loadUsageStats === "function")
        return window.loadUsageStats();
      if (typeof window.showUsageStats === "function") window.showUsageStats();
    },
    analytics: function () {
      if (typeof window.loadStatsModal === "function")
        return window.loadStatsModal();
      if (typeof window.openStatsModal === "function") window.openStatsModal();
    },
    timing: function () {
      if (typeof window.loadSpanStats === "function")
        return window.loadSpanStats();
      if (typeof window.showSpanStats === "function") window.showSpanStats();
    },
  };

  var _mounted = false;
  var _activeTab = "usage";

  function _ensureMounted() {
    if (_mounted) return;
    var panelsEl = document.getElementById("analytics-panels");
    if (!panelsEl) return;
    for (var i = 0; i < TABS.length; i++) {
      var node = document.getElementById(MODAL_IDS[TABS[i]]);
      if (node && node.parentNode !== panelsEl) {
        panelsEl.appendChild(node);
      }
    }
    _mounted = true;
  }

  function _showTabPanel(tab) {
    for (var i = 0; i < TABS.length; i++) {
      var id = MODAL_IDS[TABS[i]];
      var el = document.getElementById(id);
      if (!el) continue;
      if (TABS[i] === tab) {
        el.classList.remove("hidden");
        el.style.removeProperty("display");
      } else {
        el.classList.add("hidden");
        el.style.display = "none";
      }
    }
    var tabEls = document.querySelectorAll(
      "#analytics-mode-container [data-analytics-tab]",
    );
    for (var j = 0; j < tabEls.length; j++) {
      var t = tabEls[j];
      if (t.getAttribute("data-analytics-tab") === tab) {
        t.classList.add("active");
        t.setAttribute("aria-selected", "true");
      } else {
        t.classList.remove("active");
        t.setAttribute("aria-selected", "false");
      }
    }
  }

  window.switchAnalyticsTab = function (target) {
    if (TABS.indexOf(target) < 0) return;
    _activeTab = target;
    _ensureMounted();
    _showTabPanel(target);
    var loader = LOADERS[target];
    if (typeof loader === "function") {
      try {
        loader();
      } catch (_e) {
        // Best-effort; individual loaders log their own errors.
      }
    }
  };

  // enterAnalyticsMode is called by _applyMode in spec-mode.js when the
  // mode becomes 'analytics'. It mounts the panels and activates the
  // last-used tab (default: usage).
  window.enterAnalyticsMode = function () {
    _ensureMounted();
    window.switchAnalyticsTab(_activeTab);
  };

  window.openAnalytics = function (tab) {
    if (typeof window.switchMode === "function") {
      window.switchMode("analytics", { persist: true });
    }
    if (tab) _activeTab = tab;
  };
})();
