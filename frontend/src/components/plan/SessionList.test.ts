// The session list shows a spinner on whichever session has an in-flight agent
// turn — busyThreadId (server truth, so a backgrounded session still spins) or
// the locally-streaming thread. Other rows show no spinner.
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, ref, h, nextTick, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';

vi.mock('../../api/client', () => ({
  // refreshBusy() (fired onMounted) reports thread "b" as busy.
  api: vi.fn(async () => ({ threads: [], active_id: 'a', busy_thread_id: 'b' })),
  authHeaders: () => ({}),
}));

import SessionList from './SessionList.vue';
import { usePlanningStore } from '../../stores/planning';

let app: App | null = null;
let host: HTMLElement;

function stubSession() {
  return {
    createThread: () => {}, switchToThread: () => {}, startRename: () => {},
    commitRename: () => {}, cancelRename: () => {}, archiveThread: () => {},
    unarchiveThread: () => {}, deleteThread: () => {},
    renamingId: ref(''), renameDraft: ref(''), archiveMenuOpen: ref(false),
  };
}

async function mount() {
  setActivePinia(createPinia());
  const planning = usePlanningStore();
  planning.threads = {
    a: { id: 'a', name: 'Alpha', archived: false, mode: '', task_id: '', unread: false, scrollTop: 0, queue: [], enqueuedAt: 0, lastViewedAt: 0 },
    b: { id: 'b', name: 'Beta', archived: false, mode: '', task_id: '', unread: false, scrollTop: 0, queue: [], enqueuedAt: 0, lastViewedAt: 0 },
  };
  planning.threadOrder = ['a', 'b'];
  planning.activeThreadId = 'a';

  host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp({ render: () => h(SessionList, { session: stubSession() as never }) });
  app.mount(host);
  await nextTick();
  await nextTick(); // let onMounted refreshBusy resolve
  await nextTick();
  return planning;
}

beforeEach(() => { document.body.innerHTML = ''; });
afterEach(() => { app?.unmount(); app = null; document.body.innerHTML = ''; });

describe('SessionList running spinner', () => {
  it('shows a spinner only on the busy session', async () => {
    await mount();
    const rows = host.querySelectorAll('.chat-session-row');
    expect(rows).toHaveLength(2);
    // Row order matches threadOrder [a, b]; b is the busy one.
    expect(rows[0].querySelector('.chat-session-spinner')).toBeNull();
    expect(rows[1].querySelector('.chat-session-spinner')).not.toBeNull();
  });

  it('spins the locally-streaming thread immediately', async () => {
    const planning = await mount();
    planning.streaming = true;
    planning.streamingThreadId = 'a';
    await nextTick();
    const rows = host.querySelectorAll('.chat-session-row');
    expect(rows[0].querySelector('.chat-session-spinner')).not.toBeNull();
  });
});
