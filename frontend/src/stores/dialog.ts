// App-wide confirm/alert/prompt dialogs, replacing ui/js/utils.js's showConfirm,
// showAlert, and showPrompt. A single <ConfirmDialog> mounted at the app root
// renders whatever request is active; callers await a promise that resolves
// when the user picks.
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
  /** When set, dialog renders a text input pre-filled with this value (prompt mode). */
  prompt?: { initial: string; placeholder?: string };
}

export interface ConfirmOptions {
  title?: string;
  message: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
}

export interface PromptOptions {
  title?: string;
  message: string;
  initial?: string;
  placeholder?: string;
  confirmLabel?: string;
  cancelLabel?: string;
}

export const useDialogStore = defineStore('dialog', () => {
  const active = ref<ConfirmRequest | null>(null);
  // Confirm/alert resolve with boolean; prompt resolves with string|null.
  let resolver: ((value: boolean | string | null) => void) | null = null;
  let promptValue = '';

  function open(req: ConfirmRequest): Promise<boolean | string | null> {
    // If a dialog is already open, resolve it false/null based on *its* mode
    // before replacing it.
    if (resolver) resolver(active.value?.prompt ? null : false);
    active.value = req;
    promptValue = req.prompt?.initial ?? '';
    return new Promise((resolve) => { resolver = resolve; });
  }

  function settle(ok: boolean) {
    const r = resolver;
    const isPrompt = !!active.value?.prompt;
    resolver = null;
    const result: boolean | string | null = isPrompt ? (ok ? promptValue : null) : ok;
    active.value = null;
    promptValue = '';
    if (r) r(result);
  }

  function setPromptValue(v: string) { promptValue = v; }

  function confirm(opts: ConfirmOptions): Promise<boolean> {
    return open({
      title: opts.title,
      message: opts.message,
      confirmLabel: opts.confirmLabel ?? 'Confirm',
      cancelLabel: opts.cancelLabel ?? 'Cancel',
      danger: opts.danger ?? false,
      alert: false,
    }) as Promise<boolean>;
  }

  function alert(message: string, title?: string): Promise<boolean> {
    return open({
      title,
      message,
      confirmLabel: 'OK',
      cancelLabel: 'OK',
      danger: false,
      alert: true,
    }) as Promise<boolean>;
  }

  function prompt(opts: PromptOptions): Promise<string | null> {
    return open({
      title: opts.title,
      message: opts.message,
      confirmLabel: opts.confirmLabel ?? 'OK',
      cancelLabel: opts.cancelLabel ?? 'Cancel',
      danger: false,
      alert: false,
      prompt: { initial: opts.initial ?? '', placeholder: opts.placeholder },
    }) as Promise<string | null>;
  }

  return {
    active,
    confirm,
    alert,
    prompt,
    setPromptValue,
    accept: () => settle(true),
    dismiss: () => settle(false),
  };
});
