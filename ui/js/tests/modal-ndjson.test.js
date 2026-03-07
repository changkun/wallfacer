/**
 * Tests for modal-ndjson.js — NDJSON parsing and pretty log rendering.
 */
import { describe, it, expect, beforeAll } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function makeContext(extra = {}) {
  return vm.createContext({ console, Math, Date, ...extra });
}

function loadScript(filename, ctx) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx);
  return ctx;
}

function makeNdjsonContext() {
  const ctx = makeContext({
    // Stubs required by modal-ndjson.js at runtime
    escapeHtml: (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'),
    ansiToHtml: (s) => String(s ?? ''),
  });
  loadScript('modal-ndjson.js', ctx);
  return ctx;
}

// ---------------------------------------------------------------------------
// parseNdjsonLine
// ---------------------------------------------------------------------------
describe('parseNdjsonLine', () => {
  let ctx;
  beforeAll(() => { ctx = makeNdjsonContext(); });

  it('parses a valid JSON object line', () => {
    const result = ctx.parseNdjsonLine('{"type":"result","result":"done"}');
    expect(result).toEqual({ type: 'result', result: 'done' });
  });

  it('returns null for an empty line', () => {
    expect(ctx.parseNdjsonLine('')).toBeNull();
  });

  it('returns null for whitespace-only line', () => {
    expect(ctx.parseNdjsonLine('   ')).toBeNull();
  });

  it('returns null for a line that does not start with {', () => {
    expect(ctx.parseNdjsonLine('[1,2,3]')).toBeNull();
    expect(ctx.parseNdjsonLine('plain text')).toBeNull();
    expect(ctx.parseNdjsonLine('123')).toBeNull();
  });

  it('returns null for malformed JSON', () => {
    expect(ctx.parseNdjsonLine('{bad json')).toBeNull();
    expect(ctx.parseNdjsonLine('{"unclosed":')).toBeNull();
  });

  it('trims leading/trailing whitespace before checking', () => {
    const result = ctx.parseNdjsonLine('  {"type":"ping"}  ');
    expect(result).toEqual({ type: 'ping' });
  });

  it('handles nested JSON objects', () => {
    const line = '{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}';
    const result = ctx.parseNdjsonLine(line);
    expect(result.type).toBe('assistant');
    expect(result.message.content[0].text).toBe('hi');
  });
});

// ---------------------------------------------------------------------------
// extractToolInput
// ---------------------------------------------------------------------------
describe('extractToolInput', () => {
  let ctx;
  beforeAll(() => { ctx = makeNdjsonContext(); });

  it('extracts command from Bash tool', () => {
    expect(ctx.extractToolInput('Bash', { command: 'ls -la' })).toBe('ls -la');
  });

  it('extracts file_path from Read tool', () => {
    expect(ctx.extractToolInput('Read', { file_path: '/foo/bar.js' })).toBe('/foo/bar.js');
  });

  it('extracts file_path from Write tool', () => {
    expect(ctx.extractToolInput('Write', { file_path: '/out.txt' })).toBe('/out.txt');
  });

  it('extracts file_path from Edit tool', () => {
    expect(ctx.extractToolInput('Edit', { file_path: '/edit.go' })).toBe('/edit.go');
  });

  it('extracts pattern from Glob tool', () => {
    expect(ctx.extractToolInput('Glob', { pattern: '**/*.js' })).toBe('**/*.js');
  });

  it('extracts pattern from Grep tool', () => {
    expect(ctx.extractToolInput('Grep', { pattern: 'function\\s+\\w+' })).toBe('function\\s+\\w+');
  });

  it('extracts url from WebFetch tool', () => {
    expect(ctx.extractToolInput('WebFetch', { url: 'https://example.com' })).toBe('https://example.com');
  });

  it('extracts query from WebSearch tool', () => {
    expect(ctx.extractToolInput('WebSearch', { query: 'vitest coverage' })).toBe('vitest coverage');
  });

  it('extracts and truncates prompt from Task tool to 120 chars', () => {
    const long = 'a'.repeat(200);
    const result = ctx.extractToolInput('Task', { prompt: long });
    expect(result.length).toBe(120);
    expect(result).toBe('a'.repeat(120));
  });

  it('returns empty string for Task tool with no prompt', () => {
    expect(ctx.extractToolInput('Task', {})).toBe('');
  });

  it('returns count for TodoWrite tool', () => {
    expect(ctx.extractToolInput('TodoWrite', { todos: [{}, {}, {}] })).toBe('3 items');
  });

  it('returns empty string for TodoWrite with no todos', () => {
    expect(ctx.extractToolInput('TodoWrite', {})).toBe('');
  });

  it('falls back to file_path for unknown tools with that key', () => {
    expect(ctx.extractToolInput('UnknownTool', { file_path: '/some/path' })).toBe('/some/path');
  });

  it('falls back to command for unknown tools with that key', () => {
    expect(ctx.extractToolInput('UnknownTool', { command: 'do something' })).toBe('do something');
  });

  it('falls back to query for unknown tools with that key', () => {
    expect(ctx.extractToolInput('UnknownTool', { query: 'search term' })).toBe('search term');
  });

  it('returns empty string for unknown tool with no recognized keys', () => {
    expect(ctx.extractToolInput('UnknownTool', { foo: 'bar' })).toBe('');
  });

  it('returns empty string when inputObj is null', () => {
    expect(ctx.extractToolInput('Bash', null)).toBe('');
  });

  it('returns empty string when inputObj is a primitive', () => {
    expect(ctx.extractToolInput('Bash', 'string')).toBe('');
    expect(ctx.extractToolInput('Bash', 42)).toBe('');
  });

  it('returns empty string when inputObj is undefined', () => {
    expect(ctx.extractToolInput('Read', undefined)).toBe('');
  });
});

// ---------------------------------------------------------------------------
// renderPrettyLogs
// ---------------------------------------------------------------------------
describe('renderPrettyLogs', () => {
  let ctx;
  beforeAll(() => { ctx = makeNdjsonContext(); });

  it('returns empty string for empty buffer', () => {
    expect(ctx.renderPrettyLogs('')).toBe('');
  });

  it('renders non-JSON stderr lines with ANSI processing', () => {
    const result = ctx.renderPrettyLogs('stderr line');
    expect(result).toContain('cc-stderr');
    expect(result).toContain('stderr line');
  });

  it('skips whitespace-only non-JSON lines', () => {
    const result = ctx.renderPrettyLogs('   \n   ');
    expect(result).toBe('');
  });

  it('renders assistant text blocks', () => {
    const line = JSON.stringify({
      type: 'assistant',
      message: { content: [{ type: 'text', text: 'Hello from assistant' }] },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('cc-text');
    expect(result).toContain('Hello from assistant');
  });

  it('renders assistant tool_use blocks with tool name', () => {
    const line = JSON.stringify({
      type: 'assistant',
      message: {
        content: [{
          type: 'tool_use',
          name: 'Bash',
          input: { command: 'echo hi' },
        }],
      },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('cc-tool-call');
    expect(result).toContain('cc-tool-name');
    expect(result).toContain('Bash');
    expect(result).toContain('echo hi');
  });

  it('renders tool_use with input as JSON string', () => {
    const line = JSON.stringify({
      type: 'assistant',
      message: {
        content: [{
          type: 'tool_use',
          name: 'Read',
          input: JSON.stringify({ file_path: '/foo.js' }),
        }],
      },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('/foo.js');
  });

  it('renders tool_use with invalid JSON string input gracefully', () => {
    const line = JSON.stringify({
      type: 'assistant',
      message: {
        content: [{
          type: 'tool_use',
          name: 'Bash',
          input: '{not json',
        }],
      },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('cc-tool-call');
    expect(result).toContain('Bash');
  });

  it('renders tool result with string content', () => {
    const line = JSON.stringify({
      type: 'user',
      message: {
        content: [{
          type: 'tool_result',
          content: 'output text',
        }],
      },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('cc-tool-result');
    expect(result).toContain('output text');
  });

  it('renders tool result with array content', () => {
    const line = JSON.stringify({
      type: 'user',
      message: {
        content: [{
          type: 'tool_result',
          content: [{ text: 'part1' }, { text: 'part2' }],
        }],
      },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('part1');
    expect(result).toContain('part2');
  });

  it('renders empty tool result as "(No output)"', () => {
    const line = JSON.stringify({
      type: 'user',
      message: {
        content: [{ type: 'tool_result', content: '' }],
      },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('cc-result-empty');
    expect(result).toContain('(No output)');
  });

  it('collapses long tool results (>5 lines) with expandable details', () => {
    const longText = Array.from({ length: 10 }, (_, i) => `line ${i}`).join('\n');
    const line = JSON.stringify({
      type: 'user',
      message: {
        content: [{ type: 'tool_result', content: longText }],
      },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('cc-expand');
    expect(result).toContain('details');
    expect(result).toContain('+7 lines');
  });

  it('renders short tool results (<=5 lines) without details element', () => {
    const text = 'line1\nline2\nline3';
    const line = JSON.stringify({
      type: 'user',
      message: {
        content: [{ type: 'tool_result', content: text }],
      },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).not.toContain('<details');
  });

  it('renders final result blocks', () => {
    const line = JSON.stringify({ type: 'result', result: 'Task complete' });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('cc-final-result');
    expect(result).toContain('[Result]');
    expect(result).toContain('Task complete');
  });

  it('skips result events with empty result field', () => {
    const line = JSON.stringify({ type: 'result', result: '' });
    const result = ctx.renderPrettyLogs(line);
    expect(result).not.toContain('cc-final-result');
  });

  it('skips user messages that are not tool_result type', () => {
    const line = JSON.stringify({
      type: 'user',
      message: { content: [{ type: 'text', text: 'user text' }] },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toBe('');
  });

  it('cleans Read tool arrow notation from output text', () => {
    const text = '   1→\tconst x = 1;\n   2→\tconst y = 2;';
    const line = JSON.stringify({
      type: 'user',
      message: { content: [{ type: 'tool_result', content: text }] },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).not.toContain('→');
    expect(result).toContain('const x = 1;');
  });

  it('truncates long tool input display to 200 chars with ellipsis', () => {
    const longCmd = 'x'.repeat(250);
    const line = JSON.stringify({
      type: 'assistant',
      message: {
        content: [{
          type: 'tool_use',
          name: 'Bash',
          input: { command: longCmd },
        }],
      },
    });
    const result = ctx.renderPrettyLogs(line);
    expect(result).toContain('\u2026'); // ellipsis
  });
});
