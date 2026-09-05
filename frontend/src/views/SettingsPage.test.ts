import { describe, expect, it, vi } from 'vitest';
import { createApp, nextTick } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia } from 'pinia';

vi.mock('../api/client', () => ({ api: vi.fn(async () => ({})), ApiError: class extends Error {} }));

import SettingsPage from './SettingsPage.vue';

// The settings page is one underline tab strip and one tab body at a time,
// with the tab in the route query (specs/shared/console-redesign/settings.md).
async function mount(query = '') {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/settings', component: SettingsPage }] });
  await router.push('/settings' + query); await router.isReady();
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(SettingsPage);
  app.use(createPinia()); app.use(router); app.mount(host);
  for (let i = 0; i < 4; i++) await nextTick();
  return { host, app, router };
}

describe('SettingsPage', () => {
  it('opens the tab named in the query and renders only that body', async () => {
    const { host, app } = await mount('?tab=appearance');
    expect(host.querySelector('.tab.on')!.getAttribute('data-tab')).toBe('appearance');
    expect(host.querySelectorAll('[data-settings-tab]').length).toBe(1);
    expect(host.querySelector('[data-settings-tab="appearance"]')).toBeTruthy();
    app.unmount(); host.remove();
  });

  it('switching tabs updates the query and swaps the body', async () => {
    const { host, app, router } = await mount();
    (host.querySelector('[data-tab="about"]') as HTMLButtonElement).click();
    for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
    expect(router.currentRoute.value.query.tab).toBe('about');
    expect(host.querySelector('[data-settings-tab="about"]')).toBeTruthy();
    expect(host.querySelectorAll('.tab.on').length).toBe(1);
    app.unmount(); host.remove();
  });
});
