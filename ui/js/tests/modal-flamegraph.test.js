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
    expect(container.innerHTML).toContain('Loading');
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

// ---------------------------------------------------------------------------
// computePhaseRegions
// ---------------------------------------------------------------------------
describe('computePhaseRegions', () => {
  let ctx;
  beforeAll(() => {
    ctx = makeFlameContext().ctx;
  });

  it('returns [] for empty phases array', () => {
    const result = vm.runInContext(
      '_flamegraph.computePhaseRegions([], 0, 1000)',
      ctx
    );
    expect(result).toEqual([]);
  });

  it('returns [] for null phases', () => {
    const result = vm.runInContext(
      '_flamegraph.computePhaseRegions(null, 0, 1000)',
      ctx
    );
    expect(result).toEqual([]);
  });

  it('single phase: startMs from timestamp, endMs from globalEndMs', () => {
    const ts = new Date(500).toISOString();
    const result = vm.runInContext(
      `_flamegraph.computePhaseRegions([{timestamp: ${JSON.stringify(ts)}, title: 'Phase A', summary: 'S'}], 0, 2000)`,
      ctx
    );
    expect(result.length).toBe(1);
    expect(result[0].startMs).toBe(500);
    expect(result[0].endMs).toBe(2000);
    expect(result[0].title).toBe('Phase A');
  });

  it('two phases: first ends at second start, second ends at globalEndMs', () => {
    const ts0 = new Date(100).toISOString();
    const ts1 = new Date(600).toISOString();
    const result = vm.runInContext(
      `_flamegraph.computePhaseRegions([
        {timestamp: ${JSON.stringify(ts0)}, title: 'A', summary: ''},
        {timestamp: ${JSON.stringify(ts1)}, title: 'B', summary: ''}
      ], 0, 1000)`,
      ctx
    );
    expect(result.length).toBe(2);
    expect(result[0].startMs).toBe(100);
    expect(result[0].endMs).toBe(600);
    expect(result[1].startMs).toBe(600);
    expect(result[1].endMs).toBe(1000);
  });

  it('phase timestamp before globalStartMs is clamped to globalStartMs', () => {
    const ts = new Date(50).toISOString();
    const result = vm.runInContext(
      `_flamegraph.computePhaseRegions([{timestamp: ${JSON.stringify(ts)}, title: 'A', summary: ''}], 200, 1000)`,
      ctx
    );
    expect(result.length).toBe(1);
    expect(result[0].startMs).toBe(200);
  });

  it('phase timestamp equal to globalEndMs produces zero-width region that is skipped', () => {
    const ts = new Date(1000).toISOString();
    const result = vm.runInContext(
      `_flamegraph.computePhaseRegions([{timestamp: ${JSON.stringify(ts)}, title: 'A', summary: ''}], 0, 1000)`,
      ctx
    );
    // startMs would be clamped to 1000, endMs = globalEndMs = 1000 → zero width → skipped
    expect(result.length).toBe(0);
  });

  it('same title input produces same hue value (deterministic via labelHue)', () => {
    const ts = new Date(0).toISOString();
    const r1 = vm.runInContext(
      `_flamegraph.computePhaseRegions([{timestamp: ${JSON.stringify(ts)}, title: 'Foo', summary: ''}], 0, 1000)`,
      ctx
    );
    const r2 = vm.runInContext(
      `_flamegraph.computePhaseRegions([{timestamp: ${JSON.stringify(ts)}, title: 'Foo', summary: ''}], 0, 1000)`,
      ctx
    );
    expect(r1[0].hue).toBe(r2[0].hue);
  });

  it('single invalid timestamp falls back to even distribution (full width)', () => {
    // 'not-a-date' → NaN → no valid timestamps → evenly distribute 1 phase
    const result = vm.runInContext(
      `_flamegraph.computePhaseRegions([{timestamp: 'not-a-date', title: 'A', summary: ''}], 0, 1000)`,
      ctx
    );
    expect(result.length).toBe(1);
    expect(result[0].startMs).toBe(0);
    expect(result[0].endMs).toBe(1000);
    expect(result[0].title).toBe('A');
  });

  it('Go zero-value timestamp ("0001-01-01T00:00:00Z") is treated as invalid', () => {
    // When ALL phases carry the Go zero-value (very negative ms), the function
    // should distribute them evenly rather than collapsing all but the last.
    const result = vm.runInContext(
      `_flamegraph.computePhaseRegions([
        {timestamp: "0001-01-01T00:00:00Z", title: "Phase A", summary: ""},
        {timestamp: "0001-01-01T00:00:00Z", title: "Phase B", summary: ""},
        {timestamp: "0001-01-01T00:00:00Z", title: "Phase C", summary: ""}
      ], 0, 3000)`,
      ctx
    );
    // All three phases should be visible with equal widths.
    expect(result.length).toBe(3);
    expect(result[0].startMs).toBe(0);
    expect(result[0].endMs).toBe(1000);
    expect(result[1].startMs).toBe(1000);
    expect(result[1].endMs).toBe(2000);
    expect(result[2].startMs).toBe(2000);
    expect(result[2].endMs).toBe(3000);
  });

  it('mix of valid and invalid timestamps: only valid phases are rendered', () => {
    const ts0 = new Date(200).toISOString();
    const ts2 = new Date(700).toISOString();
    // Phase 1 has a Go zero-value timestamp (invalid), phases 0 and 2 are valid.
    const result = vm.runInContext(
      `_flamegraph.computePhaseRegions([
        {timestamp: ${JSON.stringify(ts0)}, title: "A", summary: ""},
        {timestamp: "0001-01-01T00:00:00Z", title: "B", summary: ""},
        {timestamp: ${JSON.stringify(ts2)}, title: "C", summary: ""}
      ], 0, 1000)`,
      ctx
    );
    // Only phases A and C have valid timestamps and should be rendered.
    expect(result.length).toBe(2);
    expect(result[0].title).toBe('A');
    expect(result[0].startMs).toBe(200);
    expect(result[0].endMs).toBe(700);
    expect(result[1].title).toBe('C');
    expect(result[1].startMs).toBe(700);
    expect(result[1].endMs).toBe(1000);
  });
});

