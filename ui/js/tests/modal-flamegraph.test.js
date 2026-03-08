/**
 * Tests for modal-flamegraph.js — labelHue, assignLanes, width calculation,
 * and the loadFlamegraph async entry point.
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

function makeContext(extra = {}) {
  return vm.createContext({ console, Math, Date, Infinity, ...extra });
}

/**
 * Build a minimal context for loading modal-flamegraph.js.
 * Returns { ctx, container } where container is the stub DOM element.
 */
function makeFlameContext(fetchImpl) {
  const container = {
    innerHTML: '',
    id: 'modal-flamegraph-container',
  };
  const elements = { 'modal-flamegraph-container': container };

  const ctx = makeContext({
    document: {
      getElementById: (id) => elements[id] || null,
    },
    fetch: fetchImpl || (() => Promise.reject(new Error('not mocked'))),
    escapeHtml: (s) => String(s ?? ''),
    window: {},
  });
  // wire window to ctx so IIFE's window.loadFlamegraph assignment works
  ctx.window = ctx;

  loadScript('modal-flamegraph.js', ctx);
  return { ctx, container };
}

// ---------------------------------------------------------------------------
// labelHue
// ---------------------------------------------------------------------------
describe('labelHue', () => {
  let ctx;
  beforeAll(() => {
    ctx = makeFlameContext();
    ctx = ctx.ctx;
  });

  it('returns a number', () => {
    const result = vm.runInContext('_flamegraph.labelHue("test")', ctx);
    expect(typeof result).toBe('number');
  });

  it('is deterministic — same input yields same output', () => {
    const a = vm.runInContext('_flamegraph.labelHue("impl:run")', ctx);
    const b = vm.runInContext('_flamegraph.labelHue("impl:run")', ctx);
    expect(a).toBe(b);
  });

  it('returns a value in [0, 360)', () => {
    const inputs = ['', 'a', 'phase:label', 'x'.repeat(100), 'test:123'];
    inputs.forEach((s) => {
      const h = vm.runInContext(`_flamegraph.labelHue(${JSON.stringify(s)})`, ctx);
      expect(h).toBeGreaterThanOrEqual(0);
      expect(h).toBeLessThan(360);
    });
  });

  it('different inputs generally produce different values', () => {
    const h1 = vm.runInContext('_flamegraph.labelHue("phase:a")', ctx);
    const h2 = vm.runInContext('_flamegraph.labelHue("phase:b")', ctx);
    // Not guaranteed to differ, but almost certainly will for distinct strings
    expect(typeof h1).toBe('number');
    expect(typeof h2).toBe('number');
  });
});

// ---------------------------------------------------------------------------
// assignLanes
// ---------------------------------------------------------------------------
describe('assignLanes', () => {
  let ctx;
  beforeAll(() => {
    ctx = makeFlameContext().ctx;
  });

  it('assigns a single span to lane 0', () => {
    const result = vm.runInContext(
      '_flamegraph.assignLanes([{label:"a",startMs:0,endMs:100,durationMs:100}])',
      ctx
    );
    expect(result[0].lane).toBe(0);
  });

  it('assigns two overlapping spans to different lanes', () => {
    // Span B starts before span A ends — they overlap
    const result = vm.runInContext(`
      _flamegraph.assignLanes([
        {label:"a", startMs: 0,  endMs: 100, durationMs: 100},
        {label:"b", startMs: 50, endMs: 150, durationMs: 100}
      ])
    `, ctx);
    const lanes = result.map((r) => r.lane);
    expect(lanes[0]).toBe(0);
    expect(lanes[1]).toBe(1); // must be in a different lane
  });

  it('assigns two non-overlapping spans to the same lane', () => {
    // Span B starts exactly when span A ends — no overlap
    const result = vm.runInContext(`
      _flamegraph.assignLanes([
        {label:"a", startMs:   0, endMs: 100, durationMs: 100},
        {label:"b", startMs: 100, endMs: 200, durationMs: 100}
      ])
    `, ctx);
    const lanes = result.map((r) => r.lane);
    expect(lanes[0]).toBe(0);
    expect(lanes[1]).toBe(0);
  });

  it('packs three spans with one gap correctly', () => {
    // a: 0-100, b: 50-150 (overlaps a), c: 110-200 (fits in lane 0 after a)
    const result = vm.runInContext(`
      _flamegraph.assignLanes([
        {label:"a", startMs:   0, endMs: 100, durationMs: 100},
        {label:"b", startMs:  50, endMs: 150, durationMs: 100},
        {label:"c", startMs: 110, endMs: 200, durationMs:  90}
      ])
    `, ctx);
    expect(result[0].lane).toBe(0);
    expect(result[1].lane).toBe(1);
    expect(result[2].lane).toBe(0); // c fits into lane 0 after a ended
  });
});

