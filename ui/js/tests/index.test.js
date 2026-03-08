/**
 * Frontend unit tests for pure utility functions.
 *
 * Each source file is loaded into an isolated vm context so there is no
 * dependency on a real browser DOM.  Only the minimal browser globals that
 * each script needs at module-evaluation time are provided.
 */
import { describe, it, expect, beforeAll, beforeEach } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

/**
 * Build a contextified sandbox that satisfies the browser globals used by
 * the scripts at load time.  Extra properties can be added via `extra`.
 */
function makeContext(extra = {}) {
  const ctx = {
    // Minimal document stub – getElementById returning null makes any IIFE
    // that checks for DOM elements bail out gracefully.
    document: {
      getElementById: (id) => {
        if (id === 'container-monitor-modal') {
          return { addEventListener: () => {} };
        }
        return null;
      },
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: 'complete',
      addEventListener: () => {},
    },
    window: {
      matchMedia: () => ({ matches: false, addEventListener: () => {} }),
    },
    localStorage: {
      getItem: () => null,
      setItem: () => {},
    },
    IntersectionObserver: class {
      constructor() {}
      observe() {}
      unobserve() {}
      disconnect() {}
    },
    // Host-environment globals needed for pure logic.
    Date,
    Math,
    console,
    clearInterval: () => {},
    setInterval: () => 0,
    ...extra,
  };
  return vm.createContext(ctx);
}

function loadScript(filename, ctx) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx);
  return ctx;
}

// ---------------------------------------------------------------------------
// Test 1 – escapeHtml (utils.js)
// Verifies that user-supplied strings are safe to embed in HTML.
// ---------------------------------------------------------------------------
describe('escapeHtml', () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript('utils.js', ctx);
  });

  it('converts <, >, &, and " to their HTML entities', () => {
    expect(ctx.escapeHtml('<b class="x">a & b</b>')).toBe(
      '&lt;b class=&quot;x&quot;&gt;a &amp; b&lt;/b&gt;',
    );
  });
});

// ---------------------------------------------------------------------------
// Test 2 – timeAgo (utils.js)
// Verifies human-readable relative timestamps shown on task cards.
// ---------------------------------------------------------------------------
describe('timeAgo', () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript('utils.js', ctx);
  });

  it('returns "just now" for a timestamp less than 60 seconds in the past', () => {
    const thirtySecondsAgo = new Date(Date.now() - 30_000).toISOString();
    expect(ctx.timeAgo(thirtySecondsAgo)).toBe('just now');
  });
});

// ---------------------------------------------------------------------------
// Test 3 – formatTimeout (utils.js)
// Verifies the timeout display used in the task settings panel.
// ---------------------------------------------------------------------------
describe('formatTimeout', () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript('utils.js', ctx);
  });

  it('formats an even number of hours without a minute remainder', () => {
    expect(ctx.formatTimeout(60)).toBe('1h');
    expect(ctx.formatTimeout(120)).toBe('2h');
  });
});

// ---------------------------------------------------------------------------
// Test 4 – getResolvedTheme (theme.js)
// Verifies that explicit 'dark' / 'light' values are returned as-is without
// consulting the OS colour-scheme media query.
// ---------------------------------------------------------------------------
describe('getResolvedTheme', () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript('theme.js', ctx);
  });

  it('returns explicit mode values without querying matchMedia', () => {
    expect(ctx.getResolvedTheme('dark')).toBe('dark');
    expect(ctx.getResolvedTheme('light')).toBe('light');
  });
});

// ---------------------------------------------------------------------------
// Test 5 – containerStateColor (containers.js)
// Verifies the status-dot colours shown in the container monitor modal.
// ---------------------------------------------------------------------------
describe('containerStateColor', () => {
  let ctx;
  beforeAll(() => {
    // containers.js references escapeHtml inside renderContainers (not at
    // load time), so a stub is enough to avoid ReferenceError if any path
    // ever reaches it during test setup.
    ctx = makeContext({ escapeHtml: (s) => String(s ?? '') });
    loadScript('containers.js', ctx);
  });

  it('maps known container states to their designated hex colours', () => {
    expect(ctx.containerStateColor('running')).toBe('#45b87a');
    expect(ctx.containerStateColor('dead')).toBe('#d46868');
    expect(ctx.containerStateColor('paused')).toBe('#d4a030');
    expect(ctx.containerStateColor(null)).toBe('#9c9890'); // default / unknown
  });
});

