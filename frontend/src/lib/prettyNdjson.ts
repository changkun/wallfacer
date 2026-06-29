// Pretty rendering of Claude Code's NDJSON output stream. Mirrors the
// shape of ui/js/markdown.js's renderPrettyLogs in a structured form so
// the chat panel can render rich activity rows instead of a raw text dump.

export type ActivityKind = 'tool' | 'tool_result' | 'thinking' | 'system' | 'text';

export interface ActivityRow {
  kind: ActivityKind;
  /** Short label (tool name, "thinking", "Error"). */
  label: string;
  /** One-line summary of inputs / first line of body. */
  summary?: string;
  /** A compact, secondary preview of the raw call (e.g. the bash command or
   *  full file path) shown under the friendly title — helps skim what's
   *  actually running without expanding the full detail. */
  preview?: string;
  /** Optional full body, only set when summary alone is truncated. */
  detail?: string;
  /** Expander label, e.g. "+12 lines" (used by the task-detail activity stream). */
  detailLabel?: string;
  /** True for entries that should default to expanded (errors). */
  defaultOpen?: boolean;
}

interface ContentBlock {
  type?: string;
  name?: string;
  input?: Record<string, unknown>;
  text?: string;
  thinking?: string;
  content?: string | { text?: string }[];
  is_error?: boolean;
  tool_use_id?: string;
}

export interface Frame {
  type?: string;
  /** Session-primary model, carried top-level on the system/init line
   *  (e.g. "claude-opus-4-8[1m]"). */
  model?: string;
  message?: { model?: string; content?: ContentBlock[] };
  is_error?: boolean;
  result?: string;
}

// frameModel returns the model a frame reports, or '' when it reports none.
// The system/init line carries the session-primary model top-level; an
// assistant line carries the per-turn model nested under message.model.
export function frameModel(frame: Frame): string {
  if (frame.type === 'system' && frame.model) return frame.model;
  if (frame.type === 'assistant' && frame.message?.model) return frame.message.model;
  return '';
}

const MAX_SUMMARY = 220;

function truncate(s: string, n = MAX_SUMMARY): string {
  if (!s) return '';
  const oneline = s.replace(/\s+/g, ' ').trim();
  return oneline.length > n ? oneline.slice(0, n) + '…' : oneline;
}

function basename(p: string): string {
  const cleaned = p.replace(/\/+$/, '');
  const i = cleaned.lastIndexOf('/');
  return i >= 0 ? cleaned.slice(i + 1) : cleaned;
}

// A short, human-readable title for a tool call — Codex-style ("Open deck
// preview", "auth-by-default.md") rather than a raw "key: value" dump. Prefers
// the agent's own description, then a file's name, then the command or first
// signal. No "key:" prefix: the tool name already labels the step.
function summariseToolInput(input: Record<string, unknown> | undefined): string {
  if (!input) return '';
  if (typeof input.description === 'string' && input.description.trim()) {
    return truncate(input.description);
  }
  for (const k of ['file_path', 'path']) {
    if (typeof input[k] === 'string') return basename(String(input[k]));
  }
  for (const k of ['command', 'pattern', 'query', 'url']) {
    if (typeof input[k] === 'string') return truncate(String(input[k]));
  }
  for (const [, v] of Object.entries(input)) {
    if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') {
      return truncate(String(v));
    }
  }
  return truncate(JSON.stringify(input));
}

const PREVIEW_MAX = 120;

// toolPreview returns a compact view of the raw call to show under the friendly
// title — the bash command (when a description already titles the step) or a
// file's full path (when the title is just its basename). Returns undefined
// when the title already conveys the raw input (e.g. a bare command or pattern),
// to avoid a redundant duplicate line.
function toolPreview(input: Record<string, unknown> | undefined): string | undefined {
  if (!input) return undefined;
  const hasDescription = typeof input.description === 'string' && input.description.trim() !== '';
  if (hasDescription && typeof input.command === 'string') {
    return truncate(input.command, PREVIEW_MAX);
  }
  for (const k of ['file_path', 'path']) {
    if (typeof input[k] === 'string') {
      const full = String(input[k]);
      return full.includes('/') ? truncate(full, PREVIEW_MAX) : undefined;
    }
  }
  return undefined;
}

function toolResultText(block: ContentBlock): string {
  if (typeof block.content === 'string') return block.content;
  if (Array.isArray(block.content)) {
    return block.content.map(c => c.text ?? '').filter(Boolean).join('');
  }
  return '';
}

