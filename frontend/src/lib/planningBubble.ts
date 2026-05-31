// Pure helpers for rendering planning-chat message bubbles. Extracted from
// PlanningChatPanel.vue so they can be unit-tested in isolation and reused
// by the streaming code path (which also needs to parse incoming NDJSON).
import { renderMarkdown } from './markdown';
import { parseActivity, type ActivityRow } from './prettyNdjson';
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
}

export function timeOf(ts?: string): string {
  if (!ts) return '';
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return '';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

/** Concatenate all `assistant` text blocks from a raw NDJSON stream. */
export function extractAssistantText(raw: string): string {
  let text = '';
  for (const line of raw.split('\n')) {
    const trimmed = line.trim();
    if (!trimmed || trimmed[0] !== '{') continue;
    try {
      const obj = JSON.parse(trimmed);
      if (obj.type === 'assistant' && obj.message?.content) {
        for (const block of obj.message.content) {
          if (block.type === 'text' && typeof block.text === 'string') {
            text += block.text;
          }
        }
      }
    } catch { /* skip malformed */ }
  }
  return text;
}

/** Return the most recent `result.is_error` message from a raw NDJSON stream. */
export function extractError(raw: string): string {
  const lines = raw.split('\n');
  for (let i = lines.length - 1; i >= 0; i--) {
    const trimmed = lines[i].trim();
    if (!trimmed || trimmed[0] !== '{') continue;
    try {
      const obj = JSON.parse(trimmed);
      if (obj.type === 'result' && obj.is_error && obj.result) return String(obj.result);
    } catch { /* skip */ }
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
