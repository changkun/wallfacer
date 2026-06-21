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

function rowFor(name: string): Element | undefined {
  return [...host.querySelectorAll('.chat-session-row')].find(
    (r) => r.querySelector('.chat-session-name')?.textContent === name,
  );
}

describe('SessionList running spinner', () => {
  it('shows a spinner only on the busy session', async () => {
    await mount();
    expect(host.querySelectorAll('.chat-session-row')).toHaveLength(2);
    expect(rowFor('Alpha')!.querySelector('.chat-session-spinner')).toBeNull();
    expect(rowFor('Beta')!.querySelector('.chat-session-spinner')).not.toBeNull();
  });

  it('spins the locally-streaming thread immediately', async () => {
    const planning = await mount();
    planning.streaming = true;
    planning.streamingThreadId = 'a';
    await nextTick();
    expect(rowFor('Alpha')!.querySelector('.chat-session-spinner')).not.toBeNull();
  });
});

describe('SessionList status groups', () => {
  it('groups the busy session under "In progress" and idle ones under "Sessions"', async () => {
    await mount(); // busy_thread_id = 'b' (Beta)
    const headings = [...host.querySelectorAll('.chat-sessions-title')].map((h) => h.textContent);
    expect(headings).toContain('In progress');
    expect(headings).toContain('Sessions');
    // "In progress" group comes first and contains the busy session.
    const heads = [...host.querySelectorAll('.chat-sessions-head')];
    expect(heads[0].querySelector('.chat-sessions-title')?.textContent).toBe('In progress');
  });

  it('groups an unread session under "Needs feedback"', async () => {
    const planning = await mount();
    // Make a non-active, non-busy thread unread.
    planning.busyThreadId = '';
    planning.threads.b.unread = true;
    await nextTick();
    const headings = [...host.querySelectorAll('.chat-sessions-title')].map((h) => h.textContent);
    expect(headings).toContain('Needs feedback');
  });
});