// ---------------------------------------------------------------------------
// findPhaseForSpan
// ---------------------------------------------------------------------------
describe('findPhaseForSpan', () => {
  let ctx;
  beforeAll(() => {
    ctx = makeFlameContext().ctx;
  });

  it('returns null for empty phaseRegions', () => {
    const result = vm.runInContext(
      '_flamegraph.findPhaseForSpan({startMs: 500}, [])',
      ctx
    );
    expect(result).toBeNull();
  });

  it('returns null when span startMs is before all region startMs values', () => {
    const result = vm.runInContext(
      `_flamegraph.findPhaseForSpan({startMs: 50}, [{startMs: 100, endMs: 500, title: 'A'}])`,
      ctx
    );
    expect(result).toBeNull();
  });

  it('returns region when span startMs falls within a region', () => {
    const result = vm.runInContext(
      `_flamegraph.findPhaseForSpan({startMs: 300}, [{startMs: 100, endMs: 500, title: 'A'}])`,
      ctx
    );
    expect(result).not.toBeNull();
    expect(result.title).toBe('A');
  });

  it('inclusive lower boundary: span startMs === region startMs returns that region', () => {
    const result = vm.runInContext(
      `_flamegraph.findPhaseForSpan({startMs: 100}, [{startMs: 100, endMs: 500, title: 'A'}])`,
      ctx
    );
    expect(result).not.toBeNull();
    expect(result.title).toBe('A');
  });

  it('exclusive upper boundary: span startMs === region endMs returns next region or null', () => {
    // span.startMs === first region endMs → falls into second region
    const result = vm.runInContext(
      `_flamegraph.findPhaseForSpan({startMs: 500}, [
        {startMs: 100, endMs: 500, title: 'A'},
        {startMs: 500, endMs: 900, title: 'B'}
      ])`,
      ctx
    );
    expect(result).not.toBeNull();
    expect(result.title).toBe('B');
  });

  it('returns last region when span startMs is within it', () => {
    const result = vm.runInContext(
      `_flamegraph.findPhaseForSpan({startMs: 700}, [
        {startMs: 100, endMs: 500, title: 'A'},
        {startMs: 500, endMs: 900, title: 'B'}
      ])`,
      ctx
    );
    expect(result).not.toBeNull();
    expect(result.title).toBe('B');
  });
});

