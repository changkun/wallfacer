// Smoke test: GraphCanvas renders a node per graph node, a path per edge, and
// emits `select` with the node id on click. happy-dom does no layout, so this
// asserts structure and wiring, not pixels (the visual claims rest on the
// ui-shots harness).

import { describe, it, expect } from 'vitest';
import { createApp, h, nextTick } from 'vue';
import GraphCanvas from './GraphCanvas.vue';
import type { Graph } from '../../api/types';

const graph: Graph = {
  nodes: [
    { id: 'spec:a', kind: 'spec', label: 'Spec A', status: 'validated', ref: 'a', depth: 0, available_actions: ['dispatch'] },
    { id: 'task:b', kind: 'task', label: 'Task B', status: 'backlog', ref: 'b', depth: 0, available_actions: ['start'] },
  ],
  edges: [{ from: 'spec:a', to: 'task:b', kind: 'dispatch' }],
  critical_path: ['spec:a', 'task:b'],
  blocked: [],
};

function mount(onSelect: (id: string) => void) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp({
    render: () => h(GraphCanvas, { graph, onSelect }),
  });
  app.mount(host);
  return { app, host };
}

describe('GraphCanvas', () => {
  it('renders one node group and one edge path', async () => {
    const { app, host } = mount(() => {});
    await nextTick();
    expect(host.querySelectorAll('.gc-node').length).toBe(2);
    expect(host.querySelectorAll('.gc-edge').length).toBe(1);
    app.unmount();
  });

  it('marks edges on the critical path', async () => {
    const { app, host } = mount(() => {});
    await nextTick();
    expect(host.querySelector('.gc-edge--critical')).not.toBeNull();
    app.unmount();
  });

  it('emits select with the node id on click', async () => {
    const selected: string[] = [];
    const { app, host } = mount((id) => selected.push(id));
    await nextTick();
    const node = host.querySelector('.gc-node') as SVGGElement;
    node.dispatchEvent(new Event('click', { bubbles: true }));
    await nextTick();
    expect(selected).toContain('spec:a');
    app.unmount();
  });
});
