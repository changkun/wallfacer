import { describe, expect, it } from 'vitest';
import { createApp, nextTick } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia } from 'pinia';

import Topbar from '../src/components/Topbar.vue';
import { useUiStore } from '../src/stores/ui';

// The topbar replaced the status bar's Terminal and Shortcuts buttons and
// carries the crumb (specs/shared/console-redesign/shell.md).
async function mountTopbar(path: string) {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/', component: { template: '<div />' } }, { path: '/:rest(.*)', component: { template: '<div />' } }],
  });
  await router.push(path);
  await router.isReady();
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(Topbar);
  const pinia = createPinia();
  app.use(pinia);
  app.use(router);
  app.mount(host);
  await nextTick();
  return { host, app, pinia };
}

describe('Topbar', () => {
  it('names the page in the crumb and appends a leaf set by the page', async () => {
    const { host, app, pinia } = await mountTopbar('/plan');
    expect(host.querySelector('.crumb')!.textContent).toContain('Plan');
    const ui = useUiStore(pinia);
    ui.setCrumbLeaf('roadmap.md');
    await nextTick();
    expect(host.querySelector('.crumb .leaf')!.textContent).toBe('roadmap.md');
    ui.clearCrumbLeaf();
    await nextTick();
    expect(host.querySelector('.crumb .leaf')!.textContent).toBe('Plan');
    app.unmount(); host.remove();
  });

  it('renders the page actions a page registers and toggles the terminal', async () => {
    const { host, app, pinia } = await mountTopbar('/');
    const ui = useUiStore(pinia);
    expect(host.querySelector('#topbar-actions')).toBeNull();
    const Actions = { template: '<button data-action="page">page</button>' };
    ui.setTopbarActions(Actions);
    await nextTick();
    expect(host.querySelector('#topbar-actions [data-action="page"]')).toBeTruthy();
    ui.clearTopbarActions(Actions);
    await nextTick();
    expect(host.querySelector('#topbar-actions')).toBeNull();
    const before = ui.showTerminal;
    (host.querySelector('[data-action="terminal"]') as HTMLButtonElement).click();
    await nextTick();
    expect(ui.showTerminal).toBe(!before);
    app.unmount(); host.remove();
  });

  it('opens the shortcuts modal', async () => {
    const { host, app, pinia } = await mountTopbar('/');
    (host.querySelector('[data-action="shortcuts"]') as HTMLButtonElement).click();
    expect(useUiStore(pinia).showShortcuts).toBe(true);
    app.unmount(); host.remove();
  });
});
