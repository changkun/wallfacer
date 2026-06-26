import { describe, it, expect, beforeEach } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';
import { useDiffCommentsStore, type NewDiffComment } from './diffComments';

function draft(over: Partial<NewDiffComment> = {}): NewDiffComment {
  return {
    taskId: 't1',
    filename: 'a.go',
    lineIndex: 3,
    oldLine: null,
    newLine: 42,
    kind: 'add',
    lineText: '+x := 1',
    body: 'hello',
    ...over,
  };
}

beforeEach(() => {
  setActivePinia(createPinia());
});

describe('diffComments store', () => {
  it('adds a comment with a generated id and lists it for the task', () => {
    const s = useDiffCommentsStore();
    const c = s.add(draft());
    expect(c.id).toBeTruthy();
    expect(s.forTask('t1')).toHaveLength(1);
    expect(s.forTask('t1')[0].body).toBe('hello');
  });

  it('updates a comment body by id', () => {
    const s = useDiffCommentsStore();
    const c = s.add(draft());
    s.update(c.id, 'revised');
    expect(s.forTask('t1')[0].body).toBe('revised');
  });

  it('removes a comment by id', () => {
    const s = useDiffCommentsStore();
    const c = s.add(draft());
    s.remove(c.id);
    expect(s.forTask('t1')).toHaveLength(0);
  });

  it('clear removes only the given task’s comments', () => {
    const s = useDiffCommentsStore();
    s.add(draft({ taskId: 't1' }));
    s.add(draft({ taskId: 't2' }));
    s.clear('t1');
    expect(s.forTask('t1')).toHaveLength(0);
    expect(s.forTask('t2')).toHaveLength(1);
  });

  it('forLine finds the comment anchored to a (file, lineIndex)', () => {
    const s = useDiffCommentsStore();
    s.add(draft({ filename: 'a.go', lineIndex: 3 }));
    s.add(draft({ filename: 'b.go', lineIndex: 3 }));
    expect(s.forLine('t1', 'a.go', 3)?.filename).toBe('a.go');
    expect(s.forLine('t1', 'b.go', 3)?.filename).toBe('b.go');
    expect(s.forLine('t1', 'a.go', 9)).toBeUndefined();
    expect(s.forLine('t9', 'a.go', 3)).toBeUndefined();
  });
});
