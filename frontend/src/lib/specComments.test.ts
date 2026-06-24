import { describe, it, expect } from 'vitest';
import {
  applyEvent,
  activeCount,
  blockForLine,
  buildReplyTree,
  inlineThreads,
  initials,
  outOfSyncCount,
  threadPreview,
  threadsForSpec,
  triageThreads,
  type Comment,
  type SpecCommentThread,
  type Thread,
} from './specComments';

function comment(p: Partial<Comment> & { id: string }): Comment {
  return {
    thread_id: 't',
    author_sub: 'u',
    body: '',
    created_at: '2026-01-01T00:00:00Z',
    ...p,
  };
}

function thread(p: Partial<SpecCommentThread> & { id: string }): SpecCommentThread {
  return {
    org_id: 'o',
    workspace_id: 'repo',
    spec_path: 'a/b.md',
    anchor: {
      section_path: [],
      line_hash: '',
      prefix: '',
      suffix: '',
      exact_text: '',
      line_hint: 0,
    },
    author_sub: 'u',
    created_at: '2026-01-01T00:00:00Z',
    resolved: false,
    status: 'active',
    comments: [],
    line: 5,
    orphaned: false,
    ...p,
  };
}

describe('buildReplyTree', () => {
  it('groups replies under their root, ordered by created_at', () => {
    const tree = buildReplyTree([
      comment({ id: 'r1', parent_id: 'root', created_at: '2026-01-01T00:02:00Z' }),
      comment({ id: 'root', created_at: '2026-01-01T00:00:00Z' }),
      comment({ id: 'r2', parent_id: 'root', created_at: '2026-01-01T00:01:00Z' }),
    ]);
    expect(tree).toHaveLength(1);
    expect(tree[0].comment.id).toBe('root');
    // Replies sorted by time: r2 (00:01) before r1 (00:02).
    expect(tree[0].replies.map((c) => c.id)).toEqual(['r2', 'r1']);
  });

  it('attaches an orphan reply to the first root rather than dropping it', () => {
    const tree = buildReplyTree([
      comment({ id: 'root' }),
      comment({ id: 'lost', parent_id: 'missing', created_at: '2026-01-01T00:05:00Z' }),
    ]);
    expect(tree[0].replies.map((c) => c.id)).toEqual(['lost']);
  });
});

describe('threadsForSpec', () => {
  it('matches spec_path exactly (no leading specs/)', () => {
    const all = [
      thread({ id: '1', spec_path: 'a/b.md' }),
      thread({ id: '2', spec_path: 'a/c.md' }),
    ];
    expect(threadsForSpec(all, 'a/b.md').map((t) => t.id)).toEqual(['1']);
    expect(threadsForSpec(all, '')).toEqual([]);
  });
});

describe('inlineThreads', () => {
  const specPath = 'a/b.md';
  it('includes active, excludes orphaned and outdated', () => {
    const all = [
      thread({ id: 'act', status: 'active' }),
      thread({ id: 'orph', status: 'orphaned', orphaned: true, line: 0 }),
      thread({ id: 'out', status: 'outdated' }),
    ];
    const out = inlineThreads(all, specPath, { showResolved: false });
    expect(out.map((t) => t.id)).toEqual(['act']);
  });

  it('shows resolved only when showResolved is set', () => {
    const all = [thread({ id: 'res', status: 'resolved', resolved: true })];
    expect(inlineThreads(all, specPath, { showResolved: false })).toHaveLength(0);
    expect(inlineThreads(all, specPath, { showResolved: true }).map((t) => t.id)).toEqual(['res']);
  });

  it('drops a thread with no resolved line (line <= 0)', () => {
    const all = [thread({ id: 'noline', line: 0 })];
    expect(inlineThreads(all, specPath, { showResolved: true })).toHaveLength(0);
  });
});

