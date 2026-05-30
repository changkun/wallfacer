// App-wide confirm/alert dialogs, replacing ui/js/utils.js's showConfirm /
// showAlert. A single <ConfirmDialog> mounted at the app root renders whatever
// request is active; callers await a promise that resolves when the user picks.
import { defineStore } from 'pinia';
import { ref } from 'vue';

export interface ConfirmRequest {
  title?: string;
  message: string;
  confirmLabel: string;
  cancelLabel: string;
  danger: boolean;
  /** When true the dialog has only a single dismiss button (alert mode). */
  alert: boolean;
}

export interface ConfirmOptions {
  title?: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}

export const useDialogStore = defineStore('dialog', () => {
  const active = ref<ConfirmRequest | null>(null);
  let resolver: ((ok: boolean) => void) | null = null;

  function open(req: ConfirmRequest): Promise<boolean> {
    // If a dialog is already open, resolve it false before replacing.
    if (resolver) resolver(false);
    active.value = req;
    return new Promise<boolean>((resolve) => { resolver = resolve; });
  }

  function settle(ok: boolean) {
    const r = resolver;
    resolver = null;
    active.value = null;
    if (r) r(ok);
  }

  function confirm(opts: ConfirmOptions): Promise<boolean> {
    return open({
      title: opts.title,
      message: opts.message,
      confirmLabel: opts.confirmLabel ?? 'Confirm',
      cancelLabel: opts.cancelLabel ?? 'Cancel',
      danger: opts.danger ?? false,
      alert: false,
    });
  }

  function alert(message: string, title?: string): Promise<boolean> {
    return open({
      title,
      message,
      confirmLabel: 'OK',
      cancelLabel: 'OK',
      danger: false,
      alert: true,
    });
  }

  return { active, confirm, alert, accept: () => settle(true), dismiss: () => settle(false) };
});
