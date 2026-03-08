/**
 * Tests for theme and settings modal helpers.
 */
import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function createClassList() {
  const set = new Set();
  return {
    add: (cls) => set.add(cls),
    remove: (cls) => set.delete(cls),
    toggle: (cls, force) => {
      const shouldBeAdded = force === undefined ? !set.has(cls) : force;
      if (shouldBeAdded) set.add(cls); else set.delete(cls);
      return shouldBeAdded;
    },
    contains: (cls) => set.has(cls),
    toString: () => Array.from(set).join(' '),
  };
}

function createElement(overrides = {}) {
  return {
    classList: createClassList(),
    style: {},
    setAttribute: vi.fn(),
    className: '',
    ...overrides,
  };
}

function createButton(mode, overrides = {}) {
  return {
    dataset: { mode },
    classList: createClassList(),
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const store = new Map(overrides.storage || []);
  const mediaState = { matches: overrides.matchDarkMode || false };
  let changeHandler = null;

  const localStorage = {
    getItem: (k) => (store.has(k) ? store.get(k) : null),
    setItem: vi.fn((k, v) => store.set(k, String(v))),
  };

  const ctx = {
    console,
    setTimeout,
    clearTimeout,
    Date,
    Math,
    localStorage,
    document: {
      documentElement: createElement(),
      querySelectorAll: (selector) => {
        if (selector === '#theme-switch button') return elements.get('theme-buttons') || [];
        return [];
      },
      getElementById: (id) => elements.get(id) || null,
      addEventListener: () => {},
    },
    window: {
      matchMedia: vi.fn(() => ({
        matches: mediaState.matches,
        addEventListener: (_type, handler) => {
          changeHandler = handler;
        },
      })),
      documentElement: createElement(),
    },
    ...overrides,
  };

  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx);
  return ctx;
}

describe('theme helpers', () => {
  it('initializes and resolves theme mode from storage', () => {
    const darkButton = createButton('dark');
    const autoButton = createButton('auto');
    const lightButton = createButton('light');
    const modal = createElement({
      textContent: '',
    });

    const ctx = makeContext({
      elements: [
        ['theme-buttons', [darkButton, autoButton, lightButton]],
        ['theme-switch', { id: 'theme-switch' }],
        ['settings-modal', modal],
      ],
      storage: [['wallfacer-theme', 'dark']],
      matchDarkMode: true,
      loadMaxParallel: vi.fn(),
      loadOversightInterval: vi.fn(),
      loadAutoPush: vi.fn(),
    });
    loadScript(ctx, 'theme.js');

    expect(darkButton.classList.contains('active')).toBe(true);
    expect(autoButton.classList.contains('active')).toBe(false);
    expect(ctx.getResolvedTheme('auto')).toBe('dark');
  });

  it('updates DOM and persistence when applying theme', () => {
    const autoButton = createButton('auto');
    const lightButton = createButton('light');
    const root = { setAttribute: vi.fn() };
    const ctx = makeContext({
      elements: [
        ['theme-buttons', [autoButton, lightButton]],
        ['theme-switch', { id: 'theme-switch' }],
        ['settings-modal', createElement({})],
      ],
      storage: [['wallfacer-theme', 'auto']],
      document: {
        documentElement: root,
        querySelectorAll: (selector) => {
          if (selector === '#theme-switch button') return [autoButton, lightButton];
          return [];
        },
        getElementById: (id) => ({
          'theme-buttons': [autoButton, lightButton],
          'theme-switch': { id: 'theme-switch' },
          'settings-modal': createElement({}),
        }[id] || null),
        addEventListener: () => {},
      },
      window: {
        matchMedia: vi.fn(() => ({
          matches: false,
          addEventListener: () => {},
        })),
        documentElement: root,
      },
      _matchMediaState: { matches: false },
      _setThemeChangeHandler: () => {},
    });

    loadScript(ctx, 'theme.js');
    ctx.setTheme('light');

    expect(root.setAttribute).toHaveBeenCalledWith('data-theme', 'light');
    expect(ctx.localStorage.setItem).toHaveBeenCalledWith('wallfacer-theme', 'light');
    expect(autoButton.classList.contains('active')).toBe(false);
    expect(lightButton.classList.contains('active')).toBe(true);
  });

  it('opens and closes settings modal while loading config', () => {
    const modal = createElement({ classList: createClassList(), style: {} });
    const ctx = makeContext({
      elements: [
        ['theme-buttons', [createButton('auto'), createButton('light'), createButton('dark')]],
        ['settings-modal', modal],
      ],
      loadMaxParallel: vi.fn(),
      loadOversightInterval: vi.fn(),
      loadAutoPush: vi.fn(),
    });
    loadScript(ctx, 'theme.js');

    ctx.openSettings();

    expect(modal.classList.contains('hidden')).toBe(false);
    expect(modal.style.display).toBe('flex');
    expect(ctx.loadMaxParallel).toHaveBeenCalledTimes(1);
    expect(ctx.loadOversightInterval).toHaveBeenCalledTimes(1);
    expect(ctx.loadAutoPush).toHaveBeenCalledTimes(1);

    ctx.closeSettings();

    expect(modal.classList.contains('hidden')).toBe(true);
    expect(modal.style.display).toBe('');
  });
});
