import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useAgentStore, type SpecNode } from './agentSession';
import { useToastStore } from './toast';

function node(path: string): SpecNode {
  return { path, spec: {}, children: [], is_leaf: true, depth: 0 };
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
