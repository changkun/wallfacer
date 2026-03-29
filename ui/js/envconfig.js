// --- Parallel Tasks Setting ---

async function loadMaxParallel() {
  try {
    const cfg = await api(Routes.env.get());
    const input = document.getElementById("max-parallel-input");
    if (cfg.max_parallel_tasks) {
      maxParallelTasks = cfg.max_parallel_tasks;
    }
    if (input) {
      input.value = maxParallelTasks;
    }
    updateInProgressCount();
  } catch (e) {
    console.error("Failed to load max parallel setting:", e);
  }
}

async function saveMaxParallel() {
  const input = document.getElementById("max-parallel-input");
  const statusEl = document.getElementById("max-parallel-status");
  let value = parseInt(input.value, 10);
  if (isNaN(value) || value < 1) value = 1;
  if (value > 20) value = 20;
  input.value = value;

  statusEl.textContent = "Saving…";
  try {
    await api(Routes.env.update(), {
      method: "PUT",
      body: JSON.stringify({ max_parallel_tasks: value }),
    });
    maxParallelTasks = value;
    updateInProgressCount();
    statusEl.textContent = "Saved.";
    setTimeout(() => {
      statusEl.textContent = "";
    }, 2000);
  } catch (e) {
    statusEl.textContent = "Error: " + e.message;
  }
}

// --- Oversight Interval Setting ---

async function loadOversightInterval() {
  try {
    const cfg = await api(Routes.env.get());
    const input = document.getElementById("oversight-interval-input");
    if (input) input.value = cfg.oversight_interval ?? 0;
  } catch (e) {
    console.error("Failed to load oversight interval setting:", e);
  }
}

async function saveOversightInterval() {
  const input = document.getElementById("oversight-interval-input");
  const statusEl = document.getElementById("oversight-interval-status");
  let value = parseInt(input.value, 10);
  if (isNaN(value) || value < 0) value = 0;
  if (value > 120) value = 120;
  input.value = value;
  statusEl.textContent = "Saving…";
  try {
    await api(Routes.env.update(), {
      method: "PUT",
      body: JSON.stringify({ oversight_interval: value }),
    });
    statusEl.textContent = "Saved.";
    setTimeout(() => {
      statusEl.textContent = "";
    }, 2000);
  } catch (e) {
    statusEl.textContent = "Error: " + e.message;
  }
}

// --- Archived Tasks Pagination Setting ---

async function loadArchivedTasksPerPage() {
  try {
    const cfg = await api(Routes.env.get());
    const input = document.getElementById("archived-page-size-input");
    const value = parseInt(cfg.archived_tasks_per_page, 10);
    archivedTasksPageSize = Number.isFinite(value) && value > 0 ? value : 20;
    if (input) input.value = archivedTasksPageSize;
  } catch (e) {
    console.error("Failed to load archived tasks page size setting:", e);
  }
}

async function saveArchivedTasksPerPage() {
  const input = document.getElementById("archived-page-size-input");
  const statusEl = document.getElementById("archived-page-size-status");
  let value = parseInt(input.value, 10);
  if (isNaN(value) || value < 1) value = 1;
  if (value > 200) value = 200;
  input.value = value;

  statusEl.textContent = "Saving…";
  try {
    await api(Routes.env.update(), {
      method: "PUT",
      body: JSON.stringify({ archived_tasks_per_page: value }),
    });
    archivedTasksPageSize = value;
    statusEl.textContent = "Saved.";
    if (showArchived && typeof loadArchivedTasksPage === "function") {
      await loadArchivedTasksPage("initial");
    }
    setTimeout(() => {
      statusEl.textContent = "";
    }, 2000);
  } catch (e) {
    statusEl.textContent = "Error: " + e.message;
  }
}

// --- Auto Push Setting ---