// frameActivityRows returns the activity rows contributed by a single parsed
// NDJSON frame. Shared by the one-shot parseActivity and the incremental
// stream parser so both produce byte-identical rows in identical order.
export function frameActivityRows(frame: Frame): ActivityRow[] {
  const out: ActivityRow[] = [];
  const blocks = frame.message?.content ?? [];
  if (frame.type === 'assistant') {
    for (const block of blocks) {
      if (block.type === 'tool_use') {
        out.push({
          kind: 'tool',
          label: block.name ?? 'tool',
          summary: summariseToolInput(block.input),
          preview: toolPreview(block.input),
          detail: block.input ? JSON.stringify(block.input, null, 2) : undefined,
        });
      } else if (block.type === 'thinking') {
        const text = block.thinking ?? '';
        if (text.trim()) {
          out.push({
            kind: 'thinking',
            label: 'Thinking',
            summary: truncate(text),
            detail: text.length > MAX_SUMMARY ? text : undefined,
          });
        }
      }
      // `text` blocks are the assistant's answer prose — rendered below the
      // trajectory, not as activity. Skipping them here avoids the narration
      // showing twice (once as a step, once in the answer).
    }
  } else if (frame.type === 'result') {
    // A successful result just repeats the answer; only surface errors.
    const text = frame.result ?? '';
    if (frame.is_error && text.trim()) {
      out.push({
        kind: 'system',
        label: 'error',
        summary: truncate(text),
        detail: text.length > MAX_SUMMARY ? text : undefined,
        defaultOpen: true,
      });
    }
  } else if (frame.type === 'user') {
    for (const block of blocks) {
      // Successful tool output is noise the agent's prose already summarises;
      // surface only failures.
      if (block.type === 'tool_result' && block.is_error) {
        const text = toolResultText(block);
        out.push({
          kind: 'tool_result',
          label: 'error',
          summary: truncate(text),
          detail: text.length > MAX_SUMMARY ? text : undefined,
          defaultOpen: true,
        });
      }
    }
  }
  return out;
}

// ── Turn timeline (interleaved narration + steps) ──────────────────────
//
// frameActivityRows deliberately drops assistant `text` blocks (they are the
// answer). But narration emitted *between* tool calls ("Let me check the
// specs…") is part of the working trajectory, not the conclusion — rendering it
// all as the trailing answer puts a preamble at the bottom, out of order.
//
// The accumulator below walks frames in order and flushes any pending narration
// into a `text` row right before the next step, so intermediate narration lands
// in the trajectory in chronological position. Whatever narration trails the
// final step (with no step after it) is the answer.

export interface TurnAccumulator {
  rows: ActivityRow[];
  /** Narration seen since the last flushed step — becomes the answer if no
   *  further step arrives. */
  pending: string;
}

function flushPending(acc: TurnAccumulator): void {
  const t = acc.pending.trim();
  if (t) acc.rows.push({ kind: 'text', label: 'note', summary: t });
  acc.pending = '';
}

// accumulateFrame folds one parsed frame into the turn timeline. Shared by the
// one-shot parseTurn and the incremental stream parser so both interleave
// identically.
export function accumulateFrame(frame: Frame, acc: TurnAccumulator): void {
  if (frame.type === 'assistant') {
    for (const block of frame.message?.content ?? []) {
      if (block.type === 'text') {
        acc.pending += block.text ?? '';
      } else if (block.type === 'thinking') {
        const text = block.thinking ?? '';
        if (text.trim()) {
          flushPending(acc);
          acc.rows.push({
            kind: 'thinking',
            label: 'Thinking',
            summary: truncate(text),
            detail: text.length > MAX_SUMMARY ? text : undefined,
          });
        }
      } else if (block.type === 'tool_use') {
        flushPending(acc);
        acc.rows.push({
          kind: 'tool',
          label: block.name ?? 'tool',
          summary: summariseToolInput(block.input),
          preview: toolPreview(block.input),
          detail: block.input ? JSON.stringify(block.input, null, 2) : undefined,
        });
      }
    }
  } else if (frame.type === 'result') {
    if (frame.is_error && frame.result?.trim()) {
      flushPending(acc);
      acc.rows.push({
        kind: 'system',
        label: 'error',
        summary: truncate(frame.result),
        detail: frame.result.length > MAX_SUMMARY ? frame.result : undefined,
        defaultOpen: true,
      });
    }
  } else if (frame.type === 'user') {
    for (const block of frame.message?.content ?? []) {
      if (block.type === 'tool_result' && block.is_error) {
        const text = toolResultText(block);
        flushPending(acc);
        acc.rows.push({
          kind: 'tool_result',
          label: 'error',
          summary: truncate(text),
          detail: text.length > MAX_SUMMARY ? text : undefined,
          defaultOpen: true,
        });
      }
    }
  }
}

