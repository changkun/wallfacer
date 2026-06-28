// flowDraft holds the pure, framework-free model behind the agent-graph editor
// (unified-agent-graph-ui.md, M6.2). The editor mutates a draft, the canvas
// renders it, and a save serializes it to the flow CRUD payload. Keeping the
// shaping logic here (rather than inline in AgentGraphPage.vue) makes the
// add/clone/serialize behavior unit-testable without driving DOM drag events.

import type { Flow, FlowStep, FlowTopology } from '../api/types';

// EditStep is one draft step. The stable `id` lets the canvas and the editor
// track a node across reorders without keying on agent_slug (which must stay
// unique per flow but is the thing a user edits). run_in_parallel_with holds
// sibling agent_slugs, matching flow.Step on the wire.
export interface EditStep {
  id: string;
  agent_slug: string;
  agent_name: string;
  optional: boolean;
  run_in_parallel_with: string[];
}

// EditableFlow is the draft a flow is edited through. `sourceSlug` records the
// flow it derives from; `isClone` is true when saving must create a new user
// flow (POST) rather than update in place (PUT) -- the case when editing began
// from a read-only built-in.
export interface EditableFlow {
  slug: string;
  name: string;
  description: string;
  sourceSlug: string | null;
  isClone: boolean;
  steps: EditStep[];
  agentic: boolean;
  dynamic: boolean;
  topology: FlowTopology;
  max_handoff_depth: number;
}

// FlowWritePayload mirrors the handler's flowWriteRequest (POST/PUT /api/flows),
// including the agentic execution fields accepted since the M6.2a backend change.
export interface FlowWritePayload {
  slug: string;
  name: string;
  description: string;
  steps: FlowStep[];
  agentic: boolean;
  dynamic: boolean;
  topology: FlowTopology;
  max_handoff_depth: number;
}

// makeId returns a stable step id. crypto.randomUUID is available in the browser
// and in Node test runners; the counter fallback keeps pure logic usable in any
// environment without making the module depend on a global.
let idCounter = 0;
export function makeId(): string {
  const c = (globalThis as { crypto?: { randomUUID?: () => string } }).crypto;
  if (c && typeof c.randomUUID === 'function') return c.randomUUID();
  idCounter += 1;
  return `step-${idCounter}`;
}

// suggestCloneSlug derives a kebab-case slug for a clone, bounded to the 40-char
// limit the backend enforces (slugutil.IsValid).
export function suggestCloneSlug(base: string): string {
  const s = `${base}-copy`;
  return s.length <= 40 ? s : `${base.slice(0, 35)}-copy`;
}

function toEditStep(s: FlowStep): EditStep {
  return {
    id: makeId(),
    agent_slug: s.agent_slug || '',
    agent_name: s.agent_name || '',
    optional: !!s.optional,
    run_in_parallel_with: (s.run_in_parallel_with || []).slice(),
  };
}

// buildDraftFromFlow seeds a draft from a flow. When `clone` is true (editing a
// built-in, which is read-only), the draft gets a fresh slug + a "(copy)" name
// and saving will POST a new user flow; otherwise the draft edits the flow in
// place and saving will PUT.
export function buildDraftFromFlow(flow: Flow, opts: { clone: boolean }): EditableFlow {
  return {
    slug: opts.clone ? suggestCloneSlug(flow.slug) : flow.slug,
    name: opts.clone ? `${flow.name || flow.slug} (copy)` : flow.name || '',
    description: flow.description || '',
    sourceSlug: flow.slug,
    isClone: opts.clone,
    steps: (flow.steps || []).map(toEditStep),
    agentic: !!flow.agentic,
    dynamic: !!flow.dynamic,
    topology: flow.topology || 'orchestrator-worker',
    max_handoff_depth: flow.max_handoff_depth || 0,
  };
}

// appendStep adds a sequential step for an agent and returns the new step. A
// flow may not reference the same agent twice (the backend rejects duplicates,
// since agent_slug is the wiring key), so a slug already present is a no-op and
// returns null -- the caller can surface that to the user.
export function appendStep(draft: EditableFlow, agentSlug: string, agentName = ''): EditStep | null {
  if (!agentSlug) return null;
  if (draft.steps.some((s) => s.agent_slug === agentSlug)) return null;
  const step: EditStep = {
    id: makeId(),
    agent_slug: agentSlug,
    agent_name: agentName,
    optional: false,
    run_in_parallel_with: [],
  };
  draft.steps.push(step);
  return step;
}

// removeStep deletes the step for an agent and prunes every dangling reference
// to it: a removed agent must also disappear from any sibling's
// run_in_parallel_with, or the backend rejects the flow (a parallel ref must
// resolve to a present sibling). Returns true when a step was removed.
export function removeStep(draft: EditableFlow, agentSlug: string): boolean {
  const idx = draft.steps.findIndex((s) => s.agent_slug === agentSlug);
  if (idx === -1) return false;
  draft.steps.splice(idx, 1);
  for (const s of draft.steps) {
    s.run_in_parallel_with = s.run_in_parallel_with.filter((p) => p !== agentSlug);
  }
  return true;
}

// draftToFlow projects a draft into the Flow shape the read-only canvas renders,
// so the editor reuses one renderer for both the saved flow and the live draft.
export function draftToFlow(draft: EditableFlow): Flow {
  return {
    slug: draft.slug,
    name: draft.name,
    description: draft.description,
    builtin: false,
    steps: draft.steps.map((s) => ({
      agent_slug: s.agent_slug,
      agent_name: s.agent_name,
      optional: s.optional,
      run_in_parallel_with: s.run_in_parallel_with.slice(),
    })),
    agentic: draft.agentic,
    dynamic: draft.dynamic,
    topology: draft.topology,
    max_handoff_depth: draft.max_handoff_depth,
  };
}

// draftToPayload serializes a draft to the flow CRUD body.
export function draftToPayload(draft: EditableFlow): FlowWritePayload {
  return {
    slug: draft.slug.trim(),
    name: draft.name.trim(),
    description: draft.description.trim(),
    steps: draft.steps.map((s) => ({
      agent_slug: s.agent_slug,
      optional: s.optional,
      run_in_parallel_with: s.run_in_parallel_with,
    })),
    agentic: draft.agentic,
    dynamic: draft.dynamic,
    topology: draft.topology,
    max_handoff_depth: draft.max_handoff_depth,
  };
}