async function loadAutoPush() {
  try {
    const cfg = await api(Routes.env.get());
    const checkbox = document.getElementById("auto-push-enabled");
    const thresholdInput = document.getElementById("auto-push-threshold");
    const thresholdRow = document.getElementById("auto-push-threshold-row");
    if (checkbox) {
      checkbox.checked = !!cfg.auto_push_enabled;
      if (thresholdRow)
        thresholdRow.style.display = cfg.auto_push_enabled ? "flex" : "none";
    }
    if (thresholdInput && cfg.auto_push_threshold) {
      thresholdInput.value = cfg.auto_push_threshold;
    }
  } catch (e) {
    console.error("Failed to load auto-push setting:", e);
  }
}

async function saveAutoPush() {
  const checkbox = document.getElementById("auto-push-enabled");
  const thresholdInput = document.getElementById("auto-push-threshold");
  const thresholdRow = document.getElementById("auto-push-threshold-row");
  const statusEl = document.getElementById("auto-push-status");

  const enabled = checkbox ? checkbox.checked : false;
  if (thresholdRow) thresholdRow.style.display = enabled ? "flex" : "none";

  let threshold = parseInt(thresholdInput ? thresholdInput.value : "1", 10);
  if (isNaN(threshold) || threshold < 1) threshold = 1;
  if (thresholdInput) thresholdInput.value = threshold;

  statusEl.textContent = "Saving…";
  try {
    await api(Routes.env.update(), {
      method: "PUT",
      body: JSON.stringify({
        auto_push_enabled: enabled,
        auto_push_threshold: threshold,
      }),
    });
    statusEl.textContent = "Saved.";
    setTimeout(() => {
      statusEl.textContent = "";
    }, 2000);
  } catch (e) {
    statusEl.textContent = "Error: " + e.message;
  }
}

// --- API Configuration (env file editor) ---

function buildSaveEnvPayload() {
  const oauthRaw = document.getElementById("env-oauth-token").value.trim();
  const apiKeyRaw = document.getElementById("env-api-key").value.trim();
  const claudeBaseURL = document
    .getElementById("env-claude-base-url")
    .value.trim();
  const openAIAPIKeyRaw = document
    .getElementById("env-openai-api-key")
    .value.trim();
  const openAIBaseURL = document
    .getElementById("env-openai-base-url")
    .value.trim();
  const defaultModel = document
    .getElementById("env-default-model")
    .value.trim();
  const titleModel = document.getElementById("env-title-model").value.trim();
  const codexDefaultModel = document
    .getElementById("env-codex-default-model")
    .value.trim();
  const codexTitleModel = document
    .getElementById("env-codex-title-model")
    .value.trim();
  const defaultSandbox = document
    .getElementById("env-default-sandbox")
    .value.trim();
  const sandboxByActivity = collectSandboxByActivity("env-sandbox-");
  const sandboxFastEl = document.getElementById("env-sandbox-fast");
  const containerCPUs = document.getElementById("env-container-cpus")
    ? document.getElementById("env-container-cpus").value.trim()
    : "";
  const containerMemory = document.getElementById("env-container-memory")
    ? document.getElementById("env-container-memory").value.trim()
    : "";
  const body = {};
  if (oauthRaw) body.oauth_token = oauthRaw;
  if (apiKeyRaw) body.api_key = apiKeyRaw;
  body.base_url = claudeBaseURL; // empty = clear
  if (openAIAPIKeyRaw) body.openai_api_key = openAIAPIKeyRaw;
  body.openai_base_url = openAIBaseURL; // empty = clear
  body.default_model = defaultModel; // empty = clear
  body.title_model = titleModel; // empty = clear
  body.codex_default_model = codexDefaultModel;
  body.codex_title_model = codexTitleModel;
  body.default_sandbox = defaultSandbox;
  body.sandbox_by_activity = sandboxByActivity;
  body.sandbox_fast = sandboxFastEl ? !!sandboxFastEl.checked : true;
  body.container_cpus = containerCPUs; // empty = clear
  body.container_memory = containerMemory; // empty = clear
  return body;
}

