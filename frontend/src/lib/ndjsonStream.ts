// Incremental NDJSON stream parser for the planning chat.
//
// The one-shot extractAssistantText / extractError / parseActivity / hasActivity
// helpers each re-split and re-JSON.parse the WHOLE accumulated buffer. Calling
// all four on every network chunk is O(n^2) in frames over a streaming turn.
//
// This parser consumes the stream incrementally: it buffers a partial trailing
// line across chunks and parses each NDJSON frame exactly once, accumulating the
// same derived state (assistant text, activity rows, last error, hasActivity).
// It reuses the exact per-frame helpers used by the one-shot functions, so the
// final state is identical to running those functions over the full buffer.

import { frameError } from './planningBubble';
import {
  parseFrameLine,
  accumulateFrame,
  type ActivityRow,
  type TurnAccumulator,
} from './prettyNdjson';

export interface NdjsonStreamState {
  /** Trailing assistant narration — the answer (narration before a step is
   *  folded into the activity rows instead). */
  text: string;
  /** Trajectory rows (steps + interleaved narration) in arrival order. */
  activity: ActivityRow[];
  /** Most recent error result, or '' if none. */
  errorText: string;
  /** True once any step (and thus any trajectory) exists. */
  hasActivity: boolean;
}

export interface NdjsonStreamParser {
  /** Feed a network chunk; parses only newly completed lines. */
  push(chunk: string): void;
  /** Parse any buffered trailing line (call on stream end). */
  finalize(): void;
  /** Snapshot of the accumulated derived state. */
  state(): NdjsonStreamState;
}

export function createNdjsonStreamParser(): NdjsonStreamParser {
  let lineBuf = ''; // bytes after the last newline, not yet a complete line
  const acc: TurnAccumulator = { rows: [], pending: '' };
  let errorText = '';

  function consumeLine(line: string): void {
    const frame = parseFrameLine(line);
    if (!frame) return;
    accumulateFrame(frame, acc);
    const err = frameError(frame);
    if (err) errorText = err; // last error wins
  }

  return {
    push(chunk: string) {
      lineBuf += chunk;
      let nl = lineBuf.indexOf('\n');
      while (nl !== -1) {
        consumeLine(lineBuf.slice(0, nl));
        lineBuf = lineBuf.slice(nl + 1);
        nl = lineBuf.indexOf('\n');
      }
    },
    finalize() {
      if (lineBuf.length > 0) {
        consumeLine(lineBuf);
        lineBuf = '';
      }
    },
    state(): NdjsonStreamState {
      return {
        text: acc.pending.trim(),
        activity: acc.rows,
        errorText,
        hasActivity: acc.rows.length > 0,
      };
    },
  };
}
