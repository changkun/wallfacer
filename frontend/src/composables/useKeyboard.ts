import { onMounted, onUnmounted } from 'vue';

export interface KeyboardActions {
  onNewTask?: () => void;
  onSearch?: () => void;
  onSettings?: () => void;
  onTerminal?: () => void;
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

    if (e.key === 'n') {
      e.preventDefault();
      actions.onNewTask?.();
    }
  }

  onMounted(() => document.addEventListener('keydown', handler));
  onUnmounted(() => document.removeEventListener('keydown', handler));
}
