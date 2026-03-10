import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function loadScript(filename, ctx) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
}

function createContext(options = {}) {
  const taskListeners = {};
  class MockEventSource {
    constructor(url) {
      this.url = url;
      this.readyState = 1;
      this.listeners = taskListeners;
      MockEventSource.instance = this;
    }

    addEventListener(name, handler) {
      if (!this.listeners[name]) this.listeners[name] = [];
      this.listeners[name].push(handler);
    }

    close() {}
  }
  MockEventSource.CLOSED = 2;

  const ctx = vm.createContext({
    module: { exports: {} },
    exports: {},
    console,
    Date,
    Math,
    JSON,
    Promise,
    setTimeout: vi.fn(),
    clearTimeout: vi.fn(),
    requestAnimationFrame: (cb) => cb(),
    localStorage: {
      getItem: () => null,
      setItem: () => {},
    },
    window: {
      depGraphEnabled: false,
      location: { hash: '' },
    },
    location: { hash: '' },
    document: {
      getElementById: () => null,
      createElement: () => ({ innerHTML: '' }),
      querySelectorAll: () => [],
      addEventListener: () => {},
      readyState: 'complete',
    },
    tasks: [],
    archivedTasks: [],
    showArchived: false,
    backlogSortMode: 'manual',
    filterQuery: '',
    maxParallelTasks: 0,
    ensureArchivedScrollBinding: () => {},
    loadArchivedTasksPage: vi.fn(),
    resetArchivedWindow: vi.fn(),
    sortArchivedByUpdatedDesc: (items) => items,
    trimArchivedWindow: () => {},
    scheduleRender: vi.fn(),
    announceBoardStatus: vi.fn(),
    getTaskAccessibleTitle: (task) => task.title || task.prompt || task.id,
    formatTaskStatusLabel: (status) => String(status || '').replace(/_/g, ' '),
    openModal: vi.fn(() => Promise.resolve()),
    setRightTab: vi.fn(),
    setLeftTab: vi.fn(),
    _hashHandled: false,
    tasksRetryDelay: 1000,
    tasksSource: null,
    lastTasksEventId: null,
    archivedPage: { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false },
    archivedTasksPageSize: 20,
    archivedScrollHandlerBound: false,
    Routes: {
      tasks: {
        stream: () => '/api/tasks/stream',
        list: () => '/api/tasks',
      },
    },
    EventSource: MockEventSource,
    api: vi.fn(),
    escapeHtml: (s) => String(s || ''),
    renderMarkdown: (s) => String(s || ''),
    matchesFilter: () => true,
    updateIdeationFromTasks: () => {},
    updateBacklogSortButton: () => {},
    updateRefineUI: () => {},
    renderRefineHistory: () => {},
    hideDependencyGraph: () => {},
    renderDependencyGraph: () => {},
    sandboxDisplayName: (s) => s || 'Default',
    formatTimeout: (m) => String(m || 5),
    timeAgo: () => 'just now',
    highlightMatch: (text) => text || '',
    taskDisplayPrompt: (task) => (task ? task.prompt : ''),
    syncTask: vi.fn(),
    ...options,
  });

  return { ctx, taskListeners, MockEventSource };
}

function loadRenderHarness(options = {}) {
  const harness = createContext(options);
  loadScript('render.js', harness.ctx);
  return { ...harness, renderExports: harness.ctx.module.exports };
}

function loadRenderAndApiHarness(options = {}) {
  const harness = createContext(options);
  loadScript('render.js', harness.ctx);
  const renderExports = harness.ctx.module.exports;
  loadScript('api.js', harness.ctx);
  return { ...harness, renderExports };
}

describe('render.js dependency helpers', () => {
  let ctx;
  let renderExports;

  beforeEach(() => {
    ({ ctx, renderExports } = loadRenderHarness());
    ctx.tasks = [];
    ctx.archivedTasks = [];
    renderExports.diffCache.clear();
    renderExports.cardOversightCache.clear();
  });

  it('returns false when depends_on is absent', () => {
    expect(renderExports.areDepsBlocked({ id: 'task-a' })).toBe(false);
  });

  it('returns false when all dependencies are present and done', () => {
    ctx.tasks = [
      { id: 'dep-1', status: 'done' },
      { id: 'dep-2', status: 'done' },
    ];

    expect(renderExports.areDepsBlocked({ id: 'task-a', depends_on: ['dep-1', 'dep-2'] })).toBe(false);
  });

  it('returns true when one dependency is still in progress', () => {
    ctx.tasks = [
      { id: 'dep-1', status: 'done' },
      { id: 'dep-2', status: 'in_progress' },
    ];

    expect(renderExports.areDepsBlocked({ id: 'task-a', depends_on: ['dep-1', 'dep-2'] })).toBe(true);
  });

  it('returns true when a dependency id is missing from the task list', () => {
    ctx.tasks = [{ id: 'dep-1', status: 'done' }];

    expect(renderExports.areDepsBlocked({ id: 'task-a', depends_on: ['dep-1', 'missing-dep'] })).toBe(true);
  });

  it('returns only non-done dependency names', () => {
    ctx.tasks = [
      { id: 'dep-1', status: 'done', title: 'Finished task', prompt: 'done prompt' },
      { id: 'dep-2', status: 'in_progress', title: 'Active task', prompt: 'active prompt' },
      { id: 'dep-3', status: 'failed', title: '', prompt: 'Needs manual fix' },
    ];

    const names = renderExports.getBlockingTaskNames({ id: 'task-a', depends_on: ['dep-1', 'dep-2', 'dep-3'] });

    expect(names).toBe('Active task, Needs manual fix');
  });
});

