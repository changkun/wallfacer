// Regression test for moving the Theme control out of system Settings.
//
// Theme now lives in the user/account popup menu (AccountControl ->
// AccountPrefs), so the Settings Appearance tab must NOT render a duplicate
// Theme card. This pins that the #theme-switch / Light-Dark-Auto buttons are
// gone while the archived-tasks toggle stays as the tab's content.

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import SettingsTabAppearance from './SettingsTabAppearance.vue';

let activePinia: Pinia;

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
});

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(SettingsTabAppearance);
  app.use(activePinia);
  app.mount(host);
  await nextTick();
  return { app, host };
}

describe('SettingsTabAppearance', () => {
  let app: App | null = null;
  let host: HTMLElement | null = null;

  afterEach(() => {
    app?.unmount();
    host?.remove();
    app = null;
    host = null;
  });

  it('does not render the Theme card (moved to the account menu)', async () => {
    ({ app, host } = await mount());

    expect(host.querySelector('#theme-switch')).toBeNull();
    expect(host.querySelector('[data-mode="light"]')).toBeNull();
    expect(host.querySelector('[data-mode="dark"]')).toBeNull();
    expect(host.querySelector('[data-mode="auto"]')).toBeNull();
    expect(host.textContent).not.toContain('Theme');
  });

  it('still renders the archived-tasks toggle', async () => {
    ({ app, host } = await mount());

    expect(host.querySelector('#show-archived-toggle')).not.toBeNull();
    expect(host.textContent).toContain('Show archived tasks');
  });
});
