<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { api } from '../../api/client';
import { useTaskStore } from '../../stores/tasks';
import { useEnvConfig } from '../../composables/useEnvConfig';
import { claudeModelsFor, codexModelsFor } from '../../lib/knownModels';
import { supportedHarnesses } from '../../lib/harness';
import HarnessBadge from '../HarnessBadge.vue';
import AppSelect from '../AppSelect.vue';
import type {
  EnvConfig,
  EnvUpdatePayload,
  SandboxTestResponse,
} from '../../api/types';

const taskStore = useTaskStore();
const { env, fetchEnv, updateEnv } = useEnvConfig();

// --- Sandbox list ---
// Fall back to the full harness registry so the default-harness select is
// never empty before /api/config loads its authoritative `sandboxes` list.
const sandboxes = computed<string[]>(() => supportedHarnesses(taskStore.config?.sandboxes));

// --- Form state (local refs bound to inputs) ---
const oauthToken = ref('');
const apiKey = ref('');
const claudeBaseUrl = ref('');
const openaiApiKey = ref('');
const openaiBaseUrl = ref('');
const cursorApiKey = ref('');
const defaultModel = ref('');
const titleModel = ref('');
const codexDefaultModel = ref('');
const codexTitleModel = ref('');
const defaultSandbox = ref('');

const claudeModels = computed(() => claudeModelsFor(claudeBaseUrl.value));
const codexModels = computed(() => codexModelsFor(openaiBaseUrl.value));

const claudeTestStatus = ref('');
const claudeTestReauth = ref(false);
const codexTestStatus = ref('');
const codexTestReauth = ref(false);
const cursorTestStatus = ref('');
const piTestStatus = ref('');
const opencodeTestStatus = ref('');
const saveStatus = ref('');

const claudeOauthStatus = ref('');
const codexOauthStatus = ref('');
const claudeOauthBusy = ref(false);
const codexOauthBusy = ref(false);

// Placeholders for token fields show the masked value from /api/env.
const oauthTokenPlaceholder = computed(() => env.value?.oauth_token || '(not set)');
const apiKeyPlaceholder = computed(() => env.value?.api_key || '(not set)');
const openaiApiKeyPlaceholder = computed(() => env.value?.openai_api_key || '(not set)');
const cursorApiKeyPlaceholder = computed(() => env.value?.cursor_api_key || '(not set)');

// First-launch hints — show when nothing is configured for that provider.
const claudeHasCreds = computed(() => {
  const t = env.value?.oauth_token;
  const k = env.value?.api_key;
  return !!((t && t !== '(not set)') || (k && k !== '(not set)'));
});
const codexHasCreds = computed(() => {
  const k = env.value?.openai_api_key;
  return !!(k && k !== '(not set)');
});
const cursorHasCreds = computed(() => {
  const k = env.value?.cursor_api_key;
  return !!(k && k !== '(not set)');
});
// First-launch banner: no credentials for either provider. Mirrors the
// legacy envconfig.js "No API credentials configured" alert.
const noCredentials = computed(() => env.value != null && !claudeHasCreds.value && !codexHasCreds.value);

// OAuth sign-in buttons hide when a custom base URL is set.
const showClaudeOauthBtn = computed(() => !claudeBaseUrl.value);
const showCodexOauthBtn = computed(() => !openaiBaseUrl.value);

function applyEnvToForm(cfg: EnvConfig | null): void {
  oauthToken.value = '';
  apiKey.value = '';
  openaiApiKey.value = '';
  cursorApiKey.value = '';
  claudeBaseUrl.value = cfg?.base_url || '';
  openaiBaseUrl.value = cfg?.openai_base_url || '';
  defaultModel.value = cfg?.default_model || '';
  titleModel.value = cfg?.title_model || '';
  codexDefaultModel.value = cfg?.codex_default_model || '';
  codexTitleModel.value = cfg?.codex_title_model || '';
  defaultSandbox.value = cfg?.default_sandbox || '';
  claudeTestStatus.value = '';
  claudeTestReauth.value = false;
  codexTestStatus.value = '';
  codexTestReauth.value = false;
  cursorTestStatus.value = '';
  piTestStatus.value = '';
  opencodeTestStatus.value = '';
}

watch(env, (cfg) => applyEnvToForm(cfg), { immediate: false });

