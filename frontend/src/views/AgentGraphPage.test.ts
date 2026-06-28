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

describe('AgentGraphPage', () => {
  it('lists the palette agents and renders one node per step with order edges', async () => {
    agents = [
      { slug: 'planner', title: 'Planner', description: 'Plans the work', builtin: true },
      { slug: 'builder', title: 'Builder', description: 'Writes the code', builtin: true },
    ];
    flows = [
      {
        slug: 'plan-build',
        name: 'Plan then Build',
        builtin: true,
        agentic: true,
        dynamic: true,
        topology: 'mesh',
        steps: [
          { agent_slug: 'planner', agent_name: 'Planner' },
          { agent_slug: 'builder', agent_name: 'Builder' },
        ],
      },
    ];

    const { app, host } = await mount();
    const text = host.textContent ?? '';

    // Palette lists both agents.
    expect(text).toContain('Planner');
    expect(text).toContain('Builder');
    expect(text).toContain('Plans the work');
    expect(host.querySelectorAll('.ag-card').length).toBe(2);

    // Canvas: one node per step.
    expect(host.querySelectorAll('.agc-node').length).toBe(2);

    // Order edges: Task -> Planner, Planner -> Builder (a linear two-step flow).
    expect(host.querySelectorAll('.agc-edge').length).toBe(2);

    // Topology indicator reflects the agentic + dynamic + mesh flow.
    expect(text.toLowerCase()).toContain('mesh');

    app.unmount();
    host.remove();
  });

  it('clones a built-in into an editable draft and offers a save action', async () => {
    agents = [
      { slug: 'impl', title: 'Implementation', builtin: true },
      { slug: 'test', title: 'Testing', builtin: true },
    ];
    flows = [
      {
        slug: 'implement',
        name: 'Implement',
        builtin: true,
        steps: [{ agent_slug: 'impl', agent_name: 'Implementation' }],
      },
    ];

    const { app, host } = await mount();
    // Built-in is read-only: the action reads "Clone & edit".
    const editBtn = host.querySelector('.ag-detail__edit') as HTMLButtonElement;
    expect(editBtn).toBeTruthy();
    expect(editBtn.textContent).toContain('Clone & edit');

    editBtn.click();
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    // The editor toolbar appears with a save action and a clone-of hint.
    expect(host.querySelector('.ag-edit')).toBeTruthy();
    expect(host.querySelector('.ag-edit__btn--save')).toBeTruthy();
    expect((host.textContent ?? '').toLowerCase()).toContain('clone of implement');
    // Palette cards become draggable in edit mode.
    expect((host.querySelector('.ag-card') as HTMLElement).getAttribute('draggable')).toBe('true');

    app.unmount();
    host.remove();
  });

  it('removes a step when its canvas remove control is activated', async () => {
    agents = [
      { slug: 'impl', title: 'Implementation', builtin: true },
      { slug: 'test', title: 'Testing', builtin: true },
    ];
    flows = [
      {
        slug: 'duo',
        name: 'Duo',
        builtin: false, // user flow: edits in place, no clone naming needed
        steps: [
          { agent_slug: 'impl', agent_name: 'Implementation' },
          { agent_slug: 'test', agent_name: 'Testing' },
        ],
      },
    ];

    const { app, host } = await mount();
    expect(host.querySelectorAll('.agc-node').length).toBe(2);

    // Enter edit mode; remove controls render per node.
    (host.querySelector('.ag-detail__edit') as HTMLButtonElement).click();
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
    const removers = host.querySelectorAll('.agc-node-remove');
    expect(removers.length).toBe(2);

    // Activating one drops the node count to one.
    (removers[0] as SVGGElement).dispatchEvent(new Event('click', { bubbles: true }));
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
    expect(host.querySelectorAll('.agc-node').length).toBe(1);

    app.unmount();
    host.remove();
  });

  it('drives the topology indicator from the agentic toolbar controls', async () => {
    agents = [{ slug: 'impl', title: 'Implementation', builtin: true }];
    flows = [
      {
        slug: 'duo',
        name: 'Duo',
        builtin: false,
        steps: [{ agent_slug: 'impl', agent_name: 'Implementation' }],
      },
    ];

    const { app, host } = await mount();
    // A non-agentic flow renders the pinned-chain indicator.
    expect((host.textContent ?? '').toLowerCase()).toContain('pinned');

    (host.querySelector('.ag-detail__edit') as HTMLButtonElement).click();
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    function setCheckbox(label: string, value: boolean) {
      const el = host.querySelector(`input[aria-label="${label}"]`) as HTMLInputElement;
      el.checked = value;
      el.dispatchEvent(new Event('change', { bubbles: true }));
    }
    setCheckbox('Agentic', true);
    for (let i = 0; i < 4; i++) await new Promise((r) => setTimeout(r, 0));
    setCheckbox('Dynamic', true);
    for (let i = 0; i < 4; i++) await new Promise((r) => setTimeout(r, 0));
    const sel = host.querySelector('select[aria-label="Topology"]') as HTMLSelectElement;
    sel.value = 'mesh';
    sel.dispatchEvent(new Event('change', { bubbles: true }));
    for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));

    // The canvas indicator now reflects the dynamic mesh topology live.
    const t = (host.textContent ?? '').toLowerCase();
    expect(t).toContain('mesh');
    expect(t).not.toContain('pinned chain');

    app.unmount();
    host.remove();
  });

  it('ungroups a parallel stage when a node ungroup control is activated', async () => {
    agents = [
      { slug: 'a', title: 'A', builtin: true },
      { slug: 'b', title: 'B', builtin: true },
      { slug: 'c', title: 'C', builtin: true },
    ];
    flows = [
      {
        slug: 'fan',
        name: 'Fan',
        builtin: false,
        steps: [
          { agent_slug: 'a', agent_name: 'A', run_in_parallel_with: ['b'] },
          { agent_slug: 'b', agent_name: 'B', run_in_parallel_with: ['a'] },
          { agent_slug: 'c', agent_name: 'C' },
        ],
      },
    ];

    const { app, host } = await mount();
    // One parallel stage to start (a || b), so one "parallel" badge.
    expect(host.querySelectorAll('.agc-badge').length).toBe(1);

    (host.querySelector('.ag-detail__edit') as HTMLButtonElement).click();
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    // The two parallel nodes expose an ungroup control; the sequential one does not.
    const ungroupers = host.querySelectorAll('.agc-node-ungroup');
    expect(ungroupers.length).toBe(2);

    (ungroupers[0] as SVGGElement).dispatchEvent(new Event('click', { bubbles: true }));
    for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));

    // Group dissolved: no parallel badge, no ungroup controls remain.
    expect(host.querySelectorAll('.agc-badge').length).toBe(0);
    expect(host.querySelectorAll('.agc-node-ungroup').length).toBe(0);

    app.unmount();
    host.remove();
  });

  it('draws parallel steps as siblings in one stage', async () => {
    agents = [
      { slug: 'a', title: 'Agent A', builtin: true },
      { slug: 'b', title: 'Agent B', builtin: true },
      { slug: 'c', title: 'Agent C', builtin: true },
    ];
    flows = [
      {
        slug: 'fan',
        name: 'Fan out then join',
        builtin: true,
        steps: [
          { agent_slug: 'a', agent_name: 'Agent A', run_in_parallel_with: ['b'] },
          { agent_slug: 'b', agent_name: 'Agent B', run_in_parallel_with: ['a'] },
          { agent_slug: 'c', agent_name: 'Agent C' },
        ],
      },
    ];

    const { app, host } = await mount();

    // Three step nodes total: two parallel siblings plus the join.
    expect(host.querySelectorAll('.agc-node').length).toBe(3);
    // Edges: Task->A, Task->B (entry into the parallel stage), then A->C, B->C.
    expect(host.querySelectorAll('.agc-edge').length).toBe(4);
    // The concurrent stage is labelled so the siblings read as a parallel group.
    expect(host.querySelectorAll('.agc-badge').length).toBe(1);
    expect((host.textContent ?? '').toLowerCase()).toContain('parallel');
    // A pinned (non-agentic) flow shows the pinned-chain indicator.
    expect((host.textContent ?? '').toLowerCase()).toContain('pinned');

    app.unmount();
    host.remove();
  });
});
