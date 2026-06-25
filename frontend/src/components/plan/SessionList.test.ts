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
    draft: ref(false),
  };
}

const DAY = 86_400_000;

async function mount(updatedA = Date.now(), updatedB = Date.now()) {
  setActivePinia(createPinia());
  const planning = usePlanningStore();
  planning.threads = {
    a: { id: 'a', name: 'Alpha', archived: false, mode: '', task_id: '', unread: false, scrollTop: 0, queue: [], enqueuedAt: 0, lastViewedAt: 0, created: 0, updated: updatedA },
    b: { id: 'b', name: 'Beta', archived: false, mode: '', task_id: '', unread: false, scrollTop: 0, queue: [], enqueuedAt: 0, lastViewedAt: 0, created: 0, updated: updatedB },
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

describe('SessionList date groups', () => {
  it('buckets sessions by last-activity date, recent bucket first', async () => {
    // Alpha touched just now -> Today; Beta touched 10 days ago -> Previous 30 days.
    await mount(Date.now(), Date.now() - 10 * DAY);
    const headings = [...host.querySelectorAll('.chat-sessions-title')].map((h) => h.textContent);
    expect(headings).toContain('Today');
    expect(headings).toContain('Previous 30 days');
    const heads = [...host.querySelectorAll('.chat-sessions-head')];
    expect(heads[0].querySelector('.chat-sessions-title')?.textContent).toBe('Today');
  });

  it('sorts most recently active first within a bucket', async () => {
    // Both today: Beta more recent than Alpha, so Beta renders above Alpha.
    await mount(Date.now() - 60_000, Date.now());
    const names = [...host.querySelectorAll('.chat-session-name')].map((n) => n.textContent);
    expect(names).toEqual(['Beta', 'Alpha']);
  });
});