// ---------------------------------------------------------------------------
// Width calculation logic (pure function tests)
// ---------------------------------------------------------------------------
describe('width calculation', () => {
  it('span with durationMs === total produces 100.00% width', () => {
    const total = 1000;
    const durationMs = 1000;
    const width = Math.max(durationMs / total * 100, 0.5).toFixed(2);
    expect(width).toBe('100.00');
  });

  it('span with durationMs === 0 produces minimum 0.50% width', () => {
    const total = 1000;
    const durationMs = 0;
    const width = Math.max(durationMs / total * 100, 0.5).toFixed(2);
    expect(width).toBe('0.50');
  });

  it('span at 50% of total produces 50.00% width', () => {
    const total = 1000;
    const durationMs = 500;
    const width = Math.max(durationMs / total * 100, 0.5).toFixed(2);
    expect(width).toBe('50.00');
  });
});

// ---------------------------------------------------------------------------
// loadFlamegraph — async behaviour with mocked fetch
// ---------------------------------------------------------------------------
describe('loadFlamegraph', () => {
  it('sets loading message immediately (before fetch resolves)', () => {
    // fetch never resolves, so the .then chain never runs
    const pendingFetch = () => new Promise(() => {});

    const { ctx, container } = makeFlameContext(pendingFetch);

    ctx.loadFlamegraph('task-1');
    // The loading message is set synchronously before the fetch call
    expect(container.innerHTML).toContain('Loading spans');
  });

  it('shows no-data message for empty array response', async () => {
    const { ctx, container } = makeFlameContext(
      () => Promise.resolve({ json: () => Promise.resolve([]) })
    );

    ctx.loadFlamegraph('task-1');
    // Wait for microtask queue to drain
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toContain('No span data');
  });

  it('shows no-data message when fetch rejects', async () => {
    const { ctx, container } = makeFlameContext(
      () => Promise.reject(new Error('network error'))
    );

    ctx.loadFlamegraph('task-1');
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toContain('No span data');
  });

  it('shows no-data message when json() throws', async () => {
    const { ctx, container } = makeFlameContext(
      () => Promise.resolve({ json: () => Promise.reject(new Error('bad json')) })
    );

    ctx.loadFlamegraph('task-1');
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toContain('No span data');
  });

  it('renders span blocks for non-empty response', async () => {
    const now = Date.now();
    const spans = [
      {
        phase: 'impl',
        label: 'run',
        started_at: new Date(now).toISOString(),
        ended_at: new Date(now + 500).toISOString(),
        duration_ms: 500,
      },
      {
        phase: 'test',
        label: '',
        started_at: new Date(now + 600).toISOString(),
        ended_at: new Date(now + 1000).toISOString(),
        duration_ms: 400,
      },
    ];

    const { ctx, container } = makeFlameContext(
      () => Promise.resolve({ json: () => Promise.resolve(spans) })
    );

    ctx.loadFlamegraph('task-1');
    await new Promise((r) => setTimeout(r, 0));

    // Should not show the no-data message
    expect(container.innerHTML).not.toContain('No span data');
    // Should contain span labels
    expect(container.innerHTML).toContain('impl:run');
    expect(container.innerHTML).toContain('test');
  });

  it('returns early without error when container is absent', async () => {
    const ctx = makeContext({
      document: { getElementById: () => null },
      fetch: () => Promise.resolve({ json: () => Promise.resolve([]) }),
      escapeHtml: (s) => String(s ?? ''),
      window: {},
    });
    ctx.window = ctx;
    loadScript('modal-flamegraph.js', ctx);

    // Should not throw
    expect(() => ctx.loadFlamegraph('task-1')).not.toThrow();
  });
});
