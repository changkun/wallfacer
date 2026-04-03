/**
 * Tests for modal-oversight.js — oversight phase rendering.
 */
import { describe, it, expect, beforeAll } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext(extra = {}) {
  return vm.createContext({ console, Math, Date, ...extra });
}

function loadScript(filename, ctx) {
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

function makeOversightContext() {
  const ctx = makeContext({
    escapeHtml: (s) =>
      String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
    // Stubs for variables referenced at runtime but declared in other modules
    logsMode: "oversight",
    testLogsMode: "oversight",
    currentTaskId: null,
    renderLogs: () => {},
    renderTestLogs: () => {},
  });
  // oversight-shared.js must be loaded first because modal-oversight.js
  // delegates to buildPhaseListHTML defined there.
  loadScript("oversight-shared.js", ctx);
  loadScript("modal-oversight.js", ctx);
  return ctx;
}

// ---------------------------------------------------------------------------
// renderOversightPhases
// ---------------------------------------------------------------------------
describe("renderOversightPhases", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeOversightContext();
  });

  it("returns empty-state div when phases array is empty", () => {
    const result = ctx.renderOversightPhases([]);
    expect(result).toContain("oversight-empty");
    expect(result).toContain("No phases recorded");
  });

  it("returns empty-state div when phases is null", () => {
    const result = ctx.renderOversightPhases(null);
    expect(result).toContain("oversight-empty");
  });

  it("renders a phase with title", () => {
    const result = ctx.renderOversightPhases([
      { title: "Setup", summary: "Initial setup" },
    ]);
    expect(result).toContain("oversight-phase");
    expect(result).toContain("Phase 1");
    expect(result).toContain("Setup");
  });

  it("renders phase summary text", () => {
    const result = ctx.renderOversightPhases([
      { title: "Build", summary: "Built the project" },
    ]);
    expect(result).toContain("oversight-summary");
    expect(result).toContain("Built the project");
  });

  it("renders tools_used as oversight-tool spans", () => {
    const result = ctx.renderOversightPhases([
      {
        title: "Code",
        tools_used: ["Bash", "Read", "Write"],
      },
    ]);
    expect(result).toContain("oversight-tool");
    expect(result).toContain("Bash");
    expect(result).toContain("Read");
    expect(result).toContain("Write");
  });

  it("renders commands as list items", () => {
    const result = ctx.renderOversightPhases([
      {
        title: "Test",
        commands: ["npm test", "go build"],
      },
    ]);
    expect(result).toContain("oversight-commands");
    expect(result).toContain("oversight-command");
    expect(result).toContain("npm test");
    expect(result).toContain("go build");
  });

  it("renders actions as list items", () => {
    const result = ctx.renderOversightPhases([
      {
        title: "Deploy",
        actions: ["Push to remote", "Tag release"],
      },
    ]);
    expect(result).toContain("oversight-actions");
    expect(result).toContain("oversight-action");
    expect(result).toContain("Push to remote");
    expect(result).toContain("Tag release");
  });

  it("renders multiple phases with correct numbering", () => {
    const phases = [
      { title: "Phase One" },
      { title: "Phase Two" },
      { title: "Phase Three" },
    ];
    const result = ctx.renderOversightPhases(phases);
    expect(result).toContain("Phase 1");
    expect(result).toContain("Phase 2");
    expect(result).toContain("Phase 3");
    expect(result).toContain("Phase One");
    expect(result).toContain("Phase Two");
    expect(result).toContain("Phase Three");
  });

  it("omits summary div when summary is absent", () => {
    const result = ctx.renderOversightPhases([{ title: "X", tools_used: [] }]);
    expect(result).not.toContain("oversight-summary");
  });

  it("omits tools div when tools_used is empty", () => {
    const result = ctx.renderOversightPhases([{ title: "X", tools_used: [] }]);
    expect(result).not.toContain("oversight-tools");
  });

  it("omits commands list when commands is empty", () => {
    const result = ctx.renderOversightPhases([{ title: "X", commands: [] }]);
    expect(result).not.toContain("oversight-commands");
  });

  it("omits actions list when actions is empty", () => {
    const result = ctx.renderOversightPhases([{ title: "X", actions: [] }]);
    expect(result).not.toContain("oversight-actions");
  });

  it("escapes HTML in phase titles", () => {
    const result = ctx.renderOversightPhases([
      { title: "<script>alert(1)</script>" },
    ]);
    expect(result).not.toContain("<script>");
    expect(result).toContain("&lt;script&gt;");
  });

  it("escapes HTML in summary text", () => {
    const result = ctx.renderOversightPhases([
      { title: "X", summary: "<b>bold</b>" },
    ]);
    expect(result).not.toContain("<b>");
    expect(result).toContain("&lt;b&gt;");
  });

  it("escapes HTML in tool names", () => {
    const result = ctx.renderOversightPhases([
      { title: "X", tools_used: ["<evil>"] },
    ]);
    expect(result).not.toContain("<evil>");
    expect(result).toContain("&lt;evil&gt;");
  });

  it("escapes HTML in commands", () => {
    const result = ctx.renderOversightPhases([
      { title: "X", commands: ["rm -rf <path>"] },
    ]);
    expect(result).toContain("&lt;path&gt;");
  });

  it("escapes HTML in actions", () => {
    const result = ctx.renderOversightPhases([
      { title: "X", actions: ["do <something>"] },
    ]);
    expect(result).toContain("&lt;something&gt;");
  });

  it("shows phase header structure with phase number and title", () => {
    const result = ctx.renderOversightPhases([{ title: "My Phase" }]);
    expect(result).toContain("oversight-phase-header");
    expect(result).toContain("oversight-phase-num");
    expect(result).toContain("oversight-phase-title");
  });

  it("includes timestamp when phase has a timestamp", () => {
    const ts = new Date("2024-01-01T10:30:00Z").toISOString();
    const result = ctx.renderOversightPhases([{ title: "X", timestamp: ts }]);
    expect(result).toContain("oversight-phase-time");
  });

  it("omits timestamp span when phase has no timestamp", () => {
    const result = ctx.renderOversightPhases([{ title: "X" }]);
    expect(result).not.toContain("oversight-phase-time");
  });

  it("handles phase with empty title gracefully", () => {
    const result = ctx.renderOversightPhases([{ title: "" }]);
    expect(result).toContain("oversight-phase");
    // Should not throw
  });

  it("handles undefined tools_used gracefully (uses empty default)", () => {
    const result = ctx.renderOversightPhases([{ title: "X" }]);
    expect(result).not.toContain("oversight-tools");
  });
});

