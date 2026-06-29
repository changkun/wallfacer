import { beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
vi.mock('../api/client', () => ({ api: apiMock }));

import { useWorkspacesStore } from './workspaces';
import { useTaskStore } from './tasks';

function ws(over: Partial<{ id: string; name: string; folders: string[]; dormant: boolean; active: boolean }> = {}) {
  return { id: 'w1', name: 'One', folders: ['/a'], dormant: false, active: false, ...over };
}

beforeEach(() => {
  setActivePinia(createPinia());
  apiMock.mockReset();
  apiMock.mockResolvedValue({});
});

describe('workspaces store list', () => {
  it('populates workspaces; active derives from the config workspace_id', async () => {
    apiMock.mockResolvedValueOnce({
      workspaces: [ws({ id: 'w1' }), ws({ id: 'w2', name: 'Two' })],
      active_id: 'w1',
    });
    const store = useWorkspacesStore();
    await store.list();
    expect(store.workspaces).toHaveLength(2);

    // No active workspace until config reports one.
    expect(store.activeId).toBe('');
    useTaskStore().config = { workspace_id: 'w1' } as never;
    expect(store.activeId).toBe('w1');
    expect(store.active?.name).toBe('One');
    expect(store.isActive('w1')).toBe(true);
    expect(store.isActive('w2')).toBe(false);
  });

  // Regression: the sidebar (config) and settings (registry) must never
  // disagree on which workspace is active. Active state follows config's
  // workspace_id, NOT the per-fetch DTO `active` flag, which goes stale when a
  // switch happens via a path that does not re-fetch the registry.
  it('follows config workspace_id, ignoring a stale DTO active flag', async () => {
    apiMock.mockResolvedValueOnce({
      // DTO says w1 is active (fetched when w1 was active)...
      workspaces: [ws({ id: 'w1', active: true }), ws({ id: 'w2', name: 'Two', active: false })],
      active_id: 'w1',
    });
    const store = useWorkspacesStore();
    await store.list();
    // ...but the server has since switched to w2 (config refreshed elsewhere).
    useTaskStore().config = { workspace_id: 'w2' } as never;
    expect(store.activeId).toBe('w2');
    expect(store.isActive('w2')).toBe(true);
    expect(store.isActive('w1')).toBe(false);
  });
});

describe('workspaces store update', () => {
  it('replaces the local copy with the server DTO', async () => {
    apiMock.mockResolvedValueOnce({
      workspaces: [ws({ id: 'w1', name: 'One' })],
      active_id: 'w1',
    });
    const store = useWorkspacesStore();
    await store.list();

    apiMock.mockResolvedValueOnce(ws({ id: 'w1', name: 'Renamed', folders: ['/a', '/b'] }));
    const out = await store.update('w1', { name: 'Renamed', folders: ['/a', '/b'] });
    expect(apiMock).toHaveBeenLastCalledWith('PUT', '/api/workspaces/w1', { name: 'Renamed', folders: ['/a', '/b'] });
    expect(out.name).toBe('Renamed');
    expect(store.workspaces[0].name).toBe('Renamed');
    expect(store.workspaces[0].folders).toEqual(['/a', '/b']);
  });
});

describe('workspaces store activate', () => {
  it('switches active id from the returned config and refreshes the board', async () => {
    apiMock.mockResolvedValueOnce({
      workspaces: [ws({ id: 'w1', active: true }), ws({ id: 'w2', name: 'Two' })],
      active_id: 'w1',
    });
    const store = useWorkspacesStore();
    await store.list();

    apiMock
      .mockResolvedValueOnce({ workspaces: ['/c'], workspace_id: 'w2' }) // activate -> config
      .mockResolvedValueOnce([]); // fetchTasks
    await store.activate('w2');

    expect(apiMock).toHaveBeenCalledWith('POST', '/api/workspaces/w2/activate');
    expect(store.activeId).toBe('w2');
    expect(store.active?.id).toBe('w2');
    expect(store.isActive('w2')).toBe(true);
    expect(store.isActive('w1')).toBe(false);
    // The tasks store (config-bearing) reflects the switch.
    const tasks = useTaskStore();
    expect(tasks.config?.workspace_id).toBe('w2');
  });
});
