// The spec tree can be folded to a rail (like the Board's file explorer). The
// panel itself doesn't own the layout slot, so it asks its parent to collapse
// via a `collapse` event when the toolbar's collapse button is clicked.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h, nextTick, ref, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';

const memStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (k: string) => (memStore.has(k) ? memStore.get(k)! : null),
  setItem: (k: string, v: string) => { memStore.set(k, String(v)); },
  removeItem: (k: string) => { memStore.delete(k); },
  clear: () => { memStore.clear(); },
});

import SpecTreePanel from './SpecTreePanel.vue';

describe('SpecTreePanel collapse', () => {
  beforeEach(() => {
    memStore.clear();
    setActivePinia(createPinia());
    globalThis.fetch = vi.fn(async () => new Response('{}', { status: 200 })) as never;
  });
  afterEach(() => { document.body.innerHTML = ''; });

  it('emits "collapse" when the toolbar collapse button is clicked', async () => {
    const collapsed = ref(false);
    const host = document.createElement('div');
    document.body.appendChild(host);
    const app: App = createApp({
      render: () => h(SpecTreePanel, { onCollapse: () => { collapsed.value = true; } }),
    });
    app.mount(host);
    await nextTick();

    const btn = host.querySelector('.stp-collapse') as HTMLButtonElement;
    expect(btn).not.toBeNull();
    btn.click();
    await nextTick();
    expect(collapsed.value).toBe(true);
    app.unmount();
  });
});
