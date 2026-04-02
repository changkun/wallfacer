// --- Modal controller factory ---
//
// Creates paired open/close functions for a modal with automatic
// dismiss-listener lifecycle management (backdrop click + Escape).

/**
 * Create a modal open/close controller with automatic dismiss binding.
 * Returns {open, close} functions that manage the modal lifecycle.
 *
 * @param {string} modalId      The modal element ID.
 * @param {Object} [opts]       Options.
 * @param {function} [opts.onOpen]   Called after the modal is shown (for loading data, etc.).
 * @param {function} [opts.onClose]  Called after the modal is hidden (for cleanup like clearInterval).
 * @returns {{open: function, close: function}}
 */
function createModalController(modalId, opts) {
  var _dismiss = null;
  var options = opts || {};

  function close() {
    var modal = document.getElementById(modalId);
    closeModalPanel(modal);
    if (_dismiss) {
      _dismiss();
      _dismiss = null;
    }
    if (typeof options.onClose === "function") options.onClose();
  }

  function open() {
    var modal = document.getElementById(modalId);
    if (!modal) return;
    openModalPanel(modal);
    if (_dismiss) _dismiss();
    _dismiss = bindModalDismiss(modal, close);
    if (typeof options.onOpen === "function") options.onOpen();
  }

  return { open: open, close: close };
}
