// --- Keyboard Shortcuts Help Modal ---

var _keyboardShortcutsDismiss = null;

function openKeyboardShortcuts() {
  var modal = document.getElementById("keyboard-shortcuts-modal");
  if (!modal) return;
  modal.classList.remove("hidden");
  modal.style.display = "flex";
  if (_keyboardShortcutsDismiss) _keyboardShortcutsDismiss();
  _keyboardShortcutsDismiss = bindModalDismiss(modal, closeKeyboardShortcuts);
}

function closeKeyboardShortcuts() {
  var modal = document.getElementById("keyboard-shortcuts-modal");
  if (!modal) return;
  modal.classList.add("hidden");
  modal.style.display = "";
  if (_keyboardShortcutsDismiss) {
    _keyboardShortcutsDismiss();
    _keyboardShortcutsDismiss = null;
  }
}
