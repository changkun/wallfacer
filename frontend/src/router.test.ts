// The workspace-isolation fix relies on every workspace-scoped local route
// carrying meta.needsWorkspace so App.vue can swap in the WorkspaceRequired
// prompt when no workspace is visible. A missing flag re-opens the leak
// (e.g. the Plan chat rendering under "No workspace"), so pin the wiring.
import { describe, it, expect } from 'vitest';
import { createMemoryHistory, createRouter } from 'vue-router';
import { NOT_FOUND_ROUTE, routes } from './router';

function metaFor(path: string): boolean {
  const r = routes.find((route) => route.path === path);
  if (!r) throw new Error(`route ${path} not found`);
  return r.meta?.needsWorkspace === true;
}

describe('routes workspace gating', () => {
  it('marks every workspace-scoped view as needsWorkspace', () => {
    for (const path of ['/', '/analytics', '/chat', '/plan', '/mission', '/map']) {
      expect(metaFor(path), `${path} should require a workspace`).toBe(true);
    }
  });

  it('leaves workspace-independent views ungated', () => {
    for (const path of ['/settings', '/docs', '/docs/:slug(.*)', '/:pathMatch(.*)*']) {
      expect(metaFor(path), `${path} should not require a workspace`).toBe(false);
    }
  });
});

describe('routes not-found fallback', () => {
  const router = createRouter({ history: createMemoryHistory(), routes });

  it('resolves an unknown path to the not-found page', () => {
    expect(router.resolve('/nowhere').name).toBe(NOT_FOUND_ROUTE);
  });

  it('resolves console pages to themselves', () => {
    for (const path of ['/', '/analytics', '/mission', '/docs/guide/usage']) {
      expect(router.resolve(path).name, path).not.toBe(NOT_FOUND_ROUTE);
    }
  });
});
