// The workspace-isolation fix relies on every workspace-scoped local route
// carrying meta.needsWorkspace so App.vue can swap in the WorkspaceRequired
// prompt when no workspace is visible. A missing flag re-opens the leak
// (e.g. the Plan chat rendering under "No workspace"), so pin the wiring.
import { describe, it, expect } from 'vitest';
import { createMemoryHistory, createRouter } from 'vue-router';
import { NOT_FOUND_ROUTE, localRoutes } from './router';
import { routeToRestore } from './lib/lastRoute';

function metaFor(path: string): boolean {
  const r = localRoutes.find((route) => route.path === path);
  if (!r) throw new Error(`route ${path} not found`);
  return r.meta?.needsWorkspace === true;
}

describe('localRoutes workspace gating', () => {
  it('marks every workspace-scoped view as needsWorkspace', () => {
    for (const path of ['/', '/routines', '/analytics', '/chat', '/plan', '/mission', '/map']) {
      expect(metaFor(path), `${path} should require a workspace`).toBe(true);
    }
  });

  it('leaves workspace-independent views ungated', () => {
    for (const path of ['/settings', '/docs', '/docs/:slug(.*)', '/:pathMatch(.*)*']) {
      expect(metaFor(path), `${path} should not require a workspace`).toBe(false);
    }
  });
});

describe('localRoutes not-found fallback', () => {
  const router = createRouter({ history: createMemoryHistory(), routes: localRoutes });

  it('resolves the removed agents page and its former aliases to the not-found page', () => {
    for (const path of ['/agent-graph', '/agents', '/workflows', '/flows']) {
      expect(router.resolve(path).name, path).toBe(NOT_FOUND_ROUTE);
    }
  });

  it('resolves the removed artifacts gallery to the not-found page', () => {
    expect(router.resolve('/artifacts').name).toBe(NOT_FOUND_ROUTE);
  });

  // main.ts restores the last route only when it resolves to a console page;
  // this is the predicate it passes, so a stored /artifacts keeps a cold
  // launch on the board instead of landing on the not-found page.
  it('does not restore a stored /artifacts on a cold launch', () => {
    const routable = (path: string) => router.resolve(path).name !== NOT_FOUND_ROUTE;
    expect(routeToRestore('/', '/artifacts', routable)).toBeNull();
    expect(routeToRestore('/', '/routines', routable)).toBe('/routines');
  });

  it('resolves console pages to themselves', () => {
    for (const path of ['/', '/routines', '/mission', '/docs/guide/usage']) {
      expect(router.resolve(path).name, path).not.toBe(NOT_FOUND_ROUTE);
    }
  });
});
