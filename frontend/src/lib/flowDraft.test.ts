import { describe, it, expect } from 'vitest';
import {
  buildDraftFromFlow,
  appendStep,
  removeStep,
  coordinationOf,
  setCoordination,
  promoteToLead,
  draftToFlow,
  draftToPayload,
  suggestCloneSlug,
} from './flowDraft';
import type { Flow } from '../api/types';

const implementFlow: Flow = {
  slug: 'implement',
  name: 'Implement',
  description: 'Implement, test, commit.',
  builtin: true,
  steps: [
    { agent_slug: 'impl', agent_name: 'Implementation' },
    { agent_slug: 'test', agent_name: 'Testing' },
  ],
};

describe('flowDraft', () => {
  it('clones a built-in into a fresh user-flow draft', () => {
    const d = buildDraftFromFlow(implementFlow, { clone: true });
    expect(d.isClone).toBe(true);
    expect(d.sourceSlug).toBe('implement');
    expect(d.slug).toBe('implement-copy'); // suggested, distinct from the built-in
    expect(d.name).toBe('Implement (copy)');
    expect(d.steps.map((s) => s.agent_slug)).toEqual(['impl', 'test']);
    // Each step gets a stable id so the canvas can track it across reorders.
    expect(d.steps[0].id).toBeTruthy();
    expect(d.steps[0].id).not.toBe(d.steps[1].id);
  });

  it('edits a user flow in place (no clone)', () => {
    const user: Flow = { ...implementFlow, slug: 'my-flow', name: 'My Flow', builtin: false };
    const d = buildDraftFromFlow(user, { clone: false });
    expect(d.isClone).toBe(false);
    expect(d.slug).toBe('my-flow');
    expect(d.name).toBe('My Flow');
  });

  it('appends a step for a new agent and refuses a duplicate', () => {
    const d = buildDraftFromFlow(implementFlow, { clone: true });
    const added = appendStep(d, 'oversight', 'Oversight');
    expect(added).not.toBeNull();
    expect(d.steps.map((s) => s.agent_slug)).toEqual(['impl', 'test', 'oversight']);

    // The backend rejects two steps on the same agent; appendStep guards it.
    const dup = appendStep(d, 'impl', 'Implementation');
    expect(dup).toBeNull();
    expect(d.steps).toHaveLength(3);

    // An empty slug is a no-op.
    expect(appendStep(d, '')).toBeNull();
  });

  it('removes a step and prunes parallel references to it', () => {
    const fan: Flow = {
      slug: 'fan',
      name: 'Fan',
      builtin: false,
      steps: [
        { agent_slug: 'a', run_in_parallel_with: ['b'] },
        { agent_slug: 'b', run_in_parallel_with: ['a'] },
        { agent_slug: 'c' },
      ],
    };
    const d = buildDraftFromFlow(fan, { clone: false });
    expect(removeStep(d, 'b')).toBe(true);
    expect(d.steps.map((s) => s.agent_slug)).toEqual(['a', 'c']);
    // 'a' no longer references the removed 'b' (else the backend rejects it).
    expect(d.steps[0].run_in_parallel_with).toEqual([]);
    // Removing an absent agent is a no-op.
    expect(removeStep(d, 'zzz')).toBe(false);
    expect(d.steps).toHaveLength(2);
  });

  it('reads and writes the coordination mode', () => {
    expect(coordinationOf({ dynamic: false })).toBe('sequence');
    expect(coordinationOf({ dynamic: true, topology: 'orchestrator-worker' })).toBe('lead');
    expect(coordinationOf({ dynamic: true, topology: 'mesh' })).toBe('mesh');

    const d = buildDraftFromFlow(implementFlow, { clone: true });
    setCoordination(d, 'mesh');
    expect(d).toMatchObject({ agentic: true, dynamic: true, topology: 'mesh' });
    expect(coordinationOf(d)).toBe('mesh');

    setCoordination(d, 'lead');
    expect(d).toMatchObject({ dynamic: true, topology: 'orchestrator-worker' });
    expect(coordinationOf(d)).toBe('lead');

    setCoordination(d, 'sequence');
    expect(d.dynamic).toBe(false);
    expect(coordinationOf(d)).toBe('sequence');
  });

  it('promotes a member to lead by moving it to the front', () => {
    const d = buildDraftFromFlow(implementFlow, { clone: true }); // [impl, test]
    expect(promoteToLead(d, 'test')).toBe(true);
    expect(d.steps.map((s) => s.agent_slug)).toEqual(['test', 'impl']);
    // Already-lead and unknown are no-ops.
    expect(promoteToLead(d, 'test')).toBe(false);
    expect(promoteToLead(d, 'nope')).toBe(false);
  });

  it('projects a draft into the Flow shape the canvas renders', () => {
    const d = buildDraftFromFlow(implementFlow, { clone: true });
    d.agentic = true;
    d.dynamic = true;
    d.topology = 'mesh';
    const f = draftToFlow(d);
    expect(f.builtin).toBe(false);
    expect(f.steps?.map((s) => s.agent_slug)).toEqual(['impl', 'test']);
    expect(f.agentic).toBe(true);
    expect(f.topology).toBe('mesh');
  });

  it('serializes a draft to the CRUD payload including agentic fields', () => {
    const d = buildDraftFromFlow(implementFlow, { clone: true });
    d.agentic = true;
    d.dynamic = true;
    d.topology = 'mesh';
    d.max_handoff_depth = 4;
    const p = draftToPayload(d);
    expect(p.slug).toBe('implement-copy');
    expect(p.steps).toEqual([
      { agent_slug: 'impl', optional: false, run_in_parallel_with: [] },
      { agent_slug: 'test', optional: false, run_in_parallel_with: [] },
    ]);
    expect(p).toMatchObject({ agentic: true, dynamic: true, topology: 'mesh', max_handoff_depth: 4 });
  });

  it('bounds a suggested clone slug to 40 chars', () => {
    const long = 'a'.repeat(45);
    expect(suggestCloneSlug(long).length).toBeLessThanOrEqual(40);
    expect(suggestCloneSlug('short')).toBe('short-copy');
  });
});
