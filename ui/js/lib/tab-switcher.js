// Tab switcher factory — creates a function that toggles active/hidden
// classes on tab buttons and panels following the #<prefix>-tab-<name> /
// #<prefix>-panel-<name> naming convention used throughout the UI.

/**
 * Create a tab switcher that toggles active/hidden classes on tab buttons
 * and panels.
 *
 * @param {Object} opts
 * @param {string[]} opts.tabs - Tab name list.
 * @param {string} opts.prefix - DOM ID prefix (e.g. "right" looks for
 *   #right-tab-X and #right-panel-X).
 * @param {Object} [opts.onActivate] - Optional map of tab name to
 *   callback(tabName) called when that tab becomes active.
 * @param {function} [opts.onSwitch] - Optional callback(tabName) called
 *   after every switch (after onActivate).
 * @returns {function(string): void} The switcher function: call with a tab
 *   name to switch.
 */
function createTabSwitcher(opts) {
  var tabs = opts.tabs;
  var prefix = opts.prefix;
  var onActivate = opts.onActivate || {};
  var onSwitch = opts.onSwitch || null;

  return function switchTab(tab) {
    tabs.forEach(function (t) {
      var btn = document.getElementById(prefix + "-tab-" + t);
      var panel = document.getElementById(prefix + "-panel-" + t);
      var active = t === tab;
      if (btn) btn.classList.toggle("active", active);
      if (panel) panel.classList.toggle("hidden", !active);
    });
    if (onActivate[tab]) {
      onActivate[tab](tab);
    }
    if (onSwitch) {
      onSwitch(tab);
    }
  };
}
