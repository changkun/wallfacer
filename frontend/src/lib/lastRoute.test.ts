import { describe, it, expect, beforeEach, vi } from 'vitest';

// happy-dom ships only a partial localStorage stub; install an in-memory one
// (mirrors dock.test.ts).
const memStore = new Map<string, string>();
vi.stubGlobal('localStorage', {
  getItem: (k: string) => (memStore.has(k) ? memStore.get(k)! : null),
  setItem: (k: string, v: string) => { memStore.set(k, String(v)); },
  removeItem: (k: string) => { memStore.delete(k); },
  clear: () => { memStore.clear(); },
});

import { rememberRoute, routeToRestore } from './lastRoute';

describe('lastRoute', () => {
  beforeEach(() => memStore.clear());

  it('restores a stored non-default route on a cold board landing', () => {
    expect(routeToRestore('/', '/plan?spec=specs/foo.md')).toBe('/plan?spec=specs/foo.md');
  });

  it('honours an explicit URL (does not override a non-/ landing)', () => {
    expect(routeToRestore('/plan', '/agents')).toBeNull();
    expect(routeToRestore('/?task=t1', '/plan?spec=specs/foo.md')).toBeNull();
  });

  it('stays on the board when nothing better is stored', () => {
    expect(routeToRestore('/', null)).toBeNull();
    expect(routeToRestore('/', '/')).toBeNull();
  });

  it('round-trips through storage', () => {
    rememberRoute('/plan?spec=specs/bar.md');
    expect(routeToRestore('/')).toBe('/plan?spec=specs/bar.md');
  });
});
