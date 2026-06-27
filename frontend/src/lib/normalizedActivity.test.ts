import { describe, it, expect } from 'vitest';
import { parseNormalized, createNormalizedParser, type NormalizedEvent } from './normalizedActivity';

function ndjson(...events: NormalizedEvent[]): string {
  return events.map((e) => JSON.stringify(e)).join('\n') + '\n';
}

describe('parseNormalized', () => {
  it('folds a mixed stream into trajectory rows + answer + usage', () => {
    const raw = ndjson(
      { kind: 'system_init', session_id: 's1' },
      { kind: 'thinking', text: 'planning the work' },
      { kind: 'tool_end', tool: { id: 't1', name: 'bash', input: { command: 'ls' }, output: 'a\nb' } },
      { kind: 'tool_end', tool: { id: 't2', name: 'bash', input: { command: 'cat nope' }, error: 'no such file' } },
      { kind: 'assistant', text: 'Listed the files.' },
      { kind: 'result', text: 'Listed the files.', usage: { input_tokens: 10, output_tokens: 5, cost_usd: 0.01 } },
    );
    const { rows, answer, usage } = parseNormalized(raw);

    expect(rows.map((r) => r.kind)).toEqual(['thinking', 'tool', 'tool', 'tool_result']);
    // The thinking row carries the reasoning.
    expect(rows[0].summary).toBe('planning the work');
    // Tool rows surface a generic command summary from the input.
    expect(rows[1].label).toBe('bash');
    expect(rows[1].summary).toBe('ls');
    // The errored tool adds an open error row.
    expect(rows[3].label).toBe('error');
    expect(rows[3].defaultOpen).toBe(true);
    // Trailing assistant prose is the answer (not duplicated by the result).
    expect(answer).toBe('Listed the files.');
    expect(usage?.input_tokens).toBe(10);
  });

  it('pairs tool_start/tool_end by id into a single row', () => {
    const raw = ndjson(
      { kind: 'tool_start', tool: { id: 'x', name: 'edit', input: { file_path: '/a/b/c.ts' } } },
      { kind: 'tool_end', tool: { id: 'x', name: 'edit', output: 'ok' } },
    );
    const { rows } = parseNormalized(raw);
    expect(rows).toHaveLength(1);
    expect(rows[0].kind).toBe('tool');
    expect(rows[0].label).toBe('edit');
    expect(rows[0].summary).toBe('c.ts'); // basename of file_path
  });

  it('emits an error row when a paired tool_end carries an error', () => {
    const raw = ndjson(
      { kind: 'tool_start', tool: { id: 'x', name: 'bash', input: { command: 'boom' } } },
      { kind: 'tool_end', tool: { id: 'x', name: 'bash', error: 'exit 1' } },
    );
    const { rows } = parseNormalized(raw);
    expect(rows.map((r) => r.kind)).toEqual(['tool', 'tool_result']);
  });

  it('falls back to result text for the answer when no trailing prose', () => {
    const raw = ndjson(
      { kind: 'tool_end', tool: { id: 't', name: 'bash', input: { command: 'ls' } } },
      { kind: 'result', text: 'final answer from result' },
    );
    const { answer } = parseNormalized(raw);
    expect(answer).toBe('final answer from result');
  });

  it('parses identically regardless of chunk boundaries', () => {
    const raw = ndjson(
      { kind: 'thinking', text: 'hmm' },
      { kind: 'tool_end', tool: { id: 't', name: 'bash', input: { command: 'ls' } } },
      { kind: 'assistant', text: 'done' },
    );
    const whole = parseNormalized(raw);
    const p = createNormalizedParser();
    for (let i = 0; i < raw.length; i += 3) p.push(raw.slice(i, i + 3));
    p.finalize();
    expect(p.rows()).toEqual(whole.rows);
    expect(p.answer()).toBe(whole.answer);
  });
});
