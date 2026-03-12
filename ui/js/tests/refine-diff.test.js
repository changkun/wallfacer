/**
 * Tests for the inline diff feature added to refine.js:
 *   - diffTextLines(before, after)  — line-level LCS diff
 *   - renderTextDiff(container, before, after)  — DOM rendering
 *   - renderRefineHistory(task)  — Show diff button presence
 *   - toggleRefineDiff(sessionIndex)  — lazy render + toggle
 */
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function loadScript(filename, ctx) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

// ---------------------------------------------------------------------------
// Helpers for building mock DOM elements and VM contexts
// ---------------------------------------------------------------------------

function makeClassList(initial = []) {
  const cls = new Set(initial);
  return {
    has: (c) => cls.has(c),
    contains: (c) => cls.has(c),   // DOMTokenList alias for `has`
    add: (c) => cls.add(c),
    remove: (c) => cls.delete(c),
    toggle: (c) => { if (cls.has(c)) { cls.delete(c); return false; } cls.add(c); return true; },
    toString: () => [...cls].join(' '),
  };
}

function makeEl(id, initialClasses = []) {
  let _innerHTML = '';
  return {
    id,
    dataset: {},
    classList: makeClassList(initialClasses),
    get innerHTML() { return _innerHTML; },
    set innerHTML(v) { _innerHTML = v; },
    textContent: '',
  };
}

/**
 * Build a minimal VM context that can load refine.js.
 * `overrides` is merged into the context after building defaults.
 */
function makeRefineContext(overrides = {}) {
  const elements = {};
  // Ensure section + list elements exist by default.
  elements['refine-history-section'] = makeEl('refine-history-section', ['hidden']);
  elements['refine-history-list']    = makeEl('refine-history-list');

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    JSON,
    Promise,
    Array,
    escapeHtml: (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'),
    document: {
      getElementById: (id) => {
        if (!elements[id]) elements[id] = makeEl(id);
        return elements[id];
      },
    },
    requestAnimationFrame: (cb) => cb(),
    // Globals that refine.js reads at call-time but are not exercised here.
    tasks: [],
    getOpenModalTaskId: () => 'task-1',
    api: () => Promise.resolve(),
    task: () => ({}),
    closeModal: () => {},
    waitForTaskDelta: () => Promise.resolve(),
    openModal: () => {},
    showAlert: () => {},
    collectSandboxByActivity: () => ({}),
    DEFAULT_TASK_TIMEOUT: 300,
    renderPrettyLogs: () => '',
    fetch: () => Promise.resolve({ ok: false, body: null }),
    ...overrides,
  });

  loadScript('refine.js', ctx);
  // Expose the mock element store so tests can inspect/inject elements.
  ctx._elements = elements;
  return ctx;
}

// ---------------------------------------------------------------------------
// diffTextLines — unit tests
// ---------------------------------------------------------------------------
describe('diffTextLines', () => {
  let ctx;

  beforeAll(() => { ctx = makeRefineContext(); });

  it('returns only eq ops for identical strings', () => {
    const ops = ctx.diffTextLines('foo\nbar\nbaz', 'foo\nbar\nbaz');
    expect(ops.every(o => o.op === 'eq')).toBe(true);
    expect(ops.map(o => o.text)).toEqual(['foo', 'bar', 'baz']);
  });

  it('produces ins ops for lines present only in after', () => {
    // Note: ''.split('\n') === [''] so diffing '' vs 'alpha\nbeta' yields
    // del('') + ins('alpha') + ins('beta') — the key assertion is that 'alpha'
    // and 'beta' appear as ins and there are no del ops for real content.
    const ops = ctx.diffTextLines('', 'alpha\nbeta');
    expect(ops.some(o => o.op === 'ins' && o.text === 'alpha')).toBe(true);
    expect(ops.some(o => o.op === 'ins' && o.text === 'beta')).toBe(true);
    expect(ops.some(o => o.op === 'del' && o.text !== '')).toBe(false);
  });

  it('produces del ops for lines present only in before', () => {
    // ''.split('\n') === [''] so after='' contributes an ins('') alongside
    // del('alpha') + del('beta') — the key assertion is the real lines are del.
    const ops = ctx.diffTextLines('alpha\nbeta', '');
    expect(ops.some(o => o.op === 'del' && o.text === 'alpha')).toBe(true);
    expect(ops.some(o => o.op === 'del' && o.text === 'beta')).toBe(true);
    expect(ops.some(o => o.op === 'ins' && o.text !== '')).toBe(false);
  });

  it('produces correct interleaved ops for a mixed edit', () => {
    // before: line1 / line2 / line3
    // after:  line1 / line2-modified / line4
    // Expected: eq(line1), del(line2), ins(line2-modified), del(line3), ins(line4)
    const ops = ctx.diffTextLines('line1\nline2\nline3', 'line1\nline2-modified\nline4');
    const eqOps  = ops.filter(o => o.op === 'eq');
    const insOps = ops.filter(o => o.op === 'ins');
    const delOps = ops.filter(o => o.op === 'del');
    expect(eqOps.map(o => o.text)).toContain('line1');
    expect(delOps.map(o => o.text)).toContain('line2');
    expect(insOps.map(o => o.text)).toContain('line2-modified');
    expect(delOps.map(o => o.text)).toContain('line3');
    expect(insOps.map(o => o.text)).toContain('line4');
  });

  it('preserves document order (eq before ins/del when applicable)', () => {
    const ops = ctx.diffTextLines('a\nb', 'a\nc');
    expect(ops[0]).toMatchObject({op: 'eq', text: 'a'});
  });

  it('handles single-line strings with no newlines', () => {
    const ops = ctx.diffTextLines('hello', 'world');
    expect(ops.some(o => o.op === 'del' && o.text === 'hello')).toBe(true);
    expect(ops.some(o => o.op === 'ins' && o.text === 'world')).toBe(true);
  });

  it('handles identical single-line strings', () => {
    const ops = ctx.diffTextLines('same', 'same');
    expect(ops).toEqual([{op: 'eq', text: 'same'}]);
  });
});

