// Regression: switching workspace groups under a mounted chat surface must
// reload the planning thread list (threads are per-workspace-group on the
// server). Without the watch in useChatSession the session list went stale
// until a full page reload.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, defineComponent, h, nextTick, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';
import { useChatSession } from './useChatSession';
import { useTaskStore } from '../stores/tasks';

function threadsCalls(): number {
  const f = globalThis.fetch as unknown as { mock: { calls: unknown[][] } };
  return f.mock.calls.filter((c) => String(c[0]).includes('/api/planning/threads')).length;
}

const Harness = defineComponent({
  setup() {
    useChatSession();
    return () => h('div');
  },
});

let app: App | null = null;

describe('useChatSession workspace switch', () => {
  beforeEach(() => {
    setActivePinia(createPinia());
    globalThis.fetch = vi.fn(async (input: RequestInfo | URL) => {
      const url = String(input);
      if (url.includes('/api/planning/threads')) {
        return new Response(JSON.stringify({ threads: [], active_id: '' }), { status: 200 });
      }
      return new Response('[]', { status: 200 });
    }) as never;
  });

  afterEach(() => {
    app?.unmount();
    app = null;
  });

  it('reloads threads when the active workspace changes', async () => {
    const tasks = useTaskStore();
    tasks.config = { workspaces: ['/ws/a'] } as never;

    const host = document.createElement('div');
    app = createApp(Harness);
    app.mount(host);
    await nextTick();
    await Promise.resolve();

    const initial = threadsCalls();
    expect(initial).toBeGreaterThanOrEqual(1); // onMounted load

    // Switch workspace group.
    tasks.config = { workspaces: ['/ws/b'] } as never;
    await nextTick();
    await Promise.resolve();

    expect(threadsCalls()).toBeGreaterThan(initial);
  });

  it('does not reload when the workspace list is unchanged', async () => {
    const tasks = useTaskStore();
    tasks.config = { workspaces: ['/ws/a'] } as never;

    const host = document.createElement('div');
    app = createApp(Harness);
    app.mount(host);
    await nextTick();
    await Promise.resolve();

    const initial = threadsCalls();
    // Reassign an equal workspace list — the stringified key is identical.
    tasks.config = { workspaces: ['/ws/a'] } as never;
    await nextTick();
    await Promise.resolve();

    expect(threadsCalls()).toBe(initial);
  });
});
