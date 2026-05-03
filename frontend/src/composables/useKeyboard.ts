import { onMounted, onUnmounted } from 'vue';

export interface KeyboardActions {
  onNewTask?: () => void;
  onSearch?: () => void;
  onSettings?: () => void;
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

    if (inInput) return;

    if (e.key === 'n') {
      e.preventDefault();
      actions.onNewTask?.();
    }
  }

  onMounted(() => document.addEventListener('keydown', handler));
  onUnmounted(() => document.removeEventListener('keydown', handler));
}