// ---------------------------------------------------------------------------
// Test 6 – updateMaxParallelTag (render.js)
// Verifies that the "max N" badge in the In Progress column header reflects
// the current maxParallelTasks global and responds to changes so that the UI
// stays in sync when system settings are updated.
// ---------------------------------------------------------------------------
describe('updateMaxParallelTag', () => {
  let ctx;
  let tagEl;

  beforeAll(() => {
    tagEl = {
      textContent: '',
      classList: {
        _hidden: true,
        add(cls) { if (cls === 'hidden') this._hidden = true; },
        remove(cls) { if (cls === 'hidden') this._hidden = false; },
      },
    };

    ctx = makeContext({
      document: {
        getElementById: (id) => {
          if (id === 'max-parallel-tag') return tagEl;
          if (id === 'container-monitor-modal') return { addEventListener: () => {} };
          return null;
        },
        querySelectorAll: () => ({ forEach: () => {} }),
        documentElement: { setAttribute: () => {} },
        readyState: 'complete',
        addEventListener: () => {},
      },
    });

    loadScript('state.js', ctx);
    loadScript('render.js', ctx);
  });

  // Use vm.runInContext to assign into the let binding created by state.js.
  // Direct ctx property assignment would create a shadowed property, not
  // update the let binding that the function closes over.
  function setMax(n) {
    vm.runInContext(`maxParallelTasks = ${n};`, ctx);
  }

  it('shows "max N" and removes hidden class when maxParallelTasks > 0', () => {
    setMax(5);
    ctx.updateMaxParallelTag();
    expect(tagEl.textContent).toBe('max 5');
    expect(tagEl.classList._hidden).toBe(false);
  });

  it('updates the label when maxParallelTasks changes (simulates settings save)', () => {
    setMax(10);
    ctx.updateMaxParallelTag();
    expect(tagEl.textContent).toBe('max 10');
    expect(tagEl.classList._hidden).toBe(false);
  });

  it('hides the tag when maxParallelTasks is 0 (not yet loaded)', () => {
    setMax(0);
    ctx.updateMaxParallelTag();
    expect(tagEl.classList._hidden).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// Test 7 – buildCardActions: Start button disabled during refinement (render.js)
// Verifies that the Start button on backlog cards is disabled when a refinement
// job is actively running, preventing accidental task start mid-refinement.
// ---------------------------------------------------------------------------
describe('buildCardActions refinement guard', () => {
  let ctx;

  beforeAll(() => {
    ctx = makeContext({
      escapeHtml: (s) => String(s ?? ''),
      maxParallelTasks: 0,
      // render.js uses these globals at various points; provide stubs
      fetchBehindCount: () => {},
    });
    loadScript('state.js', ctx);
    loadScript('render.js', ctx);
  });

  it('Start button is enabled when there is no current_refinement', () => {
    const task = { id: 'abc', status: 'backlog', current_refinement: null };
    const html = ctx.buildCardActions(task);
    // disabled attribute must not be present
    expect(html).not.toMatch(/disabled/);
    expect(html).toContain('card-action-start');
  });

  it('Start button is disabled when refinement is done (requires review)', () => {
    const task = { id: 'abc', status: 'backlog', current_refinement: { status: 'done' } };
    const html = ctx.buildCardActions(task);
    expect(html).toContain('disabled');
    expect(html).toContain('card-action-start');
  });

  it('Start button is disabled when refinement is running', () => {
    const task = { id: 'abc', status: 'backlog', current_refinement: { status: 'running' } };
    const html = ctx.buildCardActions(task);
    expect(html).toContain('disabled');
    expect(html).toContain('card-action-start');
  });

  it('Start button is enabled when refinement has failed', () => {
    const task = { id: 'abc', status: 'backlog', current_refinement: { status: 'failed' } };
    const html = ctx.buildCardActions(task);
    expect(html).not.toMatch(/disabled/);
    expect(html).toContain('card-action-start');
  });
});
