import { describe, it, expect, beforeEach } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useDialogStore } from './dialog';

describe('dialog store', () => {
  beforeEach(() => setActivePinia(createPinia()));

  it('confirm() opens a request and resolves true on accept', async () => {
    const d = useDialogStore();
    const p = d.confirm({ message: 'sure?', danger: true });
    expect(d.active?.message).toBe('sure?');
    expect(d.active?.danger).toBe(true);
    expect(d.active?.alert).toBe(false);
    d.accept();
    await expect(p).resolves.toBe(true);
    expect(d.active).toBeNull();
  });

  it('confirm() resolves false on dismiss', async () => {
    const d = useDialogStore();
    const p = d.confirm({ message: 'x' });
    d.dismiss();
    await expect(p).resolves.toBe(false);
  });

  it('opening a second dialog resolves the first as false', async () => {
    const d = useDialogStore();
    const first = d.confirm({ message: 'first' });
    const second = d.confirm({ message: 'second' });
    await expect(first).resolves.toBe(false);
    expect(d.active?.message).toBe('second');
    d.accept();
    await expect(second).resolves.toBe(true);
  });

  it('alert() uses defaults and single-button mode', () => {
    const d = useDialogStore();
    d.alert('heads up', 'Notice');
    expect(d.active).toMatchObject({ message: 'heads up', title: 'Notice', alert: true, confirmLabel: 'OK' });
  });
});
