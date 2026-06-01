// App-wide transient toasts (ports ui/js/dispatch-toast.js + the various
// undo/notification toasts). A single <Toaster> mounted at the app root renders
// the active stack; callers push messages with an optional action button.
import { defineStore } from 'pinia';
import { ref } from 'vue';

export interface ToastAction {
  label: string;
  run: () => void;
}

export interface Toast {
  id: number;
  message: string;
  kind: 'info' | 'success' | 'error';
  action?: ToastAction;
}

export interface ToastOptions {
  kind?: Toast['kind'];
  action?: ToastAction;
  /** Auto-dismiss after this many ms (0 = sticky). Default 6000. */
  timeout?: number;
}

// Visible toast cap. Once the stack would exceed this, the oldest is
// dismissed so a burst of errors can't bury the screen. The legacy UI
// only allowed one toast at a time; five gives the user enough room to
// read a previous one while a new one slides in without piling up.
const MAX_VISIBLE_TOASTS = 5;

export const useToastStore = defineStore('toast', () => {
  const toasts = ref<Toast[]>([]);
  let seq = 0;
  const timers = new Map<number, ReturnType<typeof setTimeout>>();

  function dismiss(id: number) {
    const t = timers.get(id);
    if (t) { clearTimeout(t); timers.delete(id); }
    toasts.value = toasts.value.filter((x) => x.id !== id);
  }

  function push(message: string, opts: ToastOptions = {}): number {
    const id = ++seq;
    toasts.value.push({ id, message, kind: opts.kind ?? 'info', action: opts.action });
    // Drop oldest if we've exceeded the visible cap. Always evict the
    // single overflow so the cap holds even when many pushes race.
    while (toasts.value.length > MAX_VISIBLE_TOASTS) {
      const evicted = toasts.value[0];
      dismiss(evicted.id);
    }
    const timeout = opts.timeout ?? 6000;
    if (timeout > 0) {
      timers.set(id, setTimeout(() => dismiss(id), timeout));
    }
    return id;
  }

  // Convenience: a toast whose action runs once then dismisses the toast.
  function pushWithAction(message: string, label: string, run: () => void, opts: ToastOptions = {}): number {
    let id = -1;
    id = push(message, {
      ...opts,
      action: { label, run: () => { run(); dismiss(id); } },
    });
    return id;
  }

  return { toasts, push, pushWithAction, dismiss };
});
