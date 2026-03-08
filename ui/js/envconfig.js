// --- Parallel Tasks Setting ---

async function loadMaxParallel() {
  try {
    const cfg = await api('/api/env');
    const input = document.getElementById('max-parallel-input');
    if (cfg.max_parallel_tasks) {
      maxParallelTasks = cfg.max_parallel_tasks;
    }
    if (input) {
      input.value = maxParallelTasks;
    }
    updateInProgressCount();
  } catch (e) {
    console.error('Failed to load max parallel setting:', e);
  }
}

async function saveMaxParallel() {
  const input = document.getElementById('max-parallel-input');
  const statusEl = document.getElementById('max-parallel-status');
  let value = parseInt(input.value, 10);
  if (isNaN(value) || value < 1) value = 1;
  if (value > 20) value = 20;
  input.value = value;

  statusEl.textContent = 'Saving…';
  try {
    await api('/api/env', { method: 'PUT', body: JSON.stringify({ max_parallel_tasks: value }) });
    maxParallelTasks = value;
    updateInProgressCount();
    statusEl.textContent = 'Saved.';
    setTimeout(() => { statusEl.textContent = ''; }, 2000);
  } catch (e) {
    statusEl.textContent = 'Error: ' + e.message;
  }
}

// --- Oversight Interval Setting ---

async function loadOversightInterval() {
  try {
    const cfg = await api('/api/env');
    const input = document.getElementById('oversight-interval-input');
    if (input) input.value = cfg.oversight_interval ?? 0;
  } catch (e) {
    console.error('Failed to load oversight interval setting:', e);
  }
}

async function saveOversightInterval() {
  const input = document.getElementById('oversight-interval-input');
  const statusEl = document.getElementById('oversight-interval-status');
  let value = parseInt(input.value, 10);
  if (isNaN(value) || value < 0) value = 0;
  if (value > 120) value = 120;
  input.value = value;
  statusEl.textContent = 'Saving…';
  try {
    await api('/api/env', { method: 'PUT', body: JSON.stringify({ oversight_interval: value }) });
    statusEl.textContent = 'Saved.';
    setTimeout(() => { statusEl.textContent = ''; }, 2000);
  } catch (e) {
    statusEl.textContent = 'Error: ' + e.message;
  }
}

// --- Auto Push Setting ---

async function loadAutoPush() {
  try {
    const cfg = await api('/api/env');
    const checkbox = document.getElementById('auto-push-enabled');
    const thresholdInput = document.getElementById('auto-push-threshold');
    const thresholdRow = document.getElementById('auto-push-threshold-row');
    if (checkbox) {
      checkbox.checked = !!cfg.auto_push_enabled;
      if (thresholdRow) thresholdRow.style.display = cfg.auto_push_enabled ? 'flex' : 'none';
    }
    if (thresholdInput && cfg.auto_push_threshold) {
      thresholdInput.value = cfg.auto_push_threshold;
    }
  } catch (e) {
    console.error('Failed to load auto-push setting:', e);
  }
}

async function saveAutoPush() {
  const checkbox = document.getElementById('auto-push-enabled');
  const thresholdInput = document.getElementById('auto-push-threshold');
  const thresholdRow = document.getElementById('auto-push-threshold-row');
  const statusEl = document.getElementById('auto-push-status');

  const enabled = checkbox ? checkbox.checked : false;
  if (thresholdRow) thresholdRow.style.display = enabled ? 'flex' : 'none';

  let threshold = parseInt(thresholdInput ? thresholdInput.value : '1', 10);
  if (isNaN(threshold) || threshold < 1) threshold = 1;
  if (thresholdInput) thresholdInput.value = threshold;

  statusEl.textContent = 'Saving…';
  try {
    await api('/api/env', {
      method: 'PUT',
      body: JSON.stringify({ auto_push_enabled: enabled, auto_push_threshold: threshold }),
    });
    statusEl.textContent = 'Saved.';
    setTimeout(() => { statusEl.textContent = ''; }, 2000);
  } catch (e) {
    statusEl.textContent = 'Error: ' + e.message;
  }
}

// --- API Configuration (env file editor) ---

async function showEnvConfigEditor(event) {
  if (event) event.stopPropagation();
  closeSettings();

  let cfg = { oauth_token: '', api_key: '', base_url: '', default_model: '', title_model: '' };
  try {
    cfg = await api('/api/env');
  } catch (e) {
    console.error('Failed to load env config:', e);
  }

  // Populate fields — tokens show masked value as placeholder only.
  document.getElementById('env-oauth-token').value = '';
  document.getElementById('env-oauth-token').placeholder = cfg.oauth_token || '(not set)';
  document.getElementById('env-api-key').value = '';
  document.getElementById('env-api-key').placeholder = cfg.api_key || '(not set)';
  document.getElementById('env-base-url').value = cfg.base_url || '';
  document.getElementById('env-default-model').value = cfg.default_model || '';
  document.getElementById('env-title-model').value = cfg.title_model || '';
  document.getElementById('env-config-status').textContent = '';

  const modal = document.getElementById('env-config-modal');
  modal.classList.remove('hidden');
  modal.classList.add('flex');
}

function closeEnvConfigEditor() {
  const modal = document.getElementById('env-config-modal');
  modal.classList.add('hidden');
  modal.classList.remove('flex');
}

async function saveEnvConfig() {
  const oauthRaw = document.getElementById('env-oauth-token').value.trim();
  const apiKeyRaw = document.getElementById('env-api-key').value.trim();
  const baseURL = document.getElementById('env-base-url').value.trim();
  const defaultModel = document.getElementById('env-default-model').value.trim();
  const titleModel = document.getElementById('env-title-model').value.trim();

  // Build the payload — only include token fields when the user typed something
  // so the server doesn't treat empty as "no change" vs "clear".
  const body = {};
  if (oauthRaw) body.oauth_token = oauthRaw;
  if (apiKeyRaw) body.api_key = apiKeyRaw;
  body.base_url = baseURL;            // empty = clear
  body.default_model = defaultModel;  // empty = clear
  body.title_model = titleModel;      // empty = clear

  const statusEl = document.getElementById('env-config-status');
  statusEl.textContent = 'Saving…';
  try {
    await api('/api/env', { method: 'PUT', body: JSON.stringify(body) });
    statusEl.textContent = 'Saved.';
    // Clear token inputs after saving so they don't linger in the DOM.
    document.getElementById('env-oauth-token').value = '';
    document.getElementById('env-api-key').value = '';
    // Refresh placeholders.
    setTimeout(() => showEnvConfigEditor(null), 600);
  } catch (e) {
    statusEl.textContent = 'Error: ' + e.message;
  }
}