function buildSandboxTestPayload(sandbox) {
  const rawPayload = buildSaveEnvPayload();
  const testPayload = {
    sandbox: sandbox,
    default_sandbox: rawPayload.default_sandbox || "",
    sandbox_by_activity: rawPayload.sandbox_by_activity || {},
    sandbox_fast: rawPayload.sandbox_fast !== false,
  };
  if (sandbox === "claude") {
    testPayload.base_url = rawPayload.base_url;
    testPayload.default_model = rawPayload.default_model;
    testPayload.title_model = rawPayload.title_model;
    if (rawPayload.oauth_token)
      testPayload.oauth_token = rawPayload.oauth_token;
    if (rawPayload.api_key) testPayload.api_key = rawPayload.api_key;
  } else {
    testPayload.openai_base_url = rawPayload.openai_base_url;
    testPayload.codex_default_model = rawPayload.codex_default_model;
    testPayload.codex_title_model = rawPayload.codex_title_model;
    if (rawPayload.openai_api_key)
      testPayload.openai_api_key = rawPayload.openai_api_key;
  }
  return testPayload;
}

function summarizeSandboxTestResult(resp) {
  if (!resp) return "No response";
  const normalized = (resp.last_test_result || "").toUpperCase();
  if (normalized === "PASS") return "PASS";
  if (normalized === "FAIL") return "FAIL";

  if (resp.status === "failed" && (resp.result || resp.stop_reason)) {
    return (resp.result || resp.stop_reason || "").slice(0, 120);
  }
  if (resp.status === "done" || resp.status === "waiting") {
    return "Test completed";
  }
  return `status ${resp.status}`;
}

async function testSandboxConfig(sandbox) {
  const statusEl = document.getElementById(
    sandbox === "claude" ? "env-claude-test-status" : "env-codex-test-status",
  );
  const payload = buildSandboxTestPayload(sandbox);
  statusEl.textContent = "Testing…";

  try {
    const resp = await api(Routes.env.test(), {
      method: "POST",
      body: JSON.stringify(payload),
    });
    statusEl.textContent = summarizeSandboxTestResult(resp);
    setTimeout(() => {
      if (
        statusEl.textContent.startsWith("status failed") ||
        statusEl.textContent.includes("FAIL") ||
        statusEl.textContent.startsWith("No response")
      )
        return;
      statusEl.textContent = "";
    }, 6000);
  } catch (e) {
    statusEl.textContent = "Error: " + e.message;
    setTimeout(() => {
      statusEl.textContent = "";
    }, 6000);
  }
}

function showEnvConfigEditor(event) {
  if (event) event.stopPropagation();
  return loadEnvConfig();
}