// --- Save / revert ---
function buildSavePayload(): EnvUpdatePayload {
  const body: EnvUpdatePayload = {};
  const oauthRaw = oauthToken.value.trim();
  const apiKeyRaw = apiKey.value.trim();
  const openaiRaw = openaiApiKey.value.trim();
  if (oauthRaw) body.oauth_token = oauthRaw;
  if (apiKeyRaw) body.api_key = apiKeyRaw;
  body.base_url = claudeBaseUrl.value.trim();
  if (openaiRaw) body.openai_api_key = openaiRaw;
  body.openai_base_url = openaiBaseUrl.value.trim();
  const cursorRaw = cursorApiKey.value.trim();
  if (cursorRaw) body.cursor_api_key = cursorRaw;
  body.default_model = defaultModel.value.trim();
  body.title_model = titleModel.value.trim();
  body.codex_default_model = codexDefaultModel.value.trim();
  body.codex_title_model = codexTitleModel.value.trim();
  body.default_sandbox = defaultSandbox.value.trim();
  // Activity-specific routing is retired — send empty map so the server
  // clears any legacy WALLFACER_SANDBOX_* entries.
  body.sandbox_by_activity = {};
  return body;
}

async function saveConfig(): Promise<void> {
  saveStatus.value = 'Saving…';
  try {
    await updateEnv(buildSavePayload());
    saveStatus.value = 'Saved.';
    // Clear sensitive inputs after save.
    oauthToken.value = '';
    apiKey.value = '';
    openaiApiKey.value = '';
    cursorApiKey.value = '';
    window.setTimeout(() => {
      saveStatus.value = '';
    }, 2000);
  } catch (e) {
    saveStatus.value = 'Error: ' + (e instanceof Error ? e.message : String(e));
  }
}

async function revertConfig(): Promise<void> {
  saveStatus.value = '';
  await fetchEnv();
}

// --- Test sandbox config ---
interface ClaudeTestPayload {
  sandbox: 'claude';
  default_sandbox: string;
  sandbox_by_activity: Record<string, string>;
  base_url?: string;
  default_model?: string;
  title_model?: string;
  oauth_token?: string;
  api_key?: string;
}

interface CodexTestPayload {
  sandbox: 'codex';
  default_sandbox: string;
  sandbox_by_activity: Record<string, string>;
  openai_base_url?: string;
  codex_default_model?: string;
  codex_title_model?: string;
  openai_api_key?: string;
}

interface CursorTestPayload {
  sandbox: 'cursor';
  default_sandbox: string;
  sandbox_by_activity: Record<string, string>;
  cursor_api_key?: string;
}

// Pi has no dedicated credential in v1: it reads provider keys (Anthropic,
// OpenAI, etc.) from the same env the Claude/Codex blocks configure.
interface PiTestPayload {
  sandbox: 'pi';
  default_sandbox: string;
  sandbox_by_activity: Record<string, string>;
}

// OpenCode manages provider auth itself (opencode auth login), so the test
// payload carries no credentials, only the routing context.
interface OpenCodeTestPayload {
  sandbox: 'opencode';
  default_sandbox: string;
  sandbox_by_activity: Record<string, string>;
}

type TestPayload =
  | ClaudeTestPayload
  | CodexTestPayload
  | CursorTestPayload
  | PiTestPayload
  | OpenCodeTestPayload;

function buildTestPayload(sandbox: 'claude' | 'codex' | 'cursor' | 'pi' | 'opencode'): TestPayload {
  const raw = buildSavePayload();
  if (sandbox === 'claude') {
    const p: ClaudeTestPayload = {
      sandbox: 'claude',
      default_sandbox: raw.default_sandbox || '',
      sandbox_by_activity: raw.sandbox_by_activity || {},
      base_url: raw.base_url,
      default_model: raw.default_model,
      title_model: raw.title_model,
    };
    if (raw.oauth_token) p.oauth_token = raw.oauth_token;
    if (raw.api_key) p.api_key = raw.api_key;
    return p;
  }
  if (sandbox === 'cursor') {
    const p: CursorTestPayload = {
      sandbox: 'cursor',
      default_sandbox: raw.default_sandbox || '',
      sandbox_by_activity: raw.sandbox_by_activity || {},
    };
    if (raw.cursor_api_key) p.cursor_api_key = raw.cursor_api_key;
    return p;
  }
  if (sandbox === 'pi') {
    const p: PiTestPayload = {
      sandbox: 'pi',
      default_sandbox: raw.default_sandbox || '',
      sandbox_by_activity: raw.sandbox_by_activity || {},
    };
    return p;
  }
  if (sandbox === 'opencode') {
    const p: OpenCodeTestPayload = {
      sandbox: 'opencode',
      default_sandbox: raw.default_sandbox || '',
      sandbox_by_activity: raw.sandbox_by_activity || {},
    };
    return p;
  }
  const p: CodexTestPayload = {
    sandbox: 'codex',
    default_sandbox: raw.default_sandbox || '',
    sandbox_by_activity: raw.sandbox_by_activity || {},
    openai_base_url: raw.openai_base_url,
    codex_default_model: raw.codex_default_model,
    codex_title_model: raw.codex_title_model,
  };
  if (raw.openai_api_key) p.openai_api_key = raw.openai_api_key;
  return p;
}

