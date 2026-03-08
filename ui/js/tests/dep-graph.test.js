/**
 * Unit tests for dep-graph.js — the bezier-curve dependency overlay.
 *
 * The script is loaded into an isolated vm context.  DOM APIs are fully
 * stubbed so no real browser is required.
 */
import { describe, it, expect, beforeEach } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Create a minimal mock SVG/HTML element that records attribute mutations. */
function makeSvgElement(tag) {
  const attrs = {};
  const children = [];
  return {
    id: '',
    tagName: tag,
    style: { cssText: '' },
    attrs,
    children,
    setAttribute(name, val) { attrs[name] = String(val); },
    getAttribute(name) { return attrs[name] ?? null; },
    appendChild(child) { children.push(child); },
  };
}

/**
 * Build a vm context that stubs the browser APIs needed by dep-graph.js.
 *
 * @param {Object} elementMap  Maps task-id strings to fake getBoundingClientRect
 *                             return values, e.g. { 'task-a': {left,width,top,bottom} }
 * @returns {{ ctx, appendedToBody }}
 */
function makeContext(elementMap = {}) {
  const appendedToBody = [];

  const document = {
    // hideDependencyGraph looks for an existing overlay to remove.
    getElementById: () => null,

    // createElementNS is used for the <svg>, <path> and <circle> elements.
    createElementNS: (_ns, tag) => makeSvgElement(tag),

    body: {
      appendChild(el) { appendedToBody.push(el); },
    },

    // querySelector('[data-task-id="<id>"]') — extract the id from the selector.
    querySelector(selector) {
      const m = selector.match(/data-task-id="([^"]+)"/);
      if (!m) return null;
      const rect = elementMap[m[1]];
      if (!rect) return null;
      return { getBoundingClientRect: () => rect };
    },
  };

  const ctx = vm.createContext({
    document,
    window: {
      depGraphEnabled: false,
      addEventListener: () => {},
    },
    clearTimeout: () => {},
    setTimeout: () => 0,
    // Stub globals that dep-graph.js may reference but does not call in tests.
    tasks: [],
    render: () => {},
    console,
  });

  // Load the script under test into the isolated context.
  const code = readFileSync(join(jsDir, 'dep-graph.js'), 'utf8');
  vm.runInContext(code, ctx);

  return { ctx, appendedToBody };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe('renderDependencyGraph', () => {
  // Two tasks — task-a depends on task-b.
  // The card rects are chosen so that the bezier coordinates are deterministic.
  const elementMap = {
    'task-a': { left: 100, width: 200, top: 300, bottom: 350 },
    'task-b': { left: 100, width: 200, top: 100, bottom: 150 },
  };

  it('creates an SVG with a green (#22c55e) path when the dependency is done', () => {
    const { ctx, appendedToBody } = makeContext(elementMap);

    const tasks = [
      { id: 'task-a', status: 'backlog', depends_on: ['task-b'] },
      { id: 'task-b', status: 'done',    depends_on: [] },
    ];

    ctx.renderDependencyGraph(tasks);

    // An SVG must have been appended to document.body.
    expect(appendedToBody).toHaveLength(1);
    const svg = appendedToBody[0];

    // The SVG should contain at least one <path> child.
    const paths = svg.children.filter(c => c.tagName === 'path');
    expect(paths).toHaveLength(1);

    // Done dependency → green stroke, solid line.
    expect(paths[0].attrs.stroke).toBe('#22c55e');
    expect(paths[0].attrs['stroke-dasharray']).toBe('none');
  });

  it('creates an SVG with a red (#ef4444) path when the dependency has failed', () => {
    const { ctx, appendedToBody } = makeContext(elementMap);

    const tasks = [
      { id: 'task-a', status: 'backlog', depends_on: ['task-b'] },
      { id: 'task-b', status: 'failed',  depends_on: [] },
    ];

    ctx.renderDependencyGraph(tasks);

    expect(appendedToBody).toHaveLength(1);
    const svg = appendedToBody[0];

    const paths = svg.children.filter(c => c.tagName === 'path');
    expect(paths).toHaveLength(1);

    // Failed dependency → red stroke, dashed line.
    expect(paths[0].attrs.stroke).toBe('#ef4444');
    expect(paths[0].attrs['stroke-dasharray']).toBe('6,3');
  });

  it('produces no SVG when there are no depends_on relationships', () => {
    const { ctx, appendedToBody } = makeContext(elementMap);

    const tasks = [
      { id: 'task-a', status: 'backlog', depends_on: [] },
      { id: 'task-b', status: 'done',    depends_on: [] },
    ];

    ctx.renderDependencyGraph(tasks);

    expect(appendedToBody).toHaveLength(0);
  });

  it('uses amber (#f59e0b) for a dependency in any non-done, non-failed status', () => {
    const { ctx, appendedToBody } = makeContext(elementMap);

    const tasks = [
      { id: 'task-a', status: 'backlog',     depends_on: ['task-b'] },
      { id: 'task-b', status: 'in_progress', depends_on: [] },
    ];

    ctx.renderDependencyGraph(tasks);

    const svg = appendedToBody[0];
    const paths = svg.children.filter(c => c.tagName === 'path');
    expect(paths[0].attrs.stroke).toBe('#f59e0b');
    expect(paths[0].attrs['stroke-dasharray']).toBe('6,3');
  });
});

describe('hideDependencyGraph', () => {
  it('removes the overlay SVG when one exists', () => {
    let removed = false;
    const ctx = vm.createContext({
      document: {
        getElementById: (id) => id === 'dep-graph-overlay'
          ? { remove() { removed = true; } }
          : null,
        createElementNS: (_ns, tag) => makeSvgElement(tag),
        body: { appendChild: () => {} },
        querySelector: () => null,
      },
      window: { depGraphEnabled: false, addEventListener: () => {} },
      clearTimeout: () => {},
      setTimeout: () => 0,
      tasks: [],
      render: () => {},
      console,
    });

    const code = readFileSync(join(jsDir, 'dep-graph.js'), 'utf8');
    vm.runInContext(code, ctx);

    ctx.hideDependencyGraph();
    expect(removed).toBe(true);
  });
});