describe('activeCount', () => {
  it('counts active && !resolved && !orphaned on the spec', () => {
    const all = [
      thread({ id: '1', status: 'active' }),
      thread({ id: '2', status: 'active', resolved: true }),
      thread({ id: '3', status: 'orphaned', orphaned: true }),
      thread({ id: '4', status: 'active', spec_path: 'other.md' }),
    ];
    expect(activeCount(all, 'a/b.md')).toBe(1);
  });
});

describe('triageThreads', () => {
  it('selects orphaned and outdated across specs', () => {
    const all = [
      thread({ id: 'act', status: 'active' }),
      thread({ id: 'orph', status: 'orphaned', orphaned: true, spec_path: 'x.md' }),
      thread({ id: 'out', status: 'outdated', spec_path: 'y.md' }),
    ];
    expect(triageThreads(all).map((t) => t.id).sort()).toEqual(['orph', 'out']);
  });
});

describe('applyEvent', () => {
  const t1: Thread = thread({ id: '1' });
  const t1b: Thread = thread({ id: '1', status: 'resolved', resolved: true });
  const t2: Thread = thread({ id: '2' });

  it('sync replaces the repo set', () => {
    const next = applyEvent({ repo: [t1] }, { op: 'sync', repo: 'repo', threads: [t2] });
    expect(next.repo.map((t) => t.id)).toEqual(['2']);
  });

  it('upserts a new thread by id', () => {
    const next = applyEvent({ repo: [t1] }, { op: 'create', repo: 'repo', thread: t2 });
    expect(next.repo.map((t) => t.id).sort()).toEqual(['1', '2']);
  });

  it('replaces an existing thread by id (reply/resolve)', () => {
    const next = applyEvent({ repo: [t1] }, { op: 'resolve', repo: 'repo', thread: t1b });
    expect(next.repo).toHaveLength(1);
    expect(next.repo[0].resolved).toBe(true);
  });

  it('does not mutate the input map', () => {
    const input = { repo: [t1] };
    applyEvent(input, { op: 'create', repo: 'repo', thread: t2 });
    expect(input.repo).toHaveLength(1);
  });

  it('ignores an event with no repo', () => {
    const input = { repo: [t1] };
    expect(applyEvent(input, { op: 'create', repo: '', thread: t2 })).toBe(input);
  });
});

describe('outOfSyncCount', () => {
  it('counts orphaned and outdated threads, not active or resolved', () => {
    const all = [
      thread({ id: '1' }),
      thread({ id: '2', orphaned: true }),
      thread({ id: '3', status: 'orphaned' }),
      thread({ id: '4', outdated: true }),
      thread({ id: '5', resolved: true, status: 'resolved' }),
    ];
    expect(outOfSyncCount(all)).toBe(3);
  });

  it('is zero when every thread is in sync', () => {
    expect(outOfSyncCount([thread({ id: '1' }), thread({ id: '2' })])).toBe(0);
  });
});

describe('blockForLine', () => {
  const blocks = [
    { line: 1, top: 0 },
    { line: 5, top: 100 },
    { line: 9, top: 200 },
  ];
  it('snaps to the largest block line <= target', () => {
    expect(blockForLine(blocks, 7)?.line).toBe(5);
    expect(blockForLine(blocks, 5)?.line).toBe(5);
    expect(blockForLine(blocks, 100)?.line).toBe(9);
  });
  it('falls back to the first block when target precedes everything', () => {
    expect(blockForLine(blocks, 0)?.line).toBe(1);
  });
  it('returns null for an empty block list', () => {
    expect(blockForLine([], 3)).toBeNull();
  });
});

describe('threadPreview', () => {
  it('uses the root body, collapses whitespace, and clamps', () => {
    const t = thread({
      id: '1',
      comments: [comment({ id: 'c', body: 'hello\n  world  again', parent_id: '' })],
    });
    expect(threadPreview(t)).toBe('hello world again');
    expect(threadPreview(t, 8)).toBe('hello w…');
  });
});

describe('initials', () => {
  it('takes two-part initials or first two chars', () => {
    expect(initials('jane.doe')).toBe('JD');
    expect(initials('alice')).toBe('AL');
    expect(initials('')).toBe('?');
  });
});
