// RAF-batched drag updates — the lag fix (bug #2).
//
// The legacy renderer rewrote every incident edge's path on *every* pointermove
// event (60-120/sec on a fast drag), so the browser couldn't keep up and the
// drag stuttered. This coalesces all moves between animation frames into a
// single flush per frame: many schedule() calls → at most one flush() per RAF
// tick, always with the latest position. The node's edges therefore re-aim once
// per painted frame, tracking the live pointer without per-event thrash.
//
// The scheduler is injected (defaults to requestAnimationFrame) so tests can
// drive frames deterministically and assert the batching, with no DOM or real
// timers involved.

import type { Point } from './layout';

export type Raf = (cb: () => void) => number;
export type FlushFn = (nodeId: string, pos: Point) => void;

export class DragBatcher {
  private pending: Point | null = null;
  private nodeId = '';
  private scheduled = false;
  private flushes = 0;

  constructor(
    private readonly flush: FlushFn,
    private readonly raf: Raf = (cb) => requestAnimationFrame(cb),
  ) {}

  // schedule records the latest position for a node and ensures exactly one
  // frame is queued. Repeated calls before the frame fires overwrite the
  // pending position rather than queueing more work.
  schedule(nodeId: string, pos: Point): void {
    this.nodeId = nodeId;
    this.pending = pos;
    if (!this.scheduled) {
      this.scheduled = true;
      this.raf(() => this.run());
    }
  }

  private run(): void {
    this.scheduled = false;
    if (this.pending) {
      this.flush(this.nodeId, this.pending);
      this.pending = null;
      this.flushes += 1;
    }
  }

  // flushCount is the number of times flush actually ran — used by tests to
  // assert that a burst of schedule() calls produced one flush per frame, not
  // one per call.
  get flushCount(): number {
    return this.flushes;
  }
}
