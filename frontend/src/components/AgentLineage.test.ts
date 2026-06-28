// AgentLineage renders an agentic run's agent graph (from GET
// /api/tasks/{id}/lineage) plus a live per-agent transcript (from the
// agentgraph-tagged events on GET /api/tasks/{id}/events). The component is
// self-contained, so the test mocks fetch (the transport behind api()) and
// routes by URL.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, type App } from 'vue';
import AgentLineage from './AgentLineage.vue';
import type { TaskLineage } from '../api/types';

let originalFetch: typeof globalThis.fetch;
let lineage: TaskLineage | null;
let events: unknown[];

beforeEach(() => {
  lineage = null;
  events = [];
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (input: RequestInfo | URL): Promise<Response> => {
    const url = String(input);
    if (url.includes('/lineage')) {
      return new Response(JSON.stringify(lineage ?? { nodes: [], edges: [] }), { status: 200 });
    }
    return new Response(JSON.stringify(events), { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(AgentLineage, { taskId: 't1', refreshKey: '1' });
  app.mount(host);
  for (let i = 0; i < 8; i++) await new Promise((r) => setTimeout(r, 0));
  return { app, host };
}

describe('AgentLineage', () => {
  it('renders nodes with name/role/status and edges by kind', async () => {
    lineage = {
      nodes: [
        { id: 'run-x/planner', name: 'planner', role: 'Planner', status: 'done' },
        { id: 'run-x/builder', name: 'builder', role: 'Builder', status: 'running' },
      ],
      edges: [{ from: 'run-x/planner', to: 'run-x/builder', kind: 'next' }],
    };
    const { app, host } = await mount();
    const text = host.textContent ?? '';
    expect(text).toContain('Planner');
    expect(text).toContain('Builder');
    expect(text).toContain('next');
    expect(host.querySelectorAll('.lineage__node').length).toBe(2);
    expect(host.querySelectorAll('.lineage__edge').length).toBe(1);
    app.unmount();
    host.remove();
  });

  it('renders the per-agent transcript from agentgraph trace events', async () => {
    events = [
      { id: 1, event_type: 'system', data: { source: 'agentgraph', kind: 'assistant', agent: 'planner', text: 'here is the **plan**' } },
      { id: 2, event_type: 'system', data: { source: 'agentgraph', kind: 'delegate', agent: 'builder', result: '↳ delegated to builder' } },
      { id: 3, event_type: 'system', data: { result: 'unrelated system event' } },
    ];
    const { app, host } = await mount();
    const text = host.textContent ?? '';
    expect(text).toContain('planner');
    expect(text).toContain('plan'); // markdown-rendered assistant text
    expect(text).toContain('delegated to builder');
    expect(text).not.toContain('unrelated system event'); // non-agentgraph filtered out
    // Bold markdown actually rendered (not raw).
    expect(host.querySelector('.lineage__turn-body strong')?.textContent).toBe('plan');
    // No lineage yet -> a provisional node per agent seen in the trace.
    expect(host.querySelectorAll('.lineage__node').length).toBe(2);
    app.unmount();
    host.remove();
  });

  it('renders nothing when there is no lineage and no trace', async () => {
    lineage = { nodes: [], edges: [] };
    events = [];
    const { app, host } = await mount();
    expect(host.querySelector('.lineage')).toBeNull();
    app.unmount();
    host.remove();
  });
});
