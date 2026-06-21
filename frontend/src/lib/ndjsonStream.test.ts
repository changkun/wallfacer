// The incremental NDJSON stream parser must produce the same derived state
// (trailing answer text, interleaved activity rows, last error, hasActivity) as
// the one-shot parseTurn over the full accumulated buffer, regardless of how the
// byte stream is chunked.
import { describe, it, expect } from 'vitest';
import { createNdjsonStreamParser } from './ndjsonStream';
import { extractError } from './planningBubble';
import { parseTurn } from './prettyNdjson';

function frames(): string[] {
  return [
    JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'Hello ' }] } }),
    JSON.stringify({ type: 'assistant', message: { content: [{ type: 'thinking', thinking: 'pondering\nmore' }] } }),
    JSON.stringify({ type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read', input: { file_path: '/a/b.go' } }] } }),
    JSON.stringify({ type: 'user', message: { content: [{ type: 'tool_result', content: 'file body', is_error: false }] } }),
    JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'world.' }] } }),
    JSON.stringify({ type: 'result', is_error: true, result: 'first error' }),
    JSON.stringify({ type: 'result', is_error: true, result: 'latest error' }),
    'not json, should be skipped',
    '',
  ];
}

// Feed `raw` to the parser split at every `size` bytes to stress the
// partial-trailing-line buffering.
function runChunked(raw: string, size: number) {
  const p = createNdjsonStreamParser();
  for (let i = 0; i < raw.length; i += size) {
    p.push(raw.slice(i, i + size));
  }
  p.finalize();
  return p.state();
}

describe('createNdjsonStreamParser', () => {
  const raw = frames().join('\n');

  const turn = parseTurn(raw);
  const oneShot = {
    text: turn.answer,
    activity: turn.rows,
    errorText: extractError(raw),
    hasActivity: turn.rows.length > 0,
  };

  it('matches the one-shot helpers when fed in one chunk', () => {
    const s = runChunked(raw, raw.length);
    expect(s.text).toBe(oneShot.text);
    expect(s.errorText).toBe(oneShot.errorText);
    expect(s.hasActivity).toBe(oneShot.hasActivity);
    expect(s.activity).toEqual(oneShot.activity);
  });

  it('matches the one-shot helpers across arbitrary chunk boundaries', () => {
    for (const size of [1, 2, 3, 7, 13, 50]) {
      const s = runChunked(raw, size);
      expect(s.text, `size=${size}`).toBe(oneShot.text);
      expect(s.errorText, `size=${size}`).toBe(oneShot.errorText);
      expect(s.hasActivity, `size=${size}`).toBe(oneShot.hasActivity);
      expect(s.activity, `size=${size}`).toEqual(oneShot.activity);
    }
  });

  it('reports the most recent error (last-wins)', () => {
    const s = runChunked(raw, 5);
    expect(s.errorText).toBe('latest error');
  });

  it('parses a trailing line without a newline via finalize', () => {
    const single = JSON.stringify({ type: 'assistant', message: { content: [{ type: 'text', text: 'tail' }] } });
    const p = createNdjsonStreamParser();
    p.push(single); // no newline
    expect(p.state().text).toBe(''); // not yet a complete line
    p.finalize();
    expect(p.state().text).toBe('tail');
  });
});
