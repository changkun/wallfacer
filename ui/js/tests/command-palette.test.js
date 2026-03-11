/**
 * Tests for the command palette filtering, navigation and action wiring.
 */
import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function makeClassList() {
  const set = new Set();
  return {
    add(cls) { set.add(cls); },
    remove(cls) { set.delete(cls); },
    toggle(cls, force) {
      if (force === undefined) {
        if (set.has(cls)) { set.delete(cls); return false; }
        set.add(cls);
        return true;
      }
      if (force) {
        set.add(cls);
        return true;
      }
      set.delete(cls);
      return false;
    },
    contains(cls) { return set.has(cls); },
  };
}

function createElement(overrides = {}) {
  const node = {
    _children: [],
    _parent: null,
    _listeners: {},
    classList: makeClassList(),
    style: {},
    dataset: {},
    textContent: '',
    innerHTML: '',
    value: '',
    selectionStart: 0,
    selectionEnd: 0,
    tagName: overrides.tagName || 'div',
    addEventListener(type, handler) {
      this._listeners[type] = this._listeners[type] || [];
      this._listeners[type].push(handler);
    },
    dispatchEvent(evt) {
      (this._listeners[evt.type] || []).forEach((fn) => fn(evt));
    },
    appendChild(child) {
      child._parent = this;
      this._children.push(child);
      return child;
    },
    remove() {
      if (!this._parent) return;
      this._parent._children = this._parent._children.filter((child) => child !== this);
    },
    querySelectorAll(selector) {
      const result = [];
      const isMatch = (el) => {
        if (!selector || selector === '*') return true;
        if (selector.startsWith('.')) {
          return el.classList.contains(selector.slice(1));
        }
        return el.tagName === selector.toUpperCase();
      };
      const visit = (current) => {
        current._children.forEach((child) => {
          if (isMatch(child)) result.push(child);
          visit(child);
        });
      };
      visit(this);
      return result;
    },
    focus() { this.focused = true; },
    setSelectionRange(start, end) {
      this.selectionStart = start;
      this.selectionEnd = end;
    },
  };
  Object.defineProperty(node, 'className', {
    get() { return Array.from(node.classList._items || []).join(' '); },
    set(value = '') {
      const next = String(value).split(/\s+/).filter(Boolean);
      node.classList = makeClassList();
      next.forEach((cls) => node.classList.add(cls));
      node.classList._items = new Set(next);
    },
  });
  return Object.assign(node, overrides);
}