async function loadEnvConfig() {
  const safeSetValue = (id, fn) => {
    const el = document.getElementById(id);
    if (!el) {
      console.error(`Missing sandbox config field: ${id}`);
      return false;
    }
    fn(el);
    return true;
  };

  safeSetValue("env-oauth-token", (el) => {
    el.value = "";
  });
  safeSetValue("env-oauth-token", (el) => {
    el.placeholder = "(not set)";
  });
  safeSetValue("env-api-key", (el) => {
    el.value = "";
  });
  safeSetValue("env-api-key", (el) => {
    el.placeholder = "(not set)";
  });
  safeSetValue("env-claude-base-url", (el) => {
    el.value = "";
  });
  safeSetValue("env-openai-api-key", (el) => {
    el.value = "";
  });
  safeSetValue("env-openai-api-key", (el) => {
    el.placeholder = "(not set)";
  });
  safeSetValue("env-openai-base-url", (el) => {
    el.value = "";
  });
  safeSetValue("env-default-model", (el) => {
    el.value = "";
  });
  safeSetValue("env-title-model", (el) => {
    el.value = "";
  });
  safeSetValue("env-codex-default-model", (el) => {
    el.value = "";
  });
  safeSetValue("env-codex-title-model", (el) => {
    el.value = "";
  });
  safeSetValue("env-default-sandbox", (el) => {
    el.value = "";
  });
  safeSetValue("env-sandbox-fast", (el) => {
    el.checked = true;
  });
  safeSetValue("env-container-cpus", (el) => {
    el.value = "";
  });
  safeSetValue("env-container-memory", (el) => {
    el.value = "";
  });
  safeSetValue("env-config-status", (el) => {
    el.textContent = "";
  });
  safeSetValue("env-claude-test-status", (el) => {
    el.textContent = "";
  });
  safeSetValue("env-codex-test-status", (el) => {
    el.textContent = "";
  });
  let cfg = {
    oauth_token: "",
    api_key: "",
    base_url: "",
    openai_api_key: "",
    openai_base_url: "",
    default_model: "",
    title_model: "",
    codex_default_model: "",
    codex_title_model: "",
    default_sandbox: "",
    sandbox_by_activity: {},
    sandbox_fast: true,
  };
  try {
    cfg = await api(Routes.env.get());
  } catch (e) {
    safeSetValue("env-config-status", (el) => {
      el.textContent = "Failed to load configuration.";
    });
    console.error("Failed to load env config:", e);
  }

  // Populate fields — tokens show masked value as placeholder only.
  safeSetValue("env-oauth-token", (el) => {
    el.placeholder = cfg.oauth_token || "(not set)";
  });
  safeSetValue("env-api-key", (el) => {
    el.placeholder = cfg.api_key || "(not set)";
  });
  safeSetValue("env-claude-base-url", (el) => {
    el.value = cfg.base_url || "";
  });
  safeSetValue("env-openai-api-key", (el) => {
    el.placeholder = cfg.openai_api_key || "(not set)";
  });
  safeSetValue("env-openai-base-url", (el) => {
    el.value = cfg.openai_base_url || "";
  });
  safeSetValue("env-default-model", (el) => {
    el.value = cfg.default_model || "";
  });
  safeSetValue("env-title-model", (el) => {
    el.value = cfg.title_model || "";
  });
  safeSetValue("env-codex-default-model", (el) => {
    el.value = cfg.codex_default_model || "";
  });
  safeSetValue("env-codex-title-model", (el) => {
    el.value = cfg.codex_title_model || "";
  });
  safeSetValue("env-default-sandbox", (el) => {
    el.value = cfg.default_sandbox || "";
  });
  safeSetValue("env-sandbox-fast", (el) => {
    el.checked = cfg.sandbox_fast !== false;
  });
  applySandboxByActivity("env-sandbox-", cfg.sandbox_by_activity || {});
  safeSetValue("env-container-cpus", (el) => {
    el.value = cfg.container_cpus || "";
  });
  safeSetValue("env-container-memory", (el) => {
    el.value = cfg.container_memory || "";
  });
  safeSetValue("env-config-status", (el) => {
    if (el.textContent === "Failed to load configuration.") return;
    el.textContent = "";
  });
  if (typeof populateSandboxSelects === "function") {
    populateSandboxSelects();
  }
  safeSetValue("env-claude-test-status", (el) => {
    el.textContent = "";
  });
  safeSetValue("env-codex-test-status", (el) => {
    el.textContent = "";
  });

  // Update OAuth sign-in button visibility based on base URLs.
  _updateOAuthButtonVisibility();

  // Add input listeners for dynamic visibility (only once).
  var claudeBase = document.getElementById("env-claude-base-url");
  if (claudeBase && claudeBase.addEventListener && !claudeBase._oauthListenerAdded) {
    claudeBase.addEventListener("input", _updateOAuthButtonVisibility);
    claudeBase._oauthListenerAdded = true;
  }
  var openaiBase = document.getElementById("env-openai-base-url");
  if (openaiBase && openaiBase.addEventListener && !openaiBase._oauthListenerAdded) {
    openaiBase.addEventListener("input", _updateOAuthButtonVisibility);
    openaiBase._oauthListenerAdded = true;
  }
}

function closeEnvConfigEditor() {
  const statusEl = document.getElementById("env-config-status");
  if (statusEl) statusEl.textContent = "";
}

