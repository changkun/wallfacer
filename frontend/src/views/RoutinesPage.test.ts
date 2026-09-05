// The Routines page lists routines as rows and drives the schedule endpoint:
// the enabled switch and the interval patch it, Run now triggers it, the form
// creates. Pins the rows vocabulary
// (specs/shared/console-redesign/secondary-screens.md).
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';

const calls: { method: string; path: string; body?: unknown }[] = [];
let routines: unknown[] = [];
vi.mock('../api/client', () => ({
  api: vi.fn(async (method: string, path: string, body?: unknown) => {
    calls.push({ method, path, body });
    if (method === 'GET' && path === '/api/routines') return { routines };
    if (method === 'GET' && path === '/api/flows') return [{ slug: 'implement', name: 'Implement' }];
    return {};
  }),
}));

import RoutinesPage from './RoutinesPage.vue';

let pinia: Pinia;
let app: App | null = null;
let host: HTMLElement;

async function mount() {
  host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp(RoutinesPage);
  app.use(pinia);
  app.mount(host);
  for (let i = 0; i < 4; i++) { await nextTick(); await Promise.resolve(); }
}

beforeEach(() => {
  pinia = createPinia();
  setActivePinia(pinia);
  calls.length = 0;
  routines = [
    { id: 'r1', kind: 'routine', status: 'backlog', title: 'Weekly cleanup', prompt: 'p', routine_interval_seconds: 3600, routine_enabled: true, routine_spawn_flow: 'implement', routine_next_run: new Date(Date.now() + 90_000).toISOString() },
    { id: 'r2', kind: 'routine', status: 'backlog', title: 'Paused one', prompt: 'p2', routine_interval_seconds: 300, routine_enabled: false },
  ];
});
afterEach(() => { app?.unmount(); app = null; host.remove(); });

describe('RoutinesPage', () => {
  it('renders one row per routine with the schedule pill and the switch state', async () => {
    await mount();
    const rows = host.querySelectorAll('.routine-row');
    expect(rows.length).toBe(2);
    expect(rows[0].querySelector('.routine-row__every')?.textContent).toBe('every 60 min');
    expect(rows[0].querySelector('.routine-row__flow')?.textContent).toBe('Implement');
    expect(rows[0].querySelector('.routine-row__next')?.textContent).toMatch(/^in 1m/);
    expect(rows[1].querySelector('.routine-row__next')?.textContent).toBe('paused');
    expect(rows[0].querySelector('[role="switch"]')?.getAttribute('aria-checked')).toBe('true');
    expect(rows[1].querySelector('[role="switch"]')?.getAttribute('aria-checked')).toBe('false');
  });

  it('patches the schedule from the switch and triggers from Run now', async () => {
    await mount();
    const row = host.querySelector('.routine-row') as HTMLElement;
    (row.querySelector('[role="switch"]') as HTMLButtonElement).click();
    await nextTick();
    const patch = calls.find((c) => c.method === 'PATCH');
    expect(patch?.path).toBe('/api/routines/r1/schedule');
    expect(patch?.body).toEqual({ enabled: false });
    // The patch reloads the list before the row is free again.
    for (let i = 0; i < 4; i++) { await nextTick(); await Promise.resolve(); }
    (host.querySelector('.routine-row__run') as HTMLButtonElement).click();
    await nextTick();
    expect(calls.some((c) => c.method === 'POST' && c.path === '/api/routines/r1/trigger')).toBe(true);
  });

  it('creates a routine from the form and shows the empty state without rows', async () => {
    routines = [];
    await mount();
    expect(host.querySelector('.routines-empty')).not.toBeNull();
    const ta = host.querySelector('.routine-create__prompt') as HTMLTextAreaElement;
    ta.value = 'Do the thing';
    ta.dispatchEvent(new Event('input'));
    await nextTick();
    (host.querySelector('.routine-create__btn') as HTMLButtonElement).click();
    await nextTick();
    const post = calls.find((c) => c.method === 'POST' && c.path === '/api/routines');
    expect(post).toBeDefined();
    expect((post!.body as { prompt: string }).prompt).toBe('Do the thing');
  });
});
