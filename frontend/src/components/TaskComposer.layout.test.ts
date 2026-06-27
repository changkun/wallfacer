// Layout regressions in the new-task composer's options/actions rows.
//
// 1. Selecting "Custom…" for Timeout used to drop the minutes input onto its
//    own line below the select (the parent .composer__opt is a vertical
//    column), so the custom input must now live in a horizontal controls row
//    alongside the select.
// 2. The Cancel and Save buttons used to wrap independently — Cancel could end
//    up on a different row than Save. They must now be grouped so they stay
//    adjacent and right-aligned (Cancel left of Save).

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import TaskComposer from './TaskComposer.vue';

vi.mock('../api/client', () => ({
  api: vi.fn(() => Promise.resolve([])),
}));

let activePinia: Pinia;

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
});

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(TaskComposer, { autoExpand: true });
  app.use(activePinia);
  app.mount(host);
  await nextTick();
  await nextTick();
  return { app, host };
}

describe('TaskComposer layout', () => {
  let app: App;
  let host: HTMLElement;

  afterEach(() => {
    app?.unmount();
    host?.remove();
  });

  it('keeps the custom timeout input beside the select on one row', async () => {
    ({ app, host } = await mount());

    const select = host.querySelector<HTMLSelectElement>('select[aria-label="Timeout preset"]');
    expect(select).not.toBeNull();
    select!.value = 'custom';
    select!.dispatchEvent(new Event('change'));
    await nextTick();

    const num = host.querySelector<HTMLInputElement>('.composer__input--num[aria-label="Custom timeout in minutes"]');
    expect(num).not.toBeNull();
    // Select and the custom input share the same horizontal controls wrapper,
    // rather than the input dropping to a new line below the select.
    const controls = num!.closest('.composer__opt-controls');
    expect(controls).not.toBeNull();
    expect(controls!.contains(select!)).toBe(true);
  });

  it('groups Cancel and Save so they stay adjacent', async () => {
    ({ app, host } = await mount());

    const group = host.querySelector('.composer__btn-group');
    expect(group).not.toBeNull();
    const ghost = group!.querySelector('.composer__btn--ghost');
    const primary = group!.querySelector('.composer__btn--primary');
    expect(ghost?.textContent?.trim()).toBe('Cancel');
    expect(primary).not.toBeNull();
    // Cancel comes before Save in document order (left of it).
    expect(ghost!.compareDocumentPosition(primary!) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
