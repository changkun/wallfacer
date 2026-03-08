/**
 * Tests for modal-logs.js — log rendering, tab switching, and mode management.
 *
 * The streaming functions (_fetchLogs, _fetchTestLogs, startImplLogFetch) are
 * not tested here because they require live fetch/ReadableStream APIs.
 * The tab-switching and mode-setting functions are fully testable with DOM stubs.
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
  vm.runInContext(code, ctx);
  return ctx;
}

/**
 * Build a context that satisfies all runtime dependencies of modal-logs.js.
 * Tab elements are tracked in the `elements` map so tests can assert on them.
 */
function makeLogsContext() {
  const elements = {};

  function makeEl(id) {
    const el = {
      id,
      innerHTML: '',
      textContent: '',
      scrollHeight: 200,
      scrollTop: 200,
      clientHeight: 100,
      style: {},
      classList: {
        _classes: new Set(),
        add(c) { this._classes.add(c); },
        remove(c) { this._classes.delete(c); },
        contains(c) { return this._classes.has(c); },
        toggle(c, force) {
          if (force !== undefined) {
            force ? this._classes.add(c) : this._classes.delete(c);
          } else {
            this._classes.has(c) ? this._classes.delete(c) : this._classes.add(c);
          }
        },
      },
    };
    elements[id] = el;
    return el;
  }

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    AbortController: class { abort() {} signal = {} },
    TextDecoder: class { decode(v) { return String(v || ''); } },
    fetch: () => Promise.reject(new Error('not mocked')),
    setTimeout: () => {},
    clearTimeout: () => {},
    requestAnimationFrame: (cb) => { cb(); },
    NodeFilter: { SHOW_TEXT: 4 },
    document: {
      getElementById: (id) => {
        if (!elements[id]) makeEl(id);
        return elements[id];
      },
      createTreeWalker: () => ({ nextNode: () => null }),
      createElement: (tag) => {
        const el = { tagName: tag, innerHTML: '', style: {}, parentNode: null };
        return el;
      },
    },
    // Runtime dependencies from other modules
    tasks: [],
    currentTaskId: null,
    logsAbort: null,
    testLogsAbort: null,
    rawLogBuffer: '',
    testRawLogBuffer: '',
    logsMode: 'pretty',
    testLogsMode: 'pretty',
    logSearchQuery: '',
    oversightData: null,
    oversightFetching: false,
    testOversightData: null,
    testOversightFetching: false,
    renderPrettyLogs: (buf) => `<div class="pretty">${buf}</div>`,
    renderOversightInLogs: () => {},
    renderTestOversightInTestLogs: () => {},
    escapeHtml: (s) => String(s ?? ''),
  });

  loadScript('modal-logs.js', ctx);
  return { ctx, elements };
}

