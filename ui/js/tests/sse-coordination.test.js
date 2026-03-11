/**
 * Tests for the SSE-coordination helper (waitForTaskDelta) added to api.js.
 *
 * Three scenarios are covered:
 *  1. Happy path: an action resolves from an incoming SSE delta; fetchTasks is
 *     never called.
 *  2. Fallback: when the stream is absent, waitForTaskDelta falls back to
 *     fetchTasks() immediately.
 *  3. Regression: openRaiseLimitInline now uses task(id).update() instead of
 *     the non-existent Routes.tasks.update(id), so no TypeError is thrown and
 *     the PATCH reaches the correct URL.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

function makeElement(overrides = {}) {
  const el = {
    value: overrides.value || '',
    checked: overrides.checked || false,
    textContent: '',
    innerHTML: '',
    style: {},
    dataset: {},
    classList: {
      _set: new Set(),
      add(c) { this._set.add(c); },
      remove(c) { this._set.delete(c); },
      contains(c) { return this._set.has(c); },
      toggle(c, f) {
        if (f === undefined) {
          if (this._set.has(c)) { this._set.delete(c); return false; }
          this._set.add(c); return true;
        }
        if (f) { this._set.add(c); return true; }
        this._set.delete(c); return false;
      },
    },
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    focus: vi.fn(),
    ...overrides,
  };
  return el;
}

/**
 * A minimal mock EventSource whose readyState defaults to OPEN (1).
 * Listeners can be added/removed and events can be fired manually via fire().
 */
function makeMockEventSource(readyState = 1) {
  const listeners = {};
  const source = {
    readyState,
    listeners,
    addEventListener(type, fn) {
      listeners[type] = listeners[type] || [];
      listeners[type].push(fn);
    },
    removeEventListener(type, fn) {
      if (!listeners[type]) return;
      listeners[type] = listeners[type].filter((f) => f !== fn);
    },
    close() { this.readyState = 2; },
    /** Synchronously dispatch a fake SSE event to all registered listeners. */
    fire(type, data) {
      const event = { data: JSON.stringify(data), lastEventId: '' };
      (listeners[type] || []).forEach((fn) => fn(event));
    },
  };
  return source;
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Date,
    Math,
    setTimeout: overrides.setTimeout || globalThis.setTimeout,
    clearTimeout: overrides.clearTimeout || globalThis.clearTimeout,
    Promise,
    EventSource: function MockEventSource() {},
    location: { hash: '' },
    fetch: overrides.fetch || vi.fn(),
    showAlert: vi.fn(),
    openModal: vi.fn().mockResolvedValue(undefined),
    setRightTab: vi.fn(),
    setLeftTab: vi.fn(),
    closeModal: vi.fn(),
    getOpenModalTaskId: vi.fn(),
    announceBoardStatus: vi.fn(),
    getTaskAccessibleTitle: vi.fn((t) => t && (t.title || t.id) || ''),
    formatTaskStatusLabel: vi.fn((s) => s),
    scheduleRender: vi.fn(),
    invalidateDiffBehindCounts: vi.fn(),
    renderMarkdown: vi.fn((s) => s),
    populateSandboxSelects: vi.fn(),
    updateIdeationConfig: vi.fn(),
    collectSandboxByActivity: vi.fn(() => ({})),
    api: overrides.api || vi.fn().mockResolvedValue(null),
    fetchTasks: overrides.fetchTasks || vi.fn().mockResolvedValue(undefined),
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelectorAll: () => [],
      querySelector: () => null,
      addEventListener: vi.fn(),
      documentElement: { setAttribute: vi.fn() },
      readyState: 'complete',
    },
    Routes: overrides.Routes || {
      tasks: {
        list: () => '/api/tasks',
        stream: () => '/api/tasks/stream',
        archiveDone: () => '/api/tasks/archive-done',
        generateTitles: () => '/api/tasks/generate-titles',
        generateOversight: () => '/api/tasks/generate-oversight',
        create: () => '/api/tasks',
        task: function(id) {
          return {
            update: () => '/api/tasks/' + id,
            delete: () => '/api/tasks/' + id,
            feedback: () => '/api/tasks/' + id + '/feedback',
            done: () => '/api/tasks/' + id + '/done',
            cancel: () => '/api/tasks/' + id + '/cancel',
            resume: () => '/api/tasks/' + id + '/resume',
            archive: () => '/api/tasks/' + id + '/archive',
            unarchive: () => '/api/tasks/' + id + '/unarchive',
            sync: () => '/api/tasks/' + id + '/sync',
            test: () => '/api/tasks/' + id + '/test',
            diff: () => '/api/tasks/' + id + '/diff',
            refine: () => '/api/tasks/' + id + '/refine',
            refineLogs: () => '/api/tasks/' + id + '/refine/logs',
            refineApply: () => '/api/tasks/' + id + '/refine/apply',
            refineDismiss: () => '/api/tasks/' + id + '/refine/dismiss',
            oversight: () => '/api/tasks/' + id + '/oversight',
          };
        },
      },
      config: { get: () => '/api/config', update: () => '/api/config' },
    },
    localStorage: {
      getItem: vi.fn(),
      setItem: vi.fn(),
      removeItem: vi.fn(),
    },
    ...overrides,
  };
  // Alias task() at the top level (mirrors routes.js: var task = Routes.tasks.task)
  if (!ctx.task) ctx.task = ctx.Routes.tasks.task;
  // Propagate EventSource.CLOSED constant
  ctx.EventSource.CLOSED = 2;
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

