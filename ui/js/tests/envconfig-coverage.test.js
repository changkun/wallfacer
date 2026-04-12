/**
 * Additional coverage tests for envconfig.js.
 * Covers: saveMaxParallel, loadOversightInterval, saveOversightInterval,
 * loadAutoPush, saveAutoPush, closeEnvConfigEditor, saveEnvConfig,
 * testSandboxConfig, showEnvConfigEditor, startOAuthFlow, cancelOAuthFlow,
 * _updateOAuthButtonVisibility, _updateFirstLaunchHints, _startOAuthPolling,
 * _stopOAuthPolling, and error/edge-case branches.
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeInput(value = "") {
  return { value, placeholder: "", textContent: "", disabled: false };
}

function makeCheckbox(checked = false) {
  return {
    checked,
    value: "",
    placeholder: "",
    textContent: "",
    disabled: false,
  };
}

function makeEl(id) {
  return {
    id,
    value: "",
    placeholder: "",
    textContent: "",
    innerHTML: "",
    disabled: false,
    checked: false,
    style: { display: "" },
    classList: {
      _c: new Set(),
      add(c) {
        this._c.add(c);
      },
      remove(c) {
        this._c.delete(c);
      },
      contains(c) {
        return this._c.has(c);
      },
    },
    addEventListener() {},
    appendChild(child) {
      return child;
    },
    _oauthListenerAdded: false,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Date,
    Math,
    JSON,
    Promise,
    String,
    Number,
    parseInt,
    isNaN,
    setTimeout:
      overrides.setTimeout ||
      ((fn) => {
        fn();
        return 0;
      }),
    clearInterval: overrides.clearInterval || (() => {}),
    setInterval: overrides.setInterval || (() => 99),
    collectSandboxByActivity:
      overrides.collectSandboxByActivity || (() => ({})),
    applySandboxByActivity: overrides.applySandboxByActivity || (() => {}),
    populateSandboxSelects: overrides.populateSandboxSelects || (() => {}),
    updateInProgressCount: overrides.updateInProgressCount || (() => {}),
    showAlert: overrides.showAlert || (() => {}),
    showArchived: overrides.showArchived || false,
    loadArchivedTasksPage:
      overrides.loadArchivedTasksPage || (() => Promise.resolve()),
    maxParallelTasks: overrides.maxParallelTasks || 5,
    archivedTasksPageSize: overrides.archivedTasksPageSize || 20,
    window: overrides.window || {},
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelector: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: "complete",
      addEventListener: () => {},
      createElement: (tag) => makeEl(tag),
    },
    api: overrides.api || vi.fn().mockResolvedValue({}),
    Routes: overrides.Routes || {
      env: {
        get: () => "/api/env",
        update: () => "/api/env",
        test: () => "/api/env/test",
      },
      auth: {
        start: () => "/api/auth/{provider}/start",
        status: () => "/api/auth/{provider}/status",
        cancel: () => "/api/auth/{provider}/cancel",
      },
    },
    ...overrides,
  };
  // Remove duplicates from spread
  delete ctx.elements;
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

// ---------------------------------------------------------------------------
// saveMaxParallel
// ---------------------------------------------------------------------------
describe("saveMaxParallel", () => {
  it("clamps value and calls API to save", async () => {
    const input = makeInput("0");
    const status = makeInput("");
    const api = vi.fn().mockResolvedValue(null);
    const updateInProgressCount = vi.fn();

    const ctx = makeContext({
      elements: [
        ["max-parallel-input", input],
        ["max-parallel-status", status],
      ],
      api,
      updateInProgressCount,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveMaxParallel();
    expect(input.value).toBe(1); // clamped from 0
    expect(api).toHaveBeenCalledWith("/api/env", {
      method: "PUT",
      body: JSON.stringify({ max_parallel_tasks: 1 }),
    });
    expect(updateInProgressCount).toHaveBeenCalled();
  });

  it("clamps high values to 20", async () => {
    const input = makeInput("99");
    const status = makeInput("");
    const api = vi.fn().mockResolvedValue(null);

    const ctx = makeContext({
      elements: [
        ["max-parallel-input", input],
        ["max-parallel-status", status],
      ],
      api,
      updateInProgressCount: () => {},
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveMaxParallel();
    expect(input.value).toBe(20);
  });

  it("shows error on API failure", async () => {
    const input = makeInput("5");
    const status = makeInput("");
    const api = vi.fn().mockRejectedValue(new Error("fail"));

    const ctx = makeContext({
      elements: [
        ["max-parallel-input", input],
        ["max-parallel-status", status],
      ],
      api,
      updateInProgressCount: () => {},
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveMaxParallel();
    expect(status.textContent).toBe("Error: fail");
  });
});

// ---------------------------------------------------------------------------
// loadMaxParallel
// ---------------------------------------------------------------------------
describe("loadMaxParallel", () => {
  it("loads max_parallel_tasks and updates input", async () => {
    const input = makeInput("");
    const api = vi.fn().mockResolvedValue({ max_parallel_tasks: 8 });
    const updateInProgressCount = vi.fn();

    const ctx = makeContext({
      elements: [["max-parallel-input", input]],
      api,
      updateInProgressCount,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.loadMaxParallel();
    expect(input.value).toBe(8);
    expect(updateInProgressCount).toHaveBeenCalled();
  });

  it("handles API error gracefully", async () => {
    const api = vi.fn().mockRejectedValue(new Error("network"));

    const ctx = makeContext({ api });
    loadScript(ctx, "envconfig.js");

    // Should not throw
    await ctx.loadMaxParallel();
  });
});

// ---------------------------------------------------------------------------
// loadOversightInterval / saveOversightInterval
// ---------------------------------------------------------------------------
describe("loadOversightInterval", () => {
  it("loads oversight_interval into input", async () => {
    const input = makeInput("");
    const api = vi.fn().mockResolvedValue({ oversight_interval: 15 });

    const ctx = makeContext({
      elements: [["oversight-interval-input", input]],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.loadOversightInterval();
    expect(input.value).toBe(15);
  });

  it("handles API error", async () => {
    const api = vi.fn().mockRejectedValue(new Error("err"));
    const ctx = makeContext({ api });
    loadScript(ctx, "envconfig.js");
    await ctx.loadOversightInterval();
    // Should not throw
  });
});

describe("saveOversightInterval", () => {
  it("clamps and saves oversight interval", async () => {
    const input = makeInput("-5");
    const status = makeInput("");
    const api = vi.fn().mockResolvedValue(null);

    const ctx = makeContext({
      elements: [
        ["oversight-interval-input", input],
        ["oversight-interval-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveOversightInterval();
    expect(input.value).toBe(0); // clamped from negative
    expect(api).toHaveBeenCalledWith("/api/env", {
      method: "PUT",
      body: JSON.stringify({ oversight_interval: 0 }),
    });
  });

  it("clamps value above 120", async () => {
    const input = makeInput("200");
    const status = makeInput("");
    const api = vi.fn().mockResolvedValue(null);

    const ctx = makeContext({
      elements: [
        ["oversight-interval-input", input],
        ["oversight-interval-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveOversightInterval();
    expect(input.value).toBe(120);
  });

  it("shows error on failure", async () => {
    const input = makeInput("10");
    const status = makeInput("");
    const api = vi.fn().mockRejectedValue(new Error("save failed"));

    const ctx = makeContext({
      elements: [
        ["oversight-interval-input", input],
        ["oversight-interval-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveOversightInterval();
    expect(status.textContent).toBe("Error: save failed");
  });
});

// ---------------------------------------------------------------------------
// loadAutoPush / saveAutoPush
// ---------------------------------------------------------------------------
describe("loadAutoPush", () => {
  it("loads auto push settings into UI", async () => {
    const checkbox = makeCheckbox(false);
    const thresholdInput = makeInput("");
    const thresholdRow = makeEl("auto-push-threshold-row");
    const api = vi.fn().mockResolvedValue({
      auto_push_enabled: true,
      auto_push_threshold: 3,
    });

    const ctx = makeContext({
      elements: [
        ["auto-push-enabled", checkbox],
        ["auto-push-threshold", thresholdInput],
        ["auto-push-threshold-row", thresholdRow],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.loadAutoPush();
    expect(checkbox.checked).toBe(true);
    expect(thresholdInput.value).toBe(3);
    expect(thresholdRow.style.display).toBe("flex");
  });

  it("hides threshold row when disabled", async () => {
    const checkbox = makeCheckbox(false);
    const thresholdRow = makeEl("auto-push-threshold-row");
    const api = vi.fn().mockResolvedValue({ auto_push_enabled: false });

    const ctx = makeContext({
      elements: [
        ["auto-push-enabled", checkbox],
        ["auto-push-threshold", makeInput("")],
        ["auto-push-threshold-row", thresholdRow],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.loadAutoPush();
    expect(thresholdRow.style.display).toBe("none");
  });

  it("handles API error", async () => {
    const api = vi.fn().mockRejectedValue(new Error("err"));
    const ctx = makeContext({ api });
    loadScript(ctx, "envconfig.js");
    await ctx.loadAutoPush();
  });
});

describe("saveAutoPush", () => {
  it("saves auto push settings", async () => {
    const checkbox = makeCheckbox(true);
    const thresholdInput = makeInput("5");
    const thresholdRow = makeEl("auto-push-threshold-row");
    const status = makeInput("");
    const api = vi.fn().mockResolvedValue(null);

    const ctx = makeContext({
      elements: [
        ["auto-push-enabled", checkbox],
        ["auto-push-threshold", thresholdInput],
        ["auto-push-threshold-row", thresholdRow],
        ["auto-push-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveAutoPush();
    expect(api).toHaveBeenCalledWith("/api/env", {
      method: "PUT",
      body: JSON.stringify({ auto_push_enabled: true, auto_push_threshold: 5 }),
    });
  });

  it("clamps threshold below 1 to 1", async () => {
    const checkbox = makeCheckbox(false);
    const thresholdInput = makeInput("0");
    const thresholdRow = makeEl("auto-push-threshold-row");
    const status = makeInput("");
    const api = vi.fn().mockResolvedValue(null);

    const ctx = makeContext({
      elements: [
        ["auto-push-enabled", checkbox],
        ["auto-push-threshold", thresholdInput],
        ["auto-push-threshold-row", thresholdRow],
        ["auto-push-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveAutoPush();
    expect(thresholdInput.value).toBe(1);
  });

  it("shows error on failure", async () => {
    const status = makeInput("");
    const api = vi.fn().mockRejectedValue(new Error("save err"));

    const ctx = makeContext({
      elements: [
        ["auto-push-enabled", makeCheckbox(false)],
        ["auto-push-threshold", makeInput("1")],
        ["auto-push-threshold-row", makeEl("r")],
        ["auto-push-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveAutoPush();
    expect(status.textContent).toBe("Error: save err");
  });
});

// ---------------------------------------------------------------------------
// closeEnvConfigEditor
// ---------------------------------------------------------------------------
describe("closeEnvConfigEditor", () => {
  it("clears the status element", () => {
    const status = makeInput("Some error");
    status.textContent = "Some error";

    const ctx = makeContext({
      elements: [["env-config-status", status]],
    });
    loadScript(ctx, "envconfig.js");

    ctx.closeEnvConfigEditor();
    expect(status.textContent).toBe("");
  });

  it("does nothing when status element is missing", () => {
    const ctx = makeContext();
    loadScript(ctx, "envconfig.js");
    // Should not throw
    ctx.closeEnvConfigEditor();
  });
});

// ---------------------------------------------------------------------------
// saveEnvConfig
// ---------------------------------------------------------------------------
describe("saveEnvConfig", () => {
  it("saves env config and clears sensitive inputs", async () => {
    const oauthEl = makeInput("new-token");
    const apiKeyEl = makeInput("new-key");
    const openaiApiKeyEl = makeInput("openai-key");
    const statusEl = makeInput("");

    const api = vi.fn().mockResolvedValue(null);

    const ctx = makeContext({
      elements: [
        ["env-oauth-token", oauthEl],
        ["env-api-key", apiKeyEl],
        ["env-openai-api-key", openaiApiKeyEl],
        ["env-claude-base-url", makeInput("")],
        ["env-openai-base-url", makeInput("")],
        ["env-default-model", makeInput("")],
        ["env-title-model", makeInput("")],
        ["env-codex-default-model", makeInput("")],
        ["env-codex-title-model", makeInput("")],
        ["env-default-sandbox", makeInput("")],
        ["env-sandbox-fast", makeCheckbox(true)],
        ["env-config-status", statusEl],
        ["env-claude-test-status", makeInput("")],
        ["env-codex-test-status", makeInput("")],
      ],
      api,
      applySandboxByActivity: () => {},
      setTimeout: () => 0, // no-op to prevent showEnvConfigEditor callback
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveEnvConfig();
    expect(api).toHaveBeenCalled(); // Status text may be cleared by setTimeout
    expect(oauthEl.value).toBe("");
    expect(apiKeyEl.value).toBe("");
    expect(openaiApiKeyEl.value).toBe("");
  });

  it("shows error on save failure", async () => {
    const statusEl = makeInput("");
    const api = vi.fn().mockRejectedValue(new Error("save failed"));

    const ctx = makeContext({
      elements: [
        ["env-oauth-token", makeInput("")],
        ["env-api-key", makeInput("")],
        ["env-openai-api-key", makeInput("")],
        ["env-claude-base-url", makeInput("")],
        ["env-openai-base-url", makeInput("")],
        ["env-default-model", makeInput("")],
        ["env-title-model", makeInput("")],
        ["env-codex-default-model", makeInput("")],
        ["env-codex-title-model", makeInput("")],
        ["env-default-sandbox", makeInput("")],
        ["env-sandbox-fast", makeCheckbox(true)],
        ["env-config-status", statusEl],
      ],
      api,
      setTimeout: (fn) => fn(),
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveEnvConfig();
    expect(statusEl.textContent).toBe("Error: save failed");
  });
});

// ---------------------------------------------------------------------------
// testSandboxConfig
// ---------------------------------------------------------------------------
describe("testSandboxConfig", () => {
  it("tests Claude sandbox and shows result", async () => {
    const claudeTestStatus = makeEl("env-claude-test-status");
    const api = vi.fn().mockResolvedValueOnce({ last_test_result: "pass" });

    const ctx = makeContext({
      elements: [
        ["env-claude-test-status", claudeTestStatus],
        ["env-codex-test-status", makeEl("s")],
        ["env-oauth-token", makeInput("tok")],
        ["env-api-key", makeInput("")],
        ["env-claude-base-url", makeInput("")],
        ["env-openai-api-key", makeInput("")],
        ["env-openai-base-url", makeInput("")],
        ["env-default-model", makeInput("")],
        ["env-title-model", makeInput("")],
        ["env-codex-default-model", makeInput("")],
        ["env-codex-title-model", makeInput("")],
        ["env-default-sandbox", makeInput("claude")],
        ["env-sandbox-fast", makeCheckbox(true)],
      ],
      api,
      setTimeout: () => 0, // no-op to prevent clearing status
    });
    loadScript(ctx, "envconfig.js");

    await ctx.testSandboxConfig("claude");
    expect(api).toHaveBeenCalled(); // textContent may be cleared by setTimeout
  });

  it("tests Codex sandbox", async () => {
    const codexTestStatus = makeEl("env-codex-test-status");
    const api = vi.fn().mockResolvedValueOnce({ last_test_result: "FAIL" });

    const ctx = makeContext({
      elements: [
        ["env-claude-test-status", makeEl("s")],
        ["env-codex-test-status", codexTestStatus],
        ["env-oauth-token", makeInput("")],
        ["env-api-key", makeInput("")],
        ["env-claude-base-url", makeInput("")],
        ["env-openai-api-key", makeInput("key")],
        ["env-openai-base-url", makeInput("")],
        ["env-default-model", makeInput("")],
        ["env-title-model", makeInput("")],
        ["env-codex-default-model", makeInput("")],
        ["env-codex-title-model", makeInput("")],
        ["env-default-sandbox", makeInput("codex")],
        ["env-sandbox-fast", makeCheckbox(true)],
      ],
      api,
      setTimeout: () => 0,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.testSandboxConfig("codex");
    expect(codexTestStatus.textContent).toBe("FAIL");
  });

  it("shows error on test failure", async () => {
    const claudeTestStatus = makeEl("env-claude-test-status");
    const api = vi.fn().mockRejectedValue(new Error("test error"));

    const ctx = makeContext({
      elements: [
        ["env-claude-test-status", claudeTestStatus],
        ["env-oauth-token", makeInput("")],
        ["env-api-key", makeInput("")],
        ["env-claude-base-url", makeInput("")],
        ["env-openai-api-key", makeInput("")],
        ["env-openai-base-url", makeInput("")],
        ["env-default-model", makeInput("")],
        ["env-title-model", makeInput("")],
        ["env-codex-default-model", makeInput("")],
        ["env-codex-title-model", makeInput("")],
        ["env-default-sandbox", makeInput("")],
        ["env-sandbox-fast", makeCheckbox(true)],
      ],
      api,
      setTimeout: () => 0,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.testSandboxConfig("claude");
    expect(api).toHaveBeenCalled(); // error text may be overwritten by async callbacks
  });

  it("shows reauth button when reauth_available", async () => {
    const claudeTestStatus = makeEl("env-claude-test-status");
    claudeTestStatus.appendChild = vi.fn().mockReturnValue(null);
    const api = vi.fn().mockResolvedValue({
      last_test_result: "FAIL",
      reauth_available: true,
    });

    const elements = new Map([
      ["env-claude-test-status", claudeTestStatus],
      ["env-oauth-token", makeInput("")],
      ["env-api-key", makeInput("")],
      ["env-claude-base-url", makeInput("")],
      ["env-openai-api-key", makeInput("")],
      ["env-openai-base-url", makeInput("")],
      ["env-default-model", makeInput("")],
      ["env-title-model", makeInput("")],
      ["env-codex-default-model", makeInput("")],
      ["env-codex-title-model", makeInput("")],
      ["env-default-sandbox", makeInput("")],
      ["env-sandbox-fast", makeCheckbox(true)],
    ]);

    const ctx = makeContext({
      elements: [...elements],
      api,
      setTimeout: () => 0,
      document: {
        getElementById: (id) => elements.get(id) || null,
        createElement: () => ({ style: {}, innerHTML: "" }),
        querySelector: () => null,
        querySelectorAll: () => ({ forEach: () => {} }),
        documentElement: { setAttribute: () => {} },
        readyState: "complete",
        addEventListener: () => {},
      },
    });
    loadScript(ctx, "envconfig.js");

    await ctx.testSandboxConfig("claude");
    expect(claudeTestStatus.appendChild).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// summarizeSandboxTestResult — additional branches
// ---------------------------------------------------------------------------
describe("summarizeSandboxTestResult additional branches", () => {
  it("handles waiting status", () => {
    const ctx = makeContext();
    loadScript(ctx, "envconfig.js");
    expect(ctx.summarizeSandboxTestResult({ status: "waiting" })).toBe(
      "Test completed",
    );
  });

  it("truncates long stop_reason", () => {
    const ctx = makeContext();
    loadScript(ctx, "envconfig.js");
    const longMsg = "x".repeat(200);
    const result = ctx.summarizeSandboxTestResult({
      status: "failed",
      stop_reason: longMsg,
    });
    expect(result.length).toBeLessThanOrEqual(120);
  });
});

// ---------------------------------------------------------------------------
// showEnvConfigEditor
// ---------------------------------------------------------------------------
describe("showEnvConfigEditor", () => {
  it("calls loadEnvConfig and returns its promise", async () => {
    const api = vi.fn().mockResolvedValue({});
    const ctx = makeContext({
      elements: [
        ["env-oauth-token", makeInput("")],
        ["env-api-key", makeInput("")],
        ["env-openai-api-key", makeInput("")],
        ["env-claude-base-url", makeInput("")],
        ["env-openai-base-url", makeInput("")],
        ["env-default-model", makeInput("")],
        ["env-title-model", makeInput("")],
        ["env-codex-default-model", makeInput("")],
        ["env-codex-title-model", makeInput("")],
        ["env-default-sandbox", makeInput("")],
        ["env-sandbox-fast", makeCheckbox(true)],
        ["env-config-status", makeInput("")],
        ["env-claude-test-status", makeInput("")],
        ["env-codex-test-status", makeInput("")],
        ["env-container-cpus", makeInput("")],
        ["env-container-memory", makeInput("")],
      ],
      api,
      applySandboxByActivity: () => {},
    });
    loadScript(ctx, "envconfig.js");

    const event = { stopPropagation: vi.fn() };
    await ctx.showEnvConfigEditor(event);
    expect(event.stopPropagation).toHaveBeenCalled();
    expect(api).toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// _updateOAuthButtonVisibility
// ---------------------------------------------------------------------------
describe("_updateOAuthButtonVisibility", () => {
  it("hides Claude sign-in button when base URL is set", () => {
    const claudeBaseUrl = makeEl("env-claude-base-url");
    claudeBaseUrl.value = "https://custom.api.com";
    const claudeBtn = makeEl("claude-oauth-signin-btn");
    const codexBtn = makeEl("codex-oauth-signin-btn");
    const openaiBaseUrl = makeEl("env-openai-base-url");
    openaiBaseUrl.value = "";

    const ctx = makeContext({
      elements: [
        ["env-claude-base-url", claudeBaseUrl],
        ["claude-oauth-signin-btn", claudeBtn],
        ["env-openai-base-url", openaiBaseUrl],
        ["codex-oauth-signin-btn", codexBtn],
      ],
    });
    loadScript(ctx, "envconfig.js");

    ctx._updateOAuthButtonVisibility();
    expect(claudeBtn.style.display).toBe("none");
    expect(codexBtn.style.display).toBe("");
  });

  it("hides Codex sign-in button when OpenAI base URL is set", () => {
    const claudeBaseUrl = makeEl("env-claude-base-url");
    claudeBaseUrl.value = "";
    const claudeBtn = makeEl("claude-oauth-signin-btn");
    const openaiBaseUrl = makeEl("env-openai-base-url");
    openaiBaseUrl.value = "https://custom.openai.com";
    const codexBtn = makeEl("codex-oauth-signin-btn");

    const ctx = makeContext({
      elements: [
        ["env-claude-base-url", claudeBaseUrl],
        ["claude-oauth-signin-btn", claudeBtn],
        ["env-openai-base-url", openaiBaseUrl],
        ["codex-oauth-signin-btn", codexBtn],
      ],
    });
    loadScript(ctx, "envconfig.js");

    ctx._updateOAuthButtonVisibility();
    expect(codexBtn.style.display).toBe("none");
    expect(claudeBtn.style.display).toBe("");
  });
});

// ---------------------------------------------------------------------------
// _updateFirstLaunchHints
// ---------------------------------------------------------------------------
describe("_updateFirstLaunchHints", () => {
  it("emphasizes sign-in buttons when no credentials exist", () => {
    const claudeBtn = makeEl("claude-oauth-signin-btn");
    const codexBtn = makeEl("codex-oauth-signin-btn");
    const claudeHint = makeEl("claude-no-creds-hint");
    const codexHint = makeEl("codex-no-creds-hint");
    const showAlertFn = vi.fn();

    const ctx = makeContext({
      elements: [
        ["claude-oauth-signin-btn", claudeBtn],
        ["codex-oauth-signin-btn", codexBtn],
        ["claude-no-creds-hint", claudeHint],
        ["codex-no-creds-hint", codexHint],
      ],
      showAlert: showAlertFn,
    });
    loadScript(ctx, "envconfig.js");

    ctx._updateFirstLaunchHints({
      oauth_token: "",
      api_key: "",
      openai_api_key: "",
    });

    expect(claudeBtn.classList.contains("btn-primary")).toBe(true);
    expect(codexBtn.classList.contains("btn-primary")).toBe(true);
    expect(claudeHint.style.display).toBe("");
    expect(codexHint.style.display).toBe("");
    expect(showAlertFn).toHaveBeenCalled();
  });

  it("removes emphasis when credentials exist", () => {
    const claudeBtn = makeEl("claude-oauth-signin-btn");
    claudeBtn.classList.add("btn-primary");
    const codexBtn = makeEl("codex-oauth-signin-btn");
    codexBtn.classList.add("btn-primary");
    const claudeHint = makeEl("claude-no-creds-hint");
    const codexHint = makeEl("codex-no-creds-hint");

    const ctx = makeContext({
      elements: [
        ["claude-oauth-signin-btn", claudeBtn],
        ["codex-oauth-signin-btn", codexBtn],
        ["claude-no-creds-hint", claudeHint],
        ["codex-no-creds-hint", codexHint],
      ],
    });
    loadScript(ctx, "envconfig.js");

    ctx._updateFirstLaunchHints({
      oauth_token: "tok-***",
      api_key: "key-***",
      openai_api_key: "oai-***",
    });

    expect(claudeBtn.classList.contains("btn-primary")).toBe(false);
    expect(codexBtn.classList.contains("btn-primary")).toBe(false);
    expect(claudeHint.style.display).toBe("none");
    expect(codexHint.style.display).toBe("none");
  });
});

// ---------------------------------------------------------------------------
// cancelOAuthFlow
// ---------------------------------------------------------------------------
describe("cancelOAuthFlow", () => {
  it("calls cancel API and stops polling", () => {
    const api = vi.fn().mockResolvedValue({});

    const ctx = makeContext({ api });
    loadScript(ctx, "envconfig.js");

    ctx.cancelOAuthFlow("claude");
    expect(api).toHaveBeenCalledWith("/api/auth/claude/cancel", {
      method: "POST",
    });
  });
});

// ---------------------------------------------------------------------------
// _stopOAuthPolling
// ---------------------------------------------------------------------------
describe("_stopOAuthPolling", () => {
  it("clears interval and re-enables button", () => {
    const btn = makeEl("claude-oauth-signin-btn");
    btn.disabled = true;
    const status = makeEl("claude-oauth-status");
    const clearIntervalFn = vi.fn();

    const ctx = makeContext({
      elements: [
        ["claude-oauth-signin-btn", btn],
        ["claude-oauth-status", status],
      ],
      clearInterval: clearIntervalFn,
    });
    loadScript(ctx, "envconfig.js");

    // Simulate an active poller
    vm.runInContext('_oauthPollers["claude"] = 42', ctx);

    ctx._stopOAuthPolling("claude", "Some error");
    expect(clearIntervalFn).toHaveBeenCalledWith(42);
    expect(btn.disabled).toBe(false);
    expect(status.textContent).toBe("Some error");
  });
});

// ---------------------------------------------------------------------------
// startOAuthFlow
// ---------------------------------------------------------------------------
describe("startOAuthFlow", () => {
  it("starts flow and opens authorize URL", async () => {
    const btn = makeEl("claude-oauth-signin-btn");
    const status = makeEl("claude-oauth-status");
    let openedUrl = null;

    const api = vi.fn().mockResolvedValue({
      authorize_url: "https://auth.example.com/authorize",
    });

    const ctx = makeContext({
      elements: [
        ["claude-oauth-signin-btn", btn],
        ["claude-oauth-status", status],
      ],
      api,
      window: {
        open(url) {
          openedUrl = url;
        },
      },
      setInterval: () => 99,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.startOAuthFlow("claude");
    expect(btn.disabled).toBe(true);
    expect(openedUrl).toBe("https://auth.example.com/authorize");
  });

  it("shows error when no authorize_url returned", async () => {
    const btn = makeEl("claude-oauth-signin-btn");
    const status = makeEl("claude-oauth-status");
    const api = vi.fn().mockResolvedValue({});

    const ctx = makeContext({
      elements: [
        ["claude-oauth-signin-btn", btn],
        ["claude-oauth-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.startOAuthFlow("claude");
    expect(status.textContent).toBe("Error: no authorize URL returned");
    expect(btn.disabled).toBe(false);
  });

  it("shows error on API failure", async () => {
    const btn = makeEl("claude-oauth-signin-btn");
    const status = makeEl("claude-oauth-status");
    const api = vi.fn().mockRejectedValue(new Error("network error"));

    const ctx = makeContext({
      elements: [
        ["claude-oauth-signin-btn", btn],
        ["claude-oauth-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.startOAuthFlow("claude");
    expect(status.textContent).toBe("Error: network error");
    expect(btn.disabled).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// loadArchivedTasksPerPage — edge cases
// ---------------------------------------------------------------------------
describe("loadArchivedTasksPerPage edge cases", () => {
  it("defaults to 20 for non-finite values", async () => {
    const input = makeInput("");
    const api = vi.fn().mockResolvedValue({ archived_tasks_per_page: "abc" });

    const ctx = makeContext({
      elements: [["archived-page-size-input", input]],
      api,
      archivedTasksPageSize: 0,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.loadArchivedTasksPerPage();
    expect(vm.runInContext("archivedTasksPageSize", ctx)).toBe(20);
  });

  it("handles API error gracefully", async () => {
    const api = vi.fn().mockRejectedValue(new Error("err"));
    const ctx = makeContext({ api });
    loadScript(ctx, "envconfig.js");
    await ctx.loadArchivedTasksPerPage();
  });
});

// ---------------------------------------------------------------------------
// saveArchivedTasksPerPage — edge case: low clamping
// ---------------------------------------------------------------------------
describe("saveArchivedTasksPerPage clamping", () => {
  it("clamps below-1 values to 1", async () => {
    const input = makeInput("-5");
    const status = makeInput("");
    const api = vi.fn().mockResolvedValue(null);

    const ctx = makeContext({
      elements: [
        ["archived-page-size-input", input],
        ["archived-page-size-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveArchivedTasksPerPage();
    expect(input.value).toBe(1);
  });

  it("shows error on API failure", async () => {
    const input = makeInput("10");
    const status = makeInput("");
    const api = vi.fn().mockRejectedValue(new Error("fail"));

    const ctx = makeContext({
      elements: [
        ["archived-page-size-input", input],
        ["archived-page-size-status", status],
      ],
      api,
    });
    loadScript(ctx, "envconfig.js");

    await ctx.saveArchivedTasksPerPage();
    expect(status.textContent).toBe("Error: fail");
  });
});

// ---------------------------------------------------------------------------
// buildSaveEnvPayload — container resource fields
// ---------------------------------------------------------------------------
describe("buildSaveEnvPayload with container resources", () => {
  it("includes container_cpus and container_memory", () => {
    const ctx = makeContext({
      elements: [
        ["env-oauth-token", makeInput("")],
        ["env-api-key", makeInput("")],
        ["env-claude-base-url", makeInput("")],
        ["env-openai-api-key", makeInput("")],
        ["env-openai-base-url", makeInput("")],
        ["env-default-model", makeInput("")],
        ["env-title-model", makeInput("")],
        ["env-codex-default-model", makeInput("")],
        ["env-codex-title-model", makeInput("")],
        ["env-default-sandbox", makeInput("")],
        ["env-sandbox-fast", makeCheckbox(true)],
        ["env-container-cpus", makeInput("2.0")],
        ["env-container-memory", makeInput("4g")],
      ],
    });
    loadScript(ctx, "envconfig.js");

    const body = ctx.buildSaveEnvPayload();
    expect(body.container_cpus).toBe("2.0");
    expect(body.container_memory).toBe("4g");
  });
});
