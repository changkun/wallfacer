<script setup lang="ts">
import { ref, computed, onMounted, onUnmounted, watch } from 'vue';
import { api } from '../../api/client';
import { useTaskStore } from '../../stores/tasks';
import { useEnvConfig } from '../../composables/useEnvConfig';
import type {
  EnvConfig,
  EnvUpdatePayload,
  SandboxImagesResponse,
  SandboxImageInfo,
  SandboxTestResponse,
} from '../../api/types';

const taskStore = useTaskStore();
const { env, fetchEnv, updateEnv } = useEnvConfig();

// --- Host-mode banner ---
const hostMode = computed(() => taskStore.config?.host_mode === true);

// --- Sandbox list ---
const sandboxes = computed<string[]>(() => taskStore.config?.sandboxes ?? []);

// --- Image status ---
const images = ref<SandboxImageInfo[]>([]);
const imagesLoading = ref(false);
const imagesError = ref('');
const pulling = ref<Record<string, boolean>>({});

async function loadImages(): Promise<void> {
  imagesLoading.value = true;
  imagesError.value = '';
  try {
    const data = await api<SandboxImagesResponse>('GET', '/api/images');
    images.value = Array.isArray(data.images) ? data.images : [];
  } catch (e) {
    imagesError.value = e instanceof Error ? e.message : String(e);
  } finally {
    imagesLoading.value = false;
  }
}

async function pullImage(sandbox: string): Promise<void> {
  pulling.value = { ...pulling.value, [sandbox]: true };
  try {
    await api('POST', '/api/images/pull', { sandbox });
    // Poll the status endpoint a few times until we see cached: true or
    // give up after ~120s. The richer SSE stream is intentionally not
    // wired up in this port.
    let ticks = 0;
    const max = 60;
    const tick = async () => {
      ticks++;
      await loadImages();
      const info = images.value.find((img) => img.sandbox === sandbox);
      if (info?.cached || ticks >= max) {
        pulling.value = { ...pulling.value, [sandbox]: false };
        return;
      }
      window.setTimeout(tick, 2000);
    };
    window.setTimeout(tick, 2000);
  } catch (e) {
    pulling.value = { ...pulling.value, [sandbox]: false };
    imagesError.value = e instanceof Error ? e.message : String(e);
  }
}

async function deleteImage(sandbox: string): Promise<void> {
  if (!window.confirm(`Remove the ${sandbox} sandbox image? You can re-pull it later.`)) {
    return;
  }
  try {
    await api('DELETE', '/api/images', { sandbox });
    await loadImages();
  } catch (e) {
    imagesError.value = e instanceof Error ? e.message : String(e);
  }
}

// --- Form state (local refs bound to inputs) ---
const oauthToken = ref('');
const apiKey = ref('');
const claudeBaseUrl = ref('');
const openaiApiKey = ref('');
const openaiBaseUrl = ref('');
const defaultModel = ref('');
const titleModel = ref('');
const codexDefaultModel = ref('');
const codexTitleModel = ref('');
const defaultSandbox = ref('');
const sandboxFast = ref(true);
const containerCpus = ref('');
const containerMemory = ref('');

const claudeTestStatus = ref('');
const claudeTestReauth = ref(false);
const codexTestStatus = ref('');
const codexTestReauth = ref(false);
const saveStatus = ref('');

const claudeOauthStatus = ref('');
const codexOauthStatus = ref('');
const claudeOauthBusy = ref(false);
const codexOauthBusy = ref(false);

// Placeholders for token fields show the masked value from /api/env.
const oauthTokenPlaceholder = computed(() => env.value?.oauth_token || '(not set)');
const apiKeyPlaceholder = computed(() => env.value?.api_key || '(not set)');
const openaiApiKeyPlaceholder = computed(() => env.value?.openai_api_key || '(not set)');

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

// OAuth sign-in buttons hide when a custom base URL is set.
const showClaudeOauthBtn = computed(() => !claudeBaseUrl.value);
const showCodexOauthBtn = computed(() => !openaiBaseUrl.value);

