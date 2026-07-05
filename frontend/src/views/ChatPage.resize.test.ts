// Regression test for the Chat session-list resize splitter and its auto-fold.
//
// ChatPage mounts SessionList plus the shared chat core; we stub the children
// and side-effecting imports, then drive the drag handle with synthetic mouse
// events. Two behaviours are asserted: the width clamps to [200, 480] like the
// Plan spec-tree splitter, and dragging narrower than the fold threshold snaps
// the list to the collapsed rail instead of sticking at the min width.
//
// happy-dom reports no real geometry, so we exercise only the pure clamp/fold
// math the handler runs against clientX deltas.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia } from 'pinia';

// The chat core reaches the API on mount; stub it to a quiet no-op.
vi.mock('../api/client', () => ({
  api: vi.fn(async () => ({ threads: [], active_id: '', busy_thread_id: '' })),
  authHeaders: () => ({}),
}));

// Stub the heavy children; SessionList renders the bound style so we can read
// back the --chat-sessions-width var the splitter drives.
vi.mock('../components/plan/SessionList.vue', () => ({
  default: { name: 'SessionList', template: '<aside class="session-list-stub"></aside>' },
}));
vi.mock('../components/plan/ChatMessageList.vue', () => ({
  default: { name: 'ChatMessageList', template: '<div></div>' },
}));
vi.mock('../components/plan/ChatComposer.vue', () => ({
  default: { name: 'ChatComposer', template: '<div></div>' },
}));
vi.mock('../components/plan/ChatModelBadge.vue', () => ({
  default: { name: 'ChatModelBadge', template: '<span></span>' },
}));
vi.mock('../components/BrandMark.vue', () => ({
  default: { name: 'BrandMark', template: '<span></span>' },
}));
// Keep the composable inert: an empty session with no messages renders the entry
// screen, which still mounts the session list + splitter.
vi.mock('../composables/useChatSession', () => ({
  useChatSession: () => ({
    renderedMessages: { value: [] },
    streaming: { value: false },
    primaryModel: { value: '' },
    draft: { value: false },
    sendMessage: () => {},
    onInterrupt: () => {},
    createThread: () => {},
    switchToThread: () => {},
  }),
}));

// happy-dom ships only a partial localStorage stub; install an in-memory one.
const memStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (k: string) => (memStore.has(k) ? memStore.get(k)! : null),
  setItem: (k: string, v: string) => { memStore.set(k, String(v)); },
  removeItem: (k: string) => { memStore.delete(k); },
  clear: () => { memStore.clear(); },
});

import ChatPage from './ChatPage.vue';

interface Mounted { app: App; host: HTMLElement }

async function mountPage(): Promise<Mounted> {
  setActivePinia(createPinia());
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(ChatPage);
  app.mount(host);
  await nextTick();
  return { app, host };
}

function drag(host: HTMLElement, fromX: number, toX: number) {
  const handle = host.querySelector('.chat-sessions-resize') as HTMLElement;
  expect(handle).toBeTruthy();
  handle.dispatchEvent(new MouseEvent('mousedown', { clientX: fromX, bubbles: true }));
  document.dispatchEvent(new MouseEvent('mousemove', { clientX: toX, bubbles: true }));
  document.dispatchEvent(new MouseEvent('mouseup', { bubbles: true }));
}

function listWidth(host: HTMLElement): string {
  const el = host.querySelector('.session-list-stub') as HTMLElement;
  return el.style.getPropertyValue('--chat-sessions-width');
}

describe('ChatPage session-list resize', () => {
  beforeEach(() => {
    memStore.clear();
  });
  afterEach(() => {
    document.body.innerHTML = '';
  });

  it('widens the list when the handle is dragged right', async () => {
    const { host, app } = await mountPage();
    // Default 248; drag +100 → 348, within [200, 480].
    drag(host, 100, 200);
    await nextTick();
    expect(listWidth(host)).toBe('348px');
    expect(localStorage.getItem('wallfacer-chat-sessions-width')).toBe('348');
    app.unmount();
  });

  it('clamps the width to the 480px maximum', async () => {
    const { host, app } = await mountPage();
    drag(host, 0, 1000);
    await nextTick();
    expect(listWidth(host)).toBe('480px');
    app.unmount();
  });

  it('clamps to the 200px minimum for a small leftward drag', async () => {
    const { host, app } = await mountPage();
    // 248 - 60 = 188 → still above the 150 fold threshold, clamps to 200.
    drag(host, 100, 40);
    await nextTick();
    expect(listWidth(host)).toBe('200px');
    // Still expanded (not folded).
    expect(host.querySelector('.session-list-stub')).toBeTruthy();
    expect(host.querySelector('.chat-sessions-rail')).toBeFalsy();
    app.unmount();
  });

  it('auto-folds to the rail when dragged past the fold threshold', async () => {
    const { host, app } = await mountPage();
    // 248 - 150 = 98 → below the 150 threshold: collapse to the rail.
    drag(host, 200, 50);
    await nextTick();
    expect(host.querySelector('.session-list-stub')).toBeFalsy();
    expect(host.querySelector('.chat-sessions-rail')).toBeTruthy();
    expect(localStorage.getItem('wallfacer-chat-sessions-collapsed')).toBe('1');
    app.unmount();
  });
});
