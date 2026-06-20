// Regression: the Trash modal must open as a fixed, full-screen, centered
// overlay like every other modal. It once used a bare `.modal-overlay` class
// without the `fixed inset-0 … flex items-center justify-center` positioning,
// so the dialog rendered in static flow below the fold — clicking Trash looked
// like nothing happened ("the trash can cannot be clicked").
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, h, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';

vi.mock('../api/client', () => ({
  api: vi.fn(async () => []),
  authHeaders: () => ({}),
}));

import TrashModal from './TrashModal.vue';

let app: App | null = null;

async function mount(modelValue: boolean): Promise<void> {
  setActivePinia(createPinia());
  app = createApp({ render: () => h(TrashModal, { modelValue }) });
  app.mount(document.createElement('div'));
  await nextTick();
  await nextTick();
}

beforeEach(() => { document.body.innerHTML = ''; });
afterEach(() => { app?.unmount(); app = null; document.body.innerHTML = ''; });

describe('TrashModal overlay', () => {
  it('opens as a fixed, centered full-screen overlay (not static flow)', async () => {
    await mount(true);
    // Teleported to body.
    const overlay = document.querySelector('.modal-overlay');
    expect(overlay).not.toBeNull();
    const cls = overlay!.classList;
    // The positioning classes that lift it out of static flow and center it.
    expect(cls.contains('fixed')).toBe(true);
    expect(cls.contains('inset-0')).toBe(true);
    expect(cls.contains('flex')).toBe(true);
    expect(cls.contains('items-center')).toBe(true);
    expect(cls.contains('justify-center')).toBe(true);
  });

  it('renders nothing when closed', async () => {
    await mount(false);
    expect(document.querySelector('.modal-overlay')).toBeNull();
  });
});