function applyEnvToForm(cfg: EnvConfig | null): void {
  oauthToken.value = '';
  apiKey.value = '';
  openaiApiKey.value = '';
  claudeBaseUrl.value = cfg?.base_url || '';
  openaiBaseUrl.value = cfg?.openai_base_url || '';
  defaultModel.value = cfg?.default_model || '';
  titleModel.value = cfg?.title_model || '';
  codexDefaultModel.value = cfg?.codex_default_model || '';
  codexTitleModel.value = cfg?.codex_title_model || '';
  defaultSandbox.value = cfg?.default_sandbox || '';
  sandboxFast.value = cfg?.sandbox_fast !== false;
  containerCpus.value = cfg?.container_cpus || '';
  containerMemory.value = cfg?.container_memory || '';
  claudeTestStatus.value = '';
  claudeTestReauth.value = false;
  codexTestStatus.value = '';
  codexTestReauth.value = false;
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
  body.default_model = defaultModel.value.trim();
  body.title_model = titleModel.value.trim();
  body.codex_default_model = codexDefaultModel.value.trim();
  body.codex_title_model = codexTitleModel.value.trim();
  body.default_sandbox = defaultSandbox.value.trim();
  // Activity-specific routing is retired — send empty map so the server
  // clears any legacy WALLFACER_SANDBOX_* entries.
  body.sandbox_by_activity = {};
  body.sandbox_fast = sandboxFast.value;
  body.container_cpus = containerCpus.value.trim();
  body.container_memory = containerMemory.value.trim();
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
  sandbox_fast: boolean;
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
  sandbox_fast: boolean;
  openai_base_url?: string;
  codex_default_model?: string;
  codex_title_model?: string;
  openai_api_key?: string;
}

