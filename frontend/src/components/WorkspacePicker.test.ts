// Wizard navigation for the Select Workspaces modal.
//
// The picker is a two step wizard: step 1 chooses the workspace's one folder,
// step 2 reviews and activates. The "Next: Name" button (and the step 2
// circle) must be disabled until a folder is chosen, and advancing must reveal
// the step 2 review pane. This pins that gating + navigation, single-folder
// choice, and re-pointing a dormant workspace saved with several folders.
//
// The open watcher is not immediate, so mounting with modelValue:true does
// not fire browse(), meaning no api call runs on mount. We drive the gate via
// "Use this folder", which chooses browsePath ('/') without any network.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h, nextTick, ref, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import WorkspacePicker from './WorkspacePicker.vue';

// Capture browse calls and feed canned directory listings so the picker can be
// driven without a real backend.
const apiCalls: { method: string; path: string; body?: unknown }[] = [];
let browseEntries: { name: string; path: string; is_git_repo: boolean }[] = [];
let registryWorkspaces: { id: string; name: string; folders: string[]; dormant: boolean; active: boolean }[] = [];
vi.mock('../api/client', () => ({
  api: vi.fn((method: string, path: string, body?: unknown) => {
    apiCalls.push({ method, path, body });
    if (method === 'PUT' && path.startsWith('/api/workspaces/')) {
      const id = path.split('/').pop()!;
      return Promise.resolve({ id, name: '', folders: [], dormant: false, active: false, ...(body as object) });
    }
    if (path === '/api/workspaces' && method === 'GET') {
      return Promise.resolve({ workspaces: registryWorkspaces, active_id: '' });
    }
    if (path.startsWith('/api/workspaces/browse')) {
      return Promise.resolve({ path: '/home/u', entries: browseEntries });
    }
    if (path.includes('/activate')) {
      return Promise.resolve({ workspaces: [], workspace_id: 'a', workspace_groups: [], active_groups: [] });
    }
    if (path === '/api/tasks') {
      return Promise.resolve([]);
    }
    return Promise.resolve({});
  }),
}));

let activePinia: Pinia;

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
});

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(WorkspacePicker, { modelValue: true });
  app.use(activePinia);
  app.mount(host);
  await nextTick();
  return { app, host };
}

// mountOpen mounts via a parent that flips modelValue false->true after mount,
// so the open watcher fires and browse() runs against the mocked api.
async function mountOpen(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const open = ref(false);
  const app = createApp({
    setup: () => () => h(WorkspacePicker, { modelValue: open.value }),
  });
  app.use(activePinia);
  app.mount(host);
  open.value = true;
  await nextTick();
  await nextTick();
  await Promise.resolve();
  await nextTick();
  return { app, host };
}

function findByText(host: HTMLElement, selector: string, text: string): HTMLElement | null {
  return Array.from(host.querySelectorAll<HTMLElement>(selector)).find(
    (el) => el.textContent?.trim().includes(text),
  ) ?? null;
}

