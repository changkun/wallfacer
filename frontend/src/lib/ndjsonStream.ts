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

import { frameAssistantText, frameError } from './planningBubble';
import {
  parseFrameLine,
  frameActivityRows,
  frameHasActivity,
  type ActivityRow,
} from './prettyNdjson';

export interface NdjsonStreamState {
  /** Concatenated assistant text across all frames seen so far. */
  text: string;
  /** Activity rows in arrival order. */
  activity: ActivityRow[];
  /** Most recent error result, or '' if none. */
  errorText: string;
  /** True once any tool/thinking/tool_result activity has been seen. */
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
  let pending = ''; // text after the last newline, not yet a complete line
  let text = '';
  const activity: ActivityRow[] = [];
  let errorText = '';
  let hasAct = false;

  function consumeLine(line: string): void {
    const frame = parseFrameLine(line);
    if (!frame) return;
    text += frameAssistantText(frame);
    const rows = frameActivityRows(frame);
    if (rows.length > 0) activity.push(...rows);
    if (!hasAct && frameHasActivity(frame)) hasAct = true;
    const err = frameError(frame);
    if (err) errorText = err; // last error wins
  }

  return {
    push(chunk: string) {
      pending += chunk;
      let nl = pending.indexOf('\n');
      while (nl !== -1) {
        consumeLine(pending.slice(0, nl));
        pending = pending.slice(nl + 1);
        nl = pending.indexOf('\n');
      }
    },
    finalize() {
      if (pending.length > 0) {
        consumeLine(pending);
        pending = '';
      }
    },
    state() {
      return { text, activity, errorText, hasActivity: hasAct };
    },
  };
}
