import { describe, it, expect } from 'vitest';
import { derivePresence } from './presence';

describe('derivePresence', () => {
  it('maps in-progress tasks to agent entries', () => {
    const r = derivePresence([{ id: 'b4e13ac4-xxxx' }, { id: 'deadbeef-yyyy' }], null);
    expect(r).toEqual([
      { id: 'b4e13ac4-xxxx', label: 'agent-b4e1', kind: 'agent' },
      { id: 'deadbeef-yyyy', label: 'agent-dead', kind: 'agent' },
    ]);
  });

  it('appends self with name preference, then email, then "you"', () => {
    expect(derivePresence([], { name: 'Changkun', email: 'c@x' }).at(-1)).toEqual({ id: 'self', label: 'Changkun', kind: 'self' });
    expect(derivePresence([], { email: 'c@x' }).at(-1)).toMatchObject({ label: 'c@x', kind: 'self' });
    expect(derivePresence([], {}).at(-1)).toMatchObject({ label: 'you' });
  });

  it('no self entry when not signed in', () => {
    expect(derivePresence([{ id: 'a-1' }], null)).toHaveLength(1);
  });

  it('empty when no agents and no user', () => {
    expect(derivePresence([], null)).toEqual([]);
  });
});
