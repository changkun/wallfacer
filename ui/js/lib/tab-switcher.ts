// Tab switcher factory — creates a function that toggles active/hidden
// classes on tab buttons and panels following the #<prefix>-tab-<name> /
// #<prefix>-panel-<name> naming convention used throughout the UI.

/**
 * Create a tab switcher that toggles active/hidden classes on tab buttons
 * and panels. Call the returned function with a tab name to switch.
 */
function createTabSwitcher(opts: {
  tabs: string[];
  prefix: string;
  onActivate?: Record<string, (tab: string) => void>;
  onSwitch?: (tab: string) => void;
}): (tab: string) => void {
  const tabs = opts.tabs;
  const prefix = opts.prefix;
  const onActivate = opts.onActivate || {};
  const onSwitch = opts.onSwitch || null;

  return function switchTab(tab: string): void {
    tabs.forEach((t) => {
      const btn = document.getElementById(prefix + "-tab-" + t);
      const panel = document.getElementById(prefix + "-panel-" + t);
      const active = t === tab;
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
