// WorkspaceEditModal is the single per-workspace settings popup (name, folders,
// parallel caps, delete). These pin the load-bearing behaviour:
//  - one Name field (no duplicated label/input box),
//  - folder add/remove and caps persist via wsStore.update (never the wizard's
//    activate-on-confirm path),
//  - the last folder cannot be removed (server 400s on an empty set),
//  - delete is blocked for the active workspace (server 409s).
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';

import WorkspaceEditModal from './WorkspaceEditModal.vue';
import { useWorkspacesStore } from '../stores/workspaces';
import { useUiStore } from '../stores/ui';
import { useTaskStore } from '../stores/tasks';
import type { Workspace } from '../api/types';

// Capture every api call (with body) and answer the few endpoints the modal hits.
const apiCalls: { method: string; path: string; body?: unknown }[] = [];
let browseEntries: { name: string; path: string; is_git_repo: boolean }[] = [];
vi.mock('../api/client', () => ({
  api: vi.fn((method: string, path: string, body?: unknown) => {
    apiCalls.push({ method, path, body });
    if (path.startsWith('/api/workspaces/browse')) {
      return Promise.resolve({ path: '/home/u', entries: browseEntries });
    }
    if (method === 'PUT' && path.startsWith('/api/workspaces/')) {
      // Echo the patch back as an updated DTO so the store swaps local state.
      const id = path.split('/').pop()!;
      return Promise.resolve({ id, name: '', folders: [], dormant: false, active: false, ...(body as object) });
    }
    if (method === 'DELETE') return Promise.resolve(undefined);
    return Promise.resolve({});
  }),
}));

let activePinia: Pinia;

function seed(workspaces: Workspace[], editId: string, activeId = ''): void {
  const ws = useWorkspacesStore();
  ws.workspaces = workspaces;
  useUiStore().editWorkspaceId = editId;
  // isActive() derives from the task store config's workspace_id.
  (useTaskStore() as unknown as { config: unknown }).config = { workspace_id: activeId };
}

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(WorkspaceEditModal);
  app.use(activePinia);
  app.mount(host);
  await nextTick();
  return { app, host };
}

const wsA: Workspace = { id: 'a', name: 'Alpha', folders: ['/x', '/y'], dormant: false, active: false } as Workspace;

describe('WorkspaceEditModal', () => {
  let app: App | null = null;
  let host: HTMLElement | null = null;

  beforeEach(() => {
    activePinia = createPinia();
    setActivePinia(activePinia);
    apiCalls.length = 0;
    browseEntries = [];
  });

  afterEach(() => {
    app?.unmount();
    host?.remove();
    app = null;
    host = null;
  });

  it('renders exactly one Name input seeded from the workspace', async () => {
    seed([{ ...wsA }], 'a');
    ({ app, host } = await mount());
    const nameInputs = host!.querySelectorAll('#ws-edit-name');
    expect(nameInputs.length).toBe(1);
    expect((nameInputs[0] as HTMLInputElement).value).toBe('Alpha');
    // Two folder rows are listed.
    expect(host!.querySelectorAll('.ws-selected-item').length).toBe(2);
  });

  it('persists a rename via wsStore.update on blur (no activate call)', async () => {
    seed([{ ...wsA }], 'a');
    ({ app, host } = await mount());
    const input = host!.querySelector('#ws-edit-name') as HTMLInputElement;
    input.value = 'Renamed';
    input.dispatchEvent(new Event('input'));
    input.dispatchEvent(new Event('blur'));
    await nextTick();
    const put = apiCalls.find(c => c.method === 'PUT' && c.path === '/api/workspaces/a');
    expect(put).toBeDefined();
    expect((put!.body as { name: string }).name).toBe('Renamed');
    // Never routes through the wizard's activate-on-confirm path.
    expect(apiCalls.some(c => c.path.includes('/activate'))).toBe(false);
  });

  it('removes a folder via update, but disables removing the last one', async () => {
    seed([{ ...wsA }], 'a');
    ({ app, host } = await mount());
    const removeBtns = host!.querySelectorAll('.ws-selected-item__remove');
    (removeBtns[0] as HTMLButtonElement).click();
    await nextTick();
    const put = apiCalls.find(c => c.method === 'PUT');
    expect((put!.body as { folders: string[] }).folders).toEqual(['/y']);

    // A single-folder workspace cannot drop its last folder.
    apiCalls.length = 0;
    useWorkspacesStore().workspaces = [{ ...wsA, folders: ['/only'] }];
    await nextTick();
    const lastBtn = host!.querySelector('.ws-selected-item__remove') as HTMLButtonElement;
    expect(lastBtn.disabled).toBe(true);
  });

  it('clears a parallel cap by sending null when the input is emptied', async () => {
    seed([{ ...wsA, max_parallel: 4 }], 'a');
    ({ app, host } = await mount());
    const capInput = host!.querySelector('.ws-edit__caps input') as HTMLInputElement;
    expect(capInput.value).toBe('4');
    capInput.value = '';
    capInput.dispatchEvent(new Event('change'));
    await nextTick();
    const put = apiCalls.find(c => c.method === 'PUT');
    expect((put!.body as { max_parallel: number | null }).max_parallel).toBeNull();
  });

  it('adds a browsed folder through update', async () => {
    browseEntries = [{ name: 'gamma', path: '/home/u/gamma', is_git_repo: false }];
    seed([{ ...wsA }], 'a');
    ({ app, host } = await mount());
    // Reveal the browser; first reveal kicks off browse('').
    (Array.from(host!.querySelectorAll('button')).find(b => b.textContent?.includes('Add folder')) as HTMLButtonElement).click();
    await nextTick();
    await Promise.resolve();
    await nextTick();
    const add = host!.querySelector('.ws-entry__add') as HTMLButtonElement;
    expect(add).not.toBeNull();
    add.click();
    await nextTick();
    const put = apiCalls.find(c => c.method === 'PUT');
    expect((put!.body as { folders: string[] }).folders).toEqual(['/x', '/y', '/home/u/gamma']);
  });

  it('blocks deleting the active workspace', async () => {
    seed([{ ...wsA }], 'a', 'a');
    ({ app, host } = await mount());
    const del = Array.from(host!.querySelectorAll('button')).find(b => b.textContent?.includes('Delete workspace')) as HTMLButtonElement;
    expect(del.disabled).toBe(true);
  });
});
