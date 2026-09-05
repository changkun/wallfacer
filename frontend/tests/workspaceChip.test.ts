import { describe, expect, it, vi } from 'vitest';
import { createApp, nextTick } from 'vue';
import { createPinia } from 'pinia';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// The workspace chip absorbed the status bar's connection dot and branch
// actions (specs/shared/console-redesign/shell.md). These guard that the dot
// reflects the SSE state and that Sync / Push / Rebase still hit the same
// endpoints the status bar did.
vi.mock('../src/api/client', () => ({
  api: vi.fn(async (_m: string, url: string) => (url === '/api/git/status' ? [] : {})),
  ApiError: class ApiError extends Error { status = 0; body: unknown; },
}));
vi.mock('../src/composables/useSse', () => ({ useSse: () => ({ connected: { value: true }, connState: { value: 'ok' } }) }));

import WorkspaceChip from '../src/components/WorkspaceChip.vue';
import { api } from '../src/api/client';

async function mountChip(connState: 'ok' | 'reconnecting' | 'closed', collapsed = false) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(WorkspaceChip, { collapsed, connState });
  app.use(createPinia());
  const vm = app.mount(host) as unknown as {
    runAction: (ws: { path: string; branch: string }, kind: 'push' | 'sync' | 'rebase') => Promise<void>;
  };
  await nextTick();
  return { host, app, vm };
}

describe('WorkspaceChip', () => {
  it.each(['ok', 'reconnecting', 'closed'] as const)('renders the %s connection dot', async (state) => {
    const { host, app } = await mountChip(state);
    const dot = host.querySelector('.ws-conn')!;
    expect(dot.classList.contains(`ws-conn--${state}`)).toBe(true);
    expect(dot.getAttribute('data-conn')).toBe(state);
    app.unmount(); host.remove();
  });

  it('keeps the dot on the folded switcher', async () => {
    const { host, app } = await mountChip('ok', true);
    expect(host.querySelector('.sb-ws-switch--icon .ws-conn')).toBeTruthy();
    app.unmount(); host.remove();
  });

  it.each([
    ['sync', '/api/git/sync'],
    ['push', '/api/git/push'],
    ['rebase', '/api/git/rebase-on-main'],
  ] as const)('%s posts to %s with the workspace path', async (kind, route) => {
    const { host, app, vm } = await mountChip('ok');
    await vm.runAction({ path: '/w', branch: 'main' }, kind);
    expect(api).toHaveBeenCalledWith('POST', route, { workspace: '/w' });
    app.unmount(); host.remove();
  });

  // The collapsed popover flies out to the right of the rail. Both anchors
  // would squeeze it to a sliver, so the rule must clear `right` and set a
  // width (no layout in the test DOM, so assert the rule source).
  it('rail.css un-squeezes the collapsed popover', () => {
    const css = readFileSync(resolve(process.cwd(), 'src/styles/rail.css'), 'utf8');
    const rule = css.match(/\.sb-ws-switch-wrap--collapsed \.sb-ws-popover--inline\s*\{([^}]*)\}/)?.[1];
    expect(rule).toBeTruthy();
    expect(rule).toMatch(/left:\s*calc\(100%/);
    expect(rule).toMatch(/right:\s*auto/);
    expect(rule).toMatch(/width:\s*\d/);
  });
});