async function saveEnvConfig() {
  const body = buildSaveEnvPayload();

  const statusEl = document.getElementById("env-config-status");
  statusEl.textContent = "Saving…";
  try {
    await api(Routes.env.update(), {
      method: "PUT",
      body: JSON.stringify(body),
    });
    statusEl.textContent = "Saved.";
    // Clear sensitive inputs after saving so they don't linger in the DOM.
    document.getElementById("env-oauth-token").value = "";
    document.getElementById("env-api-key").value = "";
    document.getElementById("env-openai-api-key").value = "";
    // Refresh placeholders.
    setTimeout(() => showEnvConfigEditor(null), 600);
  } catch (e) {
    statusEl.textContent = "Error: " + e.message;
  }
}

// --- OAuth Sign-In ---

var _oauthPollers = {};

async function startOAuthFlow(provider) {
  var btn = document.getElementById(provider + "-oauth-signin-btn");
  var status = document.getElementById(provider + "-oauth-status");
  if (btn) btn.disabled = true;
  if (status) status.textContent = "Starting...";

  try {
    var url = Routes.auth.start().replace("{provider}", provider);
    var result = await api(url, { method: "POST" });
    if (!result.authorize_url) {
      if (status) status.textContent = "Error: no authorize URL returned";
      if (btn) btn.disabled = false;
      return;
    }

    // Open the authorize URL in a new tab.
    if (
      typeof window.runtime !== "undefined" &&
      window.runtime.BrowserOpenURL
    ) {
      window.runtime.BrowserOpenURL(result.authorize_url);
    } else {
      window.open(result.authorize_url, "_blank");
    }

    if (status) status.textContent = "Waiting for browser...";

    // Poll for completion.
    _startOAuthPolling(provider);
  } catch (e) {
    if (status) status.textContent = "Error: " + e.message;
    if (btn) btn.disabled = false;
  }
}

function _startOAuthPolling(provider) {
  // Clear any existing poller.
  if (_oauthPollers[provider]) {
    clearInterval(_oauthPollers[provider]);
  }

  var pollCount = 0;
  var maxPolls = 150; // 5 minutes at 2s intervals

  _oauthPollers[provider] = setInterval(async function () {
    pollCount++;
    if (pollCount > maxPolls) {
      _stopOAuthPolling(provider, "Timed out waiting for authorization.");
      return;
    }

    try {
      var url = Routes.auth.status().replace("{provider}", provider);
      var result = await api(url);
      if (result.state === "success") {
        _stopOAuthPolling(provider, "");
        var status = document.getElementById(provider + "-oauth-status");
        if (status) status.textContent = "Signed in!";
        // Refresh the env config display to show the new token.
        loadEnvConfig();
        // Clear success message after a few seconds.
        setTimeout(function () {
          if (status) status.textContent = "";
        }, 3000);
      } else if (result.state === "error") {
        _stopOAuthPolling(provider, result.error || "Authorization failed.");
      }
    } catch (e) {
      // Network error — keep polling, it might recover.
    }
  }, 2000);
}

function _stopOAuthPolling(provider, errorMessage) {
  if (_oauthPollers[provider]) {
    clearInterval(_oauthPollers[provider]);
    delete _oauthPollers[provider];
  }
  var btn = document.getElementById(provider + "-oauth-signin-btn");
  var status = document.getElementById(provider + "-oauth-status");
  if (btn) btn.disabled = false;
  if (status && errorMessage) status.textContent = errorMessage;
}

function cancelOAuthFlow(provider) {
  var url = Routes.auth.cancel().replace("{provider}", provider);
  api(url, { method: "POST" }).catch(function () {});
  _stopOAuthPolling(provider, "Cancelled.");
}

function _updateOAuthButtonVisibility() {
  var claudeBaseUrl = document.getElementById("env-claude-base-url");
  var claudeBtn = document.getElementById("claude-oauth-signin-btn");
  if (claudeBtn && claudeBaseUrl) {
    claudeBtn.style.display = claudeBaseUrl.value ? "none" : "";
  }

  var openaiBaseUrl = document.getElementById("env-openai-base-url");
  var codexBtn = document.getElementById("codex-oauth-signin-btn");
  if (codexBtn && openaiBaseUrl) {
    codexBtn.style.display = openaiBaseUrl.value ? "none" : "";
  }
}
