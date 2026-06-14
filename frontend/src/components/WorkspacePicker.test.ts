// Wizard navigation for the Select Workspaces modal.
//
// The picker is a two step wizard: step 1 chooses folders, step 2 reviews
// and activates. The "Next: Review" button (and the step 2 circle) must be
// disabled until at least one folder is added, and advancing must reveal the
// step 2 review pane. This pins that gating + navigation.
//
// The open watcher is not immediate, so mounting with modelValue:true does
// not fire browse(), meaning no api call runs on mount. We drive the gate via
// "+ Add current folder", which adds browsePath ('/') without any network.

import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import WorkspacePicker from './WorkspacePicker.vue';

let activePinia: Pinia;

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
});

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(WorkspacePicker, { modelValue: true });
  app.use(activePinia);
  app.mount(host);
  await nextTick();
  return { app, host };
}

function findByText(host: HTMLElement, selector: string, text: string): HTMLElement | null {
  return Array.from(host.querySelectorAll<HTMLElement>(selector)).find(
    (el) => el.textContent?.trim().includes(text),
  ) ?? null;
}

describe('WorkspacePicker wizard', () => {
  let app: App | null = null;
  let host: HTMLElement | null = null;

  afterEach(() => {
    app?.unmount();
    host?.remove();
    app = null;
    host = null;
  });

  it('disables Next until a folder is added, then advances to review', async () => {
    ({ app, host } = await mount());

    // jsdom does not lay out, so check v-show via the inline display style on
    // each step body wrapper rather than offsetParent.
    const bodies = Array.from(host.querySelectorAll<HTMLElement>('.ws-picker__body--step'));
    expect(bodies.length).toBe(2);
    const [stepOne, stepTwo] = bodies;
    expect(stepOne.style.display).not.toBe('none');
    expect(stepTwo.style.display).toBe('none');

    const next = findByText(host, 'button', 'Next: Review');
    expect(next).not.toBeNull();
    expect((next as HTMLButtonElement).disabled).toBe(true);

    // Add the current folder ('/'), which flips canProceed without browsing.
    const addCurrent = findByText(host, 'button', '+ Add current folder');
    expect(addCurrent).not.toBeNull();
    (addCurrent as HTMLButtonElement).click();
    await nextTick();

    expect((next as HTMLButtonElement).disabled).toBe(false);
    const count = findByText(host, '.ws-step__count', 'folder added');
    expect(count?.textContent?.trim()).toBe('1 folder added');

    // Advance to step 2: the review pane shows, step 1 hides.
    (next as HTMLButtonElement).click();
    await nextTick();

    expect(stepOne.style.display).toBe('none');
    expect(stepTwo.style.display).not.toBe('none');
    const activate = findByText(host, 'button', 'Activate');
    expect(activate).not.toBeNull();
  });
});
