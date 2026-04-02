// --- Modal lifecycle helpers ---
//
// Shared modal open/close/dismiss utilities extracted from utils.js.
// Provides consistent show/hide, backdrop-click, and Escape-to-close behavior.

/**
 * Show a modal overlay (remove "hidden", set display to "flex").
 * @param {HTMLElement} modal
 */
function openModalPanel(modal) {
  if (!modal) return;
  modal.classList.remove("hidden");
  modal.style.display = "flex";
}

/**
 * Hide a modal overlay (add "hidden", clear inline display).
 * @param {HTMLElement} modal
 */
function closeModalPanel(modal) {
  if (!modal) return;
  modal.classList.add("hidden");
  modal.style.display = "";
}

/**
 * Add click-outside and Escape-to-close behavior to a modal overlay.
 * @param {HTMLElement} modal   The modal backdrop element.
 * @param {function} onClose    Callback invoked on dismiss.
 * @returns {function}          Call to remove the listeners.
 */
function bindModalDismiss(modal, onClose) {
  if (!modal || typeof onClose !== "function") return function () {};
  function onBackdropClick(e) {
    if (e.target === modal) onClose();
  }
  function onEsc(e) {
    if (e.key === "Escape") onClose();
  }
  modal.addEventListener("click", onBackdropClick);
  document.addEventListener("keydown", onEsc);
  return function unbind() {
    modal.removeEventListener("click", onBackdropClick);
    document.removeEventListener("keydown", onEsc);
  };
}

/**
 * Create a state controller for modal content panels (loading/error/empty/content).
 * @param {Object} nodes                    Element references.
 * @param {HTMLElement} [nodes.loadingEl]    Loading indicator element.
 * @param {HTMLElement} [nodes.errorEl]      Error message element.
 * @param {HTMLElement} [nodes.emptyEl]      Empty-state element.
 * @param {HTMLElement} [nodes.contentEl]    Main content element.
 * @param {string}      [nodes.contentState] State name that shows contentEl (default "content").
 * @returns {function(string, string=)}     setState(state, msg?) — switches visible panel.
 */
function createModalStateController(nodes) {
  var loadingEl = nodes && nodes.loadingEl;
  var errorEl = nodes && nodes.errorEl;
  var emptyEl = nodes && nodes.emptyEl;
  var contentEl = nodes && nodes.contentEl;
  var contentState = (nodes && nodes.contentState) || "content";

  return function setModalState(state, msg) {
    if (loadingEl)
      loadingEl.style.display = state === "loading" ? "flex" : "none";
    if (errorEl) errorEl.classList.toggle("hidden", state !== "error");
    if (emptyEl) emptyEl.classList.toggle("hidden", state !== "empty");
    if (contentEl) contentEl.classList.toggle("hidden", state !== contentState);
    if (state === "error" && errorEl)
      errorEl.textContent = msg || "Unknown error";
  };
}