function summarizeTestResult(resp: SandboxTestResponse | null | undefined): string {
  if (!resp) return 'No response';
  const normalized = (resp.last_test_result || '').toUpperCase();
  if (normalized === 'PASS') return 'PASS';
  if (normalized === 'FAIL') return 'FAIL';
  if (resp.status === 'failed' && (resp.result || resp.stop_reason)) {
    return (resp.result || resp.stop_reason || '').slice(0, 120);
  }
  if (resp.status === 'done' || resp.status === 'waiting') {
    return 'Test completed';
  }
  return `status ${resp.status}`;
}

function setTestStatus(sandbox: 'claude' | 'codex' | 'cursor' | 'pi' | 'opencode', text: string, reauth: boolean): void {
  if (sandbox === 'claude') {
    claudeTestStatus.value = text;
    claudeTestReauth.value = reauth;
  } else if (sandbox === 'codex') {
    codexTestStatus.value = text;
    codexTestReauth.value = reauth;
  } else if (sandbox === 'pi') {
    piTestStatus.value = text;
  } else if (sandbox === 'opencode') {
    opencodeTestStatus.value = text;
  } else {
    cursorTestStatus.value = text;
  }
}

async function testSandbox(sandbox: 'claude' | 'codex' | 'cursor' | 'pi' | 'opencode'): Promise<void> {
  setTestStatus(sandbox, 'Testing…', false);
  try {
    const resp = await api<SandboxTestResponse>('POST', '/api/env/test', buildTestPayload(sandbox));
    const text = summarizeTestResult(resp);
    setTestStatus(sandbox, text, !!resp.reauth_available);
    window.setTimeout(() => {
      const isFailish =
        text.includes('FAIL') ||
        text.startsWith('status failed') ||
        text.startsWith('No response') ||
        resp.reauth_available;
      if (isFailish) return;
      setTestStatus(sandbox, '', false);
    }, 6000);
  } catch (e) {
    const msg = 'Error: ' + (e instanceof Error ? e.message : String(e));
    setTestStatus(sandbox, msg, false);
    window.setTimeout(() => setTestStatus(sandbox, '', false), 6000);
  }
}

// --- OAuth flow ---
const oauthPollers: Record<string, number | undefined> = {};

interface OAuthStartResponse {
  authorize_url?: string;
}

interface OAuthStatusResponse {
  state: 'pending' | 'success' | 'error';
  error?: string;
}

function setOauthState(provider: 'claude' | 'codex', text: string, busy: boolean): void {
  if (provider === 'claude') {
    claudeOauthStatus.value = text;
    claudeOauthBusy.value = busy;
  } else {
    codexOauthStatus.value = text;
    codexOauthBusy.value = busy;
  }
}

function stopOauthPolling(provider: 'claude' | 'codex', errorMessage: string): void {
  const id = oauthPollers[provider];
  if (id !== undefined) {
    window.clearInterval(id);
    oauthPollers[provider] = undefined;
  }
  setOauthState(provider, errorMessage, false);
}

function pollOauth(provider: 'claude' | 'codex'): void {
  const existing = oauthPollers[provider];
  if (existing !== undefined) {
    window.clearInterval(existing);
  }
  let pollCount = 0;
  const maxPolls = 150;
  oauthPollers[provider] = window.setInterval(async () => {
    pollCount++;
    if (pollCount > maxPolls) {
      stopOauthPolling(provider, 'Timed out waiting for authorization.');
      return;
    }
    try {
      const result = await api<OAuthStatusResponse>('GET', `/api/auth/${provider}/status`);
      if (result.state === 'success') {
        stopOauthPolling(provider, 'Signed in!');
        await fetchEnv();
        window.setTimeout(() => {
          setOauthState(provider, '', false);
        }, 3000);
      } else if (result.state === 'error') {
        stopOauthPolling(provider, result.error || 'Authorization failed.');
      }
    } catch {
      // Network error — keep polling, it might recover.
    }
  }, 2000);
}

