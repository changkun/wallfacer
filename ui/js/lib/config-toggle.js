// --- Config toggle factory ---
//
// Creates async toggle handlers for boolean config flags.
// Replaces identical toggle patterns in api.js (autopilot, autotest, etc.).

/**
 * Create an async toggle handler for a boolean config flag.
 * @param {Object} opts
 * @param {string} opts.elementId    Toggle checkbox element ID.
 * @param {string} opts.configKey    Config property name sent in PUT body.
 * @param {function} opts.getState   Returns current boolean state.
 * @param {function} opts.setState   Called with new boolean state from response.
 * @param {string} opts.label        Human-readable label for error messages.
 * @param {function} [opts.onUpdate] Optional callback after successful toggle (e.g. updateAutomationActiveCount).
 * @returns {function}               Async toggle handler.
 */
function createConfigToggle(opts) {
  return async function () {
    var toggle = document.getElementById(opts.elementId);
    var enabled = toggle ? toggle.checked : !opts.getState();
    try {
      var body = {};
      body[opts.configKey] = enabled;
      var res = await api(Routes.config.update(), {
        method: "PUT",
        body: JSON.stringify(body),
      });
      var newState = !!res[opts.configKey];
      opts.setState(newState);
      if (toggle) toggle.checked = newState;
      if (opts.onUpdate) opts.onUpdate();
    } catch (e) {
      showAlert("Error toggling " + opts.label + ": " + e.message);
      if (toggle) toggle.checked = opts.getState();
    }
  };
}