export interface ParsedTurn {
  /** Trajectory: steps and any narration between them, in order. */
  rows: ActivityRow[];
  /** Trailing narration — the conclusion, rendered as the answer. */
  answer: string;
}

/** One-shot timeline parse over a full raw_output buffer. */
export function parseTurn(raw: string): ParsedTurn {
  const acc: TurnAccumulator = { rows: [], pending: '' };
  for (const line of raw.split('\n')) {
    const frame = parseFrameLine(line);
    if (frame) accumulateFrame(frame, acc);
  }
  return { rows: acc.rows, answer: acc.pending.trim() };
}

// parseLine trims and JSON-parses one NDJSON line into a Frame, or null when
// the line is blank, not a JSON object, or malformed. Shared by the one-shot
// parsers and the incremental stream parser so they skip identical lines.
export function parseFrameLine(line: string): Frame | null {
  const trimmed = line.trim();
  if (!trimmed || trimmed[0] !== '{') return null;
  try {
    return JSON.parse(trimmed) as Frame;
  } catch {
    return null;
  }
}

export function parseActivity(raw: string): ActivityRow[] {
  const out: ActivityRow[] = [];
  for (const line of raw.split('\n')) {
    const frame = parseFrameLine(line);
    if (frame) out.push(...frameActivityRows(frame));
  }
  return out;
}

// ActivityParser incrementally parses streamed NDJSON. push(chunk) parses only
// the newly completed lines and appends their rows; finalize() flushes a final
// newline-less line. The accumulated rows are byte-for-byte equal to calling
// parseActivity over the full buffer (plus finalize), but each frame is parsed
// once instead of re-splitting and re-JSON.parsing the WHOLE buffer per chunk,
// which is O(n^2) in frames for a long-running stream.
export interface ActivityParser {
  push(chunk: string): ActivityRow[];
  finalize(): ActivityRow[];
  rows(): ActivityRow[];
}

export function createActivityParser(): ActivityParser {
  const out: ActivityRow[] = [];
  let pending = '';
  const consume = (line: string) => {
    const frame = parseFrameLine(line);
    if (frame) out.push(...frameActivityRows(frame));
  };
  return {
    push(chunk: string): ActivityRow[] {
      pending += chunk;
      let nl = pending.indexOf('\n');
      while (nl !== -1) {
        consume(pending.slice(0, nl));
        pending = pending.slice(nl + 1);
        nl = pending.indexOf('\n');
      }
      return out;
    },
    finalize(): ActivityRow[] {
      if (pending) {
        consume(pending);
        pending = '';
      }
      return out;
    },
    rows(): ActivityRow[] {
      return out;
    },
  };
}

// TurnParser incrementally parses a Claude NDJSON stream into the full turn
// timeline (steps + interleaved narration) AND the trailing answer, mirroring
// the one-shot parseTurn but parsing each line once. Used by the task Activity
// tab's rendered view so a Claude transcript shows both its trajectory and the
// answer prose (the older createActivityParser dropped the answer entirely).
export interface TurnParser {
  push(chunk: string): void;
  finalize(): void;
  rows(): ActivityRow[];
  answer(): string;
}

export function createTurnParser(): TurnParser {
  const acc: TurnAccumulator = { rows: [], pending: '' };
  let buf = '';
  const consume = (line: string) => {
    const frame = parseFrameLine(line);
    if (frame) accumulateFrame(frame, acc);
  };
  return {
    push(chunk: string) {
      buf += chunk;
      let nl = buf.indexOf('\n');
      while (nl !== -1) {
        consume(buf.slice(0, nl));
        buf = buf.slice(nl + 1);
        nl = buf.indexOf('\n');
      }
    },
    finalize() {
      if (buf) {
        consume(buf);
        buf = '';
      }
    },
    rows() {
      return acc.rows;
    },
    answer() {
      return acc.pending.trim();
    },
  };
}