// ---------------------------------------------------------------------------
// Test 1 — Happy path: SSE delta resolves the wait; fetchTasks is not called
// ---------------------------------------------------------------------------

describe('waitForTaskDelta — SSE delta resolves without fetchTasks', () => {
  it('resolves from a task-updated SSE event and never calls fetchTasks', async () => {
    const ctx = makeContext();

    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    // Override the script-defined fetchTasks with a spy so we can assert on it.
    // api.js defines fetchTasks in the VM scope, so we reassign after loading.
    const fetchTasks = vi.fn().mockResolvedValue(undefined);
    ctx.fetchTasks = fetchTasks;

    // Plant a connected mock EventSource as tasksSource.
    const source = makeMockEventSource(1 /* OPEN */);
    vm.runInContext('tasksSource = source;', Object.assign(ctx, { source }));

    const taskId = 'aaaaaaaa-0000-0000-0000-000000000001';
    const deltaPromise = ctx.waitForTaskDelta(taskId, 5000);

    // Simulate the server broadcasting a task-updated event for our task.
    source.fire('task-updated', { id: taskId, status: 'done' });

    await deltaPromise;

    expect(fetchTasks).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// Test 2 — Fallback: stream absent → fetchTasks is called immediately
// ---------------------------------------------------------------------------

describe('waitForTaskDelta — stream absent triggers fetchTasks fallback', () => {
  it('calls fetchTasks when tasksSource is null', async () => {
    const ctx = makeContext();

    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    // Override the script-defined fetchTasks with a spy AFTER loading so that
    // waitForTaskDelta (also defined in api.js) looks up the spy at call time.
    const fetchTasks = vi.fn().mockResolvedValue(undefined);
    ctx.fetchTasks = fetchTasks;

    // Ensure tasksSource is null (no active stream).
    vm.runInContext('tasksSource = null;', ctx);

    await ctx.waitForTaskDelta('aaaaaaaa-0000-0000-0000-000000000002', 5000);

    expect(fetchTasks).toHaveBeenCalledOnce();
  });

  it('calls fetchTasks when tasksSource is CLOSED', async () => {
    const ctx = makeContext();

    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    // Override the script-defined fetchTasks with a spy AFTER loading.
    const fetchTasks = vi.fn().mockResolvedValue(undefined);
    ctx.fetchTasks = fetchTasks;

    const closed = makeMockEventSource(2 /* CLOSED */);
    vm.runInContext('tasksSource = source;', Object.assign(ctx, { source: closed }));

    await ctx.waitForTaskDelta('aaaaaaaa-0000-0000-0000-000000000003', 5000);

    expect(fetchTasks).toHaveBeenCalledOnce();
  });
});

// ---------------------------------------------------------------------------
// Test 3 — Regression: openRaiseLimitInline uses task(id).update() and no
//           longer crashes with a TypeError for the missing Routes.tasks.update
// ---------------------------------------------------------------------------

describe('openRaiseLimitInline — uses task(id).update() route helper', () => {
  it('PATCHes /api/tasks/{id} and does not call a nonexistent Routes.tasks.update', async () => {
    const TASK_ID = 'bbbbbbbb-0000-0000-0000-000000000001';

    const apiMock = vi.fn().mockResolvedValue(null);
    const waitForTaskDelta = vi.fn().mockResolvedValue(undefined);
    const fetchTasks = vi.fn().mockResolvedValue(undefined);

    // Provide the broken route helper explicitly absent at the collection level
    // to demonstrate the old code would have thrown.
    const RoutesWithoutCollectionUpdate = {
      tasks: {
        list: () => '/api/tasks',
        stream: () => '/api/tasks/stream',
        archiveDone: () => '/api/tasks/archive-done',
        generateTitles: () => '/api/tasks/generate-titles',
        generateOversight: () => '/api/tasks/generate-oversight',
        create: () => '/api/tasks',
        // NOTE: no update() at the collection level — the old bug
        task: function(id) {
          return {
            update: () => '/api/tasks/' + id,
            delete: () => '/api/tasks/' + id,
            feedback: () => '/api/tasks/' + id + '/feedback',
            done: () => '/api/tasks/' + id + '/done',
            cancel: () => '/api/tasks/' + id + '/cancel',
            resume: () => '/api/tasks/' + id + '/resume',
            archive: () => '/api/tasks/' + id + '/archive',
            unarchive: () => '/api/tasks/' + id + '/unarchive',
            sync: () => '/api/tasks/' + id + '/sync',
            test: () => '/api/tasks/' + id + '/test',
            diff: () => '/api/tasks/' + id + '/diff',
            refine: () => '/api/tasks/' + id + '/refine',
            refineLogs: () => '/api/tasks/' + id + '/refine/logs',
            refineApply: () => '/api/tasks/' + id + '/refine/apply',
            refineDismiss: () => '/api/tasks/' + id + '/refine/dismiss',
            oversight: () => '/api/tasks/' + id + '/oversight',
          };
        },
      },
      config: { get: () => '/api/config', update: () => '/api/config' },
    };

    const banner = makeElement({ id: 'modal-budget-exceeded-banner' });
    const ctx = makeContext({
      api: apiMock,
      waitForTaskDelta,
      fetchTasks,
      Routes: RoutesWithoutCollectionUpdate,
      getOpenModalTaskId: vi.fn().mockReturnValue(TASK_ID),
      // prompt() is used by openRaiseLimitInline to ask for new limits.
      prompt: vi.fn()
        .mockReturnValueOnce('10.00')  // new cost limit
        .mockReturnValueOnce('50000'), // new token limit
      elements: [
        ['modal-budget-exceeded-banner', banner],
      ],
    });
    ctx.task = RoutesWithoutCollectionUpdate.tasks.task;

    loadScript(ctx, 'state.js');

    // Seed tasks array so openRaiseLimitInline can find the task.
    vm.runInContext(
      `tasks = [{ id: "${TASK_ID}", max_cost_usd: 0, max_input_tokens: 0 }];`,
      ctx,
    );

    // Provide minimal stubs required by tasks.js module-level code.
    ctx.document.getElementById = (id) => {
      if (id === 'modal-budget-exceeded-banner') return banner;
      if (id === 'modal-edit-prompt') return makeElement({ addEventListener: vi.fn() });
      if (id === 'modal-edit-timeout') return makeElement({ addEventListener: vi.fn() });
      return null;
    };

    loadScript(ctx, 'tasks.js');

    // After loading tasks.js, replace fetchTasks and waitForTaskDelta with mocks
    // so we can observe calls made from within the script scope.
    ctx.fetchTasks = fetchTasks;
    ctx.waitForTaskDelta = waitForTaskDelta;

    // Should not throw — old code would throw TypeError: Routes.tasks.update is not a function
    await ctx.openRaiseLimitInline();

    // The PATCH must target the per-task update URL.
    expect(apiMock).toHaveBeenCalledWith(
      `/api/tasks/${TASK_ID}`,
      expect.objectContaining({ method: 'PATCH' }),
    );

    // The new code delegates state refresh to waitForTaskDelta, not fetchTasks.
    expect(waitForTaskDelta).toHaveBeenCalledWith(TASK_ID);
    expect(fetchTasks).not.toHaveBeenCalled();
  });
});
