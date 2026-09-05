// The console navigation model: one source for the rail's rows and the
// topbar's crumb, so a destination is named the same way in both places.

export type NavIcon =
  | 'chat' | 'plan' | 'whiteboard' | 'artifacts' | 'board' | 'agent-graph'
  | 'routines' | 'map' | 'terminal' | 'analytics' | 'docs' | 'settings';

export interface NavItem {
  id: string;
  label: string;
  /** Route path; absent for an action row (terminal). */
  to?: string;
  icon: NavIcon;
}

export interface NavGroup {
  /** Eyebrow above the group; absent for the bottom-pinned group. */
  label?: string;
  pin?: 'bottom';
  items: NavItem[];
}

export const NAV_GROUPS: NavGroup[] = [
  {
    label: 'Workspace',
    items: [
      { id: 'chat', label: 'Chat', to: '/chat', icon: 'chat' },
      { id: 'plan', label: 'Plan', to: '/plan', icon: 'plan' },
      { id: 'whiteboard', label: 'Whiteboard', to: '/whiteboard', icon: 'whiteboard' },
      { id: 'artifacts', label: 'Artifacts', to: '/artifacts', icon: 'artifacts' },
      { id: 'board', label: 'Board', to: '/', icon: 'board' },
      { id: 'agent-graph', label: 'Agents', to: '/agent-graph', icon: 'agent-graph' },
      { id: 'routines', label: 'Routines', to: '/routines', icon: 'routines' },
      { id: 'map', label: 'Mission Control', to: '/mission', icon: 'map' },
    ],
  },
  {
    label: 'Inspect',
    items: [
      { id: 'terminal', label: 'Terminal', icon: 'terminal' },
      { id: 'analytics', label: 'Analytics', to: '/analytics', icon: 'analytics' },
    ],
  },
  {
    pin: 'bottom',
    items: [
      { id: 'docs', label: 'Docs', to: '/docs', icon: 'docs' },
      { id: 'settings', label: 'Settings', to: '/settings', icon: 'settings' },
    ],
  },
];

// activeNavId maps a route path to the nav row it belongs to. `/` is the
// board; `/docs/...` stays on Docs; `/agents`, `/flows` redirect to the agent
// graph in the router, so they never reach here.
export function activeNavId(path: string): string {
  path = path.split('?')[0].split('#')[0];
  if (path === '/') return 'board';
  if (path.startsWith('/docs')) return 'docs';
  if (path.startsWith('/mission') || path.startsWith('/map')) return 'map';
  return path.slice(1).split('/')[0];
}

// navLabel names the destination for the crumb; an unknown path yields ''.
export function navLabel(path: string): string {
  const id = activeNavId(path);
  for (const g of NAV_GROUPS) {
    const hit = g.items.find((i) => i.id === id);
    if (hit) return hit.label;
  }
  return '';
}
