import { describe, it, expect, beforeEach, vi } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { usePlanningStore, type SpecNode } from './planning';
import { useToastStore } from './toast';

function node(path: string): SpecNode {
  return { path, spec: {}, children: [], is_leaf: true, depth: 0 };
}

describe('planning.applyTree bootstrap choreography', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    vi.useFakeTimers();
  });

  // Regression: a fresh page load on a workspace that already has specs
  // must NOT fire the "first spec created" toast. The initial load just
  // populates the tree from empty; it is not a creation event.
  it('does not fire on the initial load even when specs already exist', () => {
    const planning = usePlanningStore();
    const toast = useToastStore();

    expect(planning.tree.length).toBe(0);
    planning.applyTree({ nodes: [node('specs/local/foo.md'), node('specs/local/bar.md')] });

    vi.advanceTimersByTime(200);
    expect(planning.focusedSpecPath).toBe('');
    expect(toast.toasts).toHaveLength(0);
  });

  it('fires focus + toast when the first spec is created after an empty load', () => {
    const planning = usePlanningStore();
    const toast = useToastStore();

    // Initial load: empty workspace establishes the baseline.
    planning.applyTree({ nodes: [] });
    vi.advanceTimersByTime(200);
    expect(toast.toasts).toHaveLength(0);

    // User creates their first spec; a later snapshot delivers it.
    planning.applyTree({ nodes: [node('specs/local/foo.md'), node('specs/local/bar.md')] });

    vi.advanceTimersByTime(130);
    expect(planning.focusedSpecPath).toBe('specs/local/bar.md'); // sorted first

    vi.advanceTimersByTime(40);
    expect(toast.toasts).toHaveLength(1);
    expect(toast.toasts[0].message).toContain('specs/local/bar.md');
  });

  it('does not fire again on updates after the first creation', () => {
    const planning = usePlanningStore();
    const toast = useToastStore();
    planning.applyTree({ nodes: [] }); // baseline
    planning.applyTree({ nodes: [node('a.md')] }); // first creation
    vi.advanceTimersByTime(200);
    expect(toast.toasts).toHaveLength(1);

    planning.applyTree({ nodes: [node('a.md'), node('b.md')] });
    vi.advanceTimersByTime(200);
    expect(toast.toasts).toHaveLength(1); // unchanged
  });

  it('does not fire when the snapshot remains empty', () => {
    const planning = usePlanningStore();
    const toast = useToastStore();
    planning.applyTree({ nodes: [] });
    vi.advanceTimersByTime(200);
    planning.applyTree({ nodes: [] });
    vi.advanceTimersByTime(200);
    expect(toast.toasts).toHaveLength(0);
  });
});
