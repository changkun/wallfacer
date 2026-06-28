// AgentLineage fetches GET /api/tasks/{id}/lineage and renders the agent-graph:
// each node as a labeled box (name + role + status) and each edge labeled by
// kind. The component is self-contained, so the test mocks fetch (the transport
// behind api()) and asserts the rendered output.

import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { createApp, type App } from 'vue';
import AgentLineage from './AgentLineage.vue';
import type { TaskLineage } from '../api/types';

let originalFetch: typeof globalThis.fetch;
let lineage: TaskLineage | null;

beforeEach(() => {
  lineage = null;
  originalFetch = globalThis.fetch;
  globalThis.fetch = vi.fn(async (): Promise<Response> => {
    return new Response(JSON.stringify(lineage ?? { nodes: [], edges: [] }), { status: 200 });
  }) as unknown as typeof globalThis.fetch;
});

afterEach(() => {
  globalThis.fetch = originalFetch;
});

async function mount(): Promise<{ app: App; host: HTMLElement }> {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(AgentLineage, { taskId: 't1' });
  app.mount(host);
  // Let the onMounted fetch + reactive updates settle.
  for (let i = 0; i < 6; i++) await new Promise((r) => setTimeout(r, 0));
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
    expect(text).toContain('planner');
    expect(text).toContain('Planner');
    expect(text).toContain('Builder');
    expect(text).toContain('done');
    expect(text).toContain('running');
    expect(text).toContain('next');
    // Two node boxes and one edge row.
    expect(host.querySelectorAll('.lineage__node').length).toBe(2);
    expect(host.querySelectorAll('.lineage__edge').length).toBe(1);
    app.unmount();
    host.remove();
  });

  it('shows an empty-state note when there is no lineage', async () => {
    lineage = { nodes: [], edges: [] };
    const { app, host } = await mount();
    expect(host.textContent ?? '').toContain('No lineage recorded for this run.');
    expect(host.querySelectorAll('.lineage__node').length).toBe(0);
    app.unmount();
    host.remove();
  });
});
