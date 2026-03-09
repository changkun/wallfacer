/**
 * Tests for the collapsible oversight accordion on task cards (render.js).
 *
 * Verifies that:
 *  - done/failed cards get a <details class="card-oversight"> injected
 *  - the summary shows "Generating…" before any fetch or "N phases" from cache
 *  - opening the accordion for the first time fetches oversight and renders phases
 *  - subsequent toggles use the in-memory cache (no extra fetch)
 *  - buildPhaseListHTML (oversight-shared.js) renders phase titles and summaries
 */
import { describe, it, expect, beforeEach } from 'vitest';
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
// Minimal DOM element stub usable inside a vm context.
// ---------------------------------------------------------------------------
function makeEl(tag) {
  const el = {
    tagName: (tag || 'div').toLowerCase(),
    _html: '',
    _text: '',
    _listeners: {},
    _queries: {},
    dataset: {},
    open: false,
    className: '',
    style: {},
    onclick: null,
    classList: {
      _set: new Set(),
      add(c) { this._set.add(c); },
      remove(c) { this._set.delete(c); },
      toggle(c, f) {
        if (f !== undefined) { f ? this._set.add(c) : this._set.delete(c); }
        else { this._set.has(c) ? this._set.delete(c) : this._set.add(c); }
      },
      contains(c) { return this._set.has(c); },
    },
    get innerHTML() { return this._html; },
    set innerHTML(v) { this._html = String(v ?? ''); },
    get textContent() { return this._text; },
    set textContent(v) { this._text = String(v ?? ''); },
    addEventListener(evt, fn) {
      if (!this._listeners[evt]) this._listeners[evt] = [];
      this._listeners[evt].push(fn);
    },
    querySelector(sel) {
      return this._queries[sel] || null;
    },
    // Test helper: register a child element for a given CSS selector.
    _setQuery(sel, child) { this._queries[sel] = child; },
    // Test helper: fire all listeners for an event type.
    _fire(evt) { (this._listeners[evt] || []).forEach(fn => fn()); },
  };
  return el;
}

// ---------------------------------------------------------------------------
// Minimal task object with all fields that updateCard reads.
// ---------------------------------------------------------------------------
function makeTask(overrides) {
  return Object.assign({
    id: 'test-id',
    status: 'done',
    kind: '',
    prompt: 'Test task prompt',
    execution_prompt: '',
    title: 'Test Task',
    result: 'Completed successfully',
    stop_reason: '',
    session_id: null,
    fresh_start: false,
    archived: false,
    is_test_run: false,
    timeout: 15,
    sandbox: 'default',
    sandbox_by_activity: {},
    mount_worktrees: false,
    tags: [],
    depends_on: [],
    current_refinement: null,
    worktree_paths: {},
    position: 0,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    last_test_result: '',
    turns: 2,
  }, overrides);
}

