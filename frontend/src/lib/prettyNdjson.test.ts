import { describe, it, expect } from 'vitest';
import { parseActivity, hasActivity } from './prettyNdjson';

function ndjson(...frames: unknown[]): string {
  return frames.map(f => JSON.stringify(f)).join('\n');
}

describe('parseActivity', () => {
  it('ignores non-JSON and blank lines', () => {
    expect(parseActivity('not json\n\n   ')).toEqual([]);
  });

  it('extracts tool_use with summary from a priority key', () => {
    const raw = ndjson({
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'Write', input: { file_path: 'CONTRIBUTING.md', content: 'x' } }] },
    });
    const rows = parseActivity(raw);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ kind: 'tool', label: 'Write' });
    expect(rows[0].summary).toContain('file_path: CONTRIBUTING.md');
  });

  it('extracts thinking and assistant text blocks', () => {
    const raw = ndjson({
      type: 'assistant',
      message: { content: [
        { type: 'thinking', thinking: 'let me plan' },
        { type: 'text', text: 'Done — created the file.' },
      ] },
    });
    const rows = parseActivity(raw);
    expect(rows.map(r => [r.kind, r.label])).toEqual([
      ['thinking', 'thinking'],
      ['system', 'text'],
    ]);
    expect(rows[1].summary).toBe('Done — created the file.');
  });

  it('skips empty assistant text blocks', () => {
    const raw = ndjson({ type: 'assistant', message: { content: [{ type: 'text', text: '   ' }] } });
    expect(parseActivity(raw)).toEqual([]);
  });

  it('extracts tool_result and flags errors as defaultOpen', () => {
    const raw = ndjson({
      type: 'user',
      message: { content: [{ type: 'tool_result', is_error: true, content: 'boom' }] },
    });
    const rows = parseActivity(raw);
    expect(rows[0]).toMatchObject({ kind: 'tool_result', label: 'error', defaultOpen: true });
  });

  it('extracts the final result frame', () => {
    const raw = ndjson({ type: 'result', result: 'All done.' });
    const rows = parseActivity(raw);
    expect(rows).toEqual([{ kind: 'system', label: 'result', summary: 'All done.', detail: undefined, defaultOpen: false }]);
  });

  it('flags an error result frame as defaultOpen', () => {
    const raw = ndjson({ type: 'result', is_error: true, result: 'failed' });
    expect(parseActivity(raw)[0]).toMatchObject({ label: 'error', defaultOpen: true });
  });
});

describe('hasActivity', () => {
  it('is true when tool/thinking/tool_result frames exist', () => {
    expect(hasActivity(ndjson({ type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } }))).toBe(true);
  });
  it('is false for plain text', () => {
    expect(hasActivity('hello world')).toBe(false);
  });
});
