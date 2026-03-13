/**
 * Smoke tests for the api.js orchestration layer.
 *
 * After the split, the detailed unit tests for pure reducers live in
 * task-stream.test.js, workspace-controller tests live in workspace.test.js,
 * and auth-helper tests live in transport.test.js. This file keeps only the
 * tests that exercise the wiring inside api.js itself:
 *   - SSE stream management (startTasksStream, reconnect with lastTasksEventId)
 *   - Automation toggles (toggleAutopilot error-revert path)
 *   - Archived pagination (loadArchivedTasksPage seam)
 *   - Deep-link hash handling (_handleInitialHash)
 */
import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

// ---------------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------------

function makeInput(initial = false) {
  return { checked: initial, value: '' };
}

/** Build a vm context with all the globals api.js depends on at load time. */
function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Date,
    Math,
    setTimeout,
    clearTimeout,
    Promise,
    EventSource: function MockEventSource() {},
    location: { hash: '' },
    // api() is provided by transport.js. Tests that want to control api()
    // outcomes should either (a) not load transport.js (so this stub is used)
    // or (b) override ctx.api after all scripts are loaded.
    api: overrides.api || vi.fn().mockResolvedValue(null),
    fetch: overrides.fetch,
    showAlert: vi.fn(),
    openModal: vi.fn().mockResolvedValue(undefined),
    setRightTab: vi.fn(),
    setLeftTab: vi.fn(),
    announceBoardStatus: vi.fn(),
    getTaskAccessibleTitle: vi.fn((t) => (t && (t.title || t.id)) || ''),
    formatTaskStatusLabel: vi.fn((s) => s),
    scheduleRender: vi.fn(),
    invalidateDiffBehindCounts: vi.fn(),
    renderWorkspaces: vi.fn(),
    startGitStream: vi.fn(),
    stopGitStream: vi.fn(),
    // workspace.js functions — stub so api.js wiring doesn't crash
    fetchConfig: vi.fn().mockResolvedValue(undefined),
    stopTasksStream: vi.fn(),
    resetBoardState: vi.fn(),
    updateAutomationActiveCount: vi.fn(),
    localStorage: { getItem: vi.fn(), setItem: vi.fn() },
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelectorAll: () => [],
      querySelector: () => null,
      addEventListener: vi.fn(),
      documentElement: { setAttribute: () => {} },
      readyState: 'complete',
    },
    Routes: overrides.Routes || {
      config: { get: () => '/api/config', update: () => '/api/config' },
      tasks: {
        list: () => '/api/tasks',
        stream: () => '/api/tasks/stream',
        task: (id) => ({ update: () => '/api/tasks/' + id }),
      },
    },
    ...overrides,
  };
  ctx.EventSource.CLOSED = 2;
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

/**
 * Load the full module chain: state → transport → task-stream → api.
 * After this call, transport.js's async function api() replaces any ctx.api
 * stub.  Tests that need to control api() outcomes should either call
 * loadApiCoreStack() (skips transport) or reassign ctx.api afterwards.
 */
function loadApiStack(ctx) {
  loadScript(ctx, 'state.js');
  loadScript(ctx, 'transport.js');
  loadScript(ctx, 'task-stream.js');
  loadScript(ctx, 'api.js');
  return ctx;
}

/**
 * Load the minimal chain for api.js without transport.js.
 * The ctx.api stub provided in makeContext() is preserved so tests can
 * control the fetch behaviour directly.
 */
function loadApiCoreStack(ctx) {
  loadScript(ctx, 'state.js');
  loadScript(ctx, 'task-stream.js');
  loadScript(ctx, 'api.js');
  return ctx;
}

function task(id, fields = {}) {
  return {
    id,
    title: fields.title || id,
    status: fields.status || 'backlog',
    archived: !!fields.archived,
    updated_at: fields.updated_at || '2026-03-10T00:00:00Z',
    position: fields.position || 0,
    prompt: fields.prompt || '',
    ...fields,
  };
}

// ---------------------------------------------------------------------------
// startTasksStream — SSE reconnect with lastTasksEventId
// ---------------------------------------------------------------------------