// ---------------------------------------------------------------------------
// renderOversightInLogs
// ---------------------------------------------------------------------------
describe("renderOversightInLogs", () => {
  function makeOversightLogsContext(overrides = {}) {
    const logsEl = { innerHTML: "" };
    const ctx = makeContext({
      escapeHtml: (s) =>
        String(s ?? "")
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;"),
      logsMode: "oversight",
      testLogsMode: "oversight",
      renderLogs: () => {},
      renderTestLogs: () => {},
      getOpenModalTaskId: () => overrides.taskId || "task-1",
      _modalState: { seq: 1, abort: null },
      apiGet: overrides.apiGet || (() => Promise.resolve({})),
      cardOversightCache: { set: () => {} },
      scheduleRender: () => {},
      setTimeout: () => {},
      document: {
        getElementById: (id) => {
          if (id === "modal-logs") return logsEl;
          if (id === "modal-test-logs") return logsEl;
          return null;
        },
      },
      ...overrides,
    });
    loadScript("oversight-shared.js", ctx);
    loadScript("modal-oversight.js", ctx);
    return { ctx, logsEl };
  }

  it("shows loading message when oversight not yet fetched", () => {
    const { ctx, logsEl } = makeOversightLogsContext();
    ctx.renderOversightInLogs();
    expect(logsEl.innerHTML).toContain("Fetching oversight summary");
  });

  it("renders pending status", () => {
    const { ctx, logsEl } = makeOversightLogsContext();
    vm.runInContext('oversightData = { status: "pending" };', ctx);
    ctx.renderOversightInLogs();
    expect(logsEl.innerHTML).toContain("not yet generated");
  });

  it("renders generating status", () => {
    const { ctx, logsEl } = makeOversightLogsContext();
    vm.runInContext('oversightData = { status: "generating" };', ctx);
    ctx.renderOversightInLogs();
    expect(logsEl.innerHTML).toContain("Generating oversight summary");
  });

  it("renders failed status", () => {
    const { ctx, logsEl } = makeOversightLogsContext();
    vm.runInContext(
      'oversightData = { status: "failed", error: "timeout" };',
      ctx,
    );
    ctx.renderOversightInLogs();
    expect(logsEl.innerHTML).toContain("Oversight generation failed");
    expect(logsEl.innerHTML).toContain("timeout");
  });

  it("renders failed status without error detail", () => {
    const { ctx, logsEl } = makeOversightLogsContext();
    vm.runInContext('oversightData = { status: "failed" };', ctx);
    ctx.renderOversightInLogs();
    expect(logsEl.innerHTML).toContain("Oversight generation failed");
  });

  it("renders ready status with phases", () => {
    const { ctx, logsEl } = makeOversightLogsContext();
    vm.runInContext(
      'oversightData = { status: "ready", phases: [{ title: "Phase 1" }] };',
      ctx,
    );
    ctx.renderOversightInLogs();
    expect(logsEl.innerHTML).toContain("oversight-view");
    expect(logsEl.innerHTML).toContain("Phase 1");
  });

  it("renders default case for unknown status", () => {
    const { ctx, logsEl } = makeOversightLogsContext();
    vm.runInContext('oversightData = { status: "unknown" };', ctx);
    ctx.renderOversightInLogs();
    expect(logsEl.innerHTML).toContain("Loading");
  });
});

