// WorkspaceEditModal is the single per-workspace settings popup (name, folder,
// parallel caps, delete). These pin the behavior that must hold:
//  - one Name field (no duplicated label/input box),
//  - changing the folder and the caps persist via wsStore.update (never the
//    wizard's activate-on-confirm path), and a change sends exactly one folder,
//  - a dormant workspace saved with several folders offers a Keep button per
//    folder and is re-pointed to the one kept,
//  - the active workspace is deletable (server wipes data + auto-switches).
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';

import WorkspaceEditModal from './WorkspaceEditModal.vue';
import { useWorkspacesStore } from '../stores/workspaces';
import { useUiStore } from '../stores/ui';
import { useTaskStore } from '../stores/tasks';
import { useDialogStore } from '../stores/dialog';
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
    if (method === 'DELETE') {
      return Promise.resolve({ workspaces: [], workspace_id: '', workspace_groups: [], active_groups: [] });
    }
    if (path === '/api/tasks') return Promise.resolve([]);
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

const wsA: Workspace = { id: 'a', name: 'Alpha', folders: ['/x'], dormant: false, active: false } as Workspace;
const wsPair: Workspace = { id: 'p', name: 'Pair', folders: ['/x', '/y'], dormant: true, active: false } as Workspace;

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
    // The one folder is listed, with no per-row Keep choice.
    expect(host!.querySelectorAll('.ws-selected-item').length).toBe(1);
    expect(host!.querySelector('.ws-selected-item__keep')).toBeNull();
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

  it('re-points a dormant workspace saved with several folders to the one kept', async () => {
    seed([{ ...wsPair }], 'p');
    ({ app, host } = await mount());
    expect(host!.querySelector('.ws-edit__several')).not.toBeNull();
    const keepBtns = host!.querySelectorAll<HTMLButtonElement>('.ws-selected-item__keep');
    expect(keepBtns.length).toBe(2);
    // Nothing is sent until the owner keeps one.
    expect(apiCalls.some(c => c.method === 'PUT')).toBe(false);

    keepBtns[1].click();
    await nextTick();
    const put = apiCalls.find(c => c.method === 'PUT' && c.path === '/api/workspaces/p');
    expect((put!.body as { folders: string[] }).folders).toEqual(['/y']);
    expect(apiCalls.some(c => c.path.includes('/activate'))).toBe(false);
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

  it('changes the folder to a browsed one through update, sending one folder', async () => {
    browseEntries = [{ name: 'gamma', path: '/home/u/gamma', is_git_repo: false }];
    seed([{ ...wsA }], 'a');
    ({ app, host } = await mount());
    // Reveal the browser; first reveal kicks off browse('').
    (Array.from(host!.querySelectorAll('button')).find(b => b.textContent?.includes('Change folder')) as HTMLButtonElement).click();
    await nextTick();
    await Promise.resolve();
    await nextTick();
    const choose = host!.querySelector('.ws-entry__choose') as HTMLButtonElement;
    expect(choose).not.toBeNull();
    choose.click();
    await nextTick();
    const put = apiCalls.find(c => c.method === 'PUT');
    expect((put!.body as { folders: string[] }).folders).toEqual(['/home/u/gamma']);
  });

  it('allows deleting the active workspace and issues DELETE', async () => {
    seed([{ ...wsA }], 'a', 'a'); // wsA is the active workspace
    ({ app, host } = await mount());
    const del = Array.from(host!.querySelectorAll('button')).find(b => b.textContent?.includes('Delete workspace')) as HTMLButtonElement;
    // The active workspace is now deletable (server wipes data + auto-switches).
    expect(del.disabled).toBe(false);

    vi.spyOn(useDialogStore(), 'confirm').mockResolvedValue(true);
    del.click();
    await nextTick();
    await Promise.resolve();
    await nextTick();
    await nextTick();
    expect(apiCalls.some(c => c.method === 'DELETE' && c.path === '/api/workspaces/a')).toBe(true);
  });
});
