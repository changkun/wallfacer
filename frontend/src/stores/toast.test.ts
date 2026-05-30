import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import { setActivePinia, createPinia } from 'pinia';
import { useToastStore } from './toast';

describe('toast store', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.useFakeTimers(); });
  afterEach(() => vi.useRealTimers());

  it('push adds a toast and auto-dismisses after the timeout', () => {
    const t = useToastStore();
    t.push('hello', { timeout: 1000 });
    expect(t.toasts).toHaveLength(1);
    expect(t.toasts[0]).toMatchObject({ message: 'hello', kind: 'info' });
    vi.advanceTimersByTime(1000);
    expect(t.toasts).toHaveLength(0);
  });

  it('timeout 0 is sticky', () => {
    const t = useToastStore();
    t.push('stay', { timeout: 0 });
    vi.advanceTimersByTime(60000);
    expect(t.toasts).toHaveLength(1);
  });

  it('dismiss removes by id', () => {
    const t = useToastStore();
    const id = t.push('x', { timeout: 0 });
    t.dismiss(id);
    expect(t.toasts).toHaveLength(0);
  });

  it('pushWithAction runs the action then dismisses the toast', () => {
    const t = useToastStore();
    const run = vi.fn();
    t.pushWithAction('undo?', 'Undo', run, { timeout: 0 });
    expect(t.toasts).toHaveLength(1);
    t.toasts[0].action!.run();
    expect(run).toHaveBeenCalledOnce();
    expect(t.toasts).toHaveLength(0);
  });
});
