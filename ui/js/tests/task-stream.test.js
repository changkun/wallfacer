/**
 * Tests for task-stream.js — pure SSE state reducers.
 *
 * Each function under test is a pure reducer: it takes an explicit state
 * object and returns a new state object without touching any globals.
 * Consequently the tests require no DOM mocks and no vm context — they import
 * the file directly and call the functions as ordinary JavaScript.
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// ---------------------------------------------------------------------------
// Minimal context: task-stream.js needs withAuthToken from transport.js.
// Provide a no-op stub so we stay pure and offline.
// ---------------------------------------------------------------------------

function makeContext(overrides = {}) {
  const ctx = vm.createContext({
    console,
    // withAuthToken is called by buildTasksStreamUrl; stub returns url unchanged.
    withAuthToken: (url) => url,
    ...overrides,
  });
  return ctx;
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

function task(id, fields = {}) {
  return {
    id,
    title: fields.title || id,
    status: fields.status || "backlog",
    archived: !!fields.archived,
    updated_at: fields.updated_at || "2026-03-10T00:00:00Z",
    position: fields.position || 0,
    prompt: fields.prompt || "",
    ...fields,
  };
}

function emptyState() {
  return {
    tasks: [],
    archivedTasks: [],
    archivedPage: {
      loadState: "idle",
      hasMoreBefore: false,
      hasMoreAfter: false,
    },
  };
}

// ---------------------------------------------------------------------------
// createTasksState
// ---------------------------------------------------------------------------

describe("createTasksState", () => {
  it("returns empty arrays when called without arguments", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const s = ctx.createTasksState();
    expect(s.tasks).toEqual([]);
    expect(s.archivedTasks).toEqual([]);
    expect(s.archivedPage.loadState).toBe("idle");
  });

  it("clones the tasks and archivedTasks arrays (does not mutate the input)", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const input = {
      tasks: [task("a")],
      archivedTasks: [task("b", { archived: true })],
      archivedPage: {},
    };
    const s = ctx.createTasksState(input);
    s.tasks.push(task("extra"));
    expect(input.tasks).toHaveLength(1);
  });
});

// ---------------------------------------------------------------------------
// applyTasksSnapshot
// ---------------------------------------------------------------------------

describe("applyTasksSnapshot", () => {
  it("replaces active tasks from the snapshot payload", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = emptyState();
    const next = ctx.applyTasksSnapshot(state, [task("t1"), task("t2")]);
    expect(next.tasks.map((t) => t.id)).toEqual(["t1", "t2"]);
  });

  it("leaves archivedTasks untouched", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = {
      ...emptyState(),
      archivedTasks: [task("arch", { archived: true })],
    };
    const next = ctx.applyTasksSnapshot(state, [task("t1")]);
    expect(next.archivedTasks).toHaveLength(1);
    expect(next.archivedTasks[0].id).toBe("arch");
  });

  it("handles a null or empty snapshot gracefully", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const next = ctx.applyTasksSnapshot(emptyState(), null);
    expect(next.tasks).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// applyTaskDeleted
// ---------------------------------------------------------------------------

describe("applyTaskDeleted", () => {
  it("removes the identified task from the active list", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = { ...emptyState(), tasks: [task("t1"), task("t2")] };
    const next = ctx.applyTaskDeleted(state, { id: "t1" });
    expect(next.tasks.map((t) => t.id)).toEqual(["t2"]);
  });

  it("removes the identified task from the archived list", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = {
      ...emptyState(),
      archivedTasks: [
        task("arch-1", { archived: true }),
        task("arch-2", { archived: true }),
      ],
    };
    const next = ctx.applyTaskDeleted(state, { id: "arch-1" });
    expect(next.archivedTasks.map((t) => t.id)).toEqual(["arch-2"]);
  });

  it("removes from both active and archived when the id appears in both", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = {
      tasks: [task("t1"), task("t2")],
      archivedTasks: [
        task("t2", { archived: true }),
        task("t3", { archived: true }),
      ],
      archivedPage: {
        loadState: "idle",
        hasMoreBefore: false,
        hasMoreAfter: false,
      },
    };
    const next = ctx.applyTaskDeleted(state, { id: "t2" });
    expect(next.tasks.map((t) => t.id)).toEqual(["t1"]);
    expect(next.archivedTasks.map((t) => t.id)).toEqual(["t3"]);
  });

  it("is a no-op when the id does not exist in either list", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = { ...emptyState(), tasks: [task("t1")] };
    const next = ctx.applyTaskDeleted(state, { id: "ghost" });
    expect(next.tasks.map((t) => t.id)).toEqual(["t1"]);
  });
});

// ---------------------------------------------------------------------------
// applyTaskUpdated
// ---------------------------------------------------------------------------

describe("applyTaskUpdated", () => {
  it("moves an active task to archivedTasks when archived=true and showArchived=true", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = {
      ...emptyState(),
      tasks: [task("t1", { updated_at: "2026-03-10T10:00:00Z" })],
    };
    const { state: next } = ctx.applyTaskUpdated(
      state,
      task("t1", { archived: true, updated_at: "2026-03-10T11:00:00Z" }),
      { showArchived: true, pageSize: 20 },
    );
    expect(next.tasks).toEqual([]);
    expect(next.archivedTasks.map((t) => t.id)).toEqual(["t1"]);
  });

  it("discards an archived task from state when showArchived=false", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = { ...emptyState(), tasks: [task("t1")] };
    const { state: next } = ctx.applyTaskUpdated(
      state,
      task("t1", { archived: true }),
      { showArchived: false },
    );
    expect(next.tasks).toEqual([]);
    expect(next.archivedTasks).toEqual([]);
  });

  it("restores an unarchived task from archivedTasks back to active tasks", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = {
      tasks: [],
      archivedTasks: [
        task("t1", { archived: true, updated_at: "2026-03-10T11:00:00Z" }),
      ],
      archivedPage: {
        loadState: "idle",
        hasMoreBefore: false,
        hasMoreAfter: false,
      },
    };
    const { state: next } = ctx.applyTaskUpdated(
      state,
      task("t1", {
        archived: false,
        status: "done",
        updated_at: "2026-03-10T12:00:00Z",
      }),
      { showArchived: true, pageSize: 20 },
    );
    expect(next.tasks.map((t) => t.id)).toEqual(["t1"]);
    expect(next.archivedTasks).toEqual([]);
  });

  it("returns the previousTask record for status-change announcements", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const original = task("t1", { status: "backlog" });
    const state = { ...emptyState(), tasks: [original] };
    const { previousTask } = ctx.applyTaskUpdated(
      state,
      task("t1", { status: "in_progress" }),
      {},
    );
    expect(previousTask).not.toBeNull();
    expect(previousTask.status).toBe("backlog");
  });

  it("returns null previousTask for a brand-new task", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const { previousTask } = ctx.applyTaskUpdated(
      emptyState(),
      task("brand-new"),
      {},
    );
    expect(previousTask).toBeNull();
  });
});

// ---------------------------------------------------------------------------
// mergeArchivedTasksPage
// ---------------------------------------------------------------------------

describe("mergeArchivedTasksPage", () => {
  it("replaces archived window on initial load", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = {
      ...emptyState(),
      archivedTasks: [task("old", { archived: true })],
    };
    const next = ctx.mergeArchivedTasksPage(
      state,
      {
        tasks: [task("new", { archived: true })],
        has_more_before: false,
        has_more_after: false,
      },
      "initial",
      20,
    );
    expect(next.archivedTasks.map((t) => t.id)).toEqual(["new"]);
  });

  it("appends unique tasks when loading the next page (direction=before)", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const existing = task("b", {
      archived: true,
      updated_at: "2026-03-10T10:00:00Z",
    });
    const incoming = task("c", {
      archived: true,
      updated_at: "2026-03-10T09:00:00Z",
    });
    const state = { ...emptyState(), archivedTasks: [existing] };
    const next = ctx.mergeArchivedTasksPage(
      state,
      { tasks: [incoming], has_more_before: true, has_more_after: false },
      "before",
      20,
    );
    expect(next.archivedTasks.map((t) => t.id)).toContain("b");
    expect(next.archivedTasks.map((t) => t.id)).toContain("c");
  });

  it("deduplicates tasks that appear in both the window and the page", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const state = {
      ...emptyState(),
      archivedTasks: [
        task("b", { archived: true, updated_at: "2026-03-10T10:00:00Z" }),
        task("a", { archived: true, updated_at: "2026-03-10T10:00:00Z" }),
      ],
    };
    const resp = {
      tasks: [
        task("c", { archived: true, updated_at: "2026-03-10T09:00:00Z" }),
        task("a", { archived: true, updated_at: "2026-03-10T10:00:00Z" }), // duplicate
      ],
      has_more_before: true,
      has_more_after: false,
    };
    const next = ctx.mergeArchivedTasksPage(state, resp, "before", 20);
    const ids = next.archivedTasks.map((t) => t.id);
    expect(ids.filter((id) => id === "a")).toHaveLength(1);
    expect(ids).toContain("b");
    expect(ids).toContain("c");
  });

  it("sets pagination flags from the response", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const next = ctx.mergeArchivedTasksPage(
      emptyState(),
      { tasks: [], has_more_before: true, has_more_after: true },
      "initial",
      20,
    );
    expect(next.archivedPage.hasMoreBefore).toBe(true);
    expect(next.archivedPage.hasMoreAfter).toBe(true);
  });
});

// ---------------------------------------------------------------------------
// trimArchivedWindowState
// ---------------------------------------------------------------------------

describe("trimArchivedWindowState", () => {
  it("does not trim when count is within pageSize * 3", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const archivedTasks = Array.from({ length: 6 }, (_, i) =>
      task(`t${i}`, {
        archived: true,
        updated_at: `2026-03-10T0${6 - i}:00:00Z`,
      }),
    );
    const state = { ...emptyState(), archivedTasks };
    const next = ctx.trimArchivedWindowState(state, "after", 2); // maxItems = 6
    expect(next.archivedTasks).toHaveLength(6);
  });

  it("trims the oldest entries (tail) when loading after and flags hasMoreBefore", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const archivedTasks = Array.from({ length: 7 }, (_, i) =>
      task(`task-${i}`, {
        archived: true,
        updated_at: `2026-03-10T0${6 - i}:00:00Z`,
      }),
    );
    const state = { ...emptyState(), archivedTasks };
    const next = ctx.trimArchivedWindowState(state, "after", 2); // maxItems = 6, keep first 6
    expect(next.archivedTasks.map((t) => t.id)).toEqual([
      "task-0",
      "task-1",
      "task-2",
      "task-3",
      "task-4",
      "task-5",
    ]);
    expect(next.archivedPage.hasMoreBefore).toBe(true);
    expect(next.archivedPage.hasMoreAfter).toBe(false);
  });

  it("trims the newest entries (head) when loading before and flags hasMoreAfter", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const archivedTasks = Array.from({ length: 7 }, (_, i) =>
      task(`task-${i}`, {
        archived: true,
        updated_at: `2026-03-10T0${6 - i}:00:00Z`,
      }),
    );
    const state = { ...emptyState(), archivedTasks };
    const next = ctx.trimArchivedWindowState(state, "before", 2); // maxItems = 6, drop first 1
    expect(next.archivedTasks.map((t) => t.id)).toEqual([
      "task-1",
      "task-2",
      "task-3",
      "task-4",
      "task-5",
      "task-6",
    ]);
    expect(next.archivedPage.hasMoreAfter).toBe(true);
    expect(next.archivedPage.hasMoreBefore).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// buildTasksStreamUrl
// ---------------------------------------------------------------------------

describe("buildTasksStreamUrl", () => {
  it("returns the base URL unchanged when no event ID is provided", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    expect(ctx.buildTasksStreamUrl("/api/tasks/stream", null)).toBe(
      "/api/tasks/stream",
    );
    expect(ctx.buildTasksStreamUrl("/api/tasks/stream", undefined)).toBe(
      "/api/tasks/stream",
    );
  });

  it("appends last_event_id as a query parameter", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const url = ctx.buildTasksStreamUrl("/api/tasks/stream", "evt-42");
    expect(url).toBe("/api/tasks/stream?last_event_id=evt-42");
  });

  it("uses & instead of ? when the base URL already has query parameters", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const url = ctx.buildTasksStreamUrl(
      "/api/tasks/stream?token=abc",
      "evt-42",
    );
    expect(url).toBe("/api/tasks/stream?token=abc&last_event_id=evt-42");
  });

  it("URL-encodes the event ID", () => {
    const ctx = loadScript(makeContext(), "task-stream.js");
    const url = ctx.buildTasksStreamUrl("/api/tasks/stream", "evt/with spaces");
    expect(url).toContain("last_event_id=evt%2Fwith%20spaces");
  });

  it("delegates token injection to withAuthToken (stub passes url through)", () => {
    // The stub withAuthToken simply returns url unchanged, verifying the
    // delegation is present without actually testing the DOM interaction.
    let called = false;
    const ctx = makeContext({
      withAuthToken: (url) => {
        called = true;
        return url;
      },
    });
    loadScript(ctx, "task-stream.js");
    ctx.buildTasksStreamUrl("/api/tasks/stream", "evt-1");
    expect(called).toBe(true);
  });
});
