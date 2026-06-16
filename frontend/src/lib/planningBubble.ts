// Pure helpers for rendering planning-chat message bubbles. Extracted from
// PlanningChatPanel.vue so they can be unit-tested in isolation and reused
// by the streaming code path (which also needs to parse incoming NDJSON).
import { renderMarkdown } from './markdown';
import { parseActivity, parseFrameLine, type ActivityRow, type Frame } from './prettyNdjson';
import type { PlanningMessage } from '../stores/planning';

export interface RenderedBubble {
  role: 'user' | 'assistant' | 'system';
  contentHtml: string;
  rawText: string;
  rawOutput?: string;
  timestamp?: string;
  planRound: number;
  reverted: boolean;
  activity: ActivityRow[];
  hasActivity: boolean;
  isStreaming: boolean;
  errorText?: string;
  // id is a stable client-side identifier assigned to a streaming bubble so
  // its incremental updates can locate it by identity rather than by a cached
  // array index (which goes stale if the rendered list is replaced mid-stream).
  id?: string;
}

/**
 * Apply a streaming update to the bubble identified by id, mutating messages in
 * place. Returns false and mutates nothing when no bubble with that id exists —
 * which happens when the active thread changed mid-stream and the rendered list
 * was replaced (loadHistory). Dropping the update then is correct: applying it
 * at a stale index would append a foreign bubble or overwrite an unrelated one.
 */
export function applyStreamingUpdate(
  messages: RenderedBubble[],
  id: string,
  patch: Partial<RenderedBubble>,
): boolean {
  const i = messages.findIndex((b) => b.id === id);
  if (i === -1) return false;
  messages.splice(i, 1, { ...messages[i], ...patch });
  return true;
}

export function timeOf(ts?: string): string {
  if (!ts) return '';
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

/** Assistant text contributed by a single parsed frame (empty if none). */
export function frameAssistantText(frame: Frame): string {
  let text = '';
  if (frame.type === 'assistant' && frame.message?.content) {
    for (const block of frame.message.content) {
      if (block.type === 'text' && typeof block.text === 'string') {
        text += block.text;
      }
    }
  }
  return text;
}

/** The error string a single frame reports, or '' if it is not an error result. */
export function frameError(frame: Frame): string {
  if (frame.type === 'result' && frame.is_error && frame.result) return String(frame.result);
  return '';
}

/** Concatenate all `assistant` text blocks from a raw NDJSON stream. */
export function extractAssistantText(raw: string): string {
  let text = '';
  for (const line of raw.split('\n')) {
    const frame = parseFrameLine(line);
    if (frame) text += frameAssistantText(frame);
  }
  return text;
}

/** Return the most recent `result.is_error` message from a raw NDJSON stream. */
export function extractError(raw: string): string {
  const lines = raw.split('\n');
  for (let i = lines.length - 1; i >= 0; i--) {
    const frame = parseFrameLine(lines[i]);
    if (frame) {
      const err = frameError(frame);
      if (err) return err;
    }
  }
  return '';
}

export function activityIcon(kind: ActivityRow['kind']): string {
  switch (kind) {
    case 'tool': return '▶';
    case 'tool_result': return '✓';
    case 'thinking': return '🧠';
    default: return '·';
  }
}

export function bubbleFromMessage(m: PlanningMessage): RenderedBubble {
  if (m.role === 'assistant') {
    if (m.raw_output) {
      const text = extractAssistantText(m.raw_output);
      const errorText = extractError(m.raw_output);
      const activity = parseActivity(m.raw_output);
      return {
        role: 'assistant',
        contentHtml: text ? renderMarkdown(text) : '',
        rawText: text,
        rawOutput: m.raw_output,
        timestamp: m.timestamp,
        planRound: m.plan_round ?? 0,
        reverted: false,
        activity,
        hasActivity: activity.length > 0,
        isStreaming: false,
        errorText,
      };
    }
    return {
      role: 'assistant',
      contentHtml: renderMarkdown(m.content ?? ''),
      rawText: m.content ?? '',
      timestamp: m.timestamp,
      planRound: m.plan_round ?? 0,
      reverted: false,
      activity: [],
      hasActivity: false,
      isStreaming: false,
    };
  }
  return {
    role: m.role,
    contentHtml: '',
    rawText: m.content ?? '',
    timestamp: m.timestamp,
    planRound: 0,
    reverted: false,
    activity: [],
    hasActivity: false,
    isStreaming: false,
  };
}
