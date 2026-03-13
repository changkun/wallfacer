/**
 * Tests for api.js helpers used across task routing and sandbox config.
 */
import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function makeInput(initial = false) {
  return { checked: initial, value: '' };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Date,
    Math,
    setTimeout,
    clearTimeout,
    EventSource: function EventSource() {},
    localStorage: {
      getItem: vi.fn(),
      setItem: vi.fn(),
    },
    location: { hash: '' },
    fetch: overrides.fetch,
    showAlert: vi.fn(),
    openModal: vi.fn().mockResolvedValue(undefined),
    setRightTab: vi.fn(),
    setLeftTab: vi.fn(),
    collectSandboxByActivity: () => ({}),
    populateSandboxSelects: vi.fn(),
    updateIdeationConfig: vi.fn(),
    api: vi.fn(),
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelectorAll: (selector) => {
        if (selector.includes('[data-sandbox-select]')) return elements.get('sandbox-selects') || [];
        return [];
      },
      querySelector: () => null,
      addEventListener: () => {},
      documentElement: { setAttribute: () => {} },
      readyState: 'complete',
    },
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
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

describe('sandbox helpers', () => {
  it('formats sandbox labels consistently', () => {
    const ctx = makeContext();
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    expect(ctx.sandboxDisplayName('')).toBe('Default');
    expect(ctx.sandboxDisplayName('claude')).toBe('Claude');
    expect(ctx.sandboxDisplayName('codex')).toBe('Codex');
    expect(ctx.sandboxDisplayName('custom')).toBe('Custom');
  });

  it('collects and applies sandbox overrides by activity', () => {
    const ctx = makeContext({
      elements: [
        ['env-sandbox-implementation', { value: 'claude' }],
        ['env-sandbox-testing', { value: 'codex' }],
      ],
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    const collected = ctx.collectSandboxByActivity('env-sandbox-');
    expect(collected).toEqual({ implementation: 'claude', testing: 'codex' });

    const unknown = { value: 'openai' };
    const impl = { value: 'claude' };
    const testing = { value: 'codex' };
    const ctxWithTargets = makeContext({
      elements: [
        ['env-sandbox-implementation', impl],
        ['env-sandbox-testing', testing],
        ['env-sandbox-testing-2', { value: 'ignored' }],
      ],
    });
    loadScript(ctxWithTargets, 'state.js');
    loadScript(ctxWithTargets, 'api.js');
    ctxWithTargets.applySandboxByActivity('env-sandbox-', { implementation: 'custom', oversight: 'codex' });
    expect(impl.value).toBe('custom');
    expect(testing.value).toBe('');
    expect(unknown.value).toBe('openai');
  });
});

describe('_handleInitialHash', () => {
  it('opens modal for valid hash and maps right-hand tab', async () => {
    const ctx = makeContext({
      elements: [
        ['ideation-next-run', { textContent: '', style: {} }],
      ],
      location: { hash: '' },
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    vm.runInContext('tasks = [{ id: "11111111-1111-1111-1111-111111111111", title: "Task" }]; _hashHandled = false;', ctx);

    ctx.location.hash = '#11111111-1111-1111-1111-111111111111/testing';
    await ctx._handleInitialHash();

    expect(ctx.openModal).toHaveBeenCalledWith('11111111-1111-1111-1111-111111111111');
    expect(ctx.setRightTab).toHaveBeenCalledWith('testing');
  });

  it('ignores invalid hash values', async () => {
    const ctx = makeContext({
      location: { hash: '#not-a-uuid' },
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    ctx.tasks = [{ id: '11111111-1111-1111-1111-111111111111', title: 'Task' }];

    await ctx._handleInitialHash();
    expect(ctx.openModal).not.toHaveBeenCalled();
  });

  it('opens modal when task exists in archived window', async () => {
    const ctx = makeContext({
      location: { hash: '#22222222-2222-2222-2222-222222222222' },
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    vm.runInContext('tasks = []; archivedTasks = [{ id: "22222222-2222-2222-2222-222222222222", title: "Archived task" }]; _hashHandled = false;', ctx);

    await ctx._handleInitialHash();
    expect(ctx.openModal).toHaveBeenCalledWith('22222222-2222-2222-2222-222222222222');
  });
});

describe('fetchConfig', () => {
  it('hydrates client config state and applies sandbox selectors', async () => {
    const cfg = {
      autopilot: true,
      autotest: true,
      autosubmit: false,
      workspaces: ['/Users/test/repo'],
      workspace_browser_path: '/Users/test/repo',
      sandboxes: ['claude', 'codex'],
      default_sandbox: 'claude',
      activity_sandboxes: { implementation: 'codex' },
      sandbox_usable: { claude: true },
      sandbox_reasons: { codex: 'Missing token' },
    };
    const autopilotToggle = makeInput(false);
    const autotestToggle = makeInput(false);
    const autosubmitToggle = makeInput(false);
    class MockEventSource {
      constructor(url) {
        this.url = url;
        this.readyState = 1;
      }
      addEventListener() {}
      close() {}
    }
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => cfg,
      text: async () => '',
    });
    const ctx = makeContext({
      elements: [
        ['autopilot-toggle', autopilotToggle],
        ['autotest-toggle', autotestToggle],
        ['autosubmit-toggle', autosubmitToggle],
      ],
      EventSource: MockEventSource,
      fetch,
      renderWorkspaces: vi.fn(),
      startGitStream: vi.fn(),
      startTasksStream: vi.fn(),
      stopTasksStream: vi.fn(),
      stopGitStream: vi.fn(),
      resetBoardState: vi.fn(),
      Routes: {
        config: {
          get: () => '/api/config',
          update: () => '/api/config',
        },
        tasks: {
          stream: () => '/api/tasks/stream',
        },
        git: {
          stream: () => '/api/git/stream',
        },
      },
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    const populateSandboxByActivitySpy = vi.spyOn(ctx, 'populateSandboxSelects');

    await ctx.fetchConfig();

    expect(autopilotToggle.checked).toBe(true);
    expect(autotestToggle.checked).toBe(true);
    expect(autosubmitToggle.checked).toBe(false);
    expect(populateSandboxByActivitySpy).toHaveBeenCalled();
    expect(ctx.updateIdeationConfig).toHaveBeenCalledWith(cfg);
    expect(vm.runInContext('autopilot', ctx)).toBe(true);
    expect(vm.runInContext('autotest', ctx)).toBe(true);
    expect(vm.runInContext('autosubmit', ctx)).toBe(false);
    expect(vm.runInContext('workspaceBrowserPath', ctx)).toBe('/Users/test/repo');
  });

  it('prefers workspace_browser_path from config over an empty picker path', async () => {
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({
        workspaces: [],
        workspace_browser_path: '/Users/test/current',
      }),
      text: async () => '',
    });
    const ctx = makeContext({
      fetch,
      renderWorkspaces: vi.fn(),
      scheduleRender: vi.fn(),
      stopTasksStream: vi.fn(),
      stopGitStream: vi.fn(),
      resetBoardState: vi.fn(),
      Routes: {
        config: {
          get: () => '/api/config',
          update: () => '/api/config',
        },
      },
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    await ctx.fetchConfig();

    expect(vm.runInContext('workspaceBrowserPath', ctx)).toBe('/Users/test/current');
  });
});

describe('showWorkspacePicker', () => {
  it('refreshes the workspace browser every time the picker opens', () => {
    const modal = { classList: { remove: vi.fn(), add: vi.fn() } };
    const closeBtn = { style: {} };
    const filterInput = { value: 'repo' };
    const ctx = makeContext({
      elements: [
        ['workspace-picker', modal],
        ['workspace-picker-close', closeBtn],
        ['workspace-browser-filter', filterInput],
      ],
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    const browseSpy = vi.spyOn(ctx, 'browseWorkspaces').mockImplementation(() => {});
    vm.runInContext('workspaceBrowserPath = "/Users/test/dev"; workspaceBrowserFilterQuery = "repo"; activeWorkspaces = []; workspaceSelectionDraft = [];', ctx);

    ctx.showWorkspacePicker(true);

    expect(browseSpy).toHaveBeenCalledWith('/Users/test/dev');
    expect(closeBtn.style.display).toBe('none');
    expect(filterInput.value).toBe('');
    expect(vm.runInContext('workspaceBrowserFilterQuery', ctx)).toBe('');
  });
});

describe('renderWorkspaceSelectionDraft', () => {
  it('renders a safe remove button handler for selected paths', () => {
    const listEl = { innerHTML: '' };
    const ctx = makeContext({
      elements: [['workspace-selection-list', listEl]],
    });
    loadScript(ctx, 'utils.js');
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    vm.runInContext('workspaceSelectionDraft = ["/Users/test/dev/repo"];', ctx);
    ctx.renderWorkspaceSelectionDraft();

    expect(listEl.innerHTML).toContain('data-workspace-path="/Users/test/dev/repo"');
    expect(listEl.innerHTML).toContain('onclick="removeWorkspaceSelection(this.dataset.workspacePath)"');
  });
});

describe('browseWorkspaces', () => {
  it('skips hidden folders by default', async () => {
    const pathInput = { value: '/Users/test/dev' };
    const statusEl = { textContent: '' };
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ path: '/Users/test/dev', entries: [] }),
      text: async () => '',
    });
    const ctx = makeContext({
      elements: [
        ['workspace-browser-path', pathInput],
        ['workspace-browser-status', statusEl],
        ['workspace-browser-list', { innerHTML: '' }],
        ['workspace-browser-breadcrumb', { textContent: '' }],
        ['workspace-browser-include-hidden', { checked: false }],
      ],
      fetch,
      Routes: {
        workspaces: {
          browse: () => '/api/workspaces/browse',
        },
      },
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    await ctx.browseWorkspaces();

    expect(fetch).toHaveBeenCalledWith('/api/workspaces/browse?path=%2FUsers%2Ftest%2Fdev', expect.any(Object));
  });

  it('includes hidden folders when the toggle is enabled', async () => {
    const pathInput = { value: '/Users/test/dev' };
    const statusEl = { textContent: '' };
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ path: '/Users/test/dev', entries: [] }),
      text: async () => '',
    });
    const ctx = makeContext({
      elements: [
        ['workspace-browser-path', pathInput],
        ['workspace-browser-status', statusEl],
        ['workspace-browser-list', { innerHTML: '' }],
        ['workspace-browser-breadcrumb', { textContent: '' }],
        ['workspace-browser-include-hidden', { checked: true }],
      ],
      fetch,
      Routes: {
        workspaces: {
          browse: () => '/api/workspaces/browse',
        },
      },
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    await ctx.browseWorkspaces();

    expect(fetch).toHaveBeenCalledWith('/api/workspaces/browse?path=%2FUsers%2Ftest%2Fdev&include_hidden=true', expect.any(Object));
  });
});

describe('workspace browser filter', () => {
  it('filters the visible folder list client-side', () => {
    const listEl = { innerHTML: '' };
    const crumbEl = { textContent: '' };
    const ctx = makeContext({
      elements: [
        ['workspace-browser-list', listEl],
        ['workspace-browser-breadcrumb', crumbEl],
      ],
    });
    loadScript(ctx, 'utils.js');
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    vm.runInContext(`
      workspaceBrowserPath = "/Users/test/dev";
      workspaceBrowserEntries = [
        { name: "alpha-repo", path: "/Users/test/dev/alpha-repo", is_git_repo: true },
        { name: "beta-tools", path: "/Users/test/dev/beta-tools", is_git_repo: false },
        { name: "gamma-app", path: "/Users/test/dev/gamma-app", is_git_repo: true }
      ];
      workspaceBrowserFocusIndex = 0;
    `, ctx);

    ctx.setWorkspaceBrowserFilter('app');

    expect(listEl.innerHTML).toContain('gamma-app');
    expect(listEl.innerHTML).not.toContain('alpha-repo');
    expect(listEl.innerHTML).not.toContain('beta-tools');
    expect(vm.runInContext('workspaceBrowserFocusIndex', ctx)).toBe(0);
  });

  it('adds the highlighted folder on Enter', () => {
    const ctx = makeContext();
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    vm.runInContext(`
      workspaceBrowserEntries = [
        { name: "alpha-repo", path: "/Users/test/dev/alpha-repo", is_git_repo: true },
        { name: "beta-tools", path: "/Users/test/dev/beta-tools", is_git_repo: false }
      ];
      workspaceBrowserFocusIndex = 1;
      workspaceSelectionDraft = [];
    `, ctx);

    ctx.workspaceBrowserListKeydown({
      key: 'Enter',
      preventDefault: vi.fn(),
      metaKey: false,
      ctrlKey: false,
    });

    expect(vm.runInContext('workspaceSelectionDraft.slice()', ctx)).toEqual(['/Users/test/dev/beta-tools']);
  });
});

describe('toggleAutopilot', () => {
  it('updates autopilot and reverts checkbox on API failure', async () => {
    const toggle = makeInput(false);
    const api = vi.fn().mockRejectedValueOnce(new Error('network down'));
    const ctx = makeContext({
      elements: [['autopilot-toggle', toggle]],
      api,
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    ctx.autopilot = false;
    await ctx.toggleAutopilot();
    expect(ctx.showAlert).toHaveBeenCalled();
    expect(toggle.checked).toBe(false);
  });
});

describe('task stream reducers', () => {
  it('moves an archived update from active tasks into archived tasks after a snapshot', () => {
    const ctx = makeContext();
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    const snapshot = ctx.applyTasksSnapshot({
      tasks: [],
      archivedTasks: [],
      archivedPage: { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false },
    }, [task('task-1', { updated_at: '2026-03-10T10:00:00Z' })]);
    const reduced = ctx.applyTaskUpdated(snapshot, task('task-1', {
      archived: true,
      updated_at: '2026-03-10T11:00:00Z',
    }), { showArchived: true, pageSize: 20 });

    expect(reduced.state.tasks).toEqual([]);
    expect(reduced.state.archivedTasks.map((t) => t.id)).toEqual(['task-1']);
  });

  it('removes an unarchived task from archivedTasks and restores it to active tasks', () => {
    const ctx = makeContext();
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    const reduced = ctx.applyTaskUpdated({
      tasks: [],
      archivedTasks: [task('task-1', { archived: true, updated_at: '2026-03-10T11:00:00Z' })],
      archivedPage: { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false },
    }, task('task-1', {
      archived: false,
      status: 'done',
      updated_at: '2026-03-10T12:00:00Z',
    }), { showArchived: true, pageSize: 20 });

    expect(reduced.state.tasks.map((t) => t.id)).toEqual(['task-1']);
    expect(reduced.state.archivedTasks).toEqual([]);
  });

  it('merges duplicate archived pages without duplicating IDs and keeps archived sort deterministic', () => {
    const ctx = makeContext();
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    const initial = ctx.mergeArchivedTasksPage({
      tasks: [],
      archivedTasks: [
        task('b', { archived: true, updated_at: '2026-03-10T10:00:00Z' }),
        task('a', { archived: true, updated_at: '2026-03-10T10:00:00Z' }),
      ],
      archivedPage: { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false },
    }, {
      tasks: [
        task('c', { archived: true, updated_at: '2026-03-10T09:00:00Z' }),
        task('a', { archived: true, updated_at: '2026-03-10T10:00:00Z' }),
      ],
      has_more_before: true,
      has_more_after: false,
    }, 'before', 20);

    expect(initial.archivedTasks.map((t) => t.id)).toEqual(['b', 'a', 'c']);

    const duplicateMerge = ctx.mergeArchivedTasksPage(initial, {
      tasks: [
        task('c', { archived: true, updated_at: '2026-03-10T09:00:00Z' }),
        task('a', { archived: true, updated_at: '2026-03-10T10:00:00Z' }),
      ],
      has_more_before: true,
      has_more_after: false,
    }, 'before', 20);

    expect(duplicateMerge.archivedTasks.map((t) => t.id)).toEqual(['b', 'a', 'c']);
  });

  it('trims the archived window beyond pageSize * 3 and preserves pagination invariants', () => {
    const ctx = makeContext();
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    const archivedTasks = [];
    for (let i = 0; i < 7; i += 1) {
      archivedTasks.push(task(`task-${i}`, {
        archived: true,
        updated_at: `2026-03-10T0${6 - i}:00:00Z`,
      }));
    }
    const trimmedAfter = ctx.trimArchivedWindowState({
      tasks: [],
      archivedTasks,
      archivedPage: { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false },
    }, 'after', 2);

    expect(trimmedAfter.archivedTasks.map((t) => t.id)).toEqual(['task-0', 'task-1', 'task-2', 'task-3', 'task-4', 'task-5']);
    expect(trimmedAfter.archivedPage.hasMoreBefore).toBe(true);
    expect(trimmedAfter.archivedPage.hasMoreAfter).toBe(false);

    const trimmedBefore = ctx.trimArchivedWindowState({
      tasks: [],
      archivedTasks,
      archivedPage: { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false },
    }, 'before', 2);

    expect(trimmedBefore.archivedTasks.map((t) => t.id)).toEqual(['task-1', 'task-2', 'task-3', 'task-4', 'task-5', 'task-6']);
    expect(trimmedBefore.archivedPage.hasMoreAfter).toBe(true);
    expect(trimmedBefore.archivedPage.hasMoreBefore).toBe(false);
  });

  it('removes deleted tasks from both active and archived arrays', () => {
    const ctx = makeContext();
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    const next = ctx.applyTaskDeleted({
      tasks: [task('task-1'), task('task-2')],
      archivedTasks: [task('task-2', { archived: true }), task('task-3', { archived: true })],
      archivedPage: { loadState: 'idle', hasMoreBefore: false, hasMoreAfter: false },
    }, { id: 'task-2' });

    expect(next.tasks.map((t) => t.id)).toEqual(['task-1']);
    expect(next.archivedTasks.map((t) => t.id)).toEqual(['task-3']);
  });
});

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
      addEventListener(type, handler) {
        this.listeners[type] = handler;
      }
      close() {
        this.closed = true;
      }
    }
    MockEventSource.CLOSED = 2;

    const scheduled = [];
    const ctx = makeContext({
      EventSource: MockEventSource,
      Routes: {
        tasks: {
          stream: () => '/api/tasks/stream',
        },
      },
      document: {
        getElementById: () => null,
        querySelectorAll: () => [],
        querySelector: () => null,
        addEventListener: () => {},
        documentElement: { setAttribute: () => {} },
        readyState: 'complete',
      },
      setTimeout: vi.fn((fn, delay) => {
        scheduled.push({ fn, delay });
        return 1;
      }),
      scheduleRender: vi.fn(),
      invalidateDiffBehindCounts: vi.fn(),
      announceBoardStatus: vi.fn(),
      getTaskAccessibleTitle: vi.fn(() => 'Task'),
      formatTaskStatusLabel: vi.fn(() => 'Done'),
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    vm.runInContext('activeWorkspaces = ["/Users/test/repo"];', ctx);

    ctx.startTasksStream();
    expect(instances[0].url).toBe('/api/tasks/stream');

    instances[0].listeners.snapshot({
      data: JSON.stringify([task('task-1')]),
      lastEventId: 'evt-1',
    });
    expect(vm.runInContext('lastTasksEventId', ctx)).toBe('evt-1');

    instances[0].readyState = MockEventSource.CLOSED;
    instances[0].onerror();
    expect(scheduled).toHaveLength(1);

    scheduled[0].fn();
    expect(instances[1].url).toBe('/api/tasks/stream?last_event_id=evt-1');
  });
});
