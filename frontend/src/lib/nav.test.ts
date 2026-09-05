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
    expect(navLabel('/agent-graph')).toBe('Agents');
    expect(navLabel('/nowhere')).toBe('');
  });

  it('pins exactly one group to the bottom and gives every other group an eyebrow', () => {
    const pinned = NAV_GROUPS.filter((g) => g.pin === 'bottom');
    expect(pinned).toHaveLength(1);
    for (const g of NAV_GROUPS.filter((g) => !g.pin)) expect(g.label).toBeTruthy();
  });
});
