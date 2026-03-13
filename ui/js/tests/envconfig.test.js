/**
 * Tests for settings helpers in envconfig.js.
 */
import { describe, it, expect, vi } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';
import vm from 'vm';

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, '..');

function makeInput(value = '') {
  return { value, placeholder: '', textContent: '' };
}

function makeCheckbox(checked = false) {
  return { checked, value: '', placeholder: '', textContent: '' };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Date,
    Math,
    setTimeout: () => 0,
    collectSandboxByActivity: () => ({ implementation: 'codex', testing: 'claude' }),
    setInterval: () => 0,
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelector: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: 'complete',
      addEventListener: () => {},
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

describe('buildSaveEnvPayload', () => {
  it('omits token values when blank and preserves clear-to-empty model fields', () => {
    const ctx = makeContext({
      elements: [
        ['env-oauth-token', makeInput('token-a')],
        ['env-api-key', makeInput('api-b')],
        ['env-claude-base-url', makeInput('')],
        ['env-openai-api-key', makeInput('')],
        ['env-openai-base-url', makeInput('')],
        ['env-default-model', makeInput('')],
        ['env-title-model', makeInput('title')],
        ['env-codex-default-model', makeInput('codex-default')],
        ['env-codex-title-model', makeInput('')],
        ['env-default-sandbox', makeInput('codex')],
        ['env-sandbox-fast', makeCheckbox(true)],
      ],
    });
    loadScript(ctx, 'envconfig.js');

    const body = ctx.buildSaveEnvPayload();
    expect(body).toMatchObject({
      oauth_token: 'token-a',
      api_key: 'api-b',
      base_url: '',
      title_model: 'title',
      default_sandbox: 'codex',
      sandbox_by_activity: { implementation: 'codex', testing: 'claude' },
      codex_default_model: 'codex-default',
      codex_title_model: '',
      sandbox_fast: true,
    });
    expect(body.openai_api_key).toBeUndefined();
    expect(body).not.toHaveProperty('openai_api_key');
    expect(body.openai_base_url).toBe('');
    expect(body.default_model).toBe('');
  });
});

describe('buildSandboxTestPayload', () => {
  it('keeps claude fields for Claude-specific checks', () => {
    const ctx = makeContext({
      elements: [
        ['env-oauth-token', makeInput('token-a')],
        ['env-api-key', makeInput('')],
        ['env-claude-base-url', makeInput('https://claude')],
        ['env-openai-api-key', makeInput('')],
        ['env-openai-base-url', makeInput('')],
        ['env-default-model', makeInput('claude-model')],
        ['env-title-model', makeInput('claude-title')],
        ['env-codex-default-model', makeInput('')],
        ['env-codex-title-model', makeInput('')],
        ['env-default-sandbox', makeInput('claude')],
        ['env-sandbox-fast', makeCheckbox(true)],
      ],
      collectSandboxByActivity: () => ({ implementation: 'claude' }),
    });
    loadScript(ctx, 'envconfig.js');

    const payload = ctx.buildSandboxTestPayload('claude');
    expect(payload).toMatchObject({
      sandbox: 'claude',
      default_sandbox: 'claude',
      sandbox_by_activity: { implementation: 'claude' },
      sandbox_fast: true,
      base_url: 'https://claude',
      default_model: 'claude-model',
      title_model: 'claude-title',
      oauth_token: 'token-a',
    });
    expect(payload).not.toHaveProperty('openai_base_url');
    expect(payload).not.toHaveProperty('openai_api_key');
  });

  it('keeps OpenAI fields for Codex-specific checks', () => {
    const ctx = makeContext({
      elements: [
        ['env-oauth-token', makeInput('')],
        ['env-api-key', makeInput('')],
        ['env-claude-base-url', makeInput('')],
        ['env-openai-api-key', makeInput('openai')],
        ['env-openai-base-url', makeInput('https://openai')],
        ['env-default-model', makeInput('')],
        ['env-title-model', makeInput('')],
        ['env-codex-default-model', makeInput('codex-default')],
        ['env-codex-title-model', makeInput('codex-title')],
        ['env-default-sandbox', makeInput('codex')],
        ['env-sandbox-fast', makeCheckbox(false)],
      ],
      collectSandboxByActivity: () => ({ testing: 'codex' }),
    });
    loadScript(ctx, 'envconfig.js');

    const payload = ctx.buildSandboxTestPayload('codex');
    expect(payload).toMatchObject({
      sandbox: 'codex',
      default_sandbox: 'codex',
      sandbox_by_activity: { testing: 'codex' },
      sandbox_fast: false,
      openai_base_url: 'https://openai',
      codex_default_model: 'codex-default',
      codex_title_model: 'codex-title',
      openai_api_key: 'openai',
    });
    expect(payload).not.toHaveProperty('default_model');
    expect(payload).not.toHaveProperty('oauth_token');
  });
});

describe('summarizeSandboxTestResult', () => {
  it('normalizes pass/fail and status responses', () => {
    const ctx = makeContext();
    loadScript(ctx, 'envconfig.js');

    expect(ctx.summarizeSandboxTestResult(null)).toBe('No response');
    expect(ctx.summarizeSandboxTestResult({ last_test_result: 'pass' })).toBe('PASS');
    expect(ctx.summarizeSandboxTestResult({ last_test_result: 'FAIL' })).toBe('FAIL');
    expect(ctx.summarizeSandboxTestResult({ status: 'done' })).toBe('Test completed');
    expect(ctx.summarizeSandboxTestResult({ status: 'failed', result: 'Syntax error' })).toBe('Syntax error');
    expect(ctx.summarizeSandboxTestResult({ status: 'running' })).toBe('status running');
  });
});

describe('loadEnvConfig', () => {
  it('loads environment data into settings fields and refreshes sandbox selectors', async () => {
    const oauthTokenEl = makeInput('');
    const apiKeyEl = makeInput('');
    const openaiApiKeyEl = makeInput('');
    const claudeBaseUrlEl = makeInput('');
    const openaiBaseUrlEl = makeInput('');
    const defaultModelEl = makeInput('');
    const titleModelEl = makeInput('');
    const codexDefaultModelEl = makeInput('');
    const codexTitleModelEl = makeInput('');
    const defaultSandboxEl = makeInput('');
    const sandboxFastEl = makeCheckbox(false);
    const statusEl = makeInput('');
    const implementationEl = { value: '' };
    const testingEl = { value: '' };
    const codexTestStatusEl = makeInput('');
    const claudeTestStatusEl = makeInput('');

    const applySandboxByActivity = vi.fn();
    const populateSandboxSelects = vi.fn();
    const api = vi.fn().mockResolvedValue({
      oauth_token: 'token-from-server',
      api_key: 'anthropic-api-key',
      base_url: 'https://api.anthropic.com',
      openai_api_key: 'openai-key',
      openai_base_url: 'https://api.openai.com/v1',
      default_model: 'claude-model',
      title_model: 'claude-title',
      codex_default_model: 'codex-default',
      codex_title_model: 'codex-title',
      default_sandbox: 'codex',
      sandbox_by_activity: { implementation: 'claude', testing: 'codex' },
      sandbox_fast: true,
    });

    const ctx = makeContext({
      elements: [
        ['env-oauth-token', oauthTokenEl],
        ['env-api-key', apiKeyEl],
        ['env-openai-api-key', openaiApiKeyEl],
        ['env-claude-base-url', claudeBaseUrlEl],
        ['env-openai-base-url', openaiBaseUrlEl],
        ['env-default-model', defaultModelEl],
        ['env-title-model', titleModelEl],
        ['env-codex-default-model', codexDefaultModelEl],
        ['env-codex-title-model', codexTitleModelEl],
        ['env-default-sandbox', defaultSandboxEl],
        ['env-sandbox-fast', sandboxFastEl],
        ['env-config-status', statusEl],
        ['env-sandbox-implementation', implementationEl],
        ['env-sandbox-testing', testingEl],
        ['env-claude-test-status', claudeTestStatusEl],
        ['env-codex-test-status', codexTestStatusEl],
      ],
      applySandboxByActivity,
      populateSandboxSelects,
      api,
      Routes: {
        env: {
          get: () => '/api/env',
          test: '/api/env/test',
          update: '/api/env',
        },
      },
    });

    loadScript(ctx, 'envconfig.js');
    await ctx.loadEnvConfig();

    expect(api).toHaveBeenCalledWith('/api/env');
    expect(oauthTokenEl.value).toBe('');
    expect(oauthTokenEl.placeholder).toBe('token-from-server');
    expect(apiKeyEl.placeholder).toBe('anthropic-api-key');
    expect(openaiApiKeyEl.placeholder).toBe('openai-key');
    expect(claudeBaseUrlEl.value).toBe('https://api.anthropic.com');
    expect(openaiBaseUrlEl.value).toBe('https://api.openai.com/v1');
    expect(defaultModelEl.value).toBe('claude-model');
    expect(titleModelEl.value).toBe('claude-title');
    expect(codexDefaultModelEl.value).toBe('codex-default');
    expect(codexTitleModelEl.value).toBe('codex-title');
    expect(defaultSandboxEl.value).toBe('codex');
    expect(sandboxFastEl.checked).toBe(true);
    expect(statusEl.textContent).toBe('');
    expect(claudeTestStatusEl.textContent).toBe('');
    expect(codexTestStatusEl.textContent).toBe('');
    expect(applySandboxByActivity).toHaveBeenCalledWith('env-sandbox-', {
      implementation: 'claude',
      testing: 'codex',
    });
    expect(populateSandboxSelects).toHaveBeenCalled();
  });

  it('shows a load error but keeps blank config when env fetch fails', async () => {
    const statusEl = makeInput('');
    const oauthTokenEl = makeInput('manual-entry');
    const api = vi.fn().mockRejectedValue(new Error('network error'));

    const ctx = makeContext({
      elements: [
        ['env-oauth-token', oauthTokenEl],
        ['env-api-key', makeInput('')],
        ['env-openai-api-key', makeInput('')],
        ['env-claude-base-url', makeInput('')],
        ['env-openai-base-url', makeInput('')],
        ['env-default-model', makeInput('')],
        ['env-title-model', makeInput('')],
        ['env-codex-default-model', makeInput('')],
        ['env-codex-title-model', makeInput('')],
        ['env-default-sandbox', makeInput('')],
        ['env-sandbox-fast', makeCheckbox(false)],
        ['env-config-status', statusEl],
        ['env-claude-test-status', makeInput('')],
        ['env-codex-test-status', makeInput('')],
      ],
      api,
      applySandboxByActivity: vi.fn(),
      Routes: {
        env: {
          get: () => '/api/env',
          test: '/api/env/test',
          update: '/api/env',
        },
      },
    });

    loadScript(ctx, 'envconfig.js');
    await ctx.loadEnvConfig();

    expect(statusEl.textContent).toBe('Failed to load configuration.');
    expect(oauthTokenEl.value).toBe('');
    expect(oauthTokenEl.placeholder).toBe('(not set)');
  });
});

describe('archived tasks page size setting', () => {
  it('loads archived_tasks_per_page into input and global state', async () => {
    const input = makeInput('');
    const api = vi.fn().mockResolvedValue({ archived_tasks_per_page: 42 });
    const ctx = makeContext({
      elements: [['archived-page-size-input', input]],
      api,
      Routes: {
        env: {
          get: () => '/api/env',
          update: () => '/api/env',
        },
      },
      archivedTasksPageSize: 0,
    });
    loadScript(ctx, 'envconfig.js');

    await ctx.loadArchivedTasksPerPage();
    expect(api).toHaveBeenCalledWith('/api/env');
    expect(input.value).toBe(42);
    expect(vm.runInContext('archivedTasksPageSize', ctx)).toBe(42);
  });

  it('saves archived_tasks_per_page with clamping', async () => {
    const input = makeInput('999');
    const status = makeInput('');
    const api = vi.fn().mockResolvedValue(null);
    const loadArchivedTasksPage = vi.fn().mockResolvedValue(undefined);
    const ctx = makeContext({
      elements: [
        ['archived-page-size-input', input],
        ['archived-page-size-status', status],
      ],
      api,
      showArchived: true,
      loadArchivedTasksPage,
      Routes: {
        env: {
          get: () => '/api/env',
          update: () => '/api/env',
        },
      },
    });
    loadScript(ctx, 'envconfig.js');

    await ctx.saveArchivedTasksPerPage();
    expect(input.value).toBe(200);
    expect(api).toHaveBeenCalledWith('/api/env', {
      method: 'PUT',
      body: JSON.stringify({ archived_tasks_per_page: 200 }),
    });
    expect(vm.runInContext('archivedTasksPageSize', ctx)).toBe(200);
    expect(loadArchivedTasksPage).toHaveBeenCalledWith('initial');
  });
});
