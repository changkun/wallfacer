// Pretty rendering of Claude Code's NDJSON output stream. Mirrors the
// shape of ui/js/markdown.js's renderPrettyLogs in a structured form so
// the chat panel can render rich activity rows instead of a raw text dump.

export type ActivityKind = 'tool' | 'tool_result' | 'thinking' | 'system';

export interface ActivityRow {
  kind: ActivityKind;
  /** Short label (tool name, "thinking", "Error"). */
  label: string;
  /** One-line summary of inputs / first line of body. */
  summary?: string;
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
  message?: { content?: ContentBlock[] };
  is_error?: boolean;
  result?: string;
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

// frameHasActivity reports whether a single parsed frame contributes any
// tool/thinking/tool_result activity. Shared by the one-shot hasActivity and
// the incremental stream parser.
export function frameHasActivity(frame: Frame): boolean {
  if (frame.type === 'assistant' && frame.message?.content) {
    for (const block of frame.message.content) {
      if (block.type === 'tool_use' || block.type === 'thinking') return true;
    }
  }
  if (frame.type === 'user' && frame.message?.content) {
    for (const block of frame.message.content) {
      // Only failed tool results render (success is folded into the answer).
      if (block.type === 'tool_result' && block.is_error) return true;
    }
  }
  return false;
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

export function hasActivity(raw: string): boolean {
  for (const line of raw.split('\n')) {
    const frame = parseFrameLine(line);
    if (frame && frameHasActivity(frame)) return true;
  }
  return false;
}
