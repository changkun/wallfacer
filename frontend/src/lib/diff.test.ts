import { describe, it, expect } from 'vitest';
import { parseDiffFiles, classifyDiffLine } from './diff';

describe('classifyDiffLine', () => {
  it('classifies added/removed/context lines', () => {
    expect(classifyDiffLine('+new')).toBe('add');
    expect(classifyDiffLine('-old')).toBe('del');
    expect(classifyDiffLine(' ctx')).toBe('ctx');
  });
  it('treats +++/--- and headers as header, not add/del', () => {
    expect(classifyDiffLine('+++ b/file')).toBe('header');
    expect(classifyDiffLine('--- a/file')).toBe('header');
    expect(classifyDiffLine('diff --git a/f b/f')).toBe('header');
    expect(classifyDiffLine('index 000..abc')).toBe('header');
    expect(classifyDiffLine('new file mode 100644')).toBe('header');
  });
  it('classifies hunk headers', () => {
    expect(classifyDiffLine('@@ -0,0 +1,3 @@')).toBe('hunk');
  });
});

describe('parseDiffFiles', () => {
  it('returns [] for empty diff', () => {
    expect(parseDiffFiles('')).toEqual([]);
    expect(parseDiffFiles('   \n')).toEqual([]);
  });

  it('parses a single new-file diff with correct counts', () => {
    const diff = [
      'diff --git a/CONTRIBUTING.md b/CONTRIBUTING.md',
      'new file mode 100644',
      'index 0000000..db123af',
      '--- /dev/null',
      '+++ b/CONTRIBUTING.md',
      '@@ -0,0 +1,2 @@',
      '+# Contributing',
      '+',
    ].join('\n');
    const files = parseDiffFiles(diff);
    expect(files).toHaveLength(1);
    expect(files[0].filename).toBe('CONTRIBUTING.md');
    expect(files[0].adds).toBe(2);
    expect(files[0].dels).toBe(0);
    // +++ header line must not be counted as an addition
    expect(files[0].lines.some(l => l.kind === 'header' && l.text.startsWith('+++'))).toBe(true);
  });

  it('splits multiple files and counts adds/dels independently', () => {
    const diff = [
      'diff --git a/a.txt b/a.txt',
      '--- a/a.txt',
      '+++ b/a.txt',
      '@@ -1 +1 @@',
      '-old',
      '+new',
      'diff --git a/b.txt b/b.txt',
      '--- a/b.txt',
      '+++ b/b.txt',
      '@@ -0,0 +1 @@',
      '+hi',
    ].join('\n');
    const files = parseDiffFiles(diff);
    expect(files.map(f => f.filename)).toEqual(['a.txt', 'b.txt']);
    expect(files[0]).toMatchObject({ adds: 1, dels: 1 });
    expect(files[1]).toMatchObject({ adds: 1, dels: 0 });
  });

  it('attributes files to their workspace separator and drops the separator line', () => {
    const diff = [
      '=== service-a ===',
      'diff --git a/x.go b/x.go',
      '--- a/x.go',
      '+++ b/x.go',
      '@@ -1 +1 @@',
      '+x',
    ].join('\n');
    const files = parseDiffFiles(diff);
    expect(files[0].workspace).toBe('service-a');
    expect(files[0].lines.some(l => l.text.includes('=== service-a ==='))).toBe(false);
  });
});
