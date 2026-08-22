// ChatComposer button visibility: the slash "/" and mention "@" shortcuts must
// be discoverable from an empty composer, not hidden until the user types.
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia } from 'pinia';

import ChatComposer from './ChatComposer.vue';

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(ChatComposer, { streaming: false });
  // ChatComposer reads the task store to learn which harnesses are installed.
  app.use(createPinia());
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
