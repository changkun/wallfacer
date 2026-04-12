// --- Modal lifecycle helpers ---
//
// Shared modal open/close/dismiss utilities extracted from utils.js.
// Provides consistent show/hide, backdrop-click, and Escape-to-close behavior.

/** Show a modal overlay (remove "hidden", set display to "flex"). */
function openModalPanel(modal: HTMLElement | null): void {
  if (!modal) return;
  modal.classList.remove("hidden");
  modal.style.display = "flex";
}

/** Hide a modal overlay (add "hidden", clear inline display). */
function closeModalPanel(modal: HTMLElement | null): void {
  if (!modal) return;
  modal.classList.add("hidden");
  modal.style.display = "";
}

/**
 * Add click-outside and Escape-to-close behavior to a modal overlay.
 * Returns an unbind function; call it to remove the listeners.
 */
function bindModalDismiss(
  modal: HTMLElement | null,
  onClose: () => void,
): () => void {
  if (!modal || typeof onClose !== "function") return () => {};
  function onBackdropClick(e: MouseEvent): void {
    if (e.target === modal) onClose();
  }
  function onEsc(e: KeyboardEvent): void {
    if (e.key === "Escape") onClose();
  }
  modal.addEventListener("click", onBackdropClick);
  document.addEventListener("keydown", onEsc);
  return function unbind(): void {
    modal.removeEventListener("click", onBackdropClick);
    document.removeEventListener("keydown", onEsc);
  };
}

/**
 * Create a state controller for modal content panels
 * (loading/error/empty/content). The returned function accepts a state name
 * ("loading", "error", "empty", or the configured `contentState`) plus an
 * optional message shown in the error panel.
 */
function createModalStateController(nodes: {
  loadingEl?: HTMLElement | null;
  errorEl?: HTMLElement | null;
  emptyEl?: HTMLElement | null;
  contentEl?: HTMLElement | null;
  contentState?: string;
}): (state: string, msg?: string) => void {
  const loadingEl = nodes && nodes.loadingEl;
  const errorEl = nodes && nodes.errorEl;
  const emptyEl = nodes && nodes.emptyEl;
  const contentEl = nodes && nodes.contentEl;
  const contentState = (nodes && nodes.contentState) || "content";

  return function setModalState(state: string, msg?: string): void {
    if (loadingEl)
      loadingEl.style.display = state === "loading" ? "flex" : "none";
    if (errorEl) errorEl.classList.toggle("hidden", state !== "error");
    if (emptyEl) emptyEl.classList.toggle("hidden", state !== "empty");
    if (contentEl) contentEl.classList.toggle("hidden", state !== contentState);
    if (state === "error" && errorEl)
      errorEl.textContent = msg || "Unknown error";
  };
}
