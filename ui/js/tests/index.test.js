/**
 * Frontend unit tests for pure utility functions.
 *
 * Each source file is loaded into an isolated vm context so there is no
 * dependency on a real browser DOM.  Only the minimal browser globals that
 * each script needs at module-evaluation time are provided.
 */
import { describe, it, expect, beforeAll } from 'vitest';
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
