import { describe, expect, it } from 'vitest';
import { createApp, nextTick } from 'vue';
import AgentGraphCanvas from './AgentGraphCanvas.vue';

// The canvas draws nodes to the card geometry and reads its colors from the
// ramp (specs/shared/console-redesign/agent-graph.md): a 14px corner radius,
// the lead marked by class, and run states as classes the stylesheet maps to
// ok / err / run.
const flow = {
  slug: 'f', name: 'Fleet', dynamic: true, topology: 'lead',
  steps: [{ agent_slug: 'a', agent_name: 'Planner' }, { agent_slug: 'b', agent_name: 'Coder' }],
} as never;

async function mount(props: Record<string, unknown>) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(AgentGraphCanvas, props);
  app.mount(host);
  await nextTick();
  return { host, app };
}

describe('AgentGraphCanvas', () => {
  it('draws every agent node as a 14px card', async () => {
    const { host, app } = await mount({ flow, editable: false });
    const boxes = host.querySelectorAll('.agc-node--agent .agc-node-box');
    expect(boxes.length).toBe(2);
    for (const b of boxes) expect(b.getAttribute('rx')).toBe('14');
    app.unmount(); host.remove();
  });

  it('marks the lead and the run states by class', async () => {
    const { host, app } = await mount({ flow, editable: false, runStatus: { a: 'done', b: 'failed' } });
    expect(host.querySelectorAll('.agc-node--lead').length).toBe(1);
    expect(host.querySelectorAll('.agc-node--run-done').length).toBe(1);
    expect(host.querySelectorAll('.agc-node--run-failed').length).toBe(1);
    app.unmount(); host.remove();
  });
});
