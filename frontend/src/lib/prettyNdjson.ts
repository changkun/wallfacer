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
  /** Expander label, e.g. "+12 lines" for long thinking blocks. */
  detailLabel?: string;
  /** True for entries that should default to expanded (errors). */
  defaultOpen?: boolean;
}

// Thinking blocks preview the first few lines; the rest collapses behind a
// "+N lines" expander (mirrors ui/js/modal-ndjson.js).
const THINKING_PREVIEW_LINES = 5;

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

function summariseToolInput(_name: string, input: Record<string, unknown> | undefined): string {
  if (!input) return '';
  // The most common high-signal keys, in priority order. Anything beyond
  // the first match shows up in the expandable detail.
  const priority = ['file_path', 'path', 'command', 'pattern', 'query', 'description', 'url'];
  for (const k of priority) {
    if (typeof input[k] === 'string') return `${k}: ${truncate(String(input[k]))}`;
  }
  // Fall back to the first scalar entry.
  for (const [k, v] of Object.entries(input)) {
    if (typeof v === 'string' || typeof v === 'number' || typeof v === 'boolean') {
      return `${k}: ${truncate(String(v))}`;
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
          const name = block.name ?? 'tool';
          const summary = summariseToolInput(name, block.input);
          const detail = block.input ? JSON.stringify(block.input, null, 2) : undefined;
          out.push({
            kind: 'tool',
            label: name,
            summary,
            detail: summary && detail && detail.length > summary.length + 20 ? detail : undefined,
          });
        } else if (block.type === 'thinking') {
          const text = block.thinking ?? '';
          const lineCount = text.split('\n').length;
          const manyLines = lineCount > THINKING_PREVIEW_LINES;
          out.push({
            kind: 'thinking',
            label: 'thinking',
            summary: truncate(text),
            detail: manyLines || text.length > MAX_SUMMARY ? text : undefined,
            detailLabel: manyLines ? `+${lineCount - THINKING_PREVIEW_LINES} lines` : undefined,
          });
        } else if (block.type === 'text') {
          const text = block.text ?? '';
          if (text.trim()) {
            out.push({
              kind: 'system',
              label: 'text',
              summary: truncate(text),
              detail: text.length > MAX_SUMMARY ? text : undefined,
            });
          }
        }
      }
    } else if (frame.type === 'result') {
      const text = frame.result ?? '';
      if (text.trim()) {
        out.push({
          kind: 'system',
          label: frame.is_error ? 'error' : 'result',
          summary: truncate(text),
          detail: text.length > MAX_SUMMARY ? text : undefined,
          defaultOpen: !!frame.is_error,
        });
      }
  } else if (frame.type === 'user') {
    for (const block of blocks) {
      if (block.type === 'tool_result') {
        const text = toolResultText(block);
        out.push({
          kind: 'tool_result',
          label: block.is_error ? 'error' : 'result',
          summary: truncate(text),
          detail: text.length > MAX_SUMMARY ? text : undefined,
          defaultOpen: !!block.is_error,
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
      if (block.type === 'tool_result') return true;
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
