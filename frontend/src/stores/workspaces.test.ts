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
  it('populates workspaces and the active id', async () => {
    apiMock.mockResolvedValueOnce({
      workspaces: [ws({ id: 'w1', active: true }), ws({ id: 'w2', name: 'Two' })],
      active_id: 'w1',
    });
    const store = useWorkspacesStore();
    await store.list();
    expect(store.workspaces).toHaveLength(2);
    expect(store.activeId).toBe('w1');
    expect(store.active?.name).toBe('One');
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
    expect(store.workspaces.find(w => w.id === 'w2')?.active).toBe(true);
    expect(store.workspaces.find(w => w.id === 'w1')?.active).toBe(false);
    // The tasks store (config-bearing) reflects the switch.
    const tasks = useTaskStore();
    expect(tasks.config?.workspace_id).toBe('w2');
  });
});
