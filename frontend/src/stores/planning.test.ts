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

  it('fires focus + toast on the empty → first-node transition', () => {
    const planning = usePlanningStore();
    const toast = useToastStore();

    expect(planning.tree.length).toBe(0);
    planning.applyTree({ nodes: [node('specs/local/foo.md'), node('specs/local/bar.md')] });

    vi.advanceTimersByTime(130);
    expect(planning.focusedSpecPath).toBe('specs/local/bar.md'); // sorted first

    vi.advanceTimersByTime(40);
    expect(toast.toasts).toHaveLength(1);
    expect(toast.toasts[0].message).toContain('specs/local/bar.md');
  });

  it('does not fire on subsequent updates', () => {
    const planning = usePlanningStore();
    const toast = useToastStore();
    planning.applyTree({ nodes: [node('a.md')] });
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
    expect(toast.toasts).toHaveLength(0);
  });
});