function makeContext(extra = {}) {
  const storage = new Map();
  const elements = new Map(extra.elements || []);
  const body = createElement({ tagName: 'BODY' });
  const ctx = {
    console,
    Math,
    Date,
    setTimeout,
    clearTimeout,
    setInterval: () => 0,
    clearInterval: () => 0,
    Promise,
    document: {
      body,
      createElement: () => createElement(),
      getElementById: (id) => elements.get(id) || null,
      querySelector: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      readyState: 'complete',
      addEventListener: () => {},
      documentElement: { setAttribute: () => {} },
      getElementByIdOrNull: (id) => elements.get(id) || null,
      bodyClassList: {},
    },
    window: {
      addEventListener: () => {},
    },
    localStorage: {
      getItem(key) {
        return storage.has(key) ? storage.get(key) : null;
      },
      setItem(key, value) {
        storage.set(key, String(value));
      },
      removeItem(key) {
        storage.delete(key);
      },
      clear() {
        storage.clear();
      },
    },
    ...extra,
  };
  ctx.window.localStorage = ctx.localStorage;
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

function setupCommandPaletteContext(helpers = {}) {
  const palette = createElement({ tagName: 'DIV', id: 'command-palette', classList: makeClassList() });
  const panel = createElement({ tagName: 'DIV', id: 'command-palette-panel' });
  const input = createElement({ tagName: 'INPUT', id: 'command-palette-input' });
  const results = createElement({ tagName: 'DIV', id: 'command-palette-results' });
  const hint = createElement({ tagName: 'DIV', id: 'command-palette-hint-keys' });

  palette.appendChild(panel);
  palette.appendChild(input);
  palette.appendChild(results);
  palette.appendChild(hint);

  const ctx = makeContext({
    elements: [
      ['command-palette', palette],
      ['command-palette-panel', panel],
      ['command-palette-input', input],
      ['command-palette-results', results],
      ['command-palette-hint-keys', hint],
    ],
    // Provide the task() route helper so _archiveTask can call task(id).archive().
    task: (id) => ({
      archive: () => `/api/tasks/${id}/archive`,
      update: () => `/api/tasks/${id}`,
      done: () => `/api/tasks/${id}/done`,
      resume: () => `/api/tasks/${id}/resume`,
      test: () => `/api/tasks/${id}/test`,
    }),
    // Provide waitForTaskDelta so _archiveTask's .then() doesn't error.
    waitForTaskDelta: vi.fn(),
    ...helpers,
  });

  loadScript(ctx, 'state.js');
  loadScript(ctx, 'utils.js');
  loadScript(ctx, 'command-palette.js');
  return ctx;
}

describe('commandPaletteMatchTask', () => {
  it('matches title, prompt and short UUID', () => {
    const ctx = setupCommandPaletteContext();
    const task = {
      id: '11111111-aaaa-bbbb-cccc-111111111111',
      title: 'Implement task search',
      prompt: 'Search tasks quickly from a keyboard palette',
    };
    expect(ctx.commandPaletteMatchTask(task, 'search').matched).toBe(true);
    expect(ctx.commandPaletteMatchTask(task, '1111').matched).toBe(true);
    expect(ctx.commandPaletteMatchTask(task, 'missing')).toBeNull();
  });

  it('searches local tasks with ranking', () => {
    const ctx = setupCommandPaletteContext();
    const tasks = [
      { id: '1', title: 'Sync task to git', prompt: 'sync worktrees' },
      { id: '2', title: 'Run tests', prompt: 'verification and lint' },
      { id: '3', title: 'Refine prompts', prompt: 'improve clarity' },
    ];
    const result = ctx.commandPaletteSearchTasks('sync', tasks);
    expect(result).toHaveLength(1);
    expect(result[0].id).toBe('1');
  });
});

describe('command-palette key navigation', () => {
  it('moves the active row with arrow keys and wraps at edges', () => {
    const ctx = setupCommandPaletteContext();
    ctx._buildTaskListSections([
      {
        title: 'Task Targets',
        rows: [
          { type: 'task', id: '1', title: 'Alpha', taskObj: { id: '1', status: 'backlog' }, execute: () => {} },
          { type: 'task', id: '2', title: 'Bravo', taskObj: { id: '2', status: 'done' }, execute: () => {} },
          { type: 'action', id: 'a1', label: 'Action one', execute: () => {} },
        ],
      },
    ]);
    const state = () => ctx.window.__wallfacerTestState.commandPalette();

    expect(state().activeIndex).toBe(0);
    ctx.commandPaletteMoveDown();
    expect(state().activeIndex).toBe(1);
    ctx.commandPaletteMoveDown();
    expect(state().activeIndex).toBe(2);
    ctx.commandPaletteMoveDown();
    expect(state().activeIndex).toBe(0);
    ctx.commandPaletteMoveUp();
    expect(state().activeIndex).toBe(2);
  });
});

describe('commandPaletteTaskActions', () => {
  it('maps status-gated actions for backlog, waiting and failed tasks', () => {
    const ctx = setupCommandPaletteContext();
    const backlogTask = { id: 'b1', status: 'backlog' };
    const waitingTask = { id: 'w1', status: 'waiting' };
    const failedTask = { id: 'f1', status: 'failed', session_id: 's1' };
    const doneTask = { id: 'd1', status: 'done' };

    const backlogActions = ctx.commandPaletteTaskActions(backlogTask).map((action) => action.id);
    expect(backlogActions).toContain('start-task');
    expect(backlogActions).toContain('open-task');

    const waitingActions = ctx.commandPaletteTaskActions(waitingTask).map((action) => action.id);
    expect(waitingActions).toContain('run-test');
    expect(waitingActions).toContain('mark-done');
    expect(waitingActions).toContain('retry-task');

    const failedActions = ctx.commandPaletteTaskActions(failedTask).map((action) => action.id);
    expect(failedActions).toContain('resume-task');
    expect(failedActions).toContain('sync-task');
    expect(failedActions).toContain('retry-task');
    expect(failedActions).toContain('open-task');

    const doneActions = ctx.commandPaletteTaskActions(doneTask).map((action) => action.id);
    expect(doneActions).toContain('archive-task');
  });
});

describe('command-palette action wiring', () => {
  it('invokes the expected task API helpers', async () => {
    const updateTaskStatus = vi.fn(() => Promise.resolve());
    const quickTestTask = vi.fn(() => Promise.resolve());
    const quickDoneTask = vi.fn(() => Promise.resolve());
    const quickResumeTask = vi.fn(() => Promise.resolve());
    const quickRetryTask = vi.fn(() => Promise.resolve());
    const syncTask = vi.fn(() => Promise.resolve());
    const openModal = vi.fn(() => Promise.resolve());
    const setRightTab = vi.fn(() => Promise.resolve());
    const api = vi.fn(() => Promise.resolve());
    const fetchTasks = vi.fn();
    const showAlert = vi.fn();

    const waitForTaskDelta = vi.fn();
    const ctx = setupCommandPaletteContext({
      updateTaskStatus,
      quickTestTask,
      quickDoneTask,
      quickResumeTask,
      quickRetryTask,
      syncTask,
      openModal,
      setRightTab,
      api,
      fetchTasks,
      waitForTaskDelta,
      showAlert,
    });

    const backlog = ctx.commandPaletteTaskActions({ id: 'b1', status: 'backlog' });
    await backlog.find((a) => a.id === 'start-task').execute();
    expect(updateTaskStatus).toHaveBeenCalledWith('b1', 'in_progress');

    const waiting = ctx.commandPaletteTaskActions({ id: 'w1', status: 'waiting' });
    await waiting.find((a) => a.id === 'run-test').execute();
    expect(quickTestTask).toHaveBeenCalledWith('w1');
    await waiting.find((a) => a.id === 'mark-done').execute();
    expect(quickDoneTask).toHaveBeenCalledWith('w1');
    await waiting.find((a) => a.id === 'retry-task').execute();
    expect(quickRetryTask).toHaveBeenCalledWith('w1');

    const failed = ctx.commandPaletteTaskActions({ id: 'f1', status: 'failed', session_id: 's1', timeout: 27 });
    await failed.find((a) => a.id === 'resume-task').execute();
    expect(quickResumeTask).toHaveBeenCalledWith('f1', 27);
    await failed.find((a) => a.id === 'sync-task').execute();
    expect(syncTask).toHaveBeenCalledWith('f1');

    const done = ctx.commandPaletteTaskActions({ id: 'd1', status: 'done' });
    await done.find((a) => a.id === 'archive-task').execute();
    expect(api).toHaveBeenCalledWith('/api/tasks/d1/archive', { method: 'POST' });
    // _archiveTask now delegates to waitForTaskDelta rather than fetchTasks.
    expect(waitForTaskDelta).toHaveBeenCalledWith('d1');
    expect(fetchTasks).not.toHaveBeenCalled();

    const withModal = ctx.commandPaletteTaskActions({ id: 't1', status: 'done', turns: 2 });
    await withModal.find((a) => a.id === 'open-task-testing').execute();
    expect(openModal).toHaveBeenCalledWith('t1');
    expect(setRightTab).toHaveBeenCalledWith('testing');
  });
});

describe('command-palette remote search', () => {
  it('loads and renders @-prefixed server search matches', async () => {
    const fetch = vi.fn(() => Promise.resolve({
      ok: true,
      json: () => Promise.resolve([
        { id: 'r1', title: 'Remote task', status: 'done', snippet: '<mark>remote</mark> task match', matched_field: 'title' },
      ]),
    }));
    const openModal = vi.fn(() => Promise.resolve());

    const ctx = setupCommandPaletteContext({ fetch, openModal });

    ctx._commandPaletteServerSeq = 1;
    await ctx._searchRemote('@task', 1);
    const state = ctx.window.__wallfacerTestState.commandPalette();

    const taskRows = state.rows.filter((row) => row.type === 'task');
    expect(taskRows).toHaveLength(1);
    expect(taskRows[0].id).toBe('r1');
    expect(state.taskRows[0].title).toBe('Remote task');
    expect(state.rows.some((row) => row.id === 'action-open-task:Open task')).toBe(true);
    expect(fetch).toHaveBeenCalledWith('/api/tasks/search?q=' + encodeURIComponent('task'));

    await taskRows[0].execute();
    expect(openModal).toHaveBeenCalledWith('r1');
  });
});
