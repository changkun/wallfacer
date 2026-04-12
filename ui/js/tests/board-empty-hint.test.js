/**
 * Tests for the empty-Board hint — the one-line fallback element that
 * guides users back to Plan mode when the board has zero tasks.
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeHintEl() {
  const classes = new Set(["board-empty-hint", "hidden"]);
  return {
    classList: {
      add: (c) => classes.add(c),
      remove: (c) => classes.delete(c),
      contains: (c) => classes.has(c),
      toggle: (c, force) => {
        if (force === true) classes.add(c);
        else if (force === false) classes.delete(c);
        else (classes.has(c) ? classes.delete(c) : classes.add(c));
      },
    },
    _classes: classes,
  };
}

function createMinimalRenderContext({ tasks = [] } = {}) {
  const hint = makeHintEl();
  const ctx = vm.createContext({
    module: { exports: {} },
    exports: {},
    console,
    Date,
    Math,
    JSON,
    Promise,
    setTimeout: () => 0,
    clearTimeout: () => {},
    requestAnimationFrame: (cb) => cb(),
    localStorage: {
      getItem: () => null,
      setItem: () => {},
    },
    window: { depGraphEnabled: false, location: { hash: "" } },
    location: { hash: "" },
    document: {
      // Only the empty-hint element is surfaced; every other DOM lookup
      // (column containers, counts, search input, etc.) returns null so
      // render.js's "if (!el) continue" paths safely no-op.
      getElementById: (id) =>
        id === "board-empty-hint" ? hint : null,
      createElement: () => ({ innerHTML: "", appendChild: () => {} }),
      querySelectorAll: () => [],
      addEventListener: () => {},
      readyState: "complete",
    },
    tasks,
    archivedTasks: [],
    showArchived: false,
    backlogSortMode: "manual",
    filterQuery: "",
    maxParallelTasks: 0,
    updateIdeationFromTasks: () => {},
    updateWorkspaceGroupBadges: () => {},
    updateBacklogSortButton: () => {},
    announceBoardStatus: () => {},
    scheduleRender: () => {},
    notifyTaskChangeListeners: () => {},
    formatInProgressCount: (n) => String(n),
    updateMaxParallelTag: () => {},
    sortBacklogTasks: () => {},
    matchesFilter: () => true,
    renderMarkdown: (s) => String(s || ""),
    escapeHtml: (s) => String(s || ""),
    timeAgo: () => "now",
    highlightMatch: (t) => t || "",
    formatTimeout: (m) => String(m || 5),
    sandboxDisplayName: (s) => s || "Default",
    taskDisplayPrompt: () => "",
    getTaskAccessibleTitle: () => "",
    formatTaskStatusLabel: () => "",
    getTaskDependencyIds: () => [],
    getOpenModalTaskId: () => null,
    renderModalDependencies: () => {},
    activeWorkspaces: ["~/project"],
    Routes: {
      tasks: {
        diff: () => "",
        list: () => "",
        stream: () => "",
      },
    },
    task: () => ({
      diff: () => "",
      update: () => "",
      archive: () => "",
      done: () => "",
      resume: () => "",
    }),
    renderRefineHistory: () => {},
    updateRefineUI: () => {},
    hideDependencyGraph: () => {},
    renderDependencyGraph: () => {},
    api: () => Promise.resolve(),
    syncTask: () => {},
  });
  loadLibDeps("render.js", ctx);
  const code = readFileSync(join(jsDir, "render.js"), "utf8");
  vm.runInContext(code, ctx);
  return { ctx, hint };
}

describe("empty-Board hint", () => {
  it("TestEmptyBoardHint_RendersWhenZeroTasks — hint visible when no tasks", () => {
    const { ctx, hint } = createMinimalRenderContext({ tasks: [] });
    ctx.render();
    expect(hint.classList.contains("hidden")).toBe(false);
  });

  it("TestEmptyBoardHint_HiddenWhenNonEmpty — hint hidden when at least one task", () => {
    const { ctx, hint } = createMinimalRenderContext({
      tasks: [
        {
          id: "t1",
          status: "backlog",
          prompt: "x",
          updated_at: new Date().toISOString(),
          created_at: new Date().toISOString(),
          tags: [],
          title: "t1",
          archived: false,
        },
      ],
    });
    ctx.render();
    expect(hint.classList.contains("hidden")).toBe(true);
  });

  it("tolerates a missing hint element (template omitted)", () => {
    // Building a context whose document.getElementById always returns null
    // should not throw when render() runs.
    const { ctx } = createMinimalRenderContext({ tasks: [] });
    ctx.document.getElementById = () => null;
    expect(() => ctx.render()).not.toThrow();
  });
});
