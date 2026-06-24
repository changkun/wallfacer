// editorTabs is the source of truth for the board's VS Code-style file tabs.
// These pin the contract the tab strip and editor rely on: open appends and
// focuses, re-opening focuses without duplicating, the board tab is pinned,
// closing falls back sensibly, dirty close runs the discard guard, save round-
// trips through PUT and clears dirty, and buffers persist across focus changes
// (the store outlives BoardPage, so this stands in for surviving a route nav).
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';

const { apiMock } = vi.hoisted(() => ({ apiMock: vi.fn() }));
vi.mock('../api/client', () => ({
  api: apiMock,
  authHeaders: () => ({}),
  withAuthToken: (u: string) => u,
}));

import { useEditorTabsStore, BOARD_TAB_ID } from './editorTabs';
import { useDialogStore } from './dialog';

function mockRead(content: string) {
  apiMock.mockImplementation((method: string) =>
    method === 'GET' ? Promise.resolve({ content }) : Promise.resolve({}),
  );
}

beforeEach(() => {
  setActivePinia(createPinia());
  apiMock.mockReset();
});

describe('editorTabs store', () => {
  it('opens a file as a tab, focuses it, and loads its content', async () => {
    mockRead('hello world');
    const s = useEditorTabsStore();
    expect(s.tabs.length).toBe(0);
    expect(s.activeId).toBe(BOARD_TAB_ID);

    await s.openFile('/ws', 'src/a.ts');
    expect(s.tabs.length).toBe(1);
    expect(s.activeId).toBe('src/a.ts');
    const t = s.find('src/a.ts')!;
    expect(t.content).toBe('hello world');
    expect(t.baseline).toBe('hello world');
    expect(s.isDirty('src/a.ts')).toBe(false);
  });

  it('focuses an already-open file instead of duplicating it', async () => {
    mockRead('x');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'a.ts');
    await s.openFile('/ws', 'b.ts');
    expect(s.tabs.length).toBe(2);
    expect(s.activeId).toBe('b.ts');

    await s.openFile('/ws', 'a.ts');
    expect(s.tabs.length).toBe(2);
    expect(s.activeId).toBe('a.ts');
  });

  it('never closes the pinned board tab', async () => {
    const s = useEditorTabsStore();
    await s.close(BOARD_TAB_ID);
    expect(s.activeId).toBe(BOARD_TAB_ID);
  });

  it('closes a tab and falls back to a neighbour, then the board', async () => {
    mockRead('x');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'a.ts');
    await s.openFile('/ws', 'b.ts'); // active = b

    await s.close('b.ts');
    expect(s.tabs.map((t) => t.path)).toEqual(['a.ts']);
    expect(s.activeId).toBe('a.ts');

    await s.close('a.ts');
    expect(s.tabs.length).toBe(0);
    expect(s.activeId).toBe(BOARD_TAB_ID);
  });

  it('runs the discard guard when closing a dirty tab', async () => {
    mockRead('orig');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'a.ts');
    s.setContent('a.ts', 'edited');
    expect(s.isDirty('a.ts')).toBe(true);

    const dialog = useDialogStore();
    const confirmSpy = vi.spyOn(dialog, 'confirm').mockResolvedValue(false);
    await s.close('a.ts');
    expect(confirmSpy).toHaveBeenCalledOnce();
    expect(s.tabs.length).toBe(1); // declined → kept

    confirmSpy.mockResolvedValue(true);
    await s.close('a.ts');
    expect(s.tabs.length).toBe(0); // accepted → removed
  });

  it('saves through PUT and clears the dirty flag', async () => {
    mockRead('orig');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'a.ts');
    s.setContent('a.ts', 'new content');
    expect(s.isDirty('a.ts')).toBe(true);

    apiMock.mockClear();
    apiMock.mockResolvedValue({});
    await s.save('a.ts');
    expect(apiMock).toHaveBeenCalledWith('PUT', '/api/explorer/file', {
      workspace: '/ws',
      path: 'a.ts',
      content: 'new content',
    });
    expect(s.isDirty('a.ts')).toBe(false);
    expect(s.find('a.ts')!.baseline).toBe('new content');
  });

  it('keeps unsaved buffers when focus moves away and back', async () => {
    mockRead('orig');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'a.ts');
    s.setContent('a.ts', 'wip');

    s.focus(BOARD_TAB_ID);
    expect(s.activeId).toBe(BOARD_TAB_ID);
    expect(s.find('a.ts')!.content).toBe('wip'); // buffer survives

    s.focus('a.ts');
    expect(s.activeId).toBe('a.ts');
    expect(s.isDirty('a.ts')).toBe(true);
  });

  it('disambiguates tab labels by parent dir only on basename collision', async () => {
    mockRead('x');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'README.md');
    expect(s.labelFor(s.find('README.md')!)).toBe('README.md');

    await s.openFile('/ws', 'src/index.ts');
    await s.openFile('/ws', 'lib/index.ts');
    expect(s.labelFor(s.find('src/index.ts')!)).toBe('src/index.ts');
    expect(s.labelFor(s.find('lib/index.ts')!)).toBe('lib/index.ts');
    expect(s.labelFor(s.find('README.md')!)).toBe('README.md');
  });
});
