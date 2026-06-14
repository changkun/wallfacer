// The workspace-required prompt is the shared empty state shown for every
// workspace-scoped view when no workspace is visible. Its one job is to open
// the workspace picker so the user can recover; pin that.
import { describe, it, expect, beforeEach, afterEach } from 'vitest';
import { createApp, nextTick, type App } from 'vue';
import { createPinia, setActivePinia, type Pinia } from 'pinia';
import WorkspaceRequired from './WorkspaceRequired.vue';
import { useUiStore } from '../stores/ui';

let activePinia: Pinia;

beforeEach(() => {
  activePinia = createPinia();
  setActivePinia(activePinia);
});

describe('WorkspaceRequired', () => {
  let app: App | null = null;
  let host: HTMLElement | null = null;

  afterEach(() => {
    app?.unmount();
    host?.remove();
    app = null;
    host = null;
  });

  it('opens the workspace picker when the button is clicked', async () => {
    host = document.createElement('div');
    document.body.appendChild(host);
    app = createApp(WorkspaceRequired);
    app.use(activePinia);
    app.mount(host);
    await nextTick();

    const ui = useUiStore();
    expect(ui.showWorkspaces).toBe(false);
    host.querySelector<HTMLButtonElement>('button')!.click();
    await nextTick();
    expect(ui.showWorkspaces).toBe(true);
  });
});
