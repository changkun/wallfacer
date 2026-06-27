// The workspace-isolation fix relies on every workspace-scoped local route
// carrying meta.needsWorkspace so App.vue can swap in the WorkspaceRequired
// prompt when no workspace is visible. A missing flag re-opens the leak
// (e.g. the Plan chat rendering under "No workspace"), so pin the wiring.
import { describe, it, expect } from 'vitest';
import { localRoutes } from './router';

function metaFor(path: string): boolean {
  const r = localRoutes.find((route) => route.path === path);
  if (!r) throw new Error(`route ${path} not found`);
  return r.meta?.needsWorkspace === true;
}

describe('localRoutes workspace gating', () => {
  it('marks every workspace-scoped view as needsWorkspace', () => {
    for (const path of ['/', '/agents', '/flows', '/routines', '/analytics', '/chat', '/plan', '/mission', '/map']) {
      expect(metaFor(path), `${path} should require a workspace`).toBe(true);
    }
  });

  it('leaves workspace-independent views ungated', () => {
    for (const path of ['/settings', '/docs', '/docs/:slug(.*)']) {
      expect(metaFor(path), `${path} should not require a workspace`).toBe(false);
    }
  });
});