async function startOauthFlow(provider: 'claude' | 'codex'): Promise<void> {
  setOauthState(provider, 'Starting...', true);
  try {
    const result = await api<OAuthStartResponse>('POST', `/api/auth/${provider}/start`);
    if (!result.authorize_url) {
      setOauthState(provider, 'Error: no authorize URL returned', false);
      return;
    }
    window.open(result.authorize_url, '_blank');
    setOauthState(provider, 'Waiting for browser...', true);
    pollOauth(provider);
  } catch (e) {
    setOauthState(provider, 'Error: ' + (e instanceof Error ? e.message : String(e)), false);
  }
}

async function cancelOauthFlow(provider: 'claude' | 'codex'): Promise<void> {
  try {
    await api('POST', `/api/auth/${provider}/cancel`);
  } catch {
    // ignore
  }
  stopOauthPolling(provider, 'Cancelled.');
}

// --- Mount ---
onMounted(async () => {
  await fetchEnv();
  applyEnvToForm(env.value);
});

onUnmounted(() => {
  for (const provider of ['claude', 'codex'] as const) {
    const id = oauthPollers[provider];
    if (id !== undefined) {
      window.clearInterval(id);
      oauthPollers[provider] = undefined;
    }
  }
});

function capitalize(s: string): string {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : s;
}

const defaultSandboxOptions = computed(() => [
  { value: '', label: 'Auto (model defaults)' },
  ...sandboxes.value.map((sb) => ({ value: sb, label: capitalize(sb) })),
]);
</script>

