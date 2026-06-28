// WhiteboardPage hosts a React/Excalidraw island. These tests mock the dynamic
// react / react-dom / @excalidraw/excalidraw imports (so the real ~1.5MB bundle
// never loads in jsdom) and the api client, then prove the wiring: it loads the
// scene on mount, mounts the React root, follows the app theme, debounces saves,
// and tears the root down on unmount.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, type App } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';
import { createPinia, setActivePinia } from 'pinia';

const apiMock = vi.fn();
vi.mock('../api/client', () => ({ api: (...args: unknown[]) => apiMock(...args) }));

const renderSpy = vi.fn();
const unmountSpy = vi.fn();
const createElementSpy = vi.fn((type: unknown, props: unknown) => ({ type, props }));
const serializeMock = vi.fn(
  () => '{"type":"excalidraw","version":2,"elements":[{"id":"x"}],"appState":{}}',
);

vi.mock('react', () => ({ createElement: (...a: unknown[]) => createElementSpy(...a) }));
vi.mock('react-dom/client', () => ({ createRoot: () => ({ render: renderSpy, unmount: unmountSpy }) }));
vi.mock('@excalidraw/excalidraw', () => ({
  Excalidraw: { name: 'Excalidraw' },
  serializeAsJSON: (...a: unknown[]) => serializeMock(...a),
}));
vi.mock('@excalidraw/excalidraw/index.css', () => ({}));

import WhiteboardPage from './WhiteboardPage.vue';

// eslint-disable-next-line @typescript-eslint/no-explicit-any
function lastExcalidrawProps(): any {
  const calls = createElementSpy.mock.calls;
  return calls.length ? calls[calls.length - 1][1] : null;
}

// flush drains the macrotask + microtask queues under REAL timers, letting the
// async onMounted chain (api fetch + dynamic React/Excalidraw imports) settle.
async function flush(times = 8): Promise<void> {
  for (let i = 0; i < times; i++) await new Promise((r) => setTimeout(r, 0));
}

// flushMicro drains only microtasks; safe to use while fake timers are active.
async function flushMicro(times = 12): Promise<void> {
  for (let i = 0; i < times; i++) await Promise.resolve();
}

// Track mounted apps so each test tears its component down; otherwise the live
// MutationObserver from a prior test keeps firing render() and starves the next
// test's onMounted microtask chain.
const mounted: App[] = [];

async function mountPage(): Promise<{ app: App; host: HTMLElement }> {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [{ path: '/whiteboard', component: WhiteboardPage }],
  });
  await router.push('/whiteboard');
  await router.isReady();

  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(WhiteboardPage);
  app.use(createPinia());
  app.use(router);
  app.mount(host);
  mounted.push(app);
  await flush();
  return { app, host };
}

beforeEach(() => {
  setActivePinia(createPinia());
  apiMock.mockReset();
  apiMock.mockImplementation((method: string) =>
    Promise.resolve(method === 'GET' ? null : { status: 'ok' }));
  renderSpy.mockReset();
  unmountSpy.mockReset();
  createElementSpy.mockClear();
  serializeMock.mockClear();
  document.documentElement.removeAttribute('data-theme');
});

afterEach(() => {
  while (mounted.length) {
    const app = mounted.pop();
    try { app?.unmount(); } catch { /* already torn down by a test */ }
  }
  vi.useRealTimers();
  document.body.innerHTML = '';
});

describe('WhiteboardPage', () => {
  it('loads the scene on mount and mounts the Excalidraw root', async () => {
    await mountPage();
    expect(apiMock).toHaveBeenCalledWith('GET', '/api/whiteboard');
    expect(renderSpy).toHaveBeenCalled();
    expect(lastExcalidrawProps()).toBeTruthy();
  });

  it('passes the resolved app theme to Excalidraw', async () => {
    document.documentElement.setAttribute('data-theme', 'dark');
    await mountPage();
    expect(lastExcalidrawProps().theme).toBe('dark');
  });

  it('debounces saves: PUT fires only after the idle window', async () => {
    await mountPage();
    const onChange = lastExcalidrawProps().onChange as (e: unknown, a: unknown, f: unknown) => void;

    vi.useFakeTimers();
    apiMock.mockClear();
    onChange([{ id: 'x' }], {}, {});
    // Nothing persisted yet — still within the debounce window.
    expect(apiMock).not.toHaveBeenCalledWith('PUT', '/api/whiteboard', expect.anything());

    vi.advanceTimersByTime(1500);
    await flushMicro();

    expect(apiMock).toHaveBeenCalledWith('PUT', '/api/whiteboard', expect.objectContaining({
      type: 'excalidraw',
    }));
  });

  it('collapses rapid edits into a single trailing save', async () => {
    await mountPage();
    const onChange = lastExcalidrawProps().onChange as (e: unknown, a: unknown, f: unknown) => void;

    vi.useFakeTimers();
    apiMock.mockClear();
    onChange([], {}, {});
    vi.advanceTimersByTime(500);
    onChange([], {}, {});
    vi.advanceTimersByTime(500);
    onChange([], {}, {});
    vi.advanceTimersByTime(1500);
    await flushMicro();

    const puts = apiMock.mock.calls.filter((c) => c[0] === 'PUT');
    expect(puts).toHaveLength(1);
  });

  it('tears down the React root on unmount', async () => {
    const { app } = await mountPage();
    app.unmount();
    expect(unmountSpy).toHaveBeenCalled();
  });
});
