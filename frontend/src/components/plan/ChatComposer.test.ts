// ChatComposer button visibility: the slash "/" and mention "@" shortcuts must
// be discoverable from an empty composer, not hidden until the user types.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';

const memStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (k: string) => (memStore.has(k) ? memStore.get(k)! : null),
  setItem: (k: string, v: string) => { memStore.set(k, String(v)); },
  removeItem: (k: string) => { memStore.delete(k); },
  clear: () => { memStore.clear(); },
});

import ChatComposer from './ChatComposer.vue';

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(ChatComposer, { streaming: false });
  app.mount(host);
  await nextTick();
  return { app, host };
}

describe('ChatComposer', () => {
  beforeEach(() => {
    globalThis.fetch = (async () => new Response('[]', { status: 200 })) as never;
  });
  afterEach(() => { document.body.innerHTML = ''; });

  it('shows the / and @ shortcut buttons when the input is empty', async () => {
    const { host } = await mount();
    const actions = host.querySelectorAll('.pcp-composer-actions .pcp-composer-action');
    expect(actions.length).toBe(2);
    expect(Array.from(actions).map((b) => b.textContent?.trim())).toEqual(['/', '@']);
  });
});