<template>
  <div v-if="noCredentials" class="set-notice" data-settings-tab="sandbox">
    <strong>No API credentials configured.</strong>
    Sign in below or enter a Claude OAuth token / Anthropic API key (or an OpenAI key for Codex) to start running tasks.
  </div>

  <div class="card compact">
    <div class="card-head">
      <span class="eyebrow">Harness configuration</span>
      <span class="set-row__help sb-head-help">Written to <code>~/.wallfacer/.env</code>; takes effect on the next task run. Leave token fields blank to keep the existing value.</span>
    </div>
  </div>

  <!-- Claude -->
  <div class="card compact">
    <div class="card-head"><HarnessBadge harness="claude" :size="16" /></div>
    <div class="rows">
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">OAuth token <code>CLAUDE_CODE_OAUTH_TOKEN</code></span>
          <span class="set-row__help">From <code>claude setup-token</code>; takes precedence if both are set.</span>
        </div>
        <div class="set-row__end">
          <input id="env-oauth-token" v-model="oauthToken" type="password" class="field mono" :placeholder="oauthTokenPlaceholder" autocomplete="off" />
        </div>
      </div>
      <div class="set-row">
        <div class="set-row__main">
          <span id="claude-oauth-status" class="set-row__help">
            {{ claudeOauthStatus }}
            <a v-if="claudeOauthBusy && claudeOauthStatus.startsWith('Waiting')" href="#" class="set-link" @click.prevent="cancelOauthFlow('claude')">Cancel</a>
          </span>
          <span v-if="!claudeHasCreds" id="claude-no-creds-hint" class="set-status set-status--warn">No token configured, sign in to get started</span>
        </div>
        <div class="set-row__end">
          <button v-if="showClaudeOauthBtn" id="claude-oauth-signin-btn" type="button" class="btn sm" :class="{ ghost: claudeHasCreds }" :disabled="claudeOauthBusy" @click="startOauthFlow('claude')">Sign in with Claude</button>
        </div>
      </div>
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">API key <code>ANTHROPIC_API_KEY</code></span>
          <span class="set-row__help">Direct API key. If both are set, the OAuth token takes precedence.</span>
        </div>
        <div class="set-row__end">
          <input id="env-api-key" v-model="apiKey" type="password" class="field mono" :placeholder="apiKeyPlaceholder" autocomplete="off" />
        </div>
      </div>
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">Base URL <code>ANTHROPIC_BASE_URL</code></span>
          <span class="set-row__help">Custom API endpoint. Clear to use the provider default.</span>
        </div>
        <div class="set-row__end">
          <input id="env-claude-base-url" v-model="claudeBaseUrl" type="url" class="field mono" placeholder="https://api.anthropic.com" autocomplete="off" />
        </div>
      </div>
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">Default model <code>CLAUDE_DEFAULT_MODEL</code></span>
          <span class="set-row__help">Default model for Claude tasks. Clear to use the provider default.</span>
        </div>
        <div class="set-row__end">
          <input id="env-default-model" v-model="defaultModel" type="text" class="field mono" placeholder="e.g. claude-sonnet-4.6" autocomplete="off" list="env-claude-model-list" />
        </div>
      </div>
      <datalist id="env-claude-model-list">
        <option v-for="m in claudeModels" :key="m" :value="m" />
      </datalist>
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">Title model <code>CLAUDE_TITLE_MODEL</code></span>
          <span class="set-row__help">Model for auto-generating task titles. Falls back to the default model.</span>
        </div>
        <div class="set-row__end">
          <input id="env-title-model" v-model="titleModel" type="text" class="field mono" placeholder="e.g. claude-haiku-4.5" autocomplete="off" list="env-claude-model-list" />
        </div>
      </div>
    </div>
    <div class="card-foot">
      <button type="button" class="btn sm ghost" @click="testSandbox('claude')">Test</button>
      <span id="env-claude-test-status" class="set-status">
        {{ claudeTestStatus }}
        <button v-if="claudeTestReauth" type="button" class="btn sm ghost" @click="startOauthFlow('claude')">Sign in again</button>
      </span>
    </div>
  </div>

  <!-- Codex -->
  <div class="card compact">
    <div class="card-head"><HarnessBadge harness="codex" :size="16" /></div>
    <div class="rows">
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">API key <code>OPENAI_API_KEY</code></span>
          <span class="set-row__help">Optional for Codex tasks when host <code>~/.codex/auth.json</code> is mounted.</span>
        </div>
        <div class="set-row__end">
          <input id="env-openai-api-key" v-model="openaiApiKey" type="password" class="field mono" :placeholder="openaiApiKeyPlaceholder" autocomplete="off" />
        </div>
      </div>
      <div class="set-row">
        <div class="set-row__main">
          <span id="codex-oauth-status" class="set-row__help">
            {{ codexOauthStatus }}
            <a v-if="codexOauthBusy && codexOauthStatus.startsWith('Waiting')" href="#" class="set-link" @click.prevent="cancelOauthFlow('codex')">Cancel</a>
          </span>
          <span v-if="!codexHasCreds" id="codex-no-creds-hint" class="set-status set-status--warn">No API key configured, sign in to get started</span>
        </div>
        <div class="set-row__end">
          <button v-if="showCodexOauthBtn" id="codex-oauth-signin-btn" type="button" class="btn sm" :class="{ ghost: codexHasCreds }" :disabled="codexOauthBusy" @click="startOauthFlow('codex')">Sign in with OpenAI</button>
        </div>
      </div>
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">Base URL <code>OPENAI_BASE_URL</code></span>
          <span class="set-row__help">Optional OpenAI-compatible endpoint. Clear to use the provider default.</span>
        </div>
        <div class="set-row__end">
          <input id="env-openai-base-url" v-model="openaiBaseUrl" type="url" class="field mono" placeholder="https://api.openai.com/v1" autocomplete="off" />
        </div>
      </div>
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">Default model <code>CODEX_DEFAULT_MODEL</code></span>
          <span class="set-row__help">Default model for Codex tasks.</span>
        </div>
        <div class="set-row__end">
          <input id="env-codex-default-model" v-model="codexDefaultModel" type="text" class="field mono" placeholder="e.g. gpt-5-codex" autocomplete="off" list="env-codex-model-list" />
        </div>
      </div>
      <datalist id="env-codex-model-list">
        <option v-for="m in codexModels" :key="m" :value="m" />
      </datalist>
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">Title model <code>CODEX_TITLE_MODEL</code></span>
          <span class="set-row__help">Model for auto-generating task titles. Falls back to the Codex default model.</span>
        </div>
        <div class="set-row__end">
          <input id="env-codex-title-model" v-model="codexTitleModel" type="text" class="field mono" placeholder="e.g. gpt-5-codex" autocomplete="off" list="env-codex-model-list" />
        </div>
      </div>
    </div>
    <div class="card-foot">
      <button type="button" class="btn sm ghost" @click="testSandbox('codex')">Test</button>
      <span id="env-codex-test-status" class="set-status">
        {{ codexTestStatus }}
        <button v-if="codexTestReauth" type="button" class="btn sm ghost" @click="startOauthFlow('codex')">Sign in again</button>
      </span>
    </div>
  </div>

  <!-- Cursor -->
  <div class="card compact">
    <div class="card-head"><HarnessBadge harness="cursor" :size="16" /></div>
    <div class="rows">
      <div class="set-row set-row--stack">
        <div class="set-row__main">
          <span class="set-row__label">API key <code>CURSOR_API_KEY</code></span>
          <span class="set-row__help">Headless key for <code>cursor-agent</code>. Create one in Cursor under Settings → API Keys, or run <code>cursor-agent login</code> interactively.</span>
        </div>
        <div class="set-row__end">
          <input id="env-cursor-api-key" v-model="cursorApiKey" type="password" class="field mono" :placeholder="cursorApiKeyPlaceholder" autocomplete="off" />
        </div>
      </div>
      <div v-if="!cursorHasCreds" class="set-row">
        <span id="cursor-no-creds-hint" class="set-status set-status--warn">No API key configured, add one to run Cursor tasks</span>
      </div>
    </div>
    <div class="card-foot">
      <button type="button" class="btn sm ghost" @click="testSandbox('cursor')">Test</button>
      <span id="env-cursor-test-status" class="set-status">{{ cursorTestStatus }}</span>
    </div>
  </div>

  <!-- Pi -->
  <div class="card compact">
    <div class="card-head"><HarnessBadge harness="pi" :size="16" /></div>
    <div class="rows">
      <div class="set-row">
        <span class="set-row__help">
          The <code>pi</code> coding agent from earendil-works (Armin Ronacher's Pi), not Inflection's Pi chatbot. Pi has no dedicated key: it reads the provider credentials configured in the Claude and Codex blocks above. Model selection uses <code>--provider</code> plus <code>--model</code>; pass a <code>provider/model</code> string as the task model.
        </span>
      </div>
    </div>
    <div class="card-foot">
      <button type="button" class="btn sm ghost" @click="testSandbox('pi')">Test</button>
      <span id="env-pi-test-status" class="set-status">{{ piTestStatus }}</span>
    </div>
  </div>

  <!-- OpenCode -->
  <div class="card compact">
    <div class="card-head"><HarnessBadge harness="opencode" :size="16" /></div>
    <div class="rows">
      <div class="set-row">
        <span class="set-row__help">
          The <code>opencode</code> CLI manages provider credentials itself. Run <code>opencode auth login</code> once and pick a provider; the credential lives in OpenCode's own config, so no API key is needed in <code>~/.wallfacer/.env</code>.
        </span>
      </div>
    </div>
    <div class="card-foot">
      <button type="button" class="btn sm ghost" @click="testSandbox('opencode')">Test</button>
      <span id="env-opencode-test-status" class="set-status">{{ opencodeTestStatus }}</span>
    </div>
  </div>

  <!-- Routing -->
  <div class="card compact">
    <div class="card-head"><span class="eyebrow">Harness routing</span></div>
    <div class="rows">
      <div class="set-row">
        <div class="set-row__main">
          <span class="set-row__label">Default harness <code>WALLFACER_DEFAULT_SANDBOX</code></span>
          <span class="set-row__help">Activity-specific routing (Implementation, Testing, …) lives on the agent definition: clone a built-in from the Agents tab and set its Harness field. This is the workspace-wide fallback.</span>
        </div>
        <div class="set-row__end">
          <AppSelect v-model="defaultSandbox" :options="defaultSandboxOptions" aria-label="Default Harness" />
        </div>
      </div>
    </div>
  </div>

  <div class="set-foot">
    <button type="button" class="btn" @click="saveConfig">Save harness configuration</button>
    <button type="button" class="btn ghost" @click="revertConfig">Revert</button>
    <span id="env-config-status" class="set-status">{{ saveStatus }}</span>
  </div>
</template>

<style scoped>
.sb-head-help {
  margin-left: auto;
  text-align: right;
  max-width: 60%;
}
</style>
