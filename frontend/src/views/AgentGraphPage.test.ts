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

beforeEach(() => {
  agents = [];
  flows = [];
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = typeof input === 'string' ? input : input.toString();
    let body: unknown = null;
    if (url.includes('/api/agents')) body = agents;
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
});
