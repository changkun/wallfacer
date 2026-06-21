import { describe, it, expect } from 'vitest';
import { parseActivity, hasActivity } from './prettyNdjson';

function ndjson(...frames: unknown[]): string {
  return frames.map(f => JSON.stringify(f)).join('\n');
}

describe('parseActivity', () => {
  it('ignores non-JSON and blank lines', () => {
    expect(parseActivity('not json\n\n   ')).toEqual([]);
  });

  it('titles a tool by the file name, not a raw "key: value" dump', () => {
    const raw = ndjson({
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'Write', input: { file_path: 'docs/CONTRIBUTING.md', content: 'x' } }] },
    });
    const rows = parseActivity(raw);
    expect(rows).toHaveLength(1);
    expect(rows[0]).toMatchObject({ kind: 'tool', label: 'Write', summary: 'CONTRIBUTING.md' });
  });

  it('prefers the agent-supplied description as the tool title', () => {
    const raw = ndjson({
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'Bash', input: { command: 'serve .', description: 'Open deck preview' } }] },
    });
    expect(parseActivity(raw)[0].summary).toBe('Open deck preview');
  });

  it('previews the command under a described bash step, and the full path for file ops', () => {
    const bash = parseActivity(ndjson({
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'Bash', input: { command: 'find . -name Makefile', description: 'Find build files' } }] },
    }))[0];
    expect(bash.summary).toBe('Find build files');
    expect(bash.preview).toBe('find . -name Makefile');

    const read = parseActivity(ndjson({
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'Read', input: { file_path: 'specs/foo/bar.md' } }] },
    }))[0];
    expect(read.summary).toBe('bar.md');
    expect(read.preview).toBe('specs/foo/bar.md');

    // A bare command (no description) is already the title — no duplicate preview.
    const bare = parseActivity(ndjson({
      type: 'assistant',
      message: { content: [{ type: 'tool_use', name: 'Bash', input: { command: 'ls' } }] },
    }))[0];
    expect(bare.preview).toBeUndefined();
  });

  it('emits thinking but NOT assistant text (text is the answer, not activity)', () => {
    const raw = ndjson({
      type: 'assistant',
      message: { content: [
        { type: 'thinking', thinking: 'let me plan' },
        { type: 'text', text: 'Done — created the file.' },
      ] },
    });
    const rows = parseActivity(raw);
    expect(rows.map(r => [r.kind, r.label])).toEqual([['thinking', 'Thinking']]);
    expect(rows[0].summary).toBe('let me plan');
  });

  it('keeps the full thinking body as expandable detail when long', () => {
    const thinking = 'x'.repeat(400);
    const row = parseActivity(ndjson({ type: 'assistant', message: { content: [{ type: 'thinking', thinking }] } }))[0];
    expect(row.detail).toBe(thinking);
  });

  it('skips empty text and empty thinking blocks', () => {
    const raw = ndjson({ type: 'assistant', message: { content: [
      { type: 'text', text: '   ' },
      { type: 'thinking', thinking: '  ' },
    ] } });
    expect(parseActivity(raw)).toEqual([]);
  });

  it('surfaces a failed tool result, open by default', () => {
    const raw = ndjson({ type: 'user', message: { content: [{ type: 'tool_result', is_error: true, content: 'boom' }] } });
    expect(parseActivity(raw)[0]).toMatchObject({ kind: 'tool_result', label: 'error', defaultOpen: true });
  });

  it('drops a successful tool result (the answer already covers it)', () => {
    const raw = ndjson({ type: 'user', message: { content: [{ type: 'tool_result', content: 'ok' }] } });
    expect(parseActivity(raw)).toEqual([]);
  });

  it('drops a successful result frame and surfaces an error one', () => {
    expect(parseActivity(ndjson({ type: 'result', result: 'All done.' }))).toEqual([]);
    expect(parseActivity(ndjson({ type: 'result', is_error: true, result: 'failed' }))[0])
      .toMatchObject({ label: 'error', defaultOpen: true });
  });
});

describe('hasActivity', () => {
  it('is true when tool or thinking frames exist', () => {
    expect(hasActivity(ndjson({ type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Read' }] } }))).toBe(true);
  });
  it('is true for a failed tool result but false for a successful one', () => {
    expect(hasActivity(ndjson({ type: 'user', message: { content: [{ type: 'tool_result', is_error: true, content: 'x' }] } }))).toBe(true);
    expect(hasActivity(ndjson({ type: 'user', message: { content: [{ type: 'tool_result', content: 'x' }] } }))).toBe(false);
  });
  it('is false for plain text', () => {
    expect(hasActivity('hello world')).toBe(false);
  });
});
