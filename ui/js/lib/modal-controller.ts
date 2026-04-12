// --- Modal controller factory ---
//
// Creates paired open/close functions for a modal with automatic
// dismiss-listener lifecycle management (backdrop click + Escape).

/**
 * Create a modal open/close controller with automatic dismiss binding.
 * Returns {open, close} functions that manage the modal lifecycle.
 * `onOpen` runs after the modal is shown (e.g. for loading data);
 * `onClose` runs after it is hidden (e.g. for clearInterval cleanup).
 */
function createModalController(
  modalId: string,
  opts?: {
    onOpen?: () => void;
    onClose?: () => void;
  },
): { open: () => void; close: () => void } {
  let _dismiss: (() => void) | null = null;
  const options = opts || {};

  function close(): void {
    const modal = document.getElementById(modalId);
    closeModalPanel(modal);
    if (_dismiss) {
      _dismiss();
      _dismiss = null;
    }
    if (typeof options.onClose === "function") options.onClose();
  }

  function open(): void {
    const modal = document.getElementById(modalId);
    if (!modal) return;
    openModalPanel(modal);
    if (_dismiss) _dismiss();
    _dismiss = bindModalDismiss(modal, close);
    if (typeof options.onOpen === "function") options.onOpen();
  }

  return { open: open, close: close };
}
