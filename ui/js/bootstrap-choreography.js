/* global focusSpec, activeWorkspaces */
//
// First-spec bootstrap choreography — when the spec-tree SSE fires its
// first non-empty snapshot in a previously chat-first workspace, this
// module drives the short sequence of deferred side-effects that read
// as a single fluid event:
//
//   - t≈0   layout transition to three-pane (already started by the
//            layout state machine — this module does NOT drive that)
//   - t=130ms focusSpec(newPath) so the focused view starts populating
//            as the panes finish opening
//   - t=160ms a top-centre toast slides in with the spec path
//   - t=6160ms toast auto-dismisses
//
// Under `prefers-reduced-motion: reduce` the toast still appears (it
// carries user-visible info) but without the slide-in animation.
// Timers are plain setTimeout + CSS transitions — no
// requestIdleCallback chains, no long JS computations on the UI thread.

var BootstrapChoreography = (function () {
  var AUTO_FOCUS_DELAY_MS = 130;
  var TOAST_DELAY_MS = 160;
  var TOAST_DISMISS_MS = 6000;

  var _activeToast = null;
  var _toastDismissTimer = null;
  var _fired = false;

  function _prefersReducedMotion() {
    if (
      typeof window === "undefined" ||
      typeof window.matchMedia !== "function"
    ) {
      return false;
    }
    try {
      return !!window.matchMedia("(prefers-reduced-motion: reduce)").matches;
    } catch (_e) {
      return false;
    }
  }

  function _dismissToast() {
    if (_toastDismissTimer) {
      clearTimeout(_toastDismissTimer);
      _toastDismissTimer = null;
    }
    if (_activeToast && _activeToast.parentNode) {
      _activeToast.parentNode.removeChild(_activeToast);
    }
    _activeToast = null;
  }

  function _showToast(specPath) {
    _dismissToast();
    if (!document || !document.body) return;
    var toast = document.createElement("div");
    toast.className = "bootstrap-toast";
    if (_prefersReducedMotion()) {
      toast.classList.add("bootstrap-toast--no-motion");
    }
    toast.setAttribute("role", "status");
    toast.setAttribute("aria-live", "polite");
    toast.textContent =
      "Your first spec was created at " +
      specPath +
      ". Rename or move it anytime.";
    toast.addEventListener("click", _dismissToast);
    document.body.appendChild(toast);
    _activeToast = toast;
    _toastDismissTimer = setTimeout(_dismissToast, TOAST_DISMISS_MS);
    return toast;
  }

  // trigger runs the choreography: schedules the auto-focus and the
  // toast relative to "now" (when the SSE event arrived). Idempotent
  // per session — a second trigger is a no-op so reconnect-induced
  // repeated snapshots never reopen the bootstrap toast.
  function trigger(specPath, workspace) {
    if (_fired) return;
    if (!specPath) return;
    _fired = true;
    setTimeout(function () {
      if (typeof focusSpec === "function") {
        var ws =
          workspace ||
          (typeof activeWorkspaces !== "undefined" &&
          activeWorkspaces &&
          activeWorkspaces.length > 0
            ? activeWorkspaces[0]
            : "");
        if (ws) focusSpec(specPath, ws);
      }
    }, AUTO_FOCUS_DELAY_MS);
    setTimeout(function () {
      _showToast(specPath);
    }, TOAST_DELAY_MS);
  }

  function __resetForTests() {
    _fired = false;
    _dismissToast();
  }

  return {
    trigger: trigger,
    dismiss: _dismissToast,
    __resetForTests: __resetForTests,
    // Exposed as read-only constants so tests can reference the exact
    // delay values rather than hard-coding them.
    AUTO_FOCUS_DELAY_MS: AUTO_FOCUS_DELAY_MS,
    TOAST_DELAY_MS: TOAST_DELAY_MS,
    TOAST_DISMISS_MS: TOAST_DISMISS_MS,
  };
})();
