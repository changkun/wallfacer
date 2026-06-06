import { describe, it, expect } from 'vitest';
import {
  timeOf,
  extractAssistantText,
  extractError,
  activityIcon,
  bubbleFromMessage,
  applyStreamingUpdate,
  type RenderedBubble,
} from './planningBubble';

describe('applyStreamingUpdate', () => {
  const mk = (id: string, rawText: string): RenderedBubble => ({
    id,
    role: 'assistant',
    contentHtml: '',
    rawText,
    planRound: 0,
    reverted: false,
    activity: [],
    hasActivity: false,
    isStreaming: true,
  });

  it('updates the bubble matching the id in place', () => {
    const msgs = [mk('a', 'one'), mk('stream-1', '')];
    const ok = applyStreamingUpdate(msgs, 'stream-1', { rawText: 'hello' });
    expect(ok).toBe(true);
    expect(msgs[1].rawText).toBe('hello');
    expect(msgs[0].rawText).toBe('one'); // untouched
  });

  it('drops the update and mutates nothing when the id is absent', () => {
    // Simulates the rendered list being replaced (thread switch) mid-stream:
    // the streaming bubble's id is gone, so a late chunk must not corrupt any
    // unrelated bubble at a now-stale index.
    const msgs = [mk('other-1', 'foreign'), mk('other-2', 'also foreign')];
    const ok = applyStreamingUpdate(msgs, 'stream-1', { rawText: 'leaked' });
    expect(ok).toBe(false);
    expect(msgs.map((b) => b.rawText)).toEqual(['foreign', 'also foreign']);
    expect(msgs).toHaveLength(2);
  });
});

describe('timeOf', () => {
  it('returns empty for undefined or invalid', () => {
    expect(timeOf(undefined)).toBe('');
    expect(timeOf('')).toBe('');
    expect(timeOf('not-a-date')).toBe('');
  });
  it('formats a valid ISO timestamp to hh:mm', () => {
    const out = timeOf('2026-06-01T13:45:00Z');
    expect(out).toMatch(/^\d{1,2}:\d{2}( [AP]M)?$/);
  });
});

describe('extractAssistantText', () => {
  it('concatenates assistant text blocks', () => {
    const raw = [
      JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'hello ' }] } }),
      JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'world' }] } }),
    ].join('\n');
    expect(extractAssistantText(raw)).toBe('hello world');
  });
  it('ignores non-assistant rows and malformed JSON', () => {
    const raw = [
      'not json',
      JSON.stringify({ type: 'system', message: { content: [{ type: 'text', text: 'ignored' }] } }),
      JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'kept' }] } }),
    ].join('\n');
    expect(extractAssistantText(raw)).toBe('kept');
  });
  it('returns empty string for no input', () => {
    expect(extractAssistantText('')).toBe('');
  });
});

describe('extractError', () => {
  it('returns the most recent error result', () => {
    const raw = [
      JSON.stringify({ type: 'result', is_error: true, result: 'first' }),
      JSON.stringify({ type: 'result', is_error: true, result: 'last' }),
    ].join('\n');
    expect(extractError(raw)).toBe('last');
  });
  it('ignores results without is_error', () => {
    const raw = JSON.stringify({ type: 'result', is_error: false, result: 'ok' });
    expect(extractError(raw)).toBe('');
  });
  it('returns empty when no result rows', () => {
    expect(extractError('')).toBe('');
  });
});

describe('activityIcon', () => {
  it('maps known kinds', () => {
    expect(activityIcon('tool')).toBe('▶');
    expect(activityIcon('tool_result')).toBe('✓');
    expect(activityIcon('thinking')).toBe('🧠');
  });
  it('falls back to bullet for unknown', () => {
    // @ts-expect-error — exercising the default branch
    expect(activityIcon('whatever')).toBe('·');
  });
});

describe('bubbleFromMessage', () => {
  it('wraps a plain user message as a non-assistant bubble', () => {
    const b = bubbleFromMessage({ role: 'user', content: 'hi' } as never);
    expect(b.role).toBe('user');
    expect(b.rawText).toBe('hi');
    expect(b.contentHtml).toBe('');
    expect(b.hasActivity).toBe(false);
  });
  it('renders assistant content as markdown when no raw_output', () => {
    const b = bubbleFromMessage({ role: 'assistant', content: '**bold**' } as never);
    expect(b.role).toBe('assistant');
    expect(b.rawText).toBe('**bold**');
    expect(b.contentHtml).toContain('<strong>');
  });
  it('parses NDJSON raw_output into text + activity', () => {
    const raw_output = JSON.stringify({
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'streamed' }] },
    });
    const b = bubbleFromMessage({ role: 'assistant', raw_output } as never);
    expect(b.role).toBe('assistant');
    expect(b.rawText).toBe('streamed');
    expect(b.rawOutput).toBe(raw_output);
  });
});