// ---------------------------------------------------------------------------
// renderTextDiff — unit tests
// ---------------------------------------------------------------------------
describe('renderTextDiff', () => {
  let ctx;

  beforeAll(() => { ctx = makeRefineContext(); });

  it('puts a diff-add span for inserted lines', () => {
    const container = makeEl('test-container');
    ctx.renderTextDiff(container, 'only\n', 'only\nnew line');
    expect(container.innerHTML).toContain('diff-add');
  });

  it('puts a diff-del span for deleted lines', () => {
    const container = makeEl('test-container');
    ctx.renderTextDiff(container, 'old line\nonly\n', 'only\n');
    expect(container.innerHTML).toContain('diff-del');
  });

  it('prefixes inserted lines with +', () => {
    const container = makeEl('test-container');
    ctx.renderTextDiff(container, '', 'inserted');
    expect(container.innerHTML).toContain('+inserted');
  });

  it('prefixes deleted lines with -', () => {
    const container = makeEl('test-container');
    ctx.renderTextDiff(container, 'deleted', '');
    expect(container.innerHTML).toContain('-deleted');
  });

  it('HTML-escapes content in diff lines', () => {
    const container = makeEl('test-container');
    ctx.renderTextDiff(container, '<b>old</b>', '<b>new</b>');
    expect(container.innerHTML).not.toContain('<b>');
    expect(container.innerHTML).toContain('&lt;b&gt;');
  });

  it('wraps output in a font-mono div', () => {
    const container = makeEl('test-container');
    ctx.renderTextDiff(container, 'a', 'b');
    expect(container.innerHTML).toContain('font-mono');
  });
});