describe('startTasksStream', () => {
  it('reconnects with lastTasksEventId preserved in the next stream URL', () => {
    const instances = [];
    class MockEventSource {
      constructor(url) {
        this.url = url;
        this.readyState = 1;
        this.listeners = {};
        instances.push(this);
      }
      addEventListener(type, handler) { this.listeners[type] = handler; }
      close() { this.closed = true; }
    }
    MockEventSource.CLOSED = 2;

    const scheduled = [];
    const ctx = makeContext({
      EventSource: MockEventSource,
      Routes: {
        tasks: { stream: () => '/api/tasks/stream', list: () => '/api/tasks' },
        config: { get: () => '/api/config', update: () => '/api/config' },
      },
      setTimeout: vi.fn((fn, delay) => { scheduled.push({ fn, delay }); return 1; }),
    });
    loadApiStack(ctx);
    vm.runInContext('activeWorkspaces = ["/Users/test/repo"];', ctx);

    ctx.startTasksStream();
    expect(instances[0].url).toBe('/api/tasks/stream');

    // Simulate a snapshot event that delivers a lastEventId.
    instances[0].listeners.snapshot({
      data: JSON.stringify([task('task-1')]),
      lastEventId: 'evt-1',
    });
    expect(vm.runInContext('lastTasksEventId', ctx)).toBe('evt-1');

    // Simulate connection drop.
    instances[0].readyState = MockEventSource.CLOSED;
    instances[0].onerror();
    expect(scheduled).toHaveLength(1);

    // Let the reconnect timer fire.
    scheduled[0].fn();
    expect(instances[1].url).toBe('/api/tasks/stream?last_event_id=evt-1');
  });

  it('does not open a stream when activeWorkspaces is empty', () => {
    const instances = [];
    class MockEventSource {
      constructor(url) { instances.push(url); this.readyState = 1; this.listeners = {}; }
      addEventListener() {}
      close() {}
    }
    const ctx = makeContext({ EventSource: MockEventSource });
    loadApiStack(ctx);
    vm.runInContext('activeWorkspaces = [];', ctx);
    ctx.startTasksStream();
    expect(instances).toHaveLength(0);
  });
});

// ---------------------------------------------------------------------------
// toggleAutopilot — error-revert regression
// ---------------------------------------------------------------------------

describe('toggleAutopilot', () => {
  it('reverts checkbox and calls showAlert on API failure', async () => {
    const toggle = makeInput(true); // user flipped to true
    const apiFn = vi.fn().mockRejectedValue(new Error('network error'));
    const ctx = makeContext({
      elements: [['autopilot-toggle', toggle]],
      api: apiFn,
    });
    // Use the core stack so that ctx.api (the spy) is not overwritten by transport.js.
    loadApiCoreStack(ctx);
    vm.runInContext('autopilot = false;', ctx);

    await ctx.toggleAutopilot();

    expect(ctx.showAlert).toHaveBeenCalled();
    expect(toggle.checked).toBe(false); // reverted to the pre-flip value
  });
});

// ---------------------------------------------------------------------------
// _handleInitialHash — smoke test (detailed coverage in hash-deeplink.test.js)
// ---------------------------------------------------------------------------

describe('_handleInitialHash', () => {
  it('opens modal for a valid UUID hash', async () => {
    const taskId = '11111111-1111-1111-1111-111111111111';
    const ctx = makeContext({ location: { hash: '#' + taskId } });
    loadApiCoreStack(ctx);
    vm.runInContext(`tasks = [{ id: "${taskId}", title: "T" }]; _hashHandled = false;`, ctx);

    await ctx._handleInitialHash();
    expect(ctx.openModal).toHaveBeenCalledWith(taskId);
  });

  it('is idempotent — ignores calls after the first', async () => {
    const taskId = '22222222-2222-2222-2222-222222222222';
    const ctx = makeContext({ location: { hash: '#' + taskId } });
    loadApiCoreStack(ctx);
    vm.runInContext(`tasks = [{ id: "${taskId}", title: "T" }]; _hashHandled = false;`, ctx);

    await ctx._handleInitialHash();
    await ctx._handleInitialHash();
    expect(ctx.openModal).toHaveBeenCalledTimes(1);
  });
});

// ---------------------------------------------------------------------------
// loadArchivedTasksPage — pagination seam regression tests
// ---------------------------------------------------------------------------

