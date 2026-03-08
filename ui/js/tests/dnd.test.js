/**
 * Tests for drag-and-drop wiring and status transitions.
 */
import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function createElement(overrides = {}) {
  return {
    dataset: {},
    children: [],
    id: '',
    insertBefore: vi.fn(),
    querySelectorAll: () => [],
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Math,
    Date,
    api: vi.fn(),
    showAlert: vi.fn(),
    updateTaskStatus: vi.fn(),
    document: {
      getElementById: (id) => elements.get(id) || null,
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

describe('initSortable', () => {
  it('saves new backlog order when reordered', () => {
    const backlog = createElement({
      querySelectorAll: () => [
        { dataset: { id: 'task-1' } },
        { dataset: { id: 'task-2' } },
      ],
    });
    const inProgress = createElement();
    const waiting = createElement();
    const done = createElement();
    const cancelled = createElement();

    const calls = [];
    const ctx = makeContext({
      elements: [
        ['col-backlog', backlog],
        ['col-in_progress', inProgress],
        ['col-waiting', waiting],
        ['col-done', done],
        ['col-cancelled', cancelled],
      ],
      Sortable: {
        create: (element, config) => {
          calls.push({ element, config });
        },
      },
    });

    loadScript(ctx, 'dnd.js');
    ctx.initSortable();

    const backlogCfg = calls.find((c) => c.element === backlog)?.config;
    expect(backlogCfg).toBeTruthy();
    backlogCfg.onSort();

    expect(ctx.api).toHaveBeenCalledTimes(2);
    expect(ctx.api).toHaveBeenNthCalledWith(1, '/api/tasks/task-1', { method: 'PATCH', body: JSON.stringify({ position: 0 }) });
    expect(ctx.api).toHaveBeenNthCalledWith(2, '/api/tasks/task-2', { method: 'PATCH', body: JSON.stringify({ position: 1 }) });
  });

  it('blocks refinement transitions on add and updates running refinements only', () => {
    const waitingTask = { id: 'task-ref', current_refinement: { status: 'running' } };
    const doneTask = { id: 'task-done', current_refinement: { status: 'done' } };
    const goodTask = { id: 'task-ok' };
    const backlog = createElement({
      insertBefore: vi.fn(),
      children: [createElement(), createElement()],
      querySelectorAll: () => [],
    });
    const inProgress = createElement();
    const waiting = createElement();
    const done = createElement();
    const cancelled = createElement();
    const calls = [];

    const ctx = makeContext({
      tasks: [waitingTask, doneTask, goodTask],
      elements: [
        ['col-backlog', backlog],
        ['col-in_progress', inProgress],
        ['col-waiting', waiting],
        ['col-done', done],
        ['col-cancelled', cancelled],
      ],
      Sortable: {
        create: (element, config) => calls.push({ element, config }),
      },
    });

    loadScript(ctx, 'dnd.js');
    ctx.initSortable();

    const inProgressCfg = calls.find((c) => c.element === inProgress)?.config;
    expect(inProgressCfg).toBeTruthy();

    inProgressCfg.onAdd({ item: { dataset: { id: 'task-ref' } }, oldIndex: 0 });
    expect(backlog.insertBefore).toHaveBeenCalledWith({ dataset: { id: 'task-ref' } }, backlog.children[0]);
    expect(ctx.showAlert).toHaveBeenCalledWith('Refinement is in progress. Please wait for it to complete before starting.');

    inProgressCfg.onAdd({ item: { dataset: { id: 'task-done' } }, oldIndex: 1 });
    expect(ctx.showAlert).toHaveBeenCalledWith('This task has a refined prompt awaiting review. Open the task to apply or dismiss it before starting.');

    inProgressCfg.onAdd({ item: { dataset: { id: 'task-ok' } }, oldIndex: 0 });
    expect(ctx.updateTaskStatus).toHaveBeenCalledWith('task-ok', 'in_progress');
  });
});

