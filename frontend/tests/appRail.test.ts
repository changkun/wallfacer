import { describe, expect, it, vi } from 'vitest';
import { createApp, nextTick, ref } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia } from 'pinia';
import { LATERE_PRODUCTS } from 'latere-ui';

// These tests cover rail rendering. WorkspaceChip's live subscription is
// exercised separately; happy-dom has no EventSource implementation.
vi.mock('../src/api/client', () => ({ api: vi.fn(async () => []) }));
vi.mock('../src/composables/useSse', () => ({
  useSse: () => ({ connected: ref(true), connState: ref('ok'), stop: vi.fn() }),
}));

import AppRail from '../src/components/AppRail.vue';
import { NAV_GROUPS } from '../src/lib/nav';

// The rail is wallfacer's own chrome (specs/shared/console-redesign/shell.md).
// These guard what moved when it replaced latere-ui's ConsoleSidebar: every
// destination still renders as a row, the fold hides the words and keeps the
// icons, the product switcher still sits in the head, and the Board row
// carries the live count the status bar used to show.
async function mountRail(collapsed: boolean, path = '/') {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/:rest(.*)', component: { template: '<div />' } },
    ],
  });
  await router.push(path);
  await router.isReady();
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(AppRail, { collapsed, connState: 'ok' });
  const pinia = createPinia();
  app.use(pinia);
  app.use(router);
  app.mount(host);
  await nextTick();
  return { host, pinia, app };
}

describe('AppRail', () => {
  it('renders one row per nav item with its route', async () => {
    const { host, app } = await mountRail(false);
    for (const g of NAV_GROUPS) {
      for (const item of g.items) {
        const row = host.querySelector(`[data-nav="${item.id}"]`);
        expect(row, item.id).toBeTruthy();
        if (item.to) expect(row!.getAttribute('href')).toBe(item.to);
      }
    }
    app.unmount(); host.remove();
  });

  it('marks the board row current on / and keeps the words when open', async () => {
    const { host, app } = await mountRail(false, '/');
    const board = host.querySelector('[data-nav="board"]')!;
    expect(board.classList.contains('active')).toBe(true);
    expect(board.getAttribute('aria-current')).toBe('page');
    expect(host.querySelector('.nav-label')!.textContent).toBe('Chat');
    app.unmount(); host.remove();
  });

  it('folds to icons: the rail carries the fold class and no product switcher', async () => {
    const { host, app } = await mountRail(true);
    expect(host.querySelector('.app-rail')!.classList.contains('app-rail--fold')).toBe(true);
    expect(host.querySelector('.lu-ps')).toBeNull();
    expect(host.querySelectorAll('.nav-ico').length).toBeGreaterThan(0);
    app.unmount(); host.remove();
  });

  it('renders the shared ProductSwitcher in the open head with a registered slug', async () => {
    const { host, app } = await mountRail(false);
    expect(host.querySelector('.lu-ps')).toBeTruthy();
    expect(LATERE_PRODUCTS.map((p) => p.slug)).toContain('wallfacer');
    app.unmount(); host.remove();
  });

  it('shows running + waiting as the Board count', async () => {
    const { host, app, pinia } = await mountRail(false, '/plan');
    const { useTaskStore } = await import('../src/stores/tasks');
    const store = useTaskStore(pinia);
    store.setTasks([
      { id: 'a', title: 'a', status: 'in_progress' },
      { id: 'b', title: 'b', status: 'waiting' },
      { id: 'c', title: 'c', status: 'backlog' },
    ] as never);
    await nextTick();
    expect(host.querySelector('[data-nav="board"] .nav-count')!.textContent).toBe('2');
    app.unmount(); host.remove();
  });
});
