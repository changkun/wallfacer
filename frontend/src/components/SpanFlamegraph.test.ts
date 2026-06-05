// Regression test for the Timeline tab's flamegraph rendering.
//
// The bug: span bars, labels, and axis ticks were drawn as SVG <text>/<rect>
// inside an <svg viewBox="0 0 100 H" preserveAspectRatio="none" width="100%">.
// At real widths (~1900px) the horizontal scale is ~19x while vertical is 1x,
// so every <text> was smeared ~19x wide and overlapped into an unreadable mess.
//
// The fix mirrors the legacy UI: bars/labels/axis are percentage-positioned
// HTML overlays; SVG is kept only for the cost polyline, which has no text.
// happy-dom does no layout, so we can't measure the stretch directly. Instead
// we assert the new HTML structure exists (the `.flamegraph__block` class does
// not exist on the broken SVG-text implementation, so this fails pre-fix).

import { describe, it, expect } from 'vitest';
import { createApp, nextTick } from 'vue';
import SpanFlamegraph from './SpanFlamegraph.vue';
import type { SpanResult } from '../lib/flamegraph';

function mount(spans: SpanResult[]) {
  const host = document.createElement('div');
  document.body.appendChild(host);
  const app = createApp(SpanFlamegraph, { spans });
  app.mount(host);
  return { app, host };
}

describe('SpanFlamegraph', () => {
  it('renders span bars and labels as positioned HTML, not stretched SVG text', async () => {
    const spans: SpanResult[] = [
      {
        phase: 'agent_turn',
        label: 'implementation_1',
        started_at: '2026-06-05T00:00:00.000Z',
        ended_at: '2026-06-05T00:00:03.000Z',
        duration_ms: 3000,
      },
      {
        phase: 'container_run',
        label: 'implementation',
        started_at: '2026-06-05T00:00:00.000Z',
        ended_at: '2026-06-05T00:00:04.000Z',
        duration_ms: 4000,
      },
    ];
    const { app, host } = mount(spans);
    await nextTick();

    // Each span is an absolutely-positioned HTML block, not an SVG <rect>.
    const blocks = host.querySelectorAll('.flamegraph__block');
    expect(blocks.length).toBe(2);

    // Labels are plain HTML text (so they never get the non-uniform stretch).
    const labels = Array.from(host.querySelectorAll('.flamegraph__label')).map((e) => e.textContent);
    expect(labels).toContain('Impl. Turn 1');

    // No SVG <text> drives bars or axis; the only remaining SVG is the cost
    // polyline (a line, immune to horizontal stretching).
    expect(host.querySelectorAll('svg text').length).toBe(0);

    app.unmount();
  });
});
