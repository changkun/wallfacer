// Regression: the harness segmented control in the "New agent" form must
// reflect a click. The form state (`draft`) was declared via defineModel but
// the embedding page mounts AgentEditor without a v-model:draft binding, so
// nested mutations (draft.harness = ...) did not drive reactivity and the
// active pill never moved off "Default". This mounts the real editor (with
// Pinia, since it pulls the task/dialog stores) and asserts a harness click
// activates that option.

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { createApp, type App } from 'vue';
import { createPinia } from 'pinia';
import AgentEditor from './AgentEditor.vue';

let app: App;
let host: HTMLElement;

async function settle() {
  for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
}

beforeEach(async () => {
  host = document.createElement('div');
  document.body.appendChild(host);
  app = createApp(AgentEditor, { agent: null, isNew: true });
  app.use(createPinia());
  app.mount(host);
  await settle();
});

afterEach(() => {
  app.unmount();
  host.remove();
});

function harnessButtons(): HTMLButtonElement[] {
  return Array.from(host.querySelectorAll<HTMLButtonElement>('.agents-detail__segment-btn'));
}

describe('AgentEditor harness selection (new agent)', () => {
  it('renders the harness segmented control with Default selected', () => {
    const btns = harnessButtons();
    expect(btns.length).toBeGreaterThan(1);
    const active = btns.filter((b) => b.classList.contains('agents-detail__segment-btn--active'));
    expect(active).toHaveLength(1);
    expect(active[0].textContent?.trim()).toBe('Default');
  });

  it('moves the active pill to the clicked harness', async () => {
    const claude = harnessButtons().find((b) => b.textContent?.trim() === 'Claude');
    expect(claude).toBeTruthy();
    claude!.click();
    await settle();

    expect(claude!.classList.contains('agents-detail__segment-btn--active')).toBe(true);
    const active = harnessButtons().filter((b) =>
      b.classList.contains('agents-detail__segment-btn--active'),
    );
    expect(active).toHaveLength(1);
    expect(active[0].textContent?.trim()).toBe('Claude');
  });
});
