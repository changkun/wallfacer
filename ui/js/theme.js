// --- Theme management ---

function getResolvedTheme(mode) {
  if (mode === "auto")
    return window.matchMedia("(prefers-color-scheme: dark)").matches
      ? "dark"
      : "light";
  return mode;
}

function setTheme(mode) {
  localStorage.setItem("wallfacer-theme", mode);
  document.documentElement.setAttribute("data-theme", getResolvedTheme(mode));
  document.querySelectorAll("#theme-switch button").forEach(function (btn) {
    btn.classList.toggle("active", btn.dataset.mode === mode);
  });
}

// Mark the active theme button on load
(function () {
  var mode = localStorage.getItem("wallfacer-theme") || "auto";
  document.querySelectorAll("#theme-switch button").forEach(function (btn) {
    btn.classList.toggle("active", btn.dataset.mode === mode);
  });
})();

// Re-apply theme when OS preference changes
window
  .matchMedia("(prefers-color-scheme: dark)")
  .addEventListener("change", function () {
    var mode = localStorage.getItem("wallfacer-theme") || "auto";
    if (mode === "auto")
      document.documentElement.setAttribute(
        "data-theme",
        getResolvedTheme("auto"),
      );
  });

// --- Settings modal ---
var settingsTabsInitialized = false;

function setSettingsTab(tabName) {
  var tabButtons = document.querySelectorAll(".settings-tab");
  var tabPanels = document.querySelectorAll(".settings-tab-content");
  var didSetActive = false;

  tabButtons.forEach(function (btn) {
    var isActive = btn.getAttribute("data-settings-tab") === tabName;
    btn.classList.toggle("active", isActive);
    if (btn.setAttribute)
      btn.setAttribute("aria-selected", isActive ? "true" : "false");
    if (isActive) didSetActive = true;
  });

  tabPanels.forEach(function (panel) {
    var isActive = panel.getAttribute("data-settings-tab") === tabName;
    panel.classList.toggle("active", isActive);
    if (isActive) didSetActive = true;
  });

  if (tabName === "trash" && typeof loadDeletedTasks === "function") {
    loadDeletedTasks();
  }
  if (tabName === "sandbox" && typeof loadImageStatus === "function") {
    loadImageStatus();
  }
  if (tabName === "about" && typeof loadSystemStatus === "function") {
    loadSystemStatus();
  }

  return didSetActive;
}

function initSettingsTabs() {
  if (settingsTabsInitialized) return;
  var tabButtons = document.querySelectorAll(".settings-tab");
  if (!tabButtons || tabButtons.length === 0) return;

  tabButtons.forEach(function (btn) {
    btn.addEventListener("click", function () {
      var tabName = btn.getAttribute("data-settings-tab");
      if (tabName) setSettingsTab(tabName);
    });
  });
  settingsTabsInitialized = true;
}

var _settingsDismiss = null;
function openSettings() {
  var modal = document.getElementById("settings-modal");
  modal.classList.remove("hidden");
  modal.style.display = "flex";
  initSettingsTabs();
  setSettingsTab("appearance");
  loadMaxParallel();
  loadOversightInterval();
  loadArchivedTasksPerPage();
  loadAutoPush();
  if (typeof showEnvConfigEditor === "function") {
    showEnvConfigEditor(null);
  }
  if (_settingsDismiss) _settingsDismiss();
  _settingsDismiss = bindModalDismiss(modal, closeSettings);
}

function closeSettings() {
  var modal = document.getElementById("settings-modal");
  if (!modal) return;
  modal.classList.add("hidden");
  modal.style.display = "";
  if (_settingsDismiss) {
    _settingsDismiss();
    _settingsDismiss = null;
  }
}