// ---------------------------------------------------------------------------
// Build a vm context that satisfies all render.js runtime dependencies.
// ---------------------------------------------------------------------------
function makeRenderContext({ fetchImpl } = {}) {
  const bodyEl = makeEl('div');
  const summaryEl = makeEl('summary');
  const detailsEl = makeEl('details');
  detailsEl._setQuery('.card-oversight-body', bodyEl);
  detailsEl._setQuery('.card-oversight-summary', summaryEl);

  const cardEl = makeEl('div');
  // querySelector on card returns the detailsEl when the innerHTML contains
  // the oversight block (set by updateCard for done/failed tasks).
  cardEl.querySelector = function(sel) {
    if (sel === '.card-oversight' && this._html.includes('card-oversight')) {
      return detailsEl;
    }
    if (sel === '[data-diff]') return null;
    return null;
  };

  const defaultFetch = () => Promise.resolve({
    json: () => Promise.resolve({
      status: 'ready',
      phase_count: 2,
      phases: [
        { title: 'Phase Alpha', summary: 'Initial setup' },
        { title: 'Phase Beta', summary: 'Implementation work' },
      ],
    }),
  });

  const ctx = vm.createContext({
    console,
    Math,
    Date,
    Promise,
    fetch: fetchImpl || defaultFetch,
    document: {
      createElement: () => cardEl,
      getElementById: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: 'complete',
      addEventListener: () => {},
    },
    window: {
      depGraphEnabled: false,
      matchMedia: () => ({ matches: false, addEventListener: () => {} }),
    },
    localStorage: { getItem: () => null, setItem: () => {} },
    IntersectionObserver: class { observe() {} unobserve() {} disconnect() {} },
    clearInterval: () => {},
    setInterval: () => 0,
    requestAnimationFrame: (cb) => { if (cb) cb(); },
    // Stubs for functions from other modules consumed by render.js
    escapeHtml: (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'),
    timeAgo: () => '1h ago',
    formatTimeout: () => '15m',
    sandboxDisplayName: (s) => s || 'default',
    renderMarkdown: (s) => s || '',
    highlightMatch: (s) => s || '',
    taskDisplayPrompt: (t) => (t && t.prompt) || '',
    openModal: () => Promise.resolve(),
    tasks: [],
    filterQuery: '',
    maxParallelTasks: 0,
  });

  loadScript('state.js', ctx);
  loadScript('oversight-shared.js', ctx);
  loadScript('render.js', ctx);

  return { ctx, cardEl, detailsEl, bodyEl, summaryEl };
}

// Flush all pending microtasks (enough for a fetch().then().then() chain).
function flushPromises() {
  return new Promise((r) => setTimeout(r, 0));
}

// ---------------------------------------------------------------------------
// buildPhaseListHTML (oversight-shared.js)
// ---------------------------------------------------------------------------
describe('buildPhaseListHTML', () => {
  let ctx;

  beforeEach(() => {
    ctx = vm.createContext({
      console, Math, Date,
      escapeHtml: (s) => String(s ?? '').replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;').replace(/"/g, '&quot;'),
    });
    loadScript('oversight-shared.js', ctx);
  });

  it('returns oversight-empty div for null phases', () => {
    expect(ctx.buildPhaseListHTML(null)).toContain('oversight-empty');
  });

  it('returns oversight-empty div for empty array', () => {
    expect(ctx.buildPhaseListHTML([])).toContain('oversight-empty');
  });

  it('renders phase title and summary', () => {
    const html = ctx.buildPhaseListHTML([{ title: 'Setup', summary: 'Did setup' }]);
    expect(html).toContain('Setup');
    expect(html).toContain('Did setup');
    expect(html).toContain('Phase 1');
  });

  it('renders multiple phases with correct numbers', () => {
    const html = ctx.buildPhaseListHTML([
      { title: 'Alpha', summary: 'First' },
      { title: 'Beta', summary: 'Second' },
    ]);
    expect(html).toContain('Phase 1');
    expect(html).toContain('Phase 2');
    expect(html).toContain('Alpha');
    expect(html).toContain('Beta');
  });

  it('escapes HTML in title and summary', () => {
    const html = ctx.buildPhaseListHTML([{ title: '<b>X</b>', summary: '<em>y</em>' }]);
    expect(html).not.toContain('<b>');
    expect(html).toContain('&lt;b&gt;');
    expect(html).not.toContain('<em>');
  });
});

// ---------------------------------------------------------------------------
// Card accordion injection
// ---------------------------------------------------------------------------
describe('card oversight accordion — HTML injection', () => {
  it('injects <details class="card-oversight"> for a done task', () => {
    const { ctx, cardEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: 'done' }));
    expect(cardEl.innerHTML).toContain('card-oversight');
    expect(cardEl.innerHTML).toContain('<details');
  });

  it('injects oversight accordion for a failed task', () => {
    const { ctx, cardEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: 'failed' }));
    expect(cardEl.innerHTML).toContain('card-oversight');
  });

  it('does NOT inject oversight accordion for a waiting task', () => {
    const { ctx, cardEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: 'waiting' }));
    expect(cardEl.innerHTML).not.toContain('card-oversight');
  });

  it('does NOT inject oversight accordion for a backlog task', () => {
    const { ctx, cardEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: 'backlog' }));
    expect(cardEl.innerHTML).not.toContain('card-oversight');
  });

  it('does NOT inject oversight accordion for an archived done task', () => {
    const { ctx, cardEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: 'done', archived: true }));
    expect(cardEl.innerHTML).not.toContain('card-oversight');
  });
});

// ---------------------------------------------------------------------------
// Summary line — phase count display
// ---------------------------------------------------------------------------
describe('card oversight accordion — summary text', () => {
  it('shows "Generating…" in the summary when no cache entry exists', () => {
    const { ctx, cardEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: 'done' }));
    expect(cardEl.innerHTML).toContain('Generating');
    expect(cardEl.innerHTML).toContain('card-oversight-summary');
  });

  it('shows cached phase count in summary when cache is pre-populated', () => {
    const { ctx, cardEl } = makeRenderContext();
    // Pre-populate the cache before creating the card.
    vm.runInContext(
      `cardOversightCache.set('test-id', { phase_count: 3, phases: [] });`,
      ctx,
    );
    ctx.createCard(makeTask({ status: 'done' }));
    expect(cardEl.innerHTML).toContain('3 phases');
  });

  it('shows "2 phases" when cache has phase_count 2', () => {
    const { ctx, cardEl } = makeRenderContext();
    vm.runInContext(
      `cardOversightCache.set('test-id', { phase_count: 2, phases: [{ title: 'A', summary: '' }] });`,
      ctx,
    );
    ctx.createCard(makeTask({ status: 'done' }));
    expect(cardEl.innerHTML).toContain('2 phases');
  });
});

