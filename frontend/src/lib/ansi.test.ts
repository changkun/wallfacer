import { describe, it, expect } from 'vitest';
import { ansiToHtml, collapseCarriageReturns } from './ansi';

describe('collapseCarriageReturns', () => {
  it('keeps a plain line untouched', () => {
    expect(collapseCarriageReturns('hello world')).toBe('hello world');
  });
  it('drops everything before the last \\r per line', () => {
    expect(collapseCarriageReturns('progress 10%\rprogress 80%\rdone')).toBe('done');
  });
  it('operates line-by-line', () => {
    expect(collapseCarriageReturns('a\rb\nc\rd')).toBe('b\nd');
  });
});

describe('ansiToHtml', () => {
  it('escapes HTML metacharacters in plain text', () => {
    expect(ansiToHtml('<script>&amp;')).toBe('&lt;script&gt;&amp;amp;');
  });
  it('colourises a standard FG escape', () => {
    const html = ansiToHtml('\x1b[31merror\x1b[0m');
    expect(html).toContain('color:#ff7b72');
    expect(html).toContain('error');
    expect(html.endsWith('</span>')).toBe(true);
  });
  it('handles bold + bright FG', () => {
    const html = ansiToHtml('\x1b[1;92mok\x1b[0m');
    expect(html).toContain('font-weight:bold');
    expect(html).toContain('color:#56d364');
  });
  it('honours 24-bit colour escapes', () => {
    const html = ansiToHtml('\x1b[38;2;128;0;255mx\x1b[0m');
    expect(html).toContain('color:rgb(128,0,255)');
  });
  it('consumes 256-colour fg operands without leaking SGR codes', () => {
    // 38;5;1 must not surface the trailing 1 as bold.
    const html = ansiToHtml('\x1b[38;5;1mx\x1b[0m');
    expect(html).not.toContain('font-weight:bold');
    expect(html).toContain('x');
  });
  it('consumes 256-colour bg operands without leaking SGR codes', () => {
    // 48;5;1 leaked bold previously (48 + 5 fall through, 1 read as SGR 1).
    const html = ansiToHtml('\x1b[48;5;1mx\x1b[0m');
    expect(html).not.toContain('font-weight:bold');
    expect(html).toContain('x');
  });
  it('consumes 24-bit bg operands without leaking SGR codes', () => {
    const html = ansiToHtml('\x1b[48;2;1;2;3mx\x1b[0m');
    expect(html).not.toContain('font-weight:bold');
    expect(html).not.toContain('color:rgb(1,2,3)');
    expect(html).toContain('x');
  });
  it('applies a real code following a consumed 256-colour run', () => {
    // After consuming 38;5;2 the loop must land on the trailing 31.
    const html = ansiToHtml('\x1b[38;5;2;31mx\x1b[0m');
    expect(html).toContain('color:#ff7b72');
  });
  it('drops non-SGR sequences', () => {
    // Cursor up CSI ignored, no span emitted.
    expect(ansiToHtml('\x1b[2Ax')).toBe('x');
  });
  it('closes lingering spans at end of input', () => {
    const html = ansiToHtml('\x1b[31munterminated');
    const opens = (html.match(/<span/g) || []).length;
    const closes = (html.match(/<\/span>/g) || []).length;
    expect(opens).toBe(closes);
  });
});
