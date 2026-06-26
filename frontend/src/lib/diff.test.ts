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

  it('attributes the last file of a non-final workspace correctly', () => {
    // The server emits "=== name ===\n<diff>" with no trailing separator, so a
    // separator sits at the tail of the previous workspace's last file block.
    const diff = [
      '=== ws1 ===',
      'diff --git a/f1.txt b/f1.txt',
      '--- a/f1.txt',
      '+++ b/f1.txt',
      '@@ -1 +1 @@',
      '+a',
      'diff --git a/f2.txt b/f2.txt',
      '--- a/f2.txt',
      '+++ b/f2.txt',
      '@@ -1 +1 @@',
      '+b',
      '=== ws2 ===',
      'diff --git a/f3.txt b/f3.txt',
      '--- a/f3.txt',
      '+++ b/f3.txt',
      '@@ -1 +1 @@',
      '+c',
    ].join('\n');
    const files = parseDiffFiles(diff);
    expect(files.map(f => [f.filename, f.workspace])).toEqual([
      ['f1.txt', 'ws1'],
      ['f2.txt', 'ws1'], // last file of ws1 must NOT be stamped ws2
      ['f3.txt', 'ws2'],
    ]);
  });
});

describe('parseDiffFiles line numbers', () => {
  it('assigns new-file line numbers to added lines', () => {
    const diff = [
      'diff --git a/new.txt b/new.txt',
      'new file mode 100644',
      '--- /dev/null',
      '+++ b/new.txt',
      '@@ -0,0 +1,2 @@',
      '+first',
      '+second',
    ].join('\n');
    const add = parseDiffFiles(diff)[0].lines.filter(l => l.kind === 'add');
    expect(add.map(l => [l.oldLine, l.newLine])).toEqual([
      [null, 1],
      [null, 2],
    ]);
  });

  it('assigns old-file line numbers to deletions and both to context', () => {
    const diff = [
      'diff --git a/edit.txt b/edit.txt',
      '--- a/edit.txt',
      '+++ b/edit.txt',
      '@@ -10,3 +10,3 @@',
      ' ctx-a',
      '-removed',
      '+added',
      ' ctx-b',
    ].join('\n');
    const lines = parseDiffFiles(diff)[0].lines;
    const byKind = (k: string) => lines.filter(l => l.kind === k);
    // context advances both counters
    expect(byKind('ctx').map(l => [l.oldLine, l.newLine])).toEqual([
      [10, 10],
      [12, 12],
    ]);
    // deletion carries only oldLine, addition only newLine
    expect(byKind('del').map(l => [l.oldLine, l.newLine])).toEqual([[11, null]]);
    expect(byKind('add').map(l => [l.oldLine, l.newLine])).toEqual([[null, 11]]);
  });

  it('reseeds counters at each hunk header in a multi-hunk file', () => {
    const diff = [
      'diff --git a/multi.txt b/multi.txt',
      '--- a/multi.txt',
      '+++ b/multi.txt',
      '@@ -1,1 +1,1 @@',
      '-a1',
      '+b1',
      '@@ -50,1 +60,1 @@',
      '-a50',
      '+b60',
    ].join('\n');
    const lines = parseDiffFiles(diff)[0].lines;
    const dels = lines.filter(l => l.kind === 'del');
    const adds = lines.filter(l => l.kind === 'add');
    expect(dels.map(l => l.oldLine)).toEqual([1, 50]);
    expect(adds.map(l => l.newLine)).toEqual([1, 60]);
  });

  it('leaves hunk and header lines without line numbers', () => {
    const diff = [
      'diff --git a/h.txt b/h.txt',
      '--- a/h.txt',
      '+++ b/h.txt',
      '@@ -1 +1 @@',
      '+x',
    ].join('\n');
    const lines = parseDiffFiles(diff)[0].lines;
    for (const l of lines) {
      if (l.kind === 'hunk' || l.kind === 'header') {
        expect(l.oldLine).toBeNull();
        expect(l.newLine).toBeNull();
      }
    }
  });
});
