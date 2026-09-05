// The confirm dialog is the 440px dialog shape: the forward button is the ink
// button, or the danger ghost when the action destroys something, and a
// prompt request adds a field. Pins the variant classes the design system
// relies on (specs/shared/console-redesign/panels-and-overlays.md).
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import ConfirmDialog from './ConfirmDialog.vue';
import { useDialogStore } from '../stores/dialog';

let pinia: Pinia;
let app: App | null = null;

beforeEach(() => {
  pinia = createPinia();
  setActivePinia(pinia);
  document.body.innerHTML = '';
  app = createApp(ConfirmDialog);
  app.use(pinia);
  app.mount(document.body.appendChild(document.createElement('div')));
});
afterEach(() => { app?.unmount(); app = null; document.body.innerHTML = ''; });

function buttons(): HTMLButtonElement[] {
  return Array.from(document.querySelectorAll<HTMLButtonElement>('.dialog-foot .btn'));
}

describe('ConfirmDialog', () => {
  it('renders a danger request with a ghost danger confirm and the warning glyph', async () => {
    const dialog = useDialogStore();
    const p = dialog.confirm({ title: 'Delete workspace', message: 'Gone for good.', confirmLabel: 'Delete', danger: true });
    await nextTick();
    const card = document.querySelector('.dialog');
    expect(card).not.toBeNull();
    expect(card!.classList.contains('confirm--danger')).toBe(true);
    expect(document.querySelector('.dialog-title')?.textContent).toBe('Delete workspace');
    expect(document.querySelector('.confirm-icon')).not.toBeNull();
    const [cancel, confirm] = buttons();
    expect(cancel.classList.contains('ghost')).toBe(true);
    expect(cancel.classList.contains('danger')).toBe(false);
    expect(confirm.classList.contains('ghost')).toBe(true);
    expect(confirm.classList.contains('danger')).toBe(true);
    confirm.click();
    await expect(p).resolves.toBe(true);
  });

  it('renders a plain request with the ink confirm and no head when untitled', async () => {
    const dialog = useDialogStore();
    const p = dialog.confirm({ message: 'Proceed?' });
    await nextTick();
    expect(document.querySelector('.dialog-head')).toBeNull();
    expect(document.querySelector('.confirm-icon')).toBeNull();
    const [cancel, confirm] = buttons();
    expect(confirm.classList.contains('ghost')).toBe(false);
    expect(confirm.classList.contains('danger')).toBe(false);
    cancel.click();
    await expect(p).resolves.toBe(false);
  });

  it('renders a prompt request with a field and resolves its value', async () => {
    const dialog = useDialogStore();
    const p = dialog.prompt({ title: 'Rename', message: 'New name:', initial: 'old' });
    await nextTick();
    const input = document.querySelector<HTMLInputElement>('input.field.confirm-input');
    expect(input).not.toBeNull();
    input!.value = 'fresh';
    input!.dispatchEvent(new Event('input'));
    buttons()[1].click();
    await expect(p).resolves.toBe('fresh');
  });
});
