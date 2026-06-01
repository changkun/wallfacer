// Trap Tab focus inside a container while it is open, and restore focus
// to the trigger element on close. Mirrors ui/js/modal-core.js's
// _attachModalFocusTrap + _previousModalFocus.
//
// Usage:
//   const root = ref<HTMLElement | null>(null);
//   useFocusTrap(root, () => isOpen.value);
//
// The composable starts the trap whenever the predicate transitions to
// true, captures document.activeElement at that moment, and restores it
// (if still connected to the DOM) on the next false transition. While
// active, Tab and Shift+Tab cycle through the focusable descendants of
// the container; everything else passes through untouched.

import { watch, onBeforeUnmount, nextTick, type Ref, type WatchSource } from 'vue';

const FOCUSABLE = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  'details > summary',
  '[tabindex]:not([tabindex="-1"])',
].join(',');

function focusable(root: HTMLElement): HTMLElement[] {
  return Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE))
    .filter((el) => !el.hasAttribute('disabled') && el.tabIndex !== -1 && el.offsetParent !== null);
}

export function useFocusTrap(root: Ref<HTMLElement | null>, isOpen: WatchSource<boolean>) {
  let previous: HTMLElement | null = null;
  let attached = false;

  function onKeydown(e: KeyboardEvent) {
    if (e.key !== 'Tab') return;
    const r = root.value;
    if (!r) return;
    const list = focusable(r);
    if (list.length === 0) {
      e.preventDefault();
      r.focus();
      return;
    }
    const first = list[0];
    const last = list[list.length - 1];
    const active = document.activeElement as HTMLElement | null;
    if (e.shiftKey) {
      if (active === first || !r.contains(active)) {
        e.preventDefault();
        last.focus();
      }
    } else if (active === last) {
      e.preventDefault();
      first.focus();
    }
  }

  async function attach() {
    if (attached) return;
    previous = document.activeElement as HTMLElement | null;
    document.addEventListener('keydown', onKeydown, true);
    attached = true;
    await nextTick();
    const r = root.value;
    if (!r) return;
    const list = focusable(r);
    (list[0] ?? r).focus();
  }

  function detach() {
    if (!attached) return;
    document.removeEventListener('keydown', onKeydown, true);
    attached = false;
    if (previous && previous.isConnected) {
      previous.focus();
    }
    previous = null;
  }

  watch(isOpen, (open) => {
    if (open) void attach();
    else detach();
  }, { immediate: true });

  onBeforeUnmount(detach);
}
