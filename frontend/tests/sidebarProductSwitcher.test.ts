import { describe, expect, it } from 'vitest';
import { createApp } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia } from 'pinia';
import { LATERE_PRODUCTS } from 'latere-ui';

import Sidebar from '../src/components/Sidebar.vue';

// The sidebar opts into the shared latere-ui ProductSwitcher (v1.26.0) via the
// `product` prop, so the cross-console app grid renders in the sidebar head.
// Mount the real Sidebar and assert the switcher is present; also pin the slug
// against the shared registry so a typo'd slug (which would render a switcher
// with no current-product tile) fails here rather than in production.
describe('sidebar product switcher', () => {
  function mountSidebar(collapsed: boolean): HTMLElement {
    const router = createRouter({
      history: createMemoryHistory(),
      routes: [{ path: '/', component: { template: '<div />' } }],
    });
    const host = document.createElement('div');
    document.body.appendChild(host);
    const app = createApp(Sidebar, { collapsed });
    app.use(createPinia());
    app.use(router);
    app.mount(host);
    return host;
  }

  it('renders the shared ProductSwitcher in the expanded sidebar head', () => {
    const host = mountSidebar(false);
    expect(host.querySelector('.lu-ps')).toBeTruthy();
    host.remove();
  });

  it('hides the switcher while the rail is collapsed', () => {
    const host = mountSidebar(true);
    expect(host.querySelector('.lu-ps')).toBeNull();
    host.remove();
  });

  it('passes a slug that exists in the shared product registry', async () => {
    const { readFileSync } = await import('node:fs');
    const { resolve } = await import('node:path');
    const sidebar = readFileSync(
      resolve(process.cwd(), 'src/components/Sidebar.vue'),
      'utf8',
    );
    const slug = sidebar.match(/product="([^"]+)"/)?.[1];
    expect(slug).toBe('wallfacer');
    expect(LATERE_PRODUCTS.map((p) => p.slug)).toContain(slug);
  });
});
