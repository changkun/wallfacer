// Migrate legacy hash-mode deep links to history-mode routes (spec AD-2).
// Old UI used `#<uuid>` for a task modal and `#plan/<path>` / `#plan` for the
// agent-session/plan view. Returns the equivalent history route, or null when the hash
// isn't a recognised legacy deep link.

export interface HashTarget {
  path: string;
  query?: Record<string, string>;
}

const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;

export function hashToRoute(hash: string): HashTarget | null {
  const h = hash.replace(/^#/, '').trim();
  if (!h) return null;
  if (h === 'plan') return { path: '/plan' };
  if (h.startsWith('plan/')) {
    const spec = h.slice('plan/'.length);
    return spec ? { path: '/plan', query: { spec } } : { path: '/plan' };
  }
  if (UUID_RE.test(h)) return { path: '/', query: { task: h } };
  return null;
}
