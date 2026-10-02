import { describe, expect, it } from 'vitest';
import { NAV_GROUPS, activeNavId, navLabel } from './nav';

describe('nav model', () => {
  it('maps the board, docs deep links and the mission alias to their rows', () => {
    expect(activeNavId('/')).toBe('board');
    expect(activeNavId('/docs/guide/usage')).toBe('docs');
    expect(activeNavId('/mission')).toBe('map');
    expect(activeNavId('/plan')).toBe('plan');
    expect(activeNavId('/settings?tab=about')).toBe('settings');
  });

  it('names every routed destination and nothing else', () => {
    expect(navLabel('/')).toBe('Board');
    expect(navLabel('/routines')).toBe('Routines');
    expect(navLabel('/nowhere')).toBe('');
  });

  it('has no row for the removed agents page', () => {
    const items = NAV_GROUPS.flatMap((g) => g.items);
    expect(items.some((i) => i.label === 'Agents' || i.to === '/agent-graph')).toBe(false);
    expect(navLabel('/agent-graph')).toBe('');
  });

  it('has no row for the removed artifacts gallery', () => {
    const items = NAV_GROUPS.flatMap((g) => g.items);
    expect(items.some((i) => i.id === 'artifacts' || i.label === 'Artifacts' || i.to === '/artifacts')).toBe(false);
    expect(navLabel('/artifacts')).toBe('');
  });

  it('pins exactly one group to the bottom and gives every other group an eyebrow', () => {
    const pinned = NAV_GROUPS.filter((g) => g.pin === 'bottom');
    expect(pinned).toHaveLength(1);
    for (const g of NAV_GROUPS.filter((g) => !g.pin)) expect(g.label).toBeTruthy();
  });
});
