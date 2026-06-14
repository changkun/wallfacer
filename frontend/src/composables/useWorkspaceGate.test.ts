// Integration guard for the workspace gate that fixes the reported leak:
// navigating to a workspace-scoped route (e.g. /plan) with no visible
// workspace must block, while a workspace-independent route (/settings) and
// the workspace-present case must not. Drives the real localRoutes meta + the
// real store through the composable App.vue consumes.
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { createApp, defineComponent, h, nextTick, type App } from 'vue';
import {
  createRouter,
  createMemoryHistory,
  RouterView,
  type Router,
  type RouteRecordRaw,
} from 'vue-router';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import { localRoutes } from '../router';
import { useWorkspaceGate } from './useWorkspaceGate';
import { useTaskStore } from '../stores/tasks';
import type { ServerConfig } from '../api/types';

let pinia: Pinia;

const Harness = defineComponent({
  setup() {
    const blocked = useWorkspaceGate();
    return () => h('div', blocked.value ? 'BLOCKED' : 'PAGE');
  },
});

// Stub every page so the router resolves without importing real views,
// preserving each route's path + meta (which the gate reads).
const stubRoutes: RouteRecordRaw[] = localRoutes.map((r) => ({
  path: r.path,
  meta: r.meta,
  component: Harness,
}));

async function mountAt(path: string): Promise<{ app: App; router: Router; host: HTMLElement }> {
  const router = createRouter({ history: createMemoryHistory(), routes: stubRoutes });
  router.push(path);
  await router.isReady();
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp({ render: () => h(RouterView) });
  app.use(pinia);
  app.use(router);
  app.mount(host);
  await nextTick();
  return { app, router, host };
}

function setConfig(workspaces: string[] | null): void {
  const store = useTaskStore();
  store.config = workspaces == null ? null : ({ workspaces } as ServerConfig);
}

describe('useWorkspaceGate', () => {
  let app: App | null = null;
  let host: HTMLElement | null = null;

  beforeEach(() => {
    pinia = createPinia();
    setActivePinia(pinia);
  });

  afterEach(() => {
    app?.unmount();
    host?.remove();
    app = null;
    host = null;
  });

  it('blocks a workspace-scoped route when no workspace is visible', async () => {
    setConfig([]);
    let router: Router;
    ({ app, router, host } = await mountAt('/plan'));
    await router.isReady();
    await nextTick();
    expect(host.textContent).toBe('BLOCKED');
  });

  it('does not block while config is still loading', async () => {
    setConfig(null);
    ({ app, host } = await mountAt('/plan'));
    await nextTick();
    expect(host.textContent).toBe('PAGE');
  });

  it('does not block when a workspace is present', async () => {
    setConfig(['/some/workspace']);
    ({ app, host } = await mountAt('/plan'));
    await nextTick();
    expect(host.textContent).toBe('PAGE');
  });

  it('never blocks a workspace-independent route', async () => {
    setConfig([]);
    ({ app, host } = await mountAt('/settings'));
    await nextTick();
    expect(host.textContent).toBe('PAGE');
  });
});