describe('WorkspacePicker wizard', () => {
  let app: App | null = null;
  let host: HTMLElement | null = null;

  beforeEach(() => {
    apiCalls.length = 0;
    browseEntries = [];
    registryWorkspaces = [];
  });

  afterEach(() => {
    app?.unmount();
    host?.remove();
    app = null;
    host = null;
  });

  it('explains what a workspace contains and what activating it does', async () => {
    ({ app, host } = await mountOpen());
    const explanation = host!.querySelector('.dialog-sub')!.textContent;
    expect(explanation).toContain('A workspace is one project folder');
    expect(explanation).toContain('task board');
    expect(explanation).toMatch(/agents/i);
  });

  it('opens the browser at the home directory (empty path), not filesystem root', async () => {
    ({ app, host } = await mountOpen());
    const browse = apiCalls.find((c) => c.path.startsWith('/api/workspaces/browse'));
    expect(browse).toBeDefined();
    // Empty path makes the backend resolve to the user's home directory.
    expect(browse!.path).toBe('/api/workspaces/browse?path=');
  });

  it('opens to the list view when workspaces exist and activates the clicked one', async () => {
    registryWorkspaces = [
      { id: 'a', name: 'Alpha', folders: ['/x'], dormant: false, active: false },
      { id: 'b', name: 'Beta', folders: ['/y'], dormant: true, active: false },
    ];
    ({ app, host } = await mountOpen());
    // Let the registry load resolve and promote the modal to the list view.
    await Promise.resolve();
    await nextTick();
    await nextTick();

    // The list view renders one row per registry workspace (not the wizard).
    const items = host!.querySelectorAll('.ws-list__item');
    expect(items.length).toBe(2);
    expect(host!.querySelector('.ws-picker__list-view')).not.toBeNull();

    // Clicking a non-dormant workspace activates it by id.
    (host!.querySelector('.ws-list__main') as HTMLButtonElement).click();
    await nextTick();
    expect(apiCalls.some((c) => c.method === 'POST' && c.path === '/api/workspaces/a/activate')).toBe(true);
  });

  it('chooses one folder: a second choice replaces the first', async () => {
    browseEntries = [
      { name: 'alpha', path: '/srv/alpha', is_git_repo: false },
      { name: 'beta', path: '/srv/beta', is_git_repo: false },
    ];
    ({ app, host } = await mountOpen());

    const row = (name: string) => Array.from(host!.querySelectorAll<HTMLElement>('.ws-entry')).find((r) =>
      r.textContent?.includes(name),
    )!;
    (row('alpha').querySelector('.ws-entry__choose') as HTMLButtonElement).click();
    await nextTick();
    expect(row('alpha').querySelector('.ws-entry__chosen')).not.toBeNull();
    expect(host!.querySelector('.ws-step__count')!.textContent!.trim()).toBe('/srv/alpha');

    (row('beta').querySelector('.ws-entry__choose') as HTMLButtonElement).click();
    await nextTick();
    expect(row('beta').querySelector('.ws-entry__chosen')).not.toBeNull();
    expect(row('alpha').querySelector('.ws-entry__chosen')).toBeNull();
    expect(host!.querySelectorAll('.ws-entry__chosen').length).toBe(1);
    expect(host!.querySelector('.ws-step__count')!.textContent!.trim()).toBe('/srv/beta');
  });

  it('re-points a dormant workspace saved with several folders to the one the owner keeps', async () => {
    registryWorkspaces = [
      { id: 'm', name: 'Pair', folders: ['/srv/api', '/srv/web'], dormant: true, active: false },
    ];
    ({ app, host } = await mountOpen());
    await Promise.resolve();
    await nextTick();
    await nextTick();

    // The list row says it has several folders; clicking it opens the
    // re-point wizard instead of activating.
    expect(host!.querySelector('.ws-list__badge--dormant')!.textContent).toContain('several folders');
    (host!.querySelector('.ws-list__main') as HTMLButtonElement).click();
    await nextTick();
    expect(apiCalls.some((c) => c.path.includes('/activate'))).toBe(false);

    // No folder is picked for the owner: Next stays disabled until one is kept.
    const next = findByText(host!, 'button', 'Next: Name') as HTMLButtonElement;
    expect(next.disabled).toBe(true);
    expect(host!.querySelector('.ws-picker__former')!.textContent).toContain('several folders');
    const keepWeb = findByText(host!, '.ws-picker__former-choose', '/srv/web') as HTMLButtonElement;
    keepWeb.click();
    await nextTick();
    expect(next.disabled).toBe(false);

    next.click();
    await nextTick();
    (findByText(host!, 'button', 'Activate') as HTMLButtonElement).click();
    // Let the update -> activate chain settle.
    await new Promise((resolve) => setTimeout(resolve, 0));
    await nextTick();
    const put = apiCalls.find((c) => c.method === 'PUT' && c.path === '/api/workspaces/m');
    expect(put).toBeDefined();
    expect((put!.body as { folders: string[] }).folders).toEqual(['/srv/web']);
    expect(apiCalls.some((c) => c.method === 'POST' && c.path === '/api/workspaces/m/activate')).toBe(true);
  });

  it('disables Next until a folder is chosen, then advances to review', async () => {
    ({ app, host } = await mount());

    // jsdom does not lay out, so check v-show via the inline display style on
    // each step body wrapper rather than offsetParent.
    const bodies = Array.from(host.querySelectorAll<HTMLElement>('.ws-picker__body--step'));
    expect(bodies.length).toBe(2);
    const [stepOne, stepTwo] = bodies;
    expect(stepOne.style.display).not.toBe('none');
    expect(stepTwo.style.display).toBe('none');

    const next = findByText(host, 'button', 'Next: Name');
    expect(next).not.toBeNull();
    expect((next as HTMLButtonElement).disabled).toBe(true);

    const count = host.querySelector('.ws-step__count');
    expect(count?.textContent?.trim()).toBe('No folder chosen');

    // Choose the browsed folder ('/'), which flips canProceed without browsing.
    const useCurrent = findByText(host, 'button', 'Use this folder');
    expect(useCurrent).not.toBeNull();
    (useCurrent as HTMLButtonElement).click();
    await nextTick();

    expect((next as HTMLButtonElement).disabled).toBe(false);
    expect(count?.textContent?.trim()).toBe('/');

    // Advance to step 2: the review pane shows, step 1 hides.
    (next as HTMLButtonElement).click();
    await nextTick();

    expect(stepOne.style.display).toBe('none');
    expect(stepTwo.style.display).not.toBe('none');
    const activate = findByText(host, 'button', 'Activate');
    expect(activate).not.toBeNull();
  });
});
