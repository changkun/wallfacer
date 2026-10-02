// ChatComposer button visibility: the slash "/" and mention "@" shortcuts must
// be discoverable from an empty composer, not hidden until the user types.
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';

import ChatComposer from './ChatComposer.vue';
import { useTaskStore } from '../../stores/tasks';
import type { ServerConfig } from '../../api/types';

async function mount(config?: Partial<ServerConfig>): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(ChatComposer, { streaming: false });
  // ChatComposer reads the task store to learn which harnesses are installed.
  const pinia = createPinia();
  app.use(pinia);
  if (config) {
    setActivePinia(pinia);
    useTaskStore().config = config as ServerConfig;
  }
  app.mount(host);
  await nextTick();
  return { app, host };
}

// harnessMenu opens the composer's harness picker and returns the labels of the
// harnesses it offers.
async function harnessMenu(host: HTMLElement): Promise<string[]> {
  const trigger = host.querySelector<HTMLButtonElement>('.harness-select__trigger');
  if (!trigger) throw new Error('no harness picker rendered');
  trigger.click();
  await nextTick();
  return Array.from(host.querySelectorAll('.harness-select__opt .harness-badge__label'))
    .map((el) => el.textContent?.trim() ?? '');
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

  // A harness can be usable for tasks yet have no chat runtime: topos runs
  // in-process, and chat launches subprocess harnesses only. The picker offers
  // what the server lists in chat_sandboxes, and does not adopt a configured
  // default the chat runtime cannot launch.
  it('offers only the harnesses the chat runtime can launch', async () => {
    localStorage.removeItem('wallfacer-chat-harness');
    const { host } = await mount({
      sandboxes: ['claude', 'codex', 'topos'],
      sandbox_usable: { claude: true, codex: true, topos: true },
      chat_sandboxes: ['claude', 'codex'],
      default_sandbox: 'topos',
    });
    const trigger = host.querySelector('.harness-select__trigger .harness-badge__label');
    expect(trigger?.textContent?.trim()).toBe('Claude');
    expect(await harnessMenu(host)).toEqual(['Claude', 'Codex']);
  });
});
