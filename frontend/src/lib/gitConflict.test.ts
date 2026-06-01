import { describe, it, expect } from 'vitest';
import { formatGitConflict } from './gitConflict';

describe('formatGitConflict', () => {
  it('lists blocking tasks with humanised status', () => {
    const msg = formatGitConflict(
      { error: 'push blocked', blocking_tasks: [{ id: 'abc123', title: 'Fix bug', status: 'in_progress' }] },
      'Push',
    );
    expect(msg).toContain('push blocked');
    expect(msg).toContain('Blocking tasks:');
    expect(msg).toContain('- [in progress] Fix bug (abc123)');
  });
  it('falls back to (untitled task) and unknown status', () => {
    const msg = formatGitConflict({ blocking_tasks: [{ id: 'x' }] }, 'Sync');
    expect(msg).toContain('Sync blocked');
    expect(msg).toContain('- [unknown] (untitled task) (x)');
  });
  it('uses the error or fallback when there are no blocking tasks', () => {
    expect(formatGitConflict({ error: 'boom' }, 'Push')).toBe('boom');
    expect(formatGitConflict({}, 'Push')).toBe('Push failed');
    expect(formatGitConflict(null, 'Rebase')).toBe('Rebase failed');
  });
});