describe('loadArchivedTasksPage', () => {
  // Use the core stack (no transport.js) so the api spy is not overwritten.
  function makeArchiveContext(apiFn) {
    const ctx = makeContext({ api: apiFn });
    loadApiCoreStack(ctx);
    vm.runInContext('showArchived = true;', ctx);
    return ctx;
  }

  it('does nothing when showArchived is false', async () => {
    const apiFn = vi.fn();
    const ctx = makeContext({ api: apiFn });
    loadApiCoreStack(ctx);
    vm.runInContext('showArchived = false;', ctx);

    await ctx.loadArchivedTasksPage('initial');
    expect(apiFn).not.toHaveBeenCalled();
  });

  it('does nothing when loadState is not idle (concurrent guard)', async () => {
    const apiFn = vi.fn();
    const ctx = makeArchiveContext(apiFn);
    vm.runInContext('archivedPage.loadState = "loading-before";', ctx);

    await ctx.loadArchivedTasksPage('initial');
    expect(apiFn).not.toHaveBeenCalled();
  });

  it('fetches the initial archived page and commits results to globals', async () => {
    const archivedTask = task('arch-1', { archived: true, updated_at: '2026-03-10T10:00:00Z' });
    const apiFn = vi.fn().mockResolvedValue({
      tasks: [archivedTask],
      has_more_before: false,
      has_more_after: false,
    });
    const ctx = makeArchiveContext(apiFn);

    await ctx.loadArchivedTasksPage('initial');

    const storedIds = vm.runInContext('archivedTasks.map(t => t.id)', ctx);
    expect(storedIds).toEqual(['arch-1']);
  });

  it('skips the before-page request when there are no existing archived tasks', async () => {
    const apiFn = vi.fn();
    const ctx = makeArchiveContext(apiFn);
    vm.runInContext(`
      archivedTasks = [];
      archivedPage = { loadState: 'idle', hasMoreBefore: true, hasMoreAfter: false };
    `, ctx);

    await ctx.loadArchivedTasksPage('before');
    // Guard: length === 0 prevents fetch even when hasMoreBefore is true.
    expect(apiFn).not.toHaveBeenCalled();
  });

  it('appends cursor param when fetching the next before-page', async () => {
    const apiFn = vi.fn().mockResolvedValue({ tasks: [], has_more_before: false, has_more_after: false });
    const ctx = makeArchiveContext(apiFn);
    vm.runInContext(`
      archivedTasks = [${JSON.stringify(task('arch-oldest', { archived: true }))}];
      archivedPage = { loadState: 'idle', hasMoreBefore: true, hasMoreAfter: false };
    `, ctx);

    await ctx.loadArchivedTasksPage('before');

    const url = apiFn.mock.calls[0][0];
    expect(url).toContain('archived_before=arch-oldest');
  });

  it('resets loadState to idle after a successful fetch', async () => {
    const apiFn = vi.fn().mockResolvedValue({ tasks: [], has_more_before: false, has_more_after: false });
    const ctx = makeArchiveContext(apiFn);

    await ctx.loadArchivedTasksPage('initial');

    const loadState = vm.runInContext('archivedPage.loadState', ctx);
    expect(loadState).toBe('idle');
  });

  it('resets loadState to idle even when the fetch throws', async () => {
    const apiFn = vi.fn().mockRejectedValue(new Error('server error'));
    const ctx = makeArchiveContext(apiFn);

    await ctx.loadArchivedTasksPage('initial');

    const loadState = vm.runInContext('archivedPage.loadState', ctx);
    expect(loadState).toBe('idle');
  });
});

// ---------------------------------------------------------------------------
// toggleShowArchived — archived visibility seam
// ---------------------------------------------------------------------------

describe('toggleShowArchived', () => {
  it('stores the preference in localStorage when enabled', async () => {
    const apiFn = vi.fn().mockResolvedValue({ tasks: [], has_more_before: false, has_more_after: false });
    const toggle = { checked: true };
    const ctx = makeContext({
      api: apiFn,
      elements: [['show-archived-toggle', toggle]],
    });
    loadApiCoreStack(ctx);
    vm.runInContext('tasksSource = null;', ctx); // no active stream

    ctx.toggleShowArchived();

    expect(ctx.localStorage.setItem).toHaveBeenCalledWith('wallfacer-show-archived', 'true');
  });

  it('clears the archived window and triggers render when disabled', () => {
    const toggle = { checked: false };
    const ctx = makeContext({ elements: [['show-archived-toggle', toggle]] });
    loadApiCoreStack(ctx);
    vm.runInContext(`
      archivedTasks = [${JSON.stringify(task('arch-1', { archived: true }))}];
      showArchived = true;
      tasksSource = null;
    `, ctx);

    ctx.toggleShowArchived();

    expect(ctx.localStorage.setItem).toHaveBeenCalledWith('wallfacer-show-archived', 'false');
    const remaining = vm.runInContext('archivedTasks.length', ctx);
    expect(remaining).toBe(0);
  });
});