// ---------------------------------------------------------------------------
// loadFlamegraph — oversight integration tests
// ---------------------------------------------------------------------------
describe('loadFlamegraph oversight integration', () => {
  const now = 1000000; // fixed ms epoch for determinism

  const spansFixture = [
    {
      phase: 'impl',
      label: 'run',
      started_at: new Date(now).toISOString(),
      ended_at: new Date(now + 500).toISOString(),
      duration_ms: 500,
    },
  ];

  const oversightReady = {
    status: 'ready',
    phases: [
      {
        timestamp: new Date(now).toISOString(),
        title: 'Initial Exploration',
        summary: 'Explored the codebase.',
        tools_used: ['Read'],
        commands: [],
        actions: [],
      },
    ],
  };

  function makeDispatchFetch(spansResp, oversightResp) {
    return (url) => {
      if (typeof url === 'string' && url.includes('/oversight')) {
        return Promise.resolve({ json: () => Promise.resolve(oversightResp) });
      }
      return Promise.resolve({ json: () => Promise.resolve(spansResp) });
    };
  }

  it('renders phase band when oversight status is ready with phases', async () => {
    const { ctx, container } = makeFlameContext(
      makeDispatchFetch(spansFixture, oversightReady)
    );
    ctx.loadFlamegraph('task-1');
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toContain('Initial Exploration');
  });

  it('does not render phase band when oversight status is pending', async () => {
    const { ctx, container } = makeFlameContext(
      makeDispatchFetch(spansFixture, { status: 'pending', phases: [] })
    );
    ctx.loadFlamegraph('task-1');
    await new Promise((r) => setTimeout(r, 0));
    // Should still render spans but no phase band content
    expect(container.innerHTML).not.toContain('Initial Exploration');
    expect(container.innerHTML).toContain('impl:run');
  });

  it('renders normally when oversight fetch rejects', async () => {
    const fetch = (url) => {
      if (typeof url === 'string' && url.includes('/oversight')) {
        return Promise.reject(new Error('network error'));
      }
      return Promise.resolve({ json: () => Promise.resolve(spansFixture) });
    };
    const { ctx, container } = makeFlameContext(fetch);
    ctx.loadFlamegraph('task-1');
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toContain('impl:run');
    expect(container.innerHTML).not.toContain('Initial Exploration');
  });

  it('shows oversight phase title in detail table Oversight Phase column', async () => {
    const { ctx, container } = makeFlameContext(
      makeDispatchFetch(spansFixture, oversightReady)
    );
    ctx.loadFlamegraph('task-1');
    await new Promise((r) => setTimeout(r, 0));
    // The table should include the Oversight Phase header
    expect(container.innerHTML).toContain('Oversight Phase');
    // And the phase title in a td
    expect(container.innerHTML).toContain('Initial Exploration');
  });

  it('shows dash in Oversight Phase cell when no matching phase for span', async () => {
    // Span starts before any oversight phase
    const earlySpans = [
      {
        phase: 'impl',
        label: 'early',
        started_at: new Date(now - 5000).toISOString(),
        ended_at: new Date(now - 4000).toISOString(),
        duration_ms: 1000,
      },
    ];
    // Oversight phase starts at 'now', so the early span has no matching phase
    const { ctx, container } = makeFlameContext(
      makeDispatchFetch(earlySpans, oversightReady)
    );
    ctx.loadFlamegraph('task-1');
    await new Promise((r) => setTimeout(r, 0));
    expect(container.innerHTML).toContain('&mdash;');
  });
});