// ---------------------------------------------------------------------------
// Toggle event — lazy fetch and phase rendering
// ---------------------------------------------------------------------------
describe('card oversight accordion — toggle fetch', () => {
  it('fetches oversight on first open and renders both phase titles', async () => {
    const { ctx, cardEl, detailsEl, bodyEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: 'done' }));

    expect(cardEl.innerHTML).toContain('card-oversight');
    // Simulate opening the accordion.
    detailsEl.open = true;
    detailsEl._fire('toggle');
    await flushPromises();

    expect(bodyEl.innerHTML).toContain('Phase Alpha');
    expect(bodyEl.innerHTML).toContain('Phase Beta');
  });

  it('updates the summary textContent to "N phases" after successful fetch', async () => {
    const { ctx, cardEl, detailsEl, summaryEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: 'done' }));

    detailsEl.open = true;
    detailsEl._fire('toggle');
    await flushPromises();

    expect(summaryEl.textContent).toBe('2 phases');
  });

  it('stores fetched data in cardOversightCache', async () => {
    const { ctx, detailsEl } = makeRenderContext();
    ctx.createCard(makeTask({ status: 'done' }));

    detailsEl.open = true;
    detailsEl._fire('toggle');
    await flushPromises();

    const cached = vm.runInContext(`cardOversightCache.get('test-id')`, ctx);
    expect(cached).toBeDefined();
    expect(cached.phase_count).toBe(2);
    expect(cached.phases).toHaveLength(2);
  });

  it('does not fetch again on second toggle (data-loaded guard)', async () => {
    let fetchCount = 0;
    const fetchImpl = () => {
      fetchCount++;
      return Promise.resolve({
        json: () => Promise.resolve({
          status: 'ready',
          phase_count: 1,
          phases: [{ title: 'Only Phase', summary: '' }],
        }),
      });
    };
    const { ctx, detailsEl } = makeRenderContext({ fetchImpl });
    ctx.fetch = fetchImpl;
    ctx.createCard(makeTask({ status: 'done' }));

    // First toggle
    detailsEl.open = true;
    detailsEl._fire('toggle');
    await flushPromises();

    // Second toggle — data-loaded is already set, no re-fetch
    detailsEl._fire('toggle');
    await flushPromises();

    expect(fetchCount).toBe(1);
  });

  it('does not fetch when details is closed (open=false)', async () => {
    let fetchCount = 0;
    const fetchImpl = () => { fetchCount++; return Promise.resolve({ json: () => Promise.resolve({ status: 'ready', phase_count: 0, phases: [] }) }); };
    const { ctx, detailsEl } = makeRenderContext({ fetchImpl });
    ctx.fetch = fetchImpl;
    ctx.createCard(makeTask({ status: 'done' }));

    detailsEl.open = false;
    detailsEl._fire('toggle');
    await flushPromises();

    expect(fetchCount).toBe(0);
  });

  it('renders from cache on toggle when phases are already cached', async () => {
    const { ctx, cardEl, detailsEl, bodyEl } = makeRenderContext();
    // Pre-populate cache with phases.
    vm.runInContext(
      `cardOversightCache.set('test-id', { phase_count: 1, phases: [{ title: 'Cached Phase', summary: 'From cache' }] });`,
      ctx,
    );
    ctx.createCard(makeTask({ status: 'done' }));

    detailsEl.open = true;
    detailsEl._fire('toggle');
    await flushPromises();

    expect(bodyEl.innerHTML).toContain('Cached Phase');
  });

  it('shows error message when fetch fails', async () => {
    const fetchImpl = () => Promise.reject(new Error('network error'));
    const { ctx, detailsEl, bodyEl } = makeRenderContext({ fetchImpl });
    ctx.fetch = fetchImpl;
    ctx.createCard(makeTask({ status: 'done' }));

    detailsEl.open = true;
    detailsEl._fire('toggle');
    await flushPromises();

    expect(bodyEl.innerHTML).toContain('oversight-error');
  });
});

// ---------------------------------------------------------------------------
// Fingerprint includes cached phase_count so cards re-render when ready
// ---------------------------------------------------------------------------
describe('_cardFingerprint includes oversight phase_count', () => {
  it('fingerprint changes after phase_count is cached', () => {
    const { ctx } = makeRenderContext();
    const task = makeTask({ status: 'done' });

    const fp1 = vm.runInContext(
      `_cardFingerprint(${JSON.stringify(task)}, undefined)`,
      ctx,
    );

    vm.runInContext(
      `cardOversightCache.set('test-id', { phase_count: 4, phases: [] });`,
      ctx,
    );

    const fp2 = vm.runInContext(
      `_cardFingerprint(${JSON.stringify(task)}, undefined)`,
      ctx,
    );

    expect(fp1).not.toBe(fp2);
  });

  it('fingerprint is stable when cache does not change', () => {
    const { ctx } = makeRenderContext();
    const task = makeTask({ status: 'done' });

    const fp1 = vm.runInContext(`_cardFingerprint(${JSON.stringify(task)}, undefined)`, ctx);
    const fp2 = vm.runInContext(`_cardFingerprint(${JSON.stringify(task)}, undefined)`, ctx);

    expect(fp1).toBe(fp2);
  });
});
