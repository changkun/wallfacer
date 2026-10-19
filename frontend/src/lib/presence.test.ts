import { describe, it, expect } from 'vitest';
import { derivePresence } from './presence';

describe('derivePresence', () => {
  it('maps in-progress tasks to agent entries', () => {
    const r = derivePresence([{ id: 'b4e13ac4-xxxx' }, { id: 'deadbeef-yyyy' }]);
    expect(r).toEqual([
      { id: 'b4e13ac4-xxxx', label: 'agent-b4e1' },
      { id: 'deadbeef-yyyy', label: 'agent-dead' },
    ]);
  });

  it('empty when no agents are running', () => {
    expect(derivePresence([])).toEqual([]);
  });
});
