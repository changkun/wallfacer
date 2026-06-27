// Parser for the backend's normalized transcript stream
// (GET /api/tasks/{id}/logs?format=normalized). The backend runs each harness's
// native NDJSON through harness.ParseEvent and emits a stable, harness-agnostic
// event DTO; this turns that event stream into the same ActivityRow trajectory +
// trailing answer that the Claude path produces via prettyNdjson.parseTurn, so
// the renderer is shared across every harness.

import type { ActivityRow } from './prettyNdjson';

// NormalizedEvent mirrors handler.normalizedEvent (Go). `kind` is the wire token
// from harness.EventKind.String().
export interface NormalizedEvent {
  kind: string;
  text?: string;
  subtype?: string;
  tool?: {
    id?: string;
    name?: string;
    input?: unknown;
    output?: unknown;
    error?: string;
  };
  usage?: {
    input_tokens?: number;
    output_tokens?: number;
    cache_read_tokens?: number;
    cache_creation_tokens?: number;
    cost_usd?: number;
  };
  stop_reason?: string;
  session_id?: string;
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

function pretty(v: unknown): string {
  if (v == null) return '';
  if (typeof v === 'string') return v;
  try {
    return JSON.stringify(v, null, 2);
  } catch {
    return String(v);
  }
}

// toolSummary pulls a one-line hint from a harness-shaped tool input without a
// per-harness summariser (v1 generic): it walks the object shallowly for the
// common "what is this call doing" keys. Returns '' when nothing obvious is
// present, leaving the row to rely on its expandable detail.
function toolSummary(input: unknown): string {
  if (input == null) return '';
  if (typeof input === 'string') return truncate(input);
  if (typeof input !== 'object') return '';
  const obj = input as Record<string, unknown>;
  for (const k of ['command', 'cmd', 'query', 'pattern', 'description']) {
    if (typeof obj[k] === 'string' && obj[k]) return truncate(obj[k] as string);
  }
  for (const k of ['file_path', 'filePath', 'path']) {
    if (typeof obj[k] === 'string' && obj[k]) return basename(obj[k] as string);
  }
  return '';
}

// NormalizedTurn folds a normalized event stream into a renderable turn.
export interface NormalizedTurn {
  rows: ActivityRow[];
  answer: string;
  usage: NormalizedEvent['usage'] | null;
}

// createNormalizedParser incrementally parses the normalized NDJSON stream so a
// long, live stream is parsed one line at a time (not re-parsed per chunk).
export interface NormalizedParser {
  push(chunk: string): void;
  finalize(): void;
  rows(): ActivityRow[];
  answer(): string;
  usage(): NormalizedEvent['usage'] | null;
}

export function createNormalizedParser(): NormalizedParser {
  const rows: ActivityRow[] = [];
  const toolRowByID = new Map<string, number>();
  let pending = ''; // trailing assistant narration — the answer
  let resultText = ''; // answer carried on a result event (fallback)
  let usage: NormalizedEvent['usage'] | null = null;
  let buf = '';

  const flushPending = () => {
    const t = pending.trim();
    if (t) rows.push({ kind: 'text', label: 'note', summary: t });
    pending = '';
  };

  const pushTool = (tool: NonNullable<NormalizedEvent['tool']>) => {
    flushPending();
    const detail = pretty(tool.input);
    rows.push({
      kind: 'tool',
      label: tool.name || 'tool',
      summary: toolSummary(tool.input) || undefined,
      detail: detail || undefined,
    });
    if (tool.id) toolRowByID.set(tool.id, rows.length - 1);
  };

  const pushToolError = (tool: NonNullable<NormalizedEvent['tool']>) => {
    flushPending();
    rows.push({
      kind: 'tool_result',
      label: 'error',
      summary: truncate(tool.error || 'tool failed'),
      detail: (tool.error && tool.error.length > MAX_SUMMARY) ? tool.error : undefined,
      defaultOpen: true,
    });
  };

  const consume = (line: string) => {
    const trimmed = line.trim();
    if (!trimmed || trimmed[0] !== '{') return;
    let evt: NormalizedEvent;
    try {
      evt = JSON.parse(trimmed) as NormalizedEvent;
    } catch {
      return;
    }
    switch (evt.kind) {
      case 'assistant':
        pending += evt.text ?? '';
        break;
      case 'thinking': {
        const text = (evt.text ?? '').trim();
        if (text) {
          flushPending();
          rows.push({
            kind: 'thinking',
            label: 'Thinking',
            summary: truncate(text),
            detail: text.length > MAX_SUMMARY ? text : undefined,
          });
        }
        break;
      }
      case 'tool_start':
        if (evt.tool) pushTool(evt.tool);
        break;
      case 'tool_end':
        if (evt.tool) {
          // Pair with an earlier tool_start when the harness emits both; else
          // (opencode / codex emit only the end) render the end as the row.
          if (evt.tool.id && toolRowByID.has(evt.tool.id)) {
            if (evt.tool.error) pushToolError(evt.tool);
          } else if (evt.tool.error) {
            pushTool(evt.tool);
            pushToolError(evt.tool);
          } else {
            pushTool(evt.tool);
          }
        }
        break;
      case 'error':
        flushPending();
        rows.push({
          kind: 'system',
          label: 'error',
          summary: truncate(evt.text || evt.subtype || 'error'),
          detail: evt.text && evt.text.length > MAX_SUMMARY ? evt.text : undefined,
          defaultOpen: true,
        });
        break;
      case 'result':
        if (evt.text) resultText = evt.text;
        if (evt.usage) usage = evt.usage;
        break;
      // system_init / user_result / unknown: nothing to render.
    }
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
      return rows;
    },
    answer() {
      return pending.trim() || resultText.trim();
    },
    usage() {
      return usage;
    },
  };
}

// parseNormalized is the one-shot equivalent over a full buffer (tests / SSR).
export function parseNormalized(raw: string): NormalizedTurn {
  const p = createNormalizedParser();
  p.push(raw);
  p.finalize();
  return { rows: p.rows(), answer: p.answer(), usage: p.usage() };
}
