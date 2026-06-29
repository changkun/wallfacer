// AgentGraphPage is the read-only agent-graph scaffold (M6.1). It fetches the
// agent registry and flow registry, lists the agents in the palette, and
// renders the auto-selected flow as an agent-graph (one node per step, order
// edges, topology indicator). The page is store-free and router-free, so the
// test mounts it with a bare createApp and mocks fetch (the transport behind
// api()), routing responses by URL.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, type App } from 'vue';
import AgentGraphPage from './AgentGraphPage.vue';
import type { Agent, Flow } from '../api/types';

let originalFetch: typeof globalThis.fetch;
let agents: Agent[];
let flows: Flow[];
let tasks: unknown[];
let lineages: Record<string, unknown>;

beforeEach(() => {
  agents = [];
  flows = [];
  tasks = [];
  lineages = {};
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    const method = (init?.method || 'GET').toUpperCase();
    let body: unknown = null;
    const flowDetail = url.match(/\/api\/flows\/([^/?]+)/);
    if (method === 'DELETE' && flowDetail) {
      flows = flows.filter((f) => f.slug !== decodeURIComponent(flowDetail[1]));
      return new Response(null, { status: 204 });
    }
    const lin = url.match(/\/api\/tasks\/([^/?]+)\/lineage/);
    if (url.includes('/api/agents')) body = agents;
    else if (lin) body = lineages[decodeURIComponent(lin[1])] ?? { nodes: [], edges: [] };
    else if (url.includes('/api/tasks')) body = tasks;
    else {
      // GET /api/flows/<slug> returns a single flow (the detail route the editor
      // fetches before cloning); GET /api/flows returns the list.
      const m = url.match(/\/api\/flows\/([^/?]+)/);
      if (m) body = flows.find((f) => f.slug === decodeURIComponent(m[1])) ?? null;
      else if (url.includes('/api/flows')) body = flows;
    }
    return new Response(JSON.stringify(body ?? []), { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(AgentGraphPage);
  app.mount(host);
  // Let the onMounted fetches + reactive updates settle.
  for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
  return { app, host };
}

describe('AgentGraphPage (fleet)', () => {
  it('renders a fleet: a lead plus members, with delegation edges', async () => {
    agents = [
      { slug: 'lead', title: 'Lead', description: 'Leads', builtin: true },
      { slug: 'w1', title: 'Worker One', builtin: true },
      { slug: 'w2', title: 'Worker Two', builtin: true },
    ];
    flows = [
      {
        slug: 'fleet',
        name: 'A Fleet',
        builtin: true,
        agentic: true,
        dynamic: true,
        topology: 'orchestrator-worker',
        steps: [
          { agent_slug: 'lead', agent_name: 'Lead' },
          { agent_slug: 'w1', agent_name: 'Worker One' },
          { agent_slug: 'w2', agent_name: 'Worker Two' },
        ],
      },
    ];

    const { app, host } = await mount();
    const text = host.textContent ?? '';

    // Palette lists all three agents.
    expect(host.querySelectorAll('.ag-card').length).toBe(3);
    // One agent node per step (Task and Outcome are not agent nodes).
    expect(host.querySelectorAll('.agc-node--agent').length).toBe(3);
    // The first agent is the lead.
    expect(host.querySelectorAll('.agc-node--lead').length).toBe(1);
    expect(text).toContain('LEAD');
    // The lead delegates to each member: two delegation edges.
    expect(host.querySelectorAll('.agc-edge--delegate').length).toBe(2);
    // Coordination mode is surfaced.
    expect(text).toContain('Lead delegates');

    app.unmount();
    host.remove();
  });

  it('renders the simple case (fixed sequence) as a chain', async () => {
    agents = [{ slug: 'a', title: 'A', builtin: true }, { slug: 'b', title: 'B', builtin: true }];
    flows = [
      {
        slug: 'seq',
        name: 'Seq',
        builtin: true, // no agentic/dynamic -> sequence
        steps: [
          { agent_slug: 'a', agent_name: 'A' },
          { agent_slug: 'b', agent_name: 'B' },
        ],
      },
    ];

    const { app, host } = await mount();
    expect((host.textContent ?? '')).toContain('Fixed sequence');
    expect(host.querySelectorAll('.agc-node--agent').length).toBe(2);
    // A sequence has no delegation edges.
    expect(host.querySelectorAll('.agc-edge--delegate').length).toBe(0);

    app.unmount();
    host.remove();
  });

  it('clones a built-in into an editable draft and offers a save action', async () => {
    agents = [{ slug: 'impl', title: 'Implementation', builtin: true }];
    flows = [
      { slug: 'implement', name: 'Implement', builtin: true, steps: [{ agent_slug: 'impl', agent_name: 'Implementation' }] },
    ];

    const { app, host } = await mount();
    const editBtn = host.querySelector('.ag-detail__edit') as HTMLButtonElement;
    expect(editBtn.textContent).toContain('Clone & edit');
    editBtn.click();
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    expect(host.querySelector('.ag-edit')).toBeTruthy();
    expect(host.querySelector('.ag-edit__btn--save')).toBeTruthy();
    expect((host.textContent ?? '').toLowerCase()).toContain('clone of implement');
    expect((host.querySelector('.ag-card') as HTMLElement).getAttribute('draggable')).toBe('true');

    app.unmount();
    host.remove();
  });

  it('removes an agent when its remove control is activated', async () => {
    agents = [{ slug: 'impl', title: 'Implementation', builtin: true }, { slug: 'test', title: 'Testing', builtin: true }];
    flows = [
      {
        slug: 'duo', name: 'Duo', builtin: false,
        agentic: true, dynamic: true, topology: 'orchestrator-worker',
        steps: [
          { agent_slug: 'impl', agent_name: 'Implementation' },
          { agent_slug: 'test', agent_name: 'Testing' },
        ],
      },
    ];

    const { app, host } = await mount();
    expect(host.querySelectorAll('.agc-node--agent').length).toBe(2);
    (host.querySelector('.ag-detail__edit') as HTMLButtonElement).click();
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    const removers = host.querySelectorAll('.agc-node-remove');
    expect(removers.length).toBe(2);
    (removers[1] as SVGGElement).dispatchEvent(new Event('click', { bubbles: true }));
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
    expect(host.querySelectorAll('.agc-node--agent').length).toBe(1);

    app.unmount();
    host.remove();
  });

  it('promotes a member to lead via its set-lead control', async () => {
    agents = [{ slug: 'a', title: 'Alpha', builtin: true }, { slug: 'b', title: 'Bravo', builtin: true }];
    flows = [
      {
        slug: 'pair', name: 'Pair', builtin: false,
        agentic: true, dynamic: true, topology: 'orchestrator-worker',
        steps: [
          { agent_slug: 'a', agent_name: 'Alpha' },
          { agent_slug: 'b', agent_name: 'Bravo' },
        ],
      },
    ];

    const { app, host } = await mount();
    (host.querySelector('.ag-detail__edit') as HTMLButtonElement).click();
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    // Only the member (Bravo) exposes a set-lead control; Alpha is already lead.
    const leadBtns = host.querySelectorAll('.agc-node-lead-btn');
    expect(leadBtns.length).toBe(1);
    (leadBtns[0] as SVGGElement).dispatchEvent(new Event('click', { bubbles: true }));
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    // Bravo is now the lead, so it is the one that exposes no set-lead control.
    expect(host.querySelectorAll('.agc-node-lead-btn').length).toBe(1);
    // The lead node now reads Bravo.
    const leadNode = host.querySelector('.agc-node--lead');
    expect(leadNode?.textContent).toContain('Bravo');

    app.unmount();
    host.remove();
  });

  it('overlays a run: selecting a run colours the agent nodes by status', async () => {
    agents = [{ slug: 'impl', title: 'Implementation', builtin: true }, { slug: 'test', title: 'Testing', builtin: true }];
    flows = [
      {
        slug: 'runnable', name: 'Runnable', builtin: false,
        agentic: true, dynamic: true, topology: 'orchestrator-worker',
        steps: [
          { agent_slug: 'impl', agent_name: 'Implementation' },
          { agent_slug: 'test', agent_name: 'Testing' },
        ],
      },
    ];
    tasks = [
      { id: 'task-1', title: 'A run', status: 'done', created_at: '2026-06-28T10:00:00Z', flow_id: 'runnable', lineage: '{...}' },
      { id: 'task-x', title: 'Other', status: 'done', created_at: '2026-06-28T09:00:00Z', flow_id: 'other', lineage: '{...}' },
    ];
    lineages['task-1'] = {
      nodes: [
        { id: 'n1', name: 'impl', role: 'Implementation', status: 'done' },
        { id: 'n2', name: 'test', role: 'Testing', status: 'failed' },
      ],
      edges: [],
    };

    const { app, host } = await mount();

    // The run picker lists only this fleet's run (task-x is a different flow).
    const sel = host.querySelector('select[aria-label="Run overlay"]') as HTMLSelectElement;
    expect(sel).toBeTruthy();
    const opts = Array.from(sel.querySelectorAll('option')).map((o) => o.value);
    expect(opts).toContain('task-1');
    expect(opts).not.toContain('task-x');

    // No overlay until a run is chosen.
    expect(host.querySelectorAll('[class*="agc-node--run-"]').length).toBe(0);

    sel.value = 'task-1';
    sel.dispatchEvent(new Event('change', { bubbles: true }));
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    // impl -> done, test -> failed (matched by lineage node name == agent slug).
    expect(host.querySelectorAll('.agc-node--run-done').length).toBe(1);
    expect(host.querySelectorAll('.agc-node--run-failed').length).toBe(1);

    app.unmount();
    host.remove();
  });

  it('deletes a user fleet after an inline confirm; built-ins have no delete', async () => {
    agents = [{ slug: 'a', title: 'Alpha', builtin: true }];
    flows = [
      { slug: 'builtin-one', name: 'Builtin', builtin: true, steps: [{ agent_slug: 'a', agent_name: 'Alpha' }] },
      { slug: 'mine', name: 'Mine', builtin: false, steps: [{ agent_slug: 'a', agent_name: 'Alpha' }] },
    ];

    const { app, host } = await mount();
    // Built-in (auto-selected first) exposes no Delete control.
    expect(Array.from(host.querySelectorAll('button')).some((b) => b.textContent === 'Delete')).toBe(false);

    // Switch to the user fleet.
    const picker = host.querySelector('select.ag-mode__flow-select') as HTMLSelectElement;
    picker.value = 'mine';
    picker.dispatchEvent(new Event('change', { bubbles: true }));
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    const delBtn = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Delete') as HTMLButtonElement;
    expect(delBtn).toBeTruthy();
    delBtn.click(); // first click: arm the confirm
    for (let i = 0; i < 4; i++) await new Promise((r) => setTimeout(r, 0));
    const confirm = Array.from(host.querySelectorAll('button')).find((b) => b.textContent === 'Confirm') as HTMLButtonElement;
    expect(confirm).toBeTruthy();
    confirm.click();
    for (let i = 0; i < 10; i++) await new Promise((r) => setTimeout(r, 0));

    // The fleet is gone from the registry; the picker no longer lists it.
    const opts = Array.from((host.querySelector('select.ag-mode__flow-select') as HTMLSelectElement).querySelectorAll('option')).map((o) => o.value);
    expect(opts).not.toContain('mine');

    app.unmount();
    host.remove();
  });

  it('switches coordination from the editor control', async () => {
    agents = [{ slug: 'a', title: 'Alpha', builtin: true }, { slug: 'b', title: 'Bravo', builtin: true }];
    flows = [
      {
        slug: 'pair2', name: 'Pair2', builtin: false,
        agentic: true, dynamic: true, topology: 'orchestrator-worker',
        steps: [
          { agent_slug: 'a', agent_name: 'Alpha' },
          { agent_slug: 'b', agent_name: 'Bravo' },
        ],
      },
    ];

    const { app, host } = await mount();
    (host.querySelector('.ag-detail__edit') as HTMLButtonElement).click();
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
    expect((host.textContent ?? '')).toContain('Lead delegates');

    const sel = host.querySelector('select[aria-label="Coordination"]') as HTMLSelectElement;
    sel.value = 'mesh';
    sel.dispatchEvent(new Event('change', { bubbles: true }));
    for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
    expect((host.textContent ?? '')).toContain('Open mesh');

    app.unmount();
    host.remove();
  });

  it('starts a brand-new empty fleet from the New fleet action', async () => {
    agents = [{ slug: 'impl', title: 'Implementation', builtin: true }];
    flows = []; // no fleets exist yet

    const { app, host } = await mount();

    // With no fleets, the detail prompts to start one.
    expect((host.textContent ?? '')).toContain('start a New fleet');

    const newBtn = Array.from(host.querySelectorAll('button')).find(
      (b) => b.textContent?.trim() === '+ New fleet',
    ) as HTMLButtonElement;
    expect(newBtn).toBeTruthy();
    newBtn.click();
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    // The editor opens on an empty draft: edit toolbar present, no agent nodes.
    expect(host.querySelector('.ag-edit')).toBeTruthy();
    expect(host.querySelectorAll('.agc-node--agent').length).toBe(0);

    // Saving an empty fleet is rejected until an agent is added.
    (host.querySelector('.ag-edit__btn--save') as HTMLButtonElement).click();
    for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
    expect((host.textContent ?? '')).toContain('Add at least one agent');

    app.unmount();
    host.remove();
  });
});