describe('render.js isTestCard', () => {
  let renderExports;

  beforeEach(() => {
    ({ renderExports } = loadRenderHarness());
  });

  it('returns true for tasks with a last test result and positive start turn', () => {
    expect(renderExports.isTestCard({ last_test_result: 'pass', test_run_start_turn: 1 })).toBe(true);
  });

  it('returns false when last_test_result is null', () => {
    expect(renderExports.isTestCard({ last_test_result: null, test_run_start_turn: 1 })).toBe(false);
  });

  it('returns false when test_run_start_turn is zero', () => {
    expect(renderExports.isTestCard({ last_test_result: 'pass', test_run_start_turn: 0 })).toBe(false);
  });

  it('does not gate on task status', () => {
    expect(renderExports.isTestCard({
      status: 'backlog',
      last_test_result: 'pass',
      test_run_start_turn: 2,
    })).toBe(true);
  });
});

describe('render.js diffCache', () => {
  let ctx;
  let renderExports;

  beforeEach(() => {
    ({ ctx, renderExports } = loadRenderHarness());
    renderExports.diffCache.clear();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('invalidates only the requested task cache entry', () => {
    renderExports.diffCache.set('task-a', { diff: 'a', behindCounts: {}, updatedAt: 'u1', behindFetchedAt: 10 });
    renderExports.diffCache.set('task-b', { diff: 'b', behindCounts: {}, updatedAt: 'u2', behindFetchedAt: 20 });

    renderExports.invalidateDiffBehindCounts('task-a');

    expect(renderExports.diffCache.has('task-a')).toBe(false);
    expect(renderExports.diffCache.get('task-b')).toEqual({
      diff: 'b',
      behindCounts: {},
      updatedAt: 'u2',
      behindFetchedAt: 20,
    });
  });

  it('treats behind-counts as stale after the TTL expires', async () => {
    vi.useFakeTimers();
    ({ ctx, renderExports } = loadRenderHarness());
    renderExports.diffCache.clear();

    const updatedAt = '2026-03-10T00:00:00Z';
    renderExports.diffCache.set('task-a', {
      diff: 'cached diff',
      behindCounts: { repo: 1 },
      updatedAt,
      behindFetchedAt: Date.now(),
    });
    ctx.api.mockResolvedValue({ diff: 'fresh diff', behind_counts: { repo: 2 } });

    vi.advanceTimersByTime(renderExports.BEHIND_TTL_MS + 1);

    await ctx.fetchDiff({ querySelector: () => null }, 'task-a', updatedAt);

    expect(ctx.api).toHaveBeenCalledWith('/api/tasks/task-a/diff');
    expect(renderExports.diffCache.get('task-a')).toMatchObject({
      diff: 'fresh diff',
      behindCounts: { repo: 2 },
      updatedAt,
    });
  });
});

describe('render.js cardOversightCache', () => {
  let ctx;
  let renderExports;

  beforeEach(() => {
    ({ ctx, renderExports } = loadRenderAndApiHarness());
    ctx.tasks = [{ id: 'task-a', status: 'waiting', title: 'Task A' }];
    ctx.archivedTasks = [];
    renderExports.cardOversightCache.clear();
  });

  it('evicts the updated task before the next render cycle on SSE updates', () => {
    renderExports.cardOversightCache.set('task-a', { phase_count: 1, phases: [{ title: 'Cached' }] });
    ctx.scheduleRender = vi.fn(() => {
      expect(renderExports.cardOversightCache.has('task-a')).toBe(false);
    });

    ctx.startTasksStream();
    const handler = ctx.EventSource.instance.listeners['task-updated'][0];
    handler({
      data: JSON.stringify({ id: 'task-a', status: 'done', title: 'Task A', updated_at: '2026-03-10T00:00:00Z' }),
      lastEventId: 'evt-1',
    });

    expect(renderExports.cardOversightCache.has('task-a')).toBe(false);
    expect(ctx.scheduleRender).toHaveBeenCalledTimes(1);
  });
});
