import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useAgentStore, isDefaultThreadName, truncateTitle, type SpecNode } from './agentSession';
import { useToastStore } from './toast';

function node(path: string): SpecNode {
  return { path, spec: {}, children: [], is_leaf: true, depth: 0 };
}

// Stub fetch so api() (which reads res.text() then JSON.parses) sees this body.
function stubFetchJSON(body: unknown) {
  globalThis.fetch = vi.fn(async () => ({
    ok: true,
    status: 200,
    text: async () => JSON.stringify(body),
  })) as unknown as typeof fetch;
}

describe('agentStore.applyTree bootstrap choreography', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.useFakeTimers();
  });

  // Regression: a fresh page load on a workspace that already has specs
  // must NOT fire the "first spec created" toast. The initial load just
  // populates the tree from empty; it is not a creation event.
  it('does not fire on the initial load even when specs already exist', () => {
    const agentStore = useAgentStore();
    const toast = useToastStore();

    expect(agentStore.tree.length).toBe(0);
    agentStore.applyTree({ nodes: [node('specs/local/foo.md'), node('specs/local/bar.md')] });

    vi.advanceTimersByTime(200);
    expect(agentStore.focusedSpecPath).toBe('');
    expect(toast.toasts).toHaveLength(0);
  });

  // Per-folder grouping: applyTree exposes the groups verbatim, with each
  // folder's progress kept independent even when two folders share a relative
  // spec path (the collision the flat merge would lose).
  it('exposes independent per-folder groups', () => {
    const agentStore = useAgentStore();
    agentStore.applyTree({
      nodes: [node('specs/local/foo.md')],
      groups: [
        { workspace: '/a', label: 'a', nodes: [node('specs/local/foo.md')], progress: { 'specs/local': { Complete: 1, Total: 2 } }, index: null },
        { workspace: '/b', label: 'b', nodes: [node('specs/local/foo.md')], progress: { 'specs/local': { Complete: 0, Total: 3 } }, index: null },
      ],
    });
    expect(agentStore.treeGroups.length).toBe(2);
    expect(agentStore.treeGroups[0].workspace).toBe('/a');
    expect(agentStore.treeGroups[1].workspace).toBe('/b');
    expect(agentStore.treeGroups[0].progress['specs/local'].Total).toBe(2);
    expect(agentStore.treeGroups[1].progress['specs/local'].Total).toBe(3);
  });

  it('fires focus + toast when the first spec is created after an empty load', () => {
    const agentStore = useAgentStore();
    const toast = useToastStore();

    // Initial load: empty workspace establishes the baseline.
    agentStore.applyTree({ nodes: [] });
    vi.advanceTimersByTime(200);
    expect(toast.toasts).toHaveLength(0);

    // User creates their first spec; a later snapshot delivers it.
    agentStore.applyTree({ nodes: [node('specs/local/foo.md'), node('specs/local/bar.md')] });

    vi.advanceTimersByTime(130);
    expect(agentStore.focusedSpecPath).toBe('specs/local/bar.md'); // sorted first

    vi.advanceTimersByTime(40);
    expect(toast.toasts).toHaveLength(1);
    expect(toast.toasts[0].message).toContain('specs/local/bar.md');
  });

  it('does not fire again on updates after the first creation', () => {
    const agentStore = useAgentStore();
    const toast = useToastStore();
    agentStore.applyTree({ nodes: [] }); // baseline
    agentStore.applyTree({ nodes: [node('a.md')] }); // first creation
    vi.advanceTimersByTime(200);
    expect(toast.toasts).toHaveLength(1);

    agentStore.applyTree({ nodes: [node('a.md'), node('b.md')] });
    vi.advanceTimersByTime(200);
    expect(toast.toasts).toHaveLength(1); // unchanged
  });

  it('does not fire when the snapshot remains empty', () => {
    const agentStore = useAgentStore();
    const toast = useToastStore();
    agentStore.applyTree({ nodes: [] });
    vi.advanceTimersByTime(200);
    agentStore.applyTree({ nodes: [] });
    vi.advanceTimersByTime(200);
    expect(toast.toasts).toHaveLength(0);
  });
});

describe('truncateTitle / isDefaultThreadName', () => {
  it('keeps a short message verbatim, collapsing whitespace', () => {
    expect(truncateTitle('  fix   the   auth  bug ')).toBe('fix the auth bug');
  });

  it('ellipsizes past the limit (… replaces, never appends past max)', () => {
    const out = truncateTitle('a'.repeat(80), 48);
    expect(out).toBe('a'.repeat(47) + '…');
    expect(out.length).toBe(48);
  });

  it('recognises only seeded "Chat N" names as default', () => {
    expect(isDefaultThreadName('Chat 1')).toBe(true);
    expect(isDefaultThreadName('Chat 42')).toBe(true);
    expect(isDefaultThreadName('Chat')).toBe(false);
    expect(isDefaultThreadName('fix the auth bug')).toBe(false);
  });
});

describe('refreshBusy provisional-title clobber safety', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
  });

  // The provisional title (user's first message) must survive every poll while
  // the server name is still the default "Chat N" — otherwise the very poll that
  // watches for the AI title would yank the provisional back to "Chat 1".
  it('keeps a provisional name while the server name is still default', async () => {
    const store = useAgentStore();
    stubFetchJSON({ threads: [{ id: 't1', name: 'Chat 1', archived: false }] });
    await store.loadThreads();

    // Promotion-time: client shows the prompt and marks the title pending.
    store.threads['t1'].name = 'help me refactor the auth layer';
    store.threads['t1'].titlePending = true;

    // Server still reports the default name (auto-titler hasn't landed yet).
    stubFetchJSON({ threads: [{ id: 't1', name: 'Chat 1', archived: false }], busy_thread_id: '' });
    await store.refreshBusy();

    expect(store.threads['t1'].name).toBe('help me refactor the auth layer');
    expect(store.threads['t1'].titlePending).toBe(true);
  });

  it('adopts the real title and clears the pending flag once it lands', async () => {
    const store = useAgentStore();
    stubFetchJSON({ threads: [{ id: 't1', name: 'Chat 1', archived: false }] });
    await store.loadThreads();
    store.threads['t1'].name = 'help me refactor the auth layer';
    store.threads['t1'].titlePending = true;

    stubFetchJSON({ threads: [{ id: 't1', name: 'Refactor auth layer', archived: false }], busy_thread_id: '' });
    await store.refreshBusy();

    expect(store.threads['t1'].name).toBe('Refactor auth layer');
    expect(store.threads['t1'].titlePending).toBe(false);
  });

  it('preserves the provisional across a loadThreads rebuild while server is default', async () => {
    const store = useAgentStore();
    stubFetchJSON({ threads: [{ id: 't1', name: 'Chat 1', archived: false }] });
    await store.loadThreads();
    store.threads['t1'].name = 'help me refactor the auth layer';
    store.threads['t1'].titlePending = true;

    stubFetchJSON({ threads: [{ id: 't1', name: 'Chat 1', archived: false }] });
    await store.loadThreads();

    expect(store.threads['t1'].name).toBe('help me refactor the auth layer');
    expect(store.threads['t1'].titlePending).toBe(true);
  });
});