// ---------------------------------------------------------------------------
// setRightTab
// ---------------------------------------------------------------------------
describe('setRightTab', () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it('activates the implementation tab and panel', () => {
    ctx.setRightTab('implementation');
    const btn = elements['right-tab-implementation'];
    const panel = elements['right-panel-implementation'];
    expect(btn.classList.contains('active')).toBe(true);
    expect(panel.classList.contains('hidden')).toBe(false);
  });

  it('hides non-active tab buttons and panels', () => {
    ctx.setRightTab('implementation');
    const testBtn = elements['right-tab-testing'];
    const testPanel = elements['right-panel-testing'];
    expect(testBtn.classList.contains('active')).toBe(false);
    expect(testPanel.classList.contains('hidden')).toBe(true);
  });

  it('activates the testing tab', () => {
    ctx.setRightTab('testing');
    const btn = elements['right-tab-testing'];
    const panel = elements['right-panel-testing'];
    expect(btn.classList.contains('active')).toBe(true);
    expect(panel.classList.contains('hidden')).toBe(false);
  });

  it('activates the changes tab', () => {
    ctx.setRightTab('changes');
    const btn = elements['right-tab-changes'];
    const panel = elements['right-panel-changes'];
    expect(btn.classList.contains('active')).toBe(true);
    expect(panel.classList.contains('hidden')).toBe(false);
  });

  it('deactivates a previously active tab when switching', () => {
    ctx.setRightTab('implementation');
    expect(elements['right-tab-implementation'].classList.contains('active')).toBe(true);

    ctx.setRightTab('testing');
    expect(elements['right-tab-implementation'].classList.contains('active')).toBe(false);
    expect(elements['right-tab-testing'].classList.contains('active')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// _updateLogsTabs
// ---------------------------------------------------------------------------
describe('_updateLogsTabs', () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it('marks the current logsMode tab as active', () => {
    vm.runInContext('logsMode = "pretty"', ctx);
    ctx._updateLogsTabs();
    expect(elements['logs-tab-pretty'].classList.contains('active')).toBe(true);
    expect(elements['logs-tab-oversight'].classList.contains('active')).toBe(false);
    expect(elements['logs-tab-raw'].classList.contains('active')).toBe(false);
  });

  it('marks the oversight tab as active when logsMode is oversight', () => {
    vm.runInContext('logsMode = "oversight"', ctx);
    ctx._updateLogsTabs();
    expect(elements['logs-tab-oversight'].classList.contains('active')).toBe(true);
    expect(elements['logs-tab-pretty'].classList.contains('active')).toBe(false);
  });

  it('marks the raw tab as active when logsMode is raw', () => {
    vm.runInContext('logsMode = "raw"', ctx);
    ctx._updateLogsTabs();
    expect(elements['logs-tab-raw'].classList.contains('active')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// _updateTestLogsTabs
// ---------------------------------------------------------------------------
describe('_updateTestLogsTabs', () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it('marks the current testLogsMode tab as active', () => {
    vm.runInContext('testLogsMode = "pretty"', ctx);
    ctx._updateTestLogsTabs();
    expect(elements['test-logs-tab-pretty'].classList.contains('active')).toBe(true);
    expect(elements['test-logs-tab-oversight'].classList.contains('active')).toBe(false);
  });

  it('marks the oversight tab as active when testLogsMode is oversight', () => {
    vm.runInContext('testLogsMode = "oversight"', ctx);
    ctx._updateTestLogsTabs();
    expect(elements['test-logs-tab-oversight'].classList.contains('active')).toBe(true);
  });

  it('marks raw tab as active when testLogsMode is raw', () => {
    vm.runInContext('testLogsMode = "raw"', ctx);
    ctx._updateTestLogsTabs();
    expect(elements['test-logs-tab-raw'].classList.contains('active')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// setLogsMode
// ---------------------------------------------------------------------------
describe('setLogsMode', () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it('updates logsMode to pretty and triggers renderLogs', () => {
    vm.runInContext('logsMode = "oversight"', ctx);
    vm.runInContext('rawLogBuffer = "test content"', ctx);
    ctx.setLogsMode('pretty');
    expect(vm.runInContext('logsMode', ctx)).toBe('pretty');
    // renderLogs was called: the modal-logs element should have been updated
    const logsEl = elements['modal-logs'];
    expect(logsEl.innerHTML).toContain('pretty');
  });

  it('updates logsMode to raw and strips ANSI', () => {
    vm.runInContext('rawLogBuffer = "\\x1b[31mred\\x1b[0m plain"', ctx);
    ctx.setLogsMode('raw');
    expect(vm.runInContext('logsMode', ctx)).toBe('raw');
    const logsEl = elements['modal-logs'];
    // Raw mode uses textContent (no ANSI codes)
    expect(logsEl.textContent).toContain('plain');
    expect(logsEl.textContent).not.toContain('\x1b');
  });
});

// ---------------------------------------------------------------------------
// setTestLogsMode
// ---------------------------------------------------------------------------
describe('setTestLogsMode', () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it('updates testLogsMode and triggers renderTestLogs', () => {
    vm.runInContext('testLogsMode = "oversight"', ctx);
    vm.runInContext('testRawLogBuffer = "test log"', ctx);
    ctx.setTestLogsMode('pretty');
    expect(vm.runInContext('testLogsMode', ctx)).toBe('pretty');
    const logsEl = elements['modal-test-logs'];
    expect(logsEl.innerHTML).toContain('test log');
  });

  it('updates testLogsMode to raw', () => {
    vm.runInContext('testRawLogBuffer = "raw content"', ctx);
    ctx.setTestLogsMode('raw');
    expect(vm.runInContext('testLogsMode', ctx)).toBe('raw');
    const logsEl = elements['modal-test-logs'];
    expect(logsEl.textContent).toContain('raw content');
  });
});

// ---------------------------------------------------------------------------
// renderLogs (pretty and raw modes only; oversight delegates to other module)
// ---------------------------------------------------------------------------
describe('renderLogs', () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it('renders pretty logs via renderPrettyLogs stub', () => {
    vm.runInContext('logsMode = "pretty"', ctx);
    vm.runInContext('rawLogBuffer = "my log buffer"', ctx);
    ctx.renderLogs();
    expect(elements['modal-logs'].innerHTML).toContain('my log buffer');
  });

  it('strips ANSI codes and sets textContent in raw mode', () => {
    vm.runInContext('logsMode = "raw"', ctx);
    vm.runInContext('rawLogBuffer = "\\x1b[1mBold\\x1b[0m normal"', ctx);
    ctx.renderLogs();
    const logsEl = elements['modal-logs'];
    expect(logsEl.textContent).not.toContain('\x1b');
    expect(logsEl.textContent).toContain('Bold');
    expect(logsEl.textContent).toContain('normal');
  });

  it('toggles oversight-mode class on the logs element', () => {
    vm.runInContext('logsMode = "pretty"', ctx);
    ctx.renderLogs();
    expect(elements['modal-logs'].classList.contains('oversight-mode')).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// renderTestLogs (pretty and raw modes)
// ---------------------------------------------------------------------------
describe('renderTestLogs', () => {
  let ctx, elements;
  beforeEach(() => ({ ctx, elements } = makeLogsContext()));

  it('renders pretty test logs', () => {
    vm.runInContext('testLogsMode = "pretty"', ctx);
    vm.runInContext('testRawLogBuffer = "test output"', ctx);
    ctx.renderTestLogs();
    expect(elements['modal-test-logs'].innerHTML).toContain('test output');
  });

  it('strips ANSI codes in raw test mode', () => {
    vm.runInContext('testLogsMode = "raw"', ctx);
    vm.runInContext('testRawLogBuffer = "\\x1b[32mgreen\\x1b[0m text"', ctx);
    ctx.renderTestLogs();
    const logsEl = elements['modal-test-logs'];
    expect(logsEl.textContent).not.toContain('\x1b');
    expect(logsEl.textContent).toContain('green');
  });

  it('toggles oversight-mode class in oversight mode', () => {
    vm.runInContext('testLogsMode = "oversight"', ctx);
    ctx.renderTestLogs();
    expect(elements['modal-test-logs'].classList.contains('oversight-mode')).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// onLogSearchInput
// ---------------------------------------------------------------------------
describe('onLogSearchInput', () => {
  let ctx, elements;
  beforeEach(() => {
    ({ ctx, elements } = makeLogsContext());
    vm.runInContext('logsMode = "pretty"', ctx);
  });

  it('empty query renders all lines and clears count', () => {
    vm.runInContext('rawLogBuffer = "line one\\nline two\\nline three"', ctx);
    ctx.onLogSearchInput('');
    // renderPrettyLogs stub wraps in class="pretty"; with no filter the full buffer is passed
    expect(elements['modal-logs'].innerHTML).toContain('pretty');
    expect(elements['log-search-count'].textContent).toBe('');
  });

  it('non-empty query filters lines and shows match count', () => {
    // 3 lines, 2 contain 'foo'
    vm.runInContext('rawLogBuffer = "foo line\\nbar line\\nfoo baz"', ctx);
    ctx.onLogSearchInput('foo');
    expect(elements['log-search-count'].textContent).toBe('2 / 3 lines');
  });

  it('handles regex-special characters without throwing', () => {
    vm.runInContext('rawLogBuffer = "some content"', ctx);
    expect(() => ctx.onLogSearchInput('foo(bar[baz')).not.toThrow();
    // query didn't match anything → 0 / 1 lines
    expect(elements['log-search-count'].textContent).toMatch(/\d+ \/ \d+ lines/);
  });

  it('count exactly matches filtered line count', () => {
    // 5 lines, 3 contain 'target'
    vm.runInContext(
      'rawLogBuffer = "target one\\nno match\\ntarget two\\nno match\\ntarget three"',
      ctx
    );
    ctx.onLogSearchInput('target');
    expect(elements['log-search-count'].textContent).toBe('3 / 5 lines');
  });
});
