/**
 * Tests for api.js helpers used across task routing and sandbox config.
 */
import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function makeInput(initial = false) {
  return { checked: initial, value: '' };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Date,
    Math,
    setTimeout,
    clearTimeout,
    EventSource: function EventSource() {},
    localStorage: {
      getItem: vi.fn(),
      setItem: vi.fn(),
    },
    location: { hash: '' },
    fetch: overrides.fetch,
    showAlert: vi.fn(),
    openModal: vi.fn().mockResolvedValue(undefined),
    setRightTab: vi.fn(),
    setLeftTab: vi.fn(),
    collectSandboxByActivity: () => ({}),
    populateSandboxSelects: vi.fn(),
    updateIdeationConfig: vi.fn(),
    api: vi.fn(),
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelectorAll: (selector) => {
        if (selector.includes('[data-sandbox-select]')) return elements.get('sandbox-selects') || [];
        return [];
      },
      querySelector: () => null,
      addEventListener: () => {},
      documentElement: { setAttribute: () => {} },
      readyState: 'complete',
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

describe('sandbox helpers', () => {
  it('formats sandbox labels consistently', () => {
    const ctx = makeContext();
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    expect(ctx.sandboxDisplayName('')).toBe('Default');
    expect(ctx.sandboxDisplayName('claude')).toBe('Claude');
    expect(ctx.sandboxDisplayName('codex')).toBe('Codex');
    expect(ctx.sandboxDisplayName('custom')).toBe('Custom');
  });

  it('collects and applies sandbox overrides by activity', () => {
    const ctx = makeContext({
      elements: [
        ['env-sandbox-implementation', { value: 'claude' }],
        ['env-sandbox-testing', { value: 'codex' }],
      ],
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    const collected = ctx.collectSandboxByActivity('env-sandbox-');
    expect(collected).toEqual({ implementation: 'claude', testing: 'codex' });

    const unknown = { value: 'openai' };
    const impl = { value: 'claude' };
    const testing = { value: 'codex' };
    const ctxWithTargets = makeContext({
      elements: [
        ['env-sandbox-implementation', impl],
        ['env-sandbox-testing', testing],
        ['env-sandbox-testing-2', { value: 'ignored' }],
      ],
    });
    loadScript(ctxWithTargets, 'state.js');
    loadScript(ctxWithTargets, 'api.js');
    ctxWithTargets.applySandboxByActivity('env-sandbox-', { implementation: 'custom', oversight: 'codex' });
    expect(impl.value).toBe('custom');
    expect(testing.value).toBe('');
    expect(unknown.value).toBe('openai');
  });
});

describe('_handleInitialHash', () => {
  it('opens modal for valid hash and maps right-hand tab', async () => {
    const ctx = makeContext({
      elements: [
        ['ideation-next-run', { textContent: '', style: {} }],
      ],
      location: { hash: '' },
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    vm.runInContext('tasks = [{ id: "11111111-1111-1111-1111-111111111111", title: "Task" }]; _hashHandled = false;', ctx);

    ctx.location.hash = '#11111111-1111-1111-1111-111111111111/testing';
    await ctx._handleInitialHash();

    expect(ctx.openModal).toHaveBeenCalledWith('11111111-1111-1111-1111-111111111111');
    expect(ctx.setRightTab).toHaveBeenCalledWith('testing');
  });

  it('ignores invalid hash values', async () => {
    const ctx = makeContext({
      location: { hash: '#not-a-uuid' },
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    ctx.tasks = [{ id: '11111111-1111-1111-1111-111111111111', title: 'Task' }];

    await ctx._handleInitialHash();
    expect(ctx.openModal).not.toHaveBeenCalled();
  });
});

describe('fetchConfig', () => {
  it('hydrates client config state and applies sandbox selectors', async () => {
    const cfg = {
      autopilot: true,
      autotest: true,
      autosubmit: false,
      sandboxes: ['claude', 'codex'],
      default_sandbox: 'claude',
      activity_sandboxes: { implementation: 'codex' },
      sandbox_usable: { claude: true },
      sandbox_reasons: { codex: 'Missing token' },
    };
    const autopilotToggle = makeInput(false);
    const autotestToggle = makeInput(false);
    const autosubmitToggle = makeInput(false);
    const fetch = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => cfg,
      text: async () => '',
    });
    const ctx = makeContext({
      elements: [
        ['autopilot-toggle', autopilotToggle],
        ['autotest-toggle', autotestToggle],
        ['autosubmit-toggle', autosubmitToggle],
      ],
      fetch,
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');
    const populateSandboxByActivitySpy = vi.spyOn(ctx, 'populateSandboxSelects');

    await ctx.fetchConfig();

    expect(autopilotToggle.checked).toBe(true);
    expect(autotestToggle.checked).toBe(true);
    expect(autosubmitToggle.checked).toBe(false);
    expect(populateSandboxByActivitySpy).toHaveBeenCalled();
    expect(ctx.updateIdeationConfig).toHaveBeenCalledWith(cfg);
    expect(vm.runInContext('autopilot', ctx)).toBe(true);
    expect(vm.runInContext('autotest', ctx)).toBe(true);
    expect(vm.runInContext('autosubmit', ctx)).toBe(false);
  });
});

describe('toggleAutopilot', () => {
  it('updates autopilot and reverts checkbox on API failure', async () => {
    const toggle = makeInput(false);
    const api = vi.fn().mockRejectedValueOnce(new Error('network down'));
    const ctx = makeContext({
      elements: [['autopilot-toggle', toggle]],
      api,
    });
    loadScript(ctx, 'state.js');
    loadScript(ctx, 'api.js');

    ctx.autopilot = false;
    await ctx.toggleAutopilot();
    expect(ctx.showAlert).toHaveBeenCalled();
    expect(toggle.checked).toBe(false);
  });
});
