// Regression tests for the two halves of the drag bug (#2):
//
//  - LAG: the legacy renderer rewrote edge paths on every pointermove event.
//    DragBatcher must coalesce a burst of moves into one flush per animation
//    frame, so a fast drag paints at most once per frame, not once per event.
//
//  - DETACHMENT: edges must be recomputed from the node's *live* position each
//    flush. edgePaths reads positions directly, so after a move the incident
//    edge's endpoint equals the node's new position — no frozen waypoint slack.

import { describe, it, expect } from 'vitest';
import { DragBatcher } from './dragController';
import { edgePaths } from './edges';
import type { Graph } from '../../api/types';
import type { Point } from './layout';

describe('DragBatcher (lag fix)', () => {
  it('flushes once per frame regardless of how many moves arrive', () => {
    // Manual frame control: queued callbacks fire only when we say so.
    let queued: (() => void) | null = null;
    const raf = (cb: () => void) => {
      queued = cb;
      return 1;
    };
    const flushed: Point[] = [];
    const batcher = new DragBatcher((_id, p) => flushed.push(p), raf);

    // A burst of 50 moves within a single frame.
    for (let i = 0; i < 50; i++) batcher.schedule('n', { x: i, y: i });
    expect(batcher.flushCount).toBe(0); // nothing painted until the frame fires
    expect(queued).not.toBeNull();

    queued!(); // one frame
    expect(batcher.flushCount).toBe(1); // 50 events → 1 flush
    expect(flushed).toEqual([{ x: 49, y: 49 }]); // latest position wins
  });

  it('re-arms for the next frame after flushing', () => {
    const frames: Array<() => void> = [];
    const raf = (cb: () => void) => frames.push(cb) as unknown as number;
    let last: Point | null = null;
    const batcher = new DragBatcher((_id, p) => (last = p), raf);

    batcher.schedule('n', { x: 1, y: 1 });
    frames.shift()!();
    expect(last).toEqual({ x: 1, y: 1 });

    batcher.schedule('n', { x: 2, y: 2 });
    expect(frames.length).toBe(1); // a fresh frame was queued
    frames.shift()!();
    expect(last).toEqual({ x: 2, y: 2 });
    expect(batcher.flushCount).toBe(2);
  });
});

describe('edgePaths (detachment fix)', () => {
  const graph: Graph = {
    nodes: [
      { id: 'a', kind: 'spec', label: 'a', status: 'drafted', ref: 'a', depth: 0 },
      { id: 'b', kind: 'task', label: 'b', status: 'backlog', ref: 'b', depth: 0 },
    ],
    edges: [{ from: 'a', to: 'b', kind: 'dispatch' }],
    critical_path: [],
    blocked: [],
  };

  it('re-aims the edge endpoint to the live node position after a move', () => {
    const pos = new Map<string, Point>([
      ['a', { x: 0, y: 0 }],
      ['b', { x: 100, y: 0 }],
    ]);
    const before = edgePaths(graph, pos)[0].d;
    expect(before.endsWith('100 0')).toBe(true);

    // Simulate a drag flush moving b.
    pos.set('b', { x: 250, y: 80 });
    const after = edgePaths(graph, pos)[0].d;
    // The edge now ends exactly at b's new position — attached, not detached.
    expect(after.endsWith('250 80')).toBe(true);
    expect(after.startsWith('M 0 0')).toBe(true); // a's endpoint also exact
  });

  it('drops edges whose endpoints are not positioned', () => {
    const pos = new Map<string, Point>([['a', { x: 0, y: 0 }]]);
    expect(edgePaths(graph, pos)).toHaveLength(0);
  });
});
