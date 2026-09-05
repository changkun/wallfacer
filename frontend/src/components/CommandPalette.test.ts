// The command palette is a popover of grouped rows: tasks first, then specs
// and docs. Arrow keys move the selected row and Enter opens it. Pins the row
// vocabulary and the keyboard contract
// (specs/shared/console-redesign/panels-and-overlays.md).
import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import { createRouter, createMemoryHistory, type Router } from 'vue-router';

vi.mock('../api/client', () => ({
  api: vi.fn(async () => ({})),
  withAuthToken: (u: string) => u,
}));

import CommandPalette from './CommandPalette.vue';
import { useTaskStore } from '../stores/tasks';
import { useAgentStore } from '../stores/agentSession';

let pinia: Pinia;
let app: App | null = null;
let router: Router;

const tasks = [
  { id: 'aaaaaaaa-1', title: 'Fix the rail fold', prompt: 'p1', status: 'backlog', turns: 0, updated_at: '2026-09-05T10:00:00Z', created_at: '2026-09-05T10:00:00Z' },
  { id: 'bbbbbbbb-2', title: 'Ship the sheet', prompt: 'p2', status: 'done', turns: 2, updated_at: '2026-09-04T10:00:00Z', created_at: '2026-09-04T10:00:00Z' },
];

async function mount(): Promise<void> {
  router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { template: '<div />' } },
      { path: '/plan', component: { template: '<div />' } },
      { path: '/docs/:slug', component: { template: '<div />' } },
    ],
  });
  await router.push('/plan');
  await router.isReady();
  useTaskStore().tasks = tasks as never;
  useAgentStore().tree = [{ path: 'specs/a.md', spec: { title: 'Alpha', status: 'drafted' } }] as never;
  app = createApp({ render: () => h(CommandPalette, { modelValue: true }) });
  app.use(pinia);
  app.use(router);
  app.mount(document.body.appendChild(document.createElement('div')));
  await nextTick();
  await nextTick();
}

function overlay(): HTMLElement { return document.querySelector('.command-palette') as HTMLElement; }
function taskRows(): HTMLElement[] { return Array.from(document.querySelectorAll('.command-palette-row-task')); }
// A selection lands on a row or on one of the action buttons under a task.
function activeRows(): HTMLElement[] { return Array.from(document.querySelectorAll('.command-palette-row.active, .command-palette-action-btn.active')); }
function key(k: string) { overlay().dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true })); }

beforeEach(() => {
  pinia = createPinia();
  setActivePinia(pinia);
  document.body.innerHTML = '';
});
afterEach(() => { app?.unmount(); app = null; document.body.innerHTML = ''; });

describe('CommandPalette', () => {
  it('renders as a popover with grouped rows and the first row selected', async () => {
    await mount();
    expect(document.querySelector('.command-palette-panel')?.classList.contains('pop')).toBe(true);
    const titles = Array.from(document.querySelectorAll('.command-palette-section-title')).map((e) => e.textContent?.trim());
    expect(titles).toEqual(['Tasks', 'Plan', 'Docs']);
    const rows = taskRows();
    expect(rows.length).toBeGreaterThanOrEqual(3);
    expect(rows[0].textContent).toContain('Fix the rail fold');
    expect(rows[0].classList.contains('active')).toBe(true);
    expect(activeRows().length).toBe(1);
    // The status reads as a pill on the ramp, not a badge.
    expect(rows[0].querySelector('.pill.pill-neutral')).not.toBeNull();
    expect(rows[1].querySelector('.pill.pill-ok')).not.toBeNull();
  });

  it('moves the selection with the arrow keys and wraps', async () => {
    await mount();
    key('ArrowDown');
    await nextTick();
    expect(taskRows()[0].classList.contains('active')).toBe(false);
    expect(activeRows().length).toBe(1);
    key('ArrowUp');
    await nextTick();
    expect(taskRows()[0].classList.contains('active')).toBe(true);
    key('ArrowUp');
    await nextTick();
    // Wrapped to the last row: a doc, at the bottom of the list.
    const last = activeRows()[0];
    expect(last.closest('.command-palette-section')?.querySelector('.command-palette-section-title')?.textContent?.trim()).toBe('Docs');
  });

  it('opens the selected task on Enter', async () => {
    await mount();
    key('Enter');
    await nextTick();
    await router.isReady();
    await new Promise((r) => setTimeout(r, 0));
    expect(router.currentRoute.value.path).toBe('/');
    expect(router.currentRoute.value.query.task).toBe('aaaaaaaa-1');
  });
});
