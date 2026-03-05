// --- Parallel Tasks Setting ---

async function loadMaxParallel() {
  try {
    const cfg = await api('/api/env');
    const input = document.getElementById('max-parallel-input');
    if (input && cfg.max_parallel_tasks) {
      input.value = cfg.max_parallel_tasks;
    }
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
    statusEl.textContent = 'Saved.';
    setTimeout(() => { statusEl.textContent = ''; }, 2000);
  } catch (e) {
    statusEl.textContent = 'Error: ' + e.message;
  }
}

// --- API Configuration (env file editor) ---

async function showEnvConfigEditor(event) {
  if (event) event.stopPropagation();
  document.getElementById('settings-panel').classList.add('hidden');

  let cfg = { oauth_token: '', api_key: '', base_url: '', model: '' };
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
  document.getElementById('env-model').value = cfg.model || '';
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
  const model = document.getElementById('env-model').value.trim();

  // Build the payload — only include token fields when the user typed something
  // so the server doesn't treat empty as "no change" vs "clear".
  const body = {};
  if (oauthRaw) body.oauth_token = oauthRaw;
  if (apiKeyRaw) body.api_key = apiKeyRaw;
  body.base_url = baseURL;  // empty = clear
  body.model = model;       // empty = clear

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