// ---------------------------------------------------------------------------
// renderTestOversightInTestLogs
// ---------------------------------------------------------------------------
describe("renderTestOversightInTestLogs", () => {
  function makeTestOversightCtx(overrides = {}) {
    const logsEl = { innerHTML: "" };
    const ctx = makeContext({
      escapeHtml: (s) =>
        String(s ?? "")
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;"),
      logsMode: "oversight",
      testLogsMode: "oversight",
      renderLogs: () => {},
      renderTestLogs: () => {},
      getOpenModalTaskId: () => overrides.taskId || "task-1",
      _modalState: { seq: 1, abort: null },
      apiGet: overrides.apiGet || (() => Promise.resolve({})),
      setTimeout: () => {},
      document: {
        getElementById: (id) => {
          if (id === "modal-test-logs") return logsEl;
          return null;
        },
      },
      ...overrides,
    });
    loadScript("oversight-shared.js", ctx);
    loadScript("modal-oversight.js", ctx);
    return { ctx, logsEl };
  }

  it("shows loading message when test oversight not fetched", () => {
    const { ctx, logsEl } = makeTestOversightCtx();
    ctx.renderTestOversightInTestLogs();
    expect(logsEl.innerHTML).toContain("Fetching test oversight summary");
  });

  it("renders pending test oversight status", () => {
    const { ctx, logsEl } = makeTestOversightCtx();
    vm.runInContext('testOversightData = { status: "pending" };', ctx);
    ctx.renderTestOversightInTestLogs();
    expect(logsEl.innerHTML).toContain("Test oversight summary not yet generated");
  });

  it("renders generating test oversight status", () => {
    const { ctx, logsEl } = makeTestOversightCtx();
    vm.runInContext('testOversightData = { status: "generating" };', ctx);
    ctx.renderTestOversightInTestLogs();
    expect(logsEl.innerHTML).toContain("Generating test oversight summary");
  });

  it("renders failed test oversight with error", () => {
    const { ctx, logsEl } = makeTestOversightCtx();
    vm.runInContext(
      'testOversightData = { status: "failed", error: "crash" };',
      ctx,
    );
    ctx.renderTestOversightInTestLogs();
    expect(logsEl.innerHTML).toContain("Test oversight generation failed");
    expect(logsEl.innerHTML).toContain("crash");
  });

  it("renders ready test oversight with phases", () => {
    const { ctx, logsEl } = makeTestOversightCtx();
    vm.runInContext(
      'testOversightData = { status: "ready", phases: [{ title: "Test" }] };',
      ctx,
    );
    ctx.renderTestOversightInTestLogs();
    expect(logsEl.innerHTML).toContain("oversight-view");
    expect(logsEl.innerHTML).toContain("Test");
  });

  it("renders default case for unknown test status", () => {
    const { ctx, logsEl } = makeTestOversightCtx();
    vm.runInContext('testOversightData = { status: "xyz" };', ctx);
    ctx.renderTestOversightInTestLogs();
    expect(logsEl.innerHTML).toContain("Loading");
  });
});

// ---------------------------------------------------------------------------
// State variables are initialized to null/false
// let declarations in vm scripts are not global-object properties; we read
// them from within the script's lexical scope using vm.runInContext.
// ---------------------------------------------------------------------------
describe("oversight state variables", () => {
  it("oversightData starts as null", () => {
    const ctx = makeOversightContext();
    expect(vm.runInContext("oversightData", ctx)).toBeNull();
  });

  it("oversightFetching starts as false", () => {
    const ctx = makeOversightContext();
    expect(vm.runInContext("oversightFetching", ctx)).toBe(false);
  });

  it("testOversightData starts as null", () => {
    const ctx = makeOversightContext();
    expect(vm.runInContext("testOversightData", ctx)).toBeNull();
  });

  it("testOversightFetching starts as false", () => {
    const ctx = makeOversightContext();
    expect(vm.runInContext("testOversightFetching", ctx)).toBe(false);
  });
});