function buildTestPayload(sandbox: 'claude' | 'codex'): ClaudeTestPayload | CodexTestPayload {
  const raw = buildSavePayload();
  if (sandbox === 'claude') {
    const p: ClaudeTestPayload = {
      sandbox: 'claude',
      default_sandbox: raw.default_sandbox || '',
      sandbox_by_activity: raw.sandbox_by_activity || {},
      sandbox_fast: raw.sandbox_fast !== false,
      base_url: raw.base_url,
      default_model: raw.default_model,
      title_model: raw.title_model,
    };
    if (raw.oauth_token) p.oauth_token = raw.oauth_token;
    if (raw.api_key) p.api_key = raw.api_key;
    return p;
  }
  const p: CodexTestPayload = {
    sandbox: 'codex',
    default_sandbox: raw.default_sandbox || '',
    sandbox_by_activity: raw.sandbox_by_activity || {},
    sandbox_fast: raw.sandbox_fast !== false,
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

async function testSandbox(sandbox: 'claude' | 'codex'): Promise<void> {
  if (sandbox === 'claude') {
    claudeTestStatus.value = 'Testing…';
    claudeTestReauth.value = false;
  } else {
    codexTestStatus.value = 'Testing…';
    codexTestReauth.value = false;
  }
  try {
    const resp = await api<SandboxTestResponse>('POST', '/api/env/test', buildTestPayload(sandbox));
    const text = summarizeTestResult(resp);
    if (sandbox === 'claude') {
      claudeTestStatus.value = text;
      claudeTestReauth.value = !!resp.reauth_available;
    } else {
      codexTestStatus.value = text;
      codexTestReauth.value = !!resp.reauth_available;
    }
    window.setTimeout(() => {
      const isFailish =
        text.includes('FAIL') ||
        text.startsWith('status failed') ||
        text.startsWith('No response') ||
        resp.reauth_available;
      if (isFailish) return;
      if (sandbox === 'claude') claudeTestStatus.value = '';
      else codexTestStatus.value = '';
    }, 6000);
  } catch (e) {
    const msg = 'Error: ' + (e instanceof Error ? e.message : String(e));
    if (sandbox === 'claude') claudeTestStatus.value = msg;
    else codexTestStatus.value = msg;
    window.setTimeout(() => {
      if (sandbox === 'claude') claudeTestStatus.value = '';
      else codexTestStatus.value = '';
    }, 6000);
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
  await Promise.all([fetchEnv(), loadImages()]);
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
</script>

<template>
  <div class="settings-tab-content active" data-settings-tab="sandbox">
    <!-- Host-mode banner -->
    <div
      v-if="hostMode"
      class="settings-card"
      style="margin-bottom: 12px; border-left: 3px solid var(--warn, #a56a12); background: var(--tint-amber, #f1e7cf); color: var(--tint-amber-ink, #7a5418);"
    >
      <div class="settings-card-head">
        <h4 style="margin: 0">Host mode active</h4>
        <p style="margin: 4px 0 0 0">
          Tasks run directly on your machine with your user's permissions.
          Wallfacer cannot prevent an agent from writing outside the worktree.
          Recommended only on trusted machines. Start with
          <code>wallfacer run --backend container</code> to restore container
          isolation.
        </p>
      </div>
    </div>

    <!-- Container Images -->
    <div class="settings-card" style="margin-bottom: 12px">
      <div class="settings-card-head">
        <h4>Container Images</h4>
        <p>
          Sandbox images are pulled automatically on first use. You can also pull
          or re-pull them here.
        </p>
      </div>
      <div style="display: flex; flex-direction: column; gap: 8px">
        <div v-if="imagesLoading" style="font-size: 12px; color: var(--text-muted);">Loading...</div>
        <div v-else-if="imagesError" style="font-size: 12px; color: var(--text-error, red);">
          Failed to load image status.
        </div>
        <template v-else>
          <div
            v-for="img in images"
            :key="img.sandbox"
            style="display: flex; align-items: center; gap: 8px; padding: 8px 10px; border: 1px solid var(--border); border-radius: 6px; font-size: 12px;"
          >
            <div style="flex: 1; min-width: 0;">
              <div style="font-weight: 600; text-transform: capitalize;">{{ img.sandbox }}</div>
              <div
                style="font-size: 11px; color: var(--text-muted); overflow: hidden; text-overflow: ellipsis; white-space: nowrap;"
                :title="img.image"
              >
                {{ img.image }}
              </div>
              <div v-if="img.size" style="font-size: 11px; color: var(--text-muted);">
                Size: {{ img.size }}
              </div>
            </div>
            <span
              v-if="pulling[img.sandbox]"
              style="display: inline-block; padding: 1px 6px; border-radius: 4px; background: #fef9c3; color: #854d0e; font-size: 11px; font-weight: 600;"
            >Pulling…</span>
            <span
              v-else-if="img.cached"
              style="display: inline-block; padding: 1px 6px; border-radius: 4px; background: #dcfce7; color: #166534; font-size: 11px; font-weight: 600;"
            >Cached</span>
            <span
              v-else
              style="display: inline-block; padding: 1px 6px; border-radius: 4px; background: #fef2f2; color: #991b1b; font-size: 11px; font-weight: 600;"
            >Missing</span>
            <button
              type="button"
              class="btn-icon"
              style="font-size: 12px; padding: 4px 10px; white-space: nowrap;"
              :disabled="pulling[img.sandbox]"
              @click="pullImage(img.sandbox)"
            >
              {{ pulling[img.sandbox] ? 'Pulling…' : (img.cached ? 'Re-pull' : 'Pull') }}
            </button>
            <button
              v-if="img.cached && !pulling[img.sandbox]"
              type="button"
              class="btn-icon"
              style="font-size: 12px; padding: 4px 10px; white-space: nowrap; color: var(--text-error, #dc2626);"
              @click="deleteImage(img.sandbox)"
            >
              Delete
            </button>
          </div>
        </template>
      </div>
    </div>

    <!-- Sandbox Configuration -->
    <div class="settings-card">
      <div class="settings-card-head">
        <h4>Sandbox Configuration</h4>
        <p>
          Changes are written to
          <code style="font-family: monospace">~/.wallfacer/.env</code>
          and take effect on the next task run. Leave token fields blank to keep
          the existing value.
        </p>
      </div>

      <div style="display: flex; flex-direction: column; gap: 12px">
        <!-- Claude block -->
        <div style="border: 1px solid var(--border); border-radius: 8px; padding: 12px;">
          <label style="display: block; font-size: 12px; font-weight: 700; color: var(--text-secondary); margin-bottom: 10px;">Claude</label>

          <div style="display: flex; flex-direction: column; gap: 12px">
            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">OAuth Token (CLAUDE_CODE_OAUTH_TOKEN)</label>
              <input
                id="env-oauth-token"
                v-model="oauthToken"
                type="password"
                class="field"
                style="font-family: monospace; font-size: 12px"
                :placeholder="oauthTokenPlaceholder"
                autocomplete="off"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                From
                <code style="font-family: monospace">claude setup-token</code>
                (takes precedence if both are set). Leave blank to keep the
                current value.
              </div>
              <div style="margin-top: 6px; display: flex; align-items: center; gap: 8px;">
                <button
                  v-if="showClaudeOauthBtn"
                  type="button"
                  id="claude-oauth-signin-btn"
                  class="btn btn-sm btn-accent"
                  :class="{ 'btn-primary': !claudeHasCreds }"
                  style="font-size: 12px"
                  :disabled="claudeOauthBusy"
                  @click="startOauthFlow('claude')"
                >
                  Sign in with Claude
                </button>
                <span id="claude-oauth-status" style="font-size: 11px; color: var(--text-muted)">
                  {{ claudeOauthStatus }}
                  <a
                    v-if="claudeOauthBusy && claudeOauthStatus.startsWith('Waiting')"
                    href="#"
                    style="color: var(--accent); font-size: 11px; margin-left: 4px;"
                    @click.prevent="cancelOauthFlow('claude')"
                  >Cancel</a>
                </span>
              </div>
              <div
                v-if="!claudeHasCreds"
                id="claude-no-creds-hint"
                style="font-size: 11px; color: var(--accent); margin-top: 4px;"
              >
                No token configured, sign in to get started
              </div>
            </div>

            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">API Key (ANTHROPIC_API_KEY)</label>
              <input
                id="env-api-key"
                v-model="apiKey"
                type="password"
                class="field"
                style="font-family: monospace; font-size: 12px"
                :placeholder="apiKeyPlaceholder"
                autocomplete="off"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Direct API key. If both are set, the OAuth token takes precedence.
                Leave blank to keep the current value.
              </div>
            </div>

            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">Base URL (ANTHROPIC_BASE_URL)</label>
              <input
                id="env-claude-base-url"
                v-model="claudeBaseUrl"
                type="url"
                class="field"
                style="font-family: monospace; font-size: 12px"
                placeholder="https://api.anthropic.com"
                autocomplete="off"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Custom API endpoint. Clear to use the provider default.
              </div>
            </div>

            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">Default Model (CLAUDE_DEFAULT_MODEL)</label>
              <input
                id="env-default-model"
                v-model="defaultModel"
                type="text"
                class="field"
                style="font-family: monospace; font-size: 12px"
                placeholder="e.g. claude-sonnet-4.6"
                autocomplete="off"
                list="env-model-list"
              />
              <datalist id="env-model-list"></datalist>
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Default model for Claude tasks. Clear to use the container
                default.
              </div>
            </div>

            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">Title Model (CLAUDE_TITLE_MODEL)</label>
              <input
                id="env-title-model"
                v-model="titleModel"
                type="text"
                class="field"
                style="font-family: monospace; font-size: 12px"
                placeholder="e.g. claude-haiku-4.5"
                autocomplete="off"
                list="env-model-list"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Model for auto-generating task titles. Falls back to the default
                model.
              </div>
            </div>

            <div style="display: flex; align-items: center; gap: 8px; margin-top: 4px;">
              <button
                type="button"
                class="btn-icon"
                style="font-size: 12px; padding: 4px 10px"
                @click="testSandbox('claude')"
              >
                Test
              </button>
              <span id="env-claude-test-status" style="font-size: 11px; color: var(--text-muted); min-height: 1em">
                {{ claudeTestStatus }}
                <button
                  v-if="claudeTestReauth"
                  type="button"
                  class="btn btn-sm"
                  style="font-size: 11px; margin-left: 8px;"
                  @click="startOauthFlow('claude')"
                >Sign in again</button>
              </span>
            </div>
          </div>
        </div>

        <!-- Codex block -->
        <div style="border: 1px solid var(--border); border-radius: 8px; padding: 12px;">
          <label style="display: block; font-size: 12px; font-weight: 700; color: var(--text-secondary); margin-bottom: 10px;">Codex</label>
          <div style="display: flex; flex-direction: column; gap: 12px">
            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">API Key (OPENAI_API_KEY)</label>
              <input
                id="env-openai-api-key"
                v-model="openaiApiKey"
                type="password"
                class="field"
                style="font-family: monospace; font-size: 12px"
                :placeholder="openaiApiKeyPlaceholder"
                autocomplete="off"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Optional for Codex tasks when host
                <code style="font-family: monospace">~/.codex/auth.json</code>
                is mounted. Leave blank to keep the current value.
              </div>
              <div style="margin-top: 6px; display: flex; align-items: center; gap: 8px;">
                <button
                  v-if="showCodexOauthBtn"
                  type="button"
                  id="codex-oauth-signin-btn"
                  class="btn btn-sm"
                  :class="{ 'btn-primary': !codexHasCreds }"
                  style="font-size: 12px; background: #000; color: #fff; border-color: #000;"
                  :disabled="codexOauthBusy"
                  @click="startOauthFlow('codex')"
                >
                  Sign in with OpenAI
                </button>
                <span id="codex-oauth-status" style="font-size: 11px; color: var(--text-muted)">
                  {{ codexOauthStatus }}
                  <a
                    v-if="codexOauthBusy && codexOauthStatus.startsWith('Waiting')"
                    href="#"
                    style="color: var(--accent); font-size: 11px; margin-left: 4px;"
                    @click.prevent="cancelOauthFlow('codex')"
                  >Cancel</a>
                </span>
              </div>
              <div
                v-if="!codexHasCreds"
                id="codex-no-creds-hint"
                style="font-size: 11px; color: var(--accent); margin-top: 4px;"
              >
                No API key configured, sign in to get started
              </div>
            </div>

            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">Base URL (OPENAI_BASE_URL)</label>
              <input
                id="env-openai-base-url"
                v-model="openaiBaseUrl"
                type="url"
                class="field"
                style="font-family: monospace; font-size: 12px"
                placeholder="https://api.openai.com/v1"
                autocomplete="off"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Optional OpenAI-compatible endpoint. Clear to use the provider
                default.
              </div>
            </div>

            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">Default Model (CODEX_DEFAULT_MODEL)</label>
              <input
                id="env-codex-default-model"
                v-model="codexDefaultModel"
                type="text"
                class="field"
                style="font-family: monospace; font-size: 12px"
                placeholder="e.g. gpt-5-codex"
                autocomplete="off"
                list="env-model-list"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Default model for Codex tasks.
              </div>
            </div>

            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">Title Model (CODEX_TITLE_MODEL)</label>
              <input
                id="env-codex-title-model"
                v-model="codexTitleModel"
                type="text"
                class="field"
                style="font-family: monospace; font-size: 12px"
                placeholder="e.g. gpt-5-codex"
                autocomplete="off"
                list="env-model-list"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Model for auto-generating task titles. Falls back to Codex default
                model.
              </div>
            </div>

            <div style="display: flex; align-items: center; gap: 8px; margin-top: 4px;">
              <button
                type="button"
                class="btn-icon"
                style="font-size: 12px; padding: 4px 10px"
                @click="testSandbox('codex')"
              >
                Test
              </button>
              <span id="env-codex-test-status" style="font-size: 11px; color: var(--text-muted); min-height: 1em">
                {{ codexTestStatus }}
                <button
                  v-if="codexTestReauth"
                  type="button"
                  class="btn btn-sm"
                  style="font-size: 11px; margin-left: 8px;"
                  @click="startOauthFlow('codex')"
                >Sign in again</button>
              </span>
            </div>
          </div>
        </div>

        <!-- Global Sandbox Routing -->
        <div style="border: 1px solid var(--border); border-radius: 8px; padding: 12px;">
          <label style="display: block; font-size: 12px; font-weight: 700; color: var(--text-secondary); margin-bottom: 10px;">Global Sandbox Routing</label>
          <div style="display: flex; flex-direction: column; gap: 10px">
            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">Default Sandbox (WALLFACER_DEFAULT_SANDBOX)</label>
              <select
                id="env-default-sandbox"
                v-model="defaultSandbox"
                class="select"
                style="font-size: 12px"
                data-sandbox-select="true"
                data-default-text="Auto (model defaults)"
              >
                <option value="">Auto (model defaults)</option>
                <option v-for="sb in sandboxes" :key="sb" :value="sb">{{ capitalize(sb) }}</option>
              </select>
            </div>
            <p style="font-size: 11px; color: var(--text-muted); line-height: 1.5; margin: 0 0 4px;">
              Activity-specific sandbox routing (Implementation, Testing, etc.) now
              lives on the <strong>agent</strong> definition: clone a built-in
              from the Agents tab and set its <strong>Harness</strong> field to
              pin that step to Claude or Codex. Workspace-wide fallbacks still
              come from <code>WALLFACER_DEFAULT_SANDBOX</code> above.
            </p>
            <label style="display: flex; align-items: center; gap: 8px; font-size: 12px; color: var(--text-secondary);">
              <input id="env-sandbox-fast" v-model="sandboxFast" type="checkbox" />
              <span>Enable <code style="font-family: monospace">/fast</code> for sandbox runs</span>
            </label>
          </div>
        </div>

        <!-- Container Resource Limits -->
        <div style="border: 1px solid var(--border); border-radius: 8px; padding: 12px;">
          <label style="display: block; font-size: 12px; font-weight: 700; color: var(--text-secondary); margin-bottom: 10px;">Container Resource Limits</label>
          <div style="display: flex; flex-direction: column; gap: 12px">
            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">Container CPUs (WALLFACER_CONTAINER_CPUS)</label>
              <input
                id="env-container-cpus"
                v-model="containerCpus"
                type="text"
                class="field"
                style="font-family: monospace; font-size: 12px"
                placeholder="e.g. 2.0 (leave empty for no limit)"
                autocomplete="off"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Max CPU cores for each container (<code style="font-family: monospace">--cpus</code>).
                Clear to remove the limit.
              </div>
            </div>
            <div>
              <label style="display: block; font-size: 12px; font-weight: 600; color: var(--text-secondary); margin-bottom: 4px;">Container Memory (WALLFACER_CONTAINER_MEMORY)</label>
              <input
                id="env-container-memory"
                v-model="containerMemory"
                type="text"
                class="field"
                style="font-family: monospace; font-size: 12px"
                placeholder="e.g. 4g (leave empty for no limit)"
                autocomplete="off"
              />
              <div style="font-size: 11px; color: var(--text-muted); margin-top: 3px">
                Max memory for each container (<code style="font-family: monospace">--memory</code>).
                Clear to remove the limit.
              </div>
            </div>
          </div>
        </div>
      </div>

      <div style="display: flex; align-items: center; gap: 8px; margin-top: 20px">
        <button type="button" class="btn btn-accent" @click="saveConfig">
          Save Sandbox Configuration
        </button>
        <button type="button" class="btn-ghost" @click="revertConfig">
          Revert
        </button>
        <span id="env-config-status" style="font-size: 12px; color: var(--text-muted); margin-left: auto">
          {{ saveStatus }}
        </span>
      </div>
    </div>
  </div>
</template>
