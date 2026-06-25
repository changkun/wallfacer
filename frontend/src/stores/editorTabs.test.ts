// editorTabs is the source of truth for the board's VS Code-style file tabs.
// These pin the contract the tab strip and editor rely on: open appends and
// focuses, re-opening focuses without duplicating, the board tab is pinned,
// closing falls back sensibly, dirty close runs the discard guard, save round-
// trips through PUT and clears dirty, and buffers persist across focus changes
// (the store outlives BoardPage, so this stands in for surviving a route nav).
import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { watch, nextTick } from 'vue';

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

  it('clears loading reactively once content arrives', async () => {
    // Regression: openFile must mutate the reactive tab (via find), not the raw
    // pushed object. Writing the raw object changes the value but fires no
    // reactivity, so the editor stays stuck on "Loading…". Assert the watcher
    // observes the true→false transition, which a raw mutation would skip.
    mockRead('content');
    const s = useEditorTabsStore();
    const seen: (boolean | undefined)[] = [];
    const p = s.openFile('/ws', 'a.ts'); // pushes tab (loading=true), then awaits
    watch(() => s.find('a.ts')?.loading, (v) => { seen.push(v); }, { flush: 'sync' });
    await p;
    await nextTick();
    expect(s.find('a.ts')!.loading).toBe(false);
    expect(seen).toContain(false); // the reactive transition actually fired
  });

  it('focuses an already-open file instead of duplicating it', async () => {
    mockRead('x');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'a.ts', { preview: false });
    await s.openFile('/ws', 'b.ts', { preview: false });
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
    await s.openFile('/ws', 'a.ts', { preview: false });
    await s.openFile('/ws', 'b.ts', { preview: false }); // active = b

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

  it('reuses the preview slot on single-click, pins on save/double-click', async () => {
    mockRead('x');
    const s = useEditorTabsStore();

    // single-click opens a preview tab (italic, reusable)
    await s.openFile('/ws', 'a.ts');
    expect(s.tabs.length).toBe(1);
    expect(s.find('a.ts')!.preview).toBe(true);

    // another single-click reuses the (clean) preview slot, no accumulation
    await s.openFile('/ws', 'b.ts');
    expect(s.tabs.map((t) => t.path)).toEqual(['b.ts']);
    expect(s.find('b.ts')!.preview).toBe(true);

    // double-click / explicit open pins it
    await s.openFile('/ws', 'b.ts', { preview: false });
    expect(s.find('b.ts')!.preview).toBe(false);

    // now a fresh single-click adds a new preview alongside the pinned tab
    await s.openFile('/ws', 'c.ts');
    expect(s.tabs.map((t) => t.path)).toEqual(['b.ts', 'c.ts']);
  });

  it('keeps a dirty preview tab when another file is previewed', async () => {
    mockRead('orig');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'a.ts'); // preview
    s.setContent('a.ts', 'edited'); // dirty, still preview
    expect(s.find('a.ts')!.preview).toBe(true);

    await s.openFile('/ws', 'b.ts'); // dirty preview must survive, not be reused
    expect(s.tabs.map((t) => t.path)).toEqual(['a.ts', 'b.ts']);
    expect(s.find('a.ts')!.preview).toBe(false); // promoted to keep its edits
    expect(s.find('a.ts')!.content).toBe('edited');
  });

  it('promotes the preview tab on save', async () => {
    mockRead('orig');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'a.ts');
    s.setContent('a.ts', 'new');
    apiMock.mockResolvedValue({});
    await s.save('a.ts');
    expect(s.find('a.ts')!.preview).toBe(false);
  });

  it('disambiguates tab labels by parent dir only on basename collision', async () => {
    mockRead('x');
    const s = useEditorTabsStore();
    await s.openFile('/ws', 'README.md', { preview: false });
    expect(s.labelFor(s.find('README.md')!)).toBe('README.md');

    await s.openFile('/ws', 'src/index.ts', { preview: false });
    await s.openFile('/ws', 'lib/index.ts', { preview: false });
    expect(s.labelFor(s.find('src/index.ts')!)).toBe('src/index.ts');
    expect(s.labelFor(s.find('lib/index.ts')!)).toBe('lib/index.ts');
    expect(s.labelFor(s.find('README.md')!)).toBe('README.md');
  });
});
