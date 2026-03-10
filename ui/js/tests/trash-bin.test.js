import { describe, it, expect, beforeEach, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function makeClassList(initial = []) {
  const items = new Set(initial);
  return {
    add(cls) { items.add(cls); },
    remove(cls) { items.delete(cls); },
    toggle(cls, force) {
      if (force === undefined) {
        if (items.has(cls)) {
          items.delete(cls);
          return false;
        }
        items.add(cls);
        return true;
      }
      if (force) {
        items.add(cls);
        return true;
      }
      items.delete(cls);
      return false;
    },
    contains(cls) { return items.has(cls); },
    toString() { return Array.from(items).join(' '); },
  };
}

function createElement(tagName = 'div', overrides = {}) {
  const element = {
    tagName: String(tagName).toUpperCase(),
    _children: [],
    _listeners: {},
    _parent: null,
    dataset: {},
    style: {},
    attributes: {},
    classList: makeClassList(),
    textContent: '',
    _innerHTML: '',
    id: '',
    disabled: false,
    appendChild(child) {
      child._parent = this;
      this._children.push(child);
      return child;
    },
    remove() {
      if (!this._parent) return;
      this._parent._children = this._parent._children.filter((item) => item !== this);
      this._parent = null;
    },
    addEventListener(type, handler) {
      this._listeners[type] = this._listeners[type] || [];
      this._listeners[type].push(handler);
    },
    dispatchEvent(evt) {
      (this._listeners[evt.type] || []).forEach((handler) => handler(evt));
    },
    setAttribute(name, value) {
      this.attributes[name] = String(value);
    },
    querySelectorAll(selector) {
      const results = [];
      const matches = (node) => {
        if (selector.startsWith('.')) return node.classList.contains(selector.slice(1));
        if (selector.startsWith('#')) return node.id === selector.slice(1);
        if (selector === '[role="listitem"]') return node.attributes.role === 'listitem';
        return node.tagName === selector.toUpperCase();
      };
      const visit = (node) => {
        node._children.forEach((child) => {
          if (matches(child)) results.push(child);
          visit(child);
        });
      };
      visit(this);
      return results;
    },
  };

  Object.defineProperty(element, 'children', {
    get() { return this._children; },
  });
  Object.defineProperty(element, 'innerHTML', {
    get() { return this._innerHTML; },
    set(value) {
      this._innerHTML = String(value);
      this._children = [];
      this.textContent = String(value).replace(/<[^>]+>/g, '');
    },
  });

  return Object.assign(element, overrides);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
}

function collectText(node) {
  let text = node.textContent || '';
  node._children.forEach((child) => {
    text += collectText(child);
  });
  return text;
}

function setupContext(fetchImpl) {
  const elements = new Map();
  const register = (id, element) => {
    element.id = id;
    elements.set(id, element);
    return element;
  };

  register('trash-bin-error', createElement('div'));
  elements.get('trash-bin-error').classList.add('hidden');
  register('trash-bin-error-message', createElement('span'));
  register('trash-bin-error-dismiss', createElement('button'));
  register('trash-bin-loading', createElement('div'));
  elements.get('trash-bin-loading').classList.add('hidden');
  register('trash-bin-empty', createElement('div'));
  elements.get('trash-bin-empty').classList.add('hidden');
  register('trash-bin-list', createElement('div'));
  register('trash-bin-toast', createElement('div'));
  elements.get('trash-bin-toast').classList.add('hidden');

  const ctx = vm.createContext({
    console,
    Date: class extends Date {
      constructor(value) {
        super(value === undefined ? '2026-03-10T12:00:00Z' : value);
      }
      static now() {
        return new Date('2026-03-10T12:00:00Z').getTime();
      }
      static parse(value) {
        return Date.parse(value);
      }
    },
    Math,
    setTimeout: () => 1,
    clearTimeout: () => {},
    fetch: fetchImpl,
    api: async (path, opts = {}) => {
      const headers = { 'Content-Type': 'application/json', ...(opts.headers || {}) };
      const res = await fetchImpl(path, { headers, signal: opts.signal, ...opts });
      if (!res.ok && res.status !== 204) {
        throw new Error(await res.text());
      }
      if (res.status === 204) return null;
      return res.json();
    },
    window: {
      matchMedia: () => ({ matches: false, addEventListener: () => {} }),
    },
    localStorage: {
      getItem: () => null,
      setItem: () => {},
    },
    IntersectionObserver: class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
    document: {
      createElement: (tag) => createElement(tag),
      getElementById: (id) => elements.get(id) || null,
      readyState: 'complete',
      addEventListener: () => {},
      documentElement: { setAttribute: () => {} },
    },
  });

  loadScript(ctx, 'generated/routes.js');
  loadScript(ctx, 'utils.js');
  loadScript(ctx, 'trash-bin.js');
  ctx.initTrashBin();

  return { ctx, elements };
}

describe('trash-bin', () => {
  beforeEach(() => {
    vi.restoreAllMocks();
  });

  it('renders deleted rows with title fallback and remaining days', async () => {
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ([
        {
          id: 'task-1',
          title: 'Fix flaky restore flow',
          prompt: 'unused',
          status: 'waiting',
          updated_at: '2026-03-07T12:00:00Z',
        },
        {
          id: 'task-2',
          title: '',
          prompt: 'This prompt should be truncated to sixty characters for the trash panel display and no more.',
          status: 'backlog',
          updated_at: '2026-03-03T12:00:00Z',
        },
      ]),
      text: async () => '',
    });
    const { ctx, elements } = setupContext(fetch);

    await ctx.loadDeletedTasks();

    const rows = elements.get('trash-bin-list').children;
    expect(rows).toHaveLength(2);
    expect(collectText(rows[0])).toContain('Fix flaky restore flow');
    expect(collectText(rows[0])).toContain('4 days remaining');
    expect(collectText(rows[1])).toContain('This prompt should be truncated to sixty characters for the');
    expect(collectText(rows[1])).toContain('\u2026');
    expect(collectText(rows[1])).toContain('0 days remaining');
  });

  it('restores a deleted task, removes the row, and shows a toast', async () => {
    const fetch = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ([
          {
            id: 'task-restore',
            title: 'Bring back config migration',
            prompt: 'unused',
            status: 'failed',
            updated_at: '2026-03-08T12:00:00Z',
          },
        ]),
        text: async () => '',
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({}),
        text: async () => '',
      });
    const { ctx, elements } = setupContext(fetch);

    await ctx.loadDeletedTasks();

    const row = elements.get('trash-bin-list').children[0];
    const restoreButton = row.children[1];

    await ctx.restoreDeletedTask('task-restore', row, restoreButton, 'Bring back config migration');

    expect(elements.get('trash-bin-list').children).toHaveLength(0);
    expect(elements.get('trash-bin-empty').classList.contains('hidden')).toBe(false);
    expect(elements.get('trash-bin-toast').textContent).toBe('Restored "Bring back config migration"');
    expect(elements.get('trash-bin-toast').classList.contains('hidden')).toBe(false);
    expect(fetch).toHaveBeenNthCalledWith(
      2,
      '/api/tasks/task-restore/restore',
      expect.objectContaining({ method: 'POST' }),
    );
  });

  it('shows the empty placeholder when no deleted tasks exist', async () => {
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ([]),
      text: async () => '',
    });
    const { ctx, elements } = setupContext(fetch);

    await ctx.loadDeletedTasks();

    expect(elements.get('trash-bin-empty').classList.contains('hidden')).toBe(false);
    expect(elements.get('trash-bin-list').children).toHaveLength(0);
  });
});
