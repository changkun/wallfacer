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

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, h, nextTick, ref, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import WorkspacePicker from './WorkspacePicker.vue';

// Capture browse calls and feed canned directory listings so the picker can be
// driven without a real backend.
const apiCalls: { method: string; path: string }[] = [];
let browseEntries: { name: string; path: string; is_git_repo: boolean }[] = [];
vi.mock('../api/client', () => ({
  api: vi.fn((method: string, path: string) => {
    apiCalls.push({ method, path });
    if (path.startsWith('/api/workspaces/browse')) {
      return Promise.resolve({ path: '/home/u', entries: browseEntries });
    }
    return Promise.resolve({});
  }),
}));

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

// mountOpen mounts via a parent that flips modelValue false->true after mount,
// so the open watcher fires and browse() runs against the mocked api.
async function mountOpen(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const open = ref(false);
  const app = createApp({
    setup: () => () => h(WorkspacePicker, { modelValue: open.value }),
  });
  app.use(activePinia);
  app.mount(host);
  open.value = true;
  await nextTick();
  await nextTick();
  await Promise.resolve();
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

  beforeEach(() => {
    apiCalls.length = 0;
    browseEntries = [];
  });

  afterEach(() => {
    app?.unmount();
    host?.remove();
    app = null;
    host = null;
  });

  it('opens the browser at the home directory (empty path), not filesystem root', async () => {
    ({ app, host } = await mountOpen());
    const browse = apiCalls.find((c) => c.path.startsWith('/api/workspaces/browse'));
    expect(browse).toBeDefined();
    // Empty path makes the backend resolve to the user's home directory.
    expect(browse!.path).toBe('/api/workspaces/browse?path=');
  });

  it('sinks already-added folders to the bottom of the browse list', async () => {
    browseEntries = [
      { name: 'alpha', path: '/home/u/alpha', is_git_repo: false },
      { name: 'beta', path: '/home/u/beta', is_git_repo: false },
      { name: 'gamma', path: '/home/u/gamma', is_git_repo: false },
    ];
    ({ app, host } = await mountOpen());

    // Add the middle entry; it should move to the end of the list.
    const addButtons = Array.from(host!.querySelectorAll<HTMLElement>('.ws-entry__add'));
    const betaRow = Array.from(host!.querySelectorAll<HTMLElement>('.ws-entry')).find((r) =>
      r.textContent?.includes('beta'),
    );
    (betaRow!.querySelector('.ws-entry__add') as HTMLButtonElement).click();
    await nextTick();
    expect(addButtons.length).toBe(3);

    const order = Array.from(host!.querySelectorAll<HTMLElement>('.ws-entry__name')).map(
      (el) => el.textContent?.trim().replace(/git$/, '') ?? '',
    );
    expect(order).toEqual(['alpha', 'gamma', 'beta']);
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
