import { describe, expect, it } from 'vitest';
import { basename, groupLabel } from './workspaceLabel';

describe('basename', () => {
  it('returns the last path segment', () => {
    expect(basename('/Users/changkun/dev/latere.ai/wallfacer')).toBe('wallfacer');
  });
  it('ignores trailing slashes', () => {
    expect(basename('/tmp/wf-playground/')).toBe('wf-playground');
  });
  it('returns the input when there is no separator', () => {
    expect(basename('oblivion')).toBe('oblivion');
  });
});

describe('groupLabel', () => {
  it('prefers an explicit name', () => {
    expect(groupLabel({ name: 'latere.ai', workspaces: ['/Users/changkun/dev/latere.ai'] })).toBe('latere.ai');
  });
  it('falls back to the basename instead of the full (truncating) path', () => {
    // Regression: rows used to show "/Users/ch..." truncated and unreadable.
    expect(groupLabel({ workspaces: ['/Users/changkun/dev/latere.ai/wallfacer'] })).toBe('wallfacer');
  });
  it('joins multiple workspace basenames with " + "', () => {
    expect(groupLabel({ workspaces: ['/a/b/blog', '/c/d/docs'] })).toBe('blog + docs');
  });
  it('returns a placeholder when there are no workspaces', () => {
    expect(groupLabel({ workspaces: [] })).toBe('Workspace');
  });
});