// ---------------------------------------------------------------------------
// renderRefineHistory — integration: button presence
// ---------------------------------------------------------------------------
describe('renderRefineHistory diff button presence', () => {
  let ctx;

  beforeEach(() => { ctx = makeRefineContext(); });

  it('includes Show diff button when both start_prompt and result_prompt exist', () => {
    ctx.renderRefineHistory({
      refine_sessions: [{
        created_at: '2026-01-01T00:00:00Z',
        start_prompt: 'original',
        result_prompt: 'refined',
        result: '',
      }],
    });
    const html = ctx._elements['refine-history-list'].innerHTML;
    expect(html).toContain('refine-diff-btn-0');
    expect(html).toContain('Show diff');
  });

  it('omits Show diff button when result_prompt is absent', () => {
    ctx.renderRefineHistory({
      refine_sessions: [{
        created_at: '2026-01-01T00:00:00Z',
        start_prompt: 'original',
        result_prompt: null,
        result: '',
      }],
    });
    const html = ctx._elements['refine-history-list'].innerHTML;
    expect(html).not.toContain('refine-diff-btn-0');
    expect(html).not.toContain('Show diff');
  });

  it('omits Show diff button when result_prompt is empty string', () => {
    ctx.renderRefineHistory({
      refine_sessions: [{
        created_at: '2026-01-01T00:00:00Z',
        start_prompt: 'original',
        result_prompt: '',
        result: '',
      }],
    });
    const html = ctx._elements['refine-history-list'].innerHTML;
    expect(html).not.toContain('refine-diff-btn-0');
  });

  it('adds a hidden diff container alongside the button', () => {
    ctx.renderRefineHistory({
      refine_sessions: [{
        created_at: '2026-01-01T00:00:00Z',
        start_prompt: 'before',
        result_prompt: 'after',
        result: '',
      }],
    });
    const html = ctx._elements['refine-history-list'].innerHTML;
    expect(html).toContain('id="refine-diff-0"');
    expect(html).toContain('hidden');
  });

  it('existing Revert button is still present when result_prompt exists', () => {
    ctx.renderRefineHistory({
      refine_sessions: [{
        created_at: '2026-01-01T00:00:00Z',
        start_prompt: 'before',
        result_prompt: 'after',
        result: '',
      }],
    });
    const html = ctx._elements['refine-history-list'].innerHTML;
    expect(html).toContain('revertToHistoryPrompt(0)');
  });
});

// ---------------------------------------------------------------------------
// toggleRefineDiff — integration: lazy render + toggle
// ---------------------------------------------------------------------------
describe('toggleRefineDiff', () => {
  const FIXTURE_BEFORE = 'keep me\nremove this\nkeep too';
  const FIXTURE_AFTER  = 'keep me\nadd this\nkeep too';

  function makeToggleContext() {
    const ctx = makeRefineContext();
    // Inject pre-made mock elements for session index 0.
    const container = makeEl('refine-diff-0', ['hidden']);
    const btn       = makeEl('refine-diff-btn-0');
    btn.textContent = 'Show diff';
    ctx._elements['refine-diff-0']     = container;
    ctx._elements['refine-diff-btn-0'] = btn;
    // Provide a task with the fixture session at index 0.
    ctx.tasks = [{
      id: 'task-1',
      refine_sessions: [{
        start_prompt:  FIXTURE_BEFORE,
        result_prompt: FIXTURE_AFTER,
      }],
    }];
    return { ctx, container, btn };
  }

  it('renders diff content with at least one diff-add span on first click', () => {
    const { ctx, container } = makeToggleContext();
    ctx.toggleRefineDiff(0);
    expect(container.innerHTML).toContain('diff-add');
  });

  it('renders diff content with at least one diff-del span on first click', () => {
    const { ctx, container } = makeToggleContext();
    ctx.toggleRefineDiff(0);
    expect(container.innerHTML).toContain('diff-del');
  });

  it('reveals the container (removes hidden class) on first click', () => {
    const { ctx, container } = makeToggleContext();
    ctx.toggleRefineDiff(0);
    expect(container.classList.has('hidden')).toBe(false);
  });

  it('updates button text to "Hide diff" after first click', () => {
    const { ctx, btn } = makeToggleContext();
    ctx.toggleRefineDiff(0);
    expect(btn.textContent).toBe('Hide diff');
  });

  it('hides the container again on second click (toggle)', () => {
    const { ctx, container } = makeToggleContext();
    ctx.toggleRefineDiff(0); // show
    ctx.toggleRefineDiff(0); // hide
    expect(container.classList.has('hidden')).toBe(true);
  });

  it('restores button text to "Show diff" on second click', () => {
    const { ctx, btn } = makeToggleContext();
    ctx.toggleRefineDiff(0); // show → "Hide diff"
    ctx.toggleRefineDiff(0); // hide → "Show diff"
    expect(btn.textContent).toBe('Show diff');
  });

  it('does not re-render diff on second click (dataset.rendered preserved)', () => {
    const { ctx, container } = makeToggleContext();
    ctx.toggleRefineDiff(0);
    const htmlAfterFirst = container.innerHTML;
    ctx.toggleRefineDiff(0);
    ctx.toggleRefineDiff(0);
    // innerHTML should not have changed after the first render.
    expect(container.innerHTML).toBe(htmlAfterFirst);
    expect(container.dataset.rendered).toBe('true');
  });

  it('does nothing when the container element is absent', () => {
    const ctx = makeRefineContext();
    // No elements injected — getElementById returns an empty mock.
    // Should not throw.
    expect(() => ctx.toggleRefineDiff(99)).not.toThrow();
  });
});
