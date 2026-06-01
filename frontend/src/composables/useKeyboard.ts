import { onMounted, onUnmounted } from 'vue';

export interface KeyboardActions {
  onNewTask?: () => void;
  onSearch?: () => void;
  onFocusSearch?: () => void;
  onSettings?: () => void;
  onTerminal?: () => void;
  onShortcuts?: () => void;
  onExplorer?: () => void;
  onToggleMode?: () => void;
}

export function useKeyboard(actions: KeyboardActions) {
  function handler(e: KeyboardEvent) {
    const target = e.target as HTMLElement;
    const inInput = target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' || target.isContentEditable;

    if ((e.metaKey || e.ctrlKey) && e.key === 'k') {
      e.preventDefault();
      actions.onSearch?.();
      return;
    }

    if ((e.metaKey || e.ctrlKey) && e.key === ',') {
      e.preventDefault();
      actions.onSettings?.();
      return;
    }

    // Ctrl+` toggles the terminal panel (matches the legacy UI shortcut).
    if (e.ctrlKey && (e.key === '`' || e.code === 'Backquote')) {
      e.preventDefault();
      actions.onTerminal?.();
      return;
    }

    if (inInput) return;

    if (e.key === '?' || (e.key === '/' && e.shiftKey)) {
      e.preventDefault();
      actions.onShortcuts?.();
      return;
    }

    // Bare "/" focuses the search bar — matches the shortcut hint shown in
    // KeyboardShortcutsModal. Shift+"/" handled above as "?".
    if (e.key === '/' && !e.shiftKey && !e.metaKey && !e.ctrlKey && !e.altKey) {
      e.preventDefault();
      actions.onFocusSearch?.();
      return;
    }

    if (e.key === 'n') {
      e.preventDefault();
      actions.onNewTask?.();
      return;
    }

    // e = open the file explorer; p = toggle board ↔ plan (legacy ui shortcuts).
    if (e.key === 'e') {
      e.preventDefault();
      actions.onExplorer?.();
      return;
    }
    if (e.key === 'p') {
      e.preventDefault();
      actions.onToggleMode?.();
    }
  }

  onMounted(() => document.addEventListener('keydown', handler));
  onUnmounted(() => document.removeEventListener('keydown', handler));
}
