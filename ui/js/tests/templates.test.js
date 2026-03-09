/**
 * Tests for template manager helpers.
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
    contains: (cls) => set.has(cls),
  };
}

function createElement(overrides = {}) {
  return {
    classList: createClassList(),
    style: {},
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console: {
      error: vi.fn(),
      log: () => {},
      warn: () => {},
    },
    Date,
    Math,
    Promise,
    alert: vi.fn(),
    window: {
      alert: vi.fn(),
    },
    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: () => createElement(),
      body: { appendChild: () => {} },
      addEventListener: () => {},
      querySelector: () => null,
    },
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), 'utf8');
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

describe('openTemplatesManagerFromSettings', () => {
  it('closes settings, prevents default, and opens templates manager', async () => {
    const settingsModal = createElement({ id: 'settings-modal' });
    const calls = [];
    const ctx = makeContext({
      elements: [['settings-modal', settingsModal]],
    });
    loadScript(ctx, 'templates.js');

    // Rebind these helpers after load so we can assert call order.
    vm.runInContext(
      `closeSettings = function() {
         calls.push('close');
         return;
       };
       openTemplatesManager = function() {
         calls.push('open');
         return Promise.resolve();
       };`,
      Object.assign(ctx, { calls }),
    );

    vm.runInContext(
      'openTemplatesManagerFromSettings({ preventDefault: function() { calls.push("pd"); } });',
      ctx,
    );
    await Promise.resolve();

    expect(calls).toEqual(['pd', 'close', 'open']);
  });

  it('alerts and logs when opening templates manager fails', async () => {
    const calls = [];
    const settingsModal = createElement({ id: 'settings-modal' });
    const ctx = makeContext({
      elements: [['settings-modal', settingsModal]],
      closeSettings: vi.fn(() => calls.push('close')),
    });
    loadScript(ctx, 'templates.js');
    vm.runInContext(
      `openTemplatesManager = function() {
         calls.push('open');
         return Promise.reject(new Error('network down'));
       };`,
      Object.assign(ctx, { calls }),
    );
    ctx.openTemplatesManagerFromSettings();
    await Promise.resolve();

    expect(calls).toEqual(['close', 'open']); // sync path executes both
    expect(ctx.closeSettings).toHaveBeenCalledTimes(1);
    expect(ctx.alert).toHaveBeenCalledWith('Failed to open Templates: network down');
    expect(ctx.console.error).toHaveBeenCalledWith(
      'Failed to open templates manager:',
      expect.anything(),
    );
    const loggedError = ctx.console.error.mock.calls[0][1];
    expect(String(loggedError && loggedError.message || loggedError)).toBe('network down');
  });
});
