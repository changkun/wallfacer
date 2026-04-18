/**
 * Tests for git.js — git status bar, URL conversion, workspace mutations.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);

  const ctx = {
    console,
    Date,
    Math,
    parseInt,
    String,
    Array,
    JSON,
    Error,
    Object,
    Promise,
    encodeURIComponent,
    setTimeout: overrides.setTimeout || (() => 0),
    setInterval: () => 0,
    clearInterval: () => {},
    EventSource:
      overrides.EventSource ||
      class MockEventSource {
        constructor() {
          this.readyState = 0;
        }
        close() {}
      },
    fetch: overrides.fetch || vi.fn(),
    document: {
      getElementById: (id) => elements.get(id) || null,
      querySelector: overrides.querySelector || (() => null),
      querySelectorAll: () => ({ forEach: () => {} }),
      createElement: (tag) => ({
        tagName: tag.toUpperCase(),
        className: "",
        innerHTML: "",
        style: {},
        setAttribute: vi.fn(),
        getAttribute: vi.fn(),
        appendChild: vi.fn(),
        querySelector: () => null,
        querySelectorAll: () => ({ forEach: () => {} }),
        getBoundingClientRect: () => ({ bottom: 100, left: 50 }),
        remove: vi.fn(),
        focus: vi.fn(),
        addEventListener: vi.fn(),
      }),
      body: {
        appendChild: vi.fn(),
      },
      title: "Wallfacer",
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      readyState: "complete",
    },
    api: overrides.api || vi.fn(),
    escapeHtml: overrides.escapeHtml || ((s) => String(s)),
    showAlert: overrides.showAlert || vi.fn(),
    withAuthToken: overrides.withAuthToken || ((url) => url),
    withAuthHeaders: overrides.withAuthHeaders || ((h) => h),
    withBearerHeaders: overrides.withBearerHeaders || ((h) => h),
    Routes: overrides.Routes || {
      git: {
        status: () => "/api/git/status",
        stream: () => "/api/git/stream",
        push: () => "/api/git/push",
        sync: () => "/api/git/sync",
        rebaseOnMain: () => "/api/git/rebase-on-main",
        branches: () => "/api/git/branches",
        checkout: () => "/api/git/checkout",
        createBranch: () => "/api/git/create-branch",
        openFolder: () => "/api/git/open-folder",
      },
    },
    activeWorkspaces: overrides.activeWorkspaces || [],
    gitStatuses: overrides.gitStatuses || null,
    gitStatusSource: overrides.gitStatusSource || null,
    gitRetryDelay: overrides.gitRetryDelay || 1000,
    _sseIsLeader: overrides._sseIsLeader || (() => true),
    _sseOnFollowerEvent: overrides._sseOnFollowerEvent || vi.fn(),
    _sseRelay: overrides._sseRelay || vi.fn(),
    updateStatusBar: overrides.updateStatusBar || vi.fn(),
    toggleBranchDropdown: overrides.toggleBranchDropdown || vi.fn(),
    closeBranchDropdown: overrides.closeBranchDropdown || vi.fn(),
  };

  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "git.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "git.js") });
  return ctx;
}

// ---------------------------------------------------------------------------
// remoteUrlToHttps
// ---------------------------------------------------------------------------
describe("remoteUrlToHttps", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
    loadScript(ctx);
  });

  it("returns null for empty/falsy input", () => {
    expect(ctx.remoteUrlToHttps(null)).toBe(null);
    expect(ctx.remoteUrlToHttps("")).toBe(null);
    expect(ctx.remoteUrlToHttps(undefined)).toBe(null);
  });

  it("passes through https URLs and strips .git suffix", () => {
    expect(ctx.remoteUrlToHttps("https://github.com/user/repo.git")).toBe(
      "https://github.com/user/repo",
    );
    expect(ctx.remoteUrlToHttps("https://github.com/user/repo")).toBe(
      "https://github.com/user/repo",
    );
  });

  it("passes through http URLs and strips .git suffix", () => {
    expect(ctx.remoteUrlToHttps("http://github.com/user/repo.git")).toBe(
      "http://github.com/user/repo",
    );
    expect(ctx.remoteUrlToHttps("http://github.com/user/repo")).toBe(
      "http://github.com/user/repo",
    );
  });

  it("converts git@host:user/repo.git to https", () => {
    expect(ctx.remoteUrlToHttps("git@github.com:user/repo.git")).toBe(
      "https://github.com/user/repo",
    );
  });

  it("converts git@host:user/repo (no .git) to https", () => {
    expect(ctx.remoteUrlToHttps("git@github.com:user/repo")).toBe(
      "https://github.com/user/repo",
    );
  });

  it("converts ssh://git@host/user/repo.git to https", () => {
    expect(ctx.remoteUrlToHttps("ssh://git@github.com/user/repo.git")).toBe(
      "https://github.com/user/repo",
    );
  });

  it("converts ssh://git@host/user/repo (no .git) to https", () => {
    expect(ctx.remoteUrlToHttps("ssh://git@github.com/user/repo")).toBe(
      "https://github.com/user/repo",
    );
  });

  it("handles ssh:// without user@ prefix", () => {
    expect(ctx.remoteUrlToHttps("ssh://github.com/user/repo.git")).toBe(
      "https://github.com/user/repo",
    );
  });

  it("trims whitespace around URL", () => {
    expect(ctx.remoteUrlToHttps("  https://github.com/user/repo.git  ")).toBe(
      "https://github.com/user/repo",
    );
    expect(ctx.remoteUrlToHttps("  git@github.com:user/repo.git  ")).toBe(
      "https://github.com/user/repo",
    );
  });

  it("returns null for unrecognized schemes", () => {
    expect(ctx.remoteUrlToHttps("ftp://github.com/repo")).toBe(null);
    expect(ctx.remoteUrlToHttps("just-some-string")).toBe(null);
  });

  it("handles nested paths like user/org/repo", () => {
    expect(ctx.remoteUrlToHttps("git@gitlab.com:org/sub/repo.git")).toBe(
      "https://gitlab.com/org/sub/repo",
    );
    expect(ctx.remoteUrlToHttps("ssh://git@gitlab.com/org/sub/repo.git")).toBe(
      "https://gitlab.com/org/sub/repo",
    );
  });
});

// ---------------------------------------------------------------------------
// formatGitWorkspaceConflict
// ---------------------------------------------------------------------------
describe("formatGitWorkspaceConflict", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
    loadScript(ctx);
  });

  it("returns fallback message when err is null", () => {
    expect(ctx.formatGitWorkspaceConflict(null, "Push")).toBe("Push failed");
  });

  it("returns err.error when no blocking_tasks", () => {
    expect(
      ctx.formatGitWorkspaceConflict({ error: "Some error" }, "Push"),
    ).toBe("Some error");
  });

  it("returns fallback when err has no error and no blocking_tasks", () => {
    expect(ctx.formatGitWorkspaceConflict({}, "Sync")).toBe("Sync failed");
  });

  it("returns fallback when blocking_tasks is empty array", () => {
    expect(ctx.formatGitWorkspaceConflict({ blocking_tasks: [] }, "Push")).toBe(
      "Push failed",
    );
  });

  it("returns fallback when blocking_tasks is not an array", () => {
    expect(
      ctx.formatGitWorkspaceConflict({ blocking_tasks: "not-array" }, "Push"),
    ).toBe("Push failed");
  });

  it("formats blocking tasks with error message", () => {
    const err = {
      error: "Workspace busy",
      blocking_tasks: [
        { id: "abc-123", title: "Fix bug", status: "in_progress" },
      ],
    };
    const result = ctx.formatGitWorkspaceConflict(err, "Push");
    expect(result).toContain("Workspace busy");
    expect(result).toContain("Blocking tasks:");
    expect(result).toContain("[in progress]");
    expect(result).toContain("Fix bug");
    expect(result).toContain("abc-123");
  });

  it("formats multiple blocking tasks", () => {
    const err = {
      error: "Blocked",
      blocking_tasks: [
        { id: "aaa", title: "Task A", status: "in_progress" },
        { id: "bbb", title: "Task B", status: "waiting" },
      ],
    };
    const result = ctx.formatGitWorkspaceConflict(err, "Sync");
    expect(result).toContain("Task A");
    expect(result).toContain("Task B");
    expect(result).toContain("[in progress]");
    expect(result).toContain("[waiting]");
  });

  it("handles missing title and status in blocking tasks", () => {
    const err = {
      blocking_tasks: [{ id: "xyz" }],
    };
    const result = ctx.formatGitWorkspaceConflict(err, "Rebase");
    expect(result).toContain("(untitled task)");
    expect(result).toContain("[unknown]");
    expect(result).toContain("Rebase blocked");
  });

  it("uses fallback action for the 'blocked' prefix when err.error is missing", () => {
    const err = {
      blocking_tasks: [{ id: "xyz", title: "T", status: "done" }],
    };
    const result = ctx.formatGitWorkspaceConflict(err, "Branch switch");
    expect(result).toMatch(/^Branch switch blocked/);
  });
});

// ---------------------------------------------------------------------------
// setGitActionPending
// ---------------------------------------------------------------------------
describe("setGitActionPending", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
    loadScript(ctx);
  });

  it("returns a no-op restore when btn is null", () => {
    const restore = ctx.setGitActionPending(null, "Loading...");
    expect(typeof restore).toBe("function");
    restore(); // should not throw
  });

  it("disables the button and sets pending label", () => {
    const btn = { disabled: false, textContent: "Push" };
    const restore = ctx.setGitActionPending(btn, "Pushing...");
    expect(btn.disabled).toBe(true);
    expect(btn.textContent).toBe("Pushing...");
  });

  it("restore function resets original state", () => {
    const btn = { disabled: false, textContent: "Push" };
    const restore = ctx.setGitActionPending(btn, "Pushing...");
    restore();
    expect(btn.disabled).toBe(false);
    expect(btn.textContent).toBe("Push");
  });

  it("preserves originally-disabled state", () => {
    const btn = { disabled: true, textContent: "Already disabled" };
    const restore = ctx.setGitActionPending(btn, "...");
    expect(btn.disabled).toBe(true);
    expect(btn.textContent).toBe("...");
    restore();
    expect(btn.disabled).toBe(true);
    expect(btn.textContent).toBe("Already disabled");
  });

  it("does not change textContent when pendingLabel is falsy", () => {
    const btn = { disabled: false, textContent: "Sync" };
    ctx.setGitActionPending(btn, null);
    expect(btn.textContent).toBe("Sync");

    const btn2 = { disabled: false, textContent: "Sync" };
    ctx.setGitActionPending(btn2, "");
    expect(btn2.textContent).toBe("Sync");
  });
});

// ---------------------------------------------------------------------------
// requestGitWorkspaceMutation
// ---------------------------------------------------------------------------
describe("requestGitWorkspaceMutation", () => {
  it("returns null for 204 responses", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 204,
      ok: true,
      text: vi.fn().mockResolvedValue(""),
    });
    const ctx = makeContext({ fetch: mockFetch });
    loadScript(ctx);

    const result = await ctx.requestGitWorkspaceMutation("/api/git/push", {
      workspace: "/foo",
    });
    expect(result).toBe(null);
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/git/push",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify({ workspace: "/foo" }),
      }),
    );
  });

  it("returns parsed JSON for successful response", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      text: vi.fn().mockResolvedValue('{"result":"ok"}'),
    });
    const ctx = makeContext({ fetch: mockFetch });
    loadScript(ctx);

    const result = await ctx.requestGitWorkspaceMutation("/api/git/sync", {
      workspace: "/bar",
    });
    expect(result).toEqual({ result: "ok" });
  });

  it("returns null for empty body on success", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 200,
      ok: true,
      text: vi.fn().mockResolvedValue(""),
    });
    const ctx = makeContext({ fetch: mockFetch });
    loadScript(ctx);

    const result = await ctx.requestGitWorkspaceMutation("/test", {});
    expect(result).toBe(null);
  });

  it("throws error with status and data for non-ok responses", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 409,
      ok: false,
      text: vi
        .fn()
        .mockResolvedValue('{"error":"conflict","blocking_tasks":[]}'),
    });
    const ctx = makeContext({ fetch: mockFetch });
    loadScript(ctx);

    try {
      await ctx.requestGitWorkspaceMutation("/api/git/checkout", {
        workspace: "/baz",
      });
      expect.unreachable("should have thrown");
    } catch (e) {
      expect(e.message).toBe("conflict");
      expect(e.status).toBe(409);
      expect(e.data).toEqual({ error: "conflict", blocking_tasks: [] });
    }
  });

  it("throws error with text fallback when JSON parse fails on error response", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 500,
      ok: false,
      text: vi.fn().mockResolvedValue("Internal Server Error"),
    });
    const ctx = makeContext({ fetch: mockFetch });
    loadScript(ctx);

    try {
      await ctx.requestGitWorkspaceMutation("/test", {});
      expect.unreachable("should have thrown");
    } catch (e) {
      expect(e.message).toBe("Internal Server Error");
      expect(e.status).toBe(500);
      expect(e.data).toBe(null);
    }
  });

  it("throws HTTP status fallback when both text and JSON are empty on error", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 502,
      ok: false,
      text: vi.fn().mockResolvedValue(""),
    });
    const ctx = makeContext({ fetch: mockFetch });
    loadScript(ctx);

    try {
      await ctx.requestGitWorkspaceMutation("/test", {});
      expect.unreachable("should have thrown");
    } catch (e) {
      expect(e.message).toBe("HTTP 502");
      expect(e.status).toBe(502);
    }
  });
});

// ---------------------------------------------------------------------------
// renderWorkspaces
// ---------------------------------------------------------------------------
describe("renderWorkspaces", () => {
  it("returns early when #status-bar-branches element is missing", () => {
    const ctx = makeContext();
    loadScript(ctx);
    // Should not throw even without any target element.
    ctx.renderWorkspaces();
  });

  it("clears the status-bar branches when gitStatuses is empty", () => {
    const el = { innerHTML: "existing" };
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      activeWorkspaces: ["/home/user/project"],
      gitStatuses: null,
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(el.innerHTML).toBe("");
  });

  it("resets document.title to 'Wallfacer' when gitStatuses is empty but workspaces exist", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      activeWorkspaces: ["/home/user/myrepo"],
      gitStatuses: null,
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(ctx.document.title).toBe("Wallfacer");
  });

  it("renders a branch pill and updates document.title from gitStatuses", () => {
    const el = { innerHTML: "" };
    const updateStatusBar = vi.fn();
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      gitStatuses: [
        {
          name: "my-repo",
          path: "/home/user/my-repo",
          is_git_repo: true,
          has_remote: true,
          remote_url: "https://github.com/user/my-repo.git",
          branch: "main",
          main_branch: "main",
          ahead_count: 0,
          behind_count: 0,
          behind_main_count: 0,
        },
      ],
      updateStatusBar,
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(el.innerHTML).toContain("main");
    expect(el.innerHTML).toContain("status-bar-branch");
    expect(ctx.document.title).toContain("my-repo");
    expect(updateStatusBar).toHaveBeenCalled();
  });

  it("renders a push action and ahead badge when ahead_count > 0", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      gitStatuses: [
        {
          name: "repo",
          path: "/repo",
          is_git_repo: true,
          has_remote: true,
          remote_url: "https://github.com/user/repo",
          branch: "feat",
          main_branch: "main",
          ahead_count: 3,
          behind_count: 0,
          behind_main_count: 0,
        },
      ],
      updateStatusBar: vi.fn(),
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(el.innerHTML).toContain(">Push<");
    expect(el.innerHTML).toContain("3↑");
  });

  it("renders a sync action and behind badge when behind_count > 0", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      gitStatuses: [
        {
          name: "repo",
          path: "/repo",
          is_git_repo: true,
          has_remote: true,
          remote_url: "https://github.com/user/repo",
          branch: "main",
          main_branch: "main",
          ahead_count: 0,
          behind_count: 5,
          behind_main_count: 0,
        },
      ],
      updateStatusBar: vi.fn(),
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(el.innerHTML).toContain(">Sync<");
    expect(el.innerHTML).toContain("5↓");
  });

  it("renders rebase-on-main when branch differs from main_branch", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      gitStatuses: [
        {
          name: "repo",
          path: "/repo",
          is_git_repo: true,
          has_remote: true,
          remote_url: "https://github.com/user/repo",
          branch: "feature",
          main_branch: "main",
          ahead_count: 0,
          behind_count: 0,
          behind_main_count: 2,
        },
      ],
      updateStatusBar: vi.fn(),
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(el.innerHTML).toContain("Rebase on main");
    expect(el.innerHTML).toContain("2↓ Rebase");
  });

  it("does not render rebase when already on main branch", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      gitStatuses: [
        {
          name: "repo",
          path: "/repo",
          is_git_repo: true,
          has_remote: true,
          remote_url: "https://github.com/user/repo",
          branch: "main",
          main_branch: "main",
          ahead_count: 0,
          behind_count: 0,
          behind_main_count: 0,
        },
      ],
      updateStatusBar: vi.fn(),
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(el.innerHTML).not.toContain("Rebase on");
  });

  it("skips non-git workspaces (nothing to render in the branch slot)", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      gitStatuses: [
        {
          name: "plain-dir",
          path: "/plain-dir",
          is_git_repo: false,
          has_remote: false,
        },
      ],
      updateStatusBar: vi.fn(),
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(el.innerHTML).toBe("");
  });

  it("renders the branch pill without actions when there is no remote", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      gitStatuses: [
        {
          name: "local-repo",
          path: "/local-repo",
          is_git_repo: true,
          has_remote: false,
          branch: "main",
        },
      ],
      updateStatusBar: vi.fn(),
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(el.innerHTML).toContain("main");
    expect(el.innerHTML).not.toContain(">Push<");
    expect(el.innerHTML).not.toContain(">Sync<");
    expect(el.innerHTML).not.toContain("Rebase on");
  });

  it("renders multiple workspaces with name:branch labels", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["status-bar-branches", el]],
      gitStatuses: [
        {
          name: "repo-a",
          path: "/a",
          is_git_repo: true,
          has_remote: true,
          remote_url: "https://github.com/u/a",
          branch: "main",
          main_branch: "main",
          ahead_count: 0,
          behind_count: 0,
          behind_main_count: 0,
        },
        {
          name: "repo-b",
          path: "/b",
          is_git_repo: true,
          has_remote: false,
          branch: "feat",
        },
      ],
      updateStatusBar: vi.fn(),
    });
    loadScript(ctx);

    ctx.renderWorkspaces();
    expect(el.innerHTML).toContain("repo-a:main");
    expect(el.innerHTML).toContain("repo-b:feat");
    expect(ctx.document.title).toContain("repo-a");
    expect(ctx.document.title).toContain("repo-b");
  });
});

// ---------------------------------------------------------------------------
// openWorkspaceFolder
// ---------------------------------------------------------------------------
describe("openWorkspaceFolder", () => {
  it("calls api with correct route and payload", async () => {
    const mockApi = vi.fn().mockResolvedValue({});
    const ctx = makeContext({ api: mockApi });
    loadScript(ctx);

    await ctx.openWorkspaceFolder("/my/path");
    expect(mockApi).toHaveBeenCalledWith("/api/git/open-folder", {
      method: "POST",
      body: JSON.stringify({ path: "/my/path" }),
    });
  });

  it("shows alert on failure", async () => {
    const mockApi = vi.fn().mockRejectedValue(new Error("no xdg-open"));
    const mockAlert = vi.fn();
    const ctx = makeContext({ api: mockApi, showAlert: mockAlert });
    loadScript(ctx);

    await ctx.openWorkspaceFolder("/fail");
    expect(mockAlert).toHaveBeenCalledWith(
      expect.stringContaining("no xdg-open"),
    );
  });
});

// ---------------------------------------------------------------------------
// pushWorkspace
// ---------------------------------------------------------------------------
describe("pushWorkspace", () => {
  it("calls api to push workspace", async () => {
    const mockApi = vi.fn().mockResolvedValue({});
    const ctx = makeContext({
      api: mockApi,
      gitStatuses: [{ name: "repo", path: "/repo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Push",
      getAttribute: (_a) => "0",
    };
    await ctx.pushWorkspace(btn);
    expect(mockApi).toHaveBeenCalledWith("/api/git/push", {
      method: "POST",
      body: JSON.stringify({ workspace: "/repo" }),
    });
    expect(btn.disabled).toBe(true);
    expect(btn.textContent).toBe("...");
  });

  it("shows alert and restores button on push failure", async () => {
    const mockApi = vi.fn().mockRejectedValue(new Error("push rejected"));
    const mockAlert = vi.fn();
    const ctx = makeContext({
      api: mockApi,
      showAlert: mockAlert,
      gitStatuses: [{ name: "repo", path: "/repo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Push",
      getAttribute: (_a) => "0",
    };
    await ctx.pushWorkspace(btn);
    expect(mockAlert).toHaveBeenCalledWith(
      expect.stringContaining("push rejected"),
    );
    expect(btn.disabled).toBe(false);
    expect(btn.textContent).toBe("Push");
  });

  it("suggests sync for non-fast-forward errors", async () => {
    const mockApi = vi
      .fn()
      .mockRejectedValue(new Error("non-fast-forward update"));
    const mockAlert = vi.fn();
    const ctx = makeContext({
      api: mockApi,
      showAlert: mockAlert,
      gitStatuses: [{ name: "repo", path: "/repo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Push",
      getAttribute: (_a) => "0",
    };
    await ctx.pushWorkspace(btn);
    expect(mockAlert).toHaveBeenCalledWith(expect.stringContaining("Sync"));
  });

  it("returns early when workspace not found", async () => {
    const mockApi = vi.fn();
    const ctx = makeContext({
      api: mockApi,
      gitStatuses: [],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Push",
      getAttribute: (_a) => "5",
    };
    await ctx.pushWorkspace(btn);
    expect(mockApi).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// syncWorkspace
// ---------------------------------------------------------------------------
describe("syncWorkspace", () => {
  it("calls requestGitWorkspaceMutation with sync route", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 204,
      ok: true,
      text: vi.fn().mockResolvedValue(""),
    });
    const ctx = makeContext({
      fetch: mockFetch,
      gitStatuses: [{ name: "repo", path: "/repo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Sync",
      getAttribute: (_a) => "0",
    };
    await ctx.syncWorkspace(btn);
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/git/sync",
      expect.objectContaining({
        method: "POST",
      }),
    );
  });

  it("shows conflict alert for 409 with blocking_tasks", async () => {
    const errData = {
      error: "Workspace busy",
      blocking_tasks: [{ id: "abc", title: "Task", status: "in_progress" }],
    };
    const mockFetch = vi.fn().mockResolvedValue({
      status: 409,
      ok: false,
      text: vi.fn().mockResolvedValue(JSON.stringify(errData)),
    });
    const mockAlert = vi.fn();
    const ctx = makeContext({
      fetch: mockFetch,
      showAlert: mockAlert,
      gitStatuses: [{ name: "repo", path: "/repo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Sync",
      getAttribute: (_a) => "0",
    };
    await ctx.syncWorkspace(btn);
    expect(mockAlert).toHaveBeenCalledWith(
      expect.stringContaining("Sync blocked"),
    );
  });

  it("shows rebase conflict alert", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 500,
      ok: false,
      text: vi.fn().mockResolvedValue('{"error":"rebase conflict detected"}'),
    });
    const mockAlert = vi.fn();
    const ctx = makeContext({
      fetch: mockFetch,
      showAlert: mockAlert,
      gitStatuses: [{ name: "myrepo", path: "/myrepo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Sync",
      getAttribute: (_a) => "0",
    };
    await ctx.syncWorkspace(btn);
    expect(mockAlert).toHaveBeenCalledWith(
      expect.stringContaining("rebase conflict"),
    );
    expect(mockAlert).toHaveBeenCalledWith(expect.stringContaining("myrepo"));
  });

  it("shows generic error for other failures", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 500,
      ok: false,
      text: vi.fn().mockResolvedValue('{"error":"unknown failure"}'),
    });
    const mockAlert = vi.fn();
    const ctx = makeContext({
      fetch: mockFetch,
      showAlert: mockAlert,
      gitStatuses: [{ name: "repo", path: "/repo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Sync",
      getAttribute: (_a) => "0",
    };
    await ctx.syncWorkspace(btn);
    expect(mockAlert).toHaveBeenCalledWith(
      expect.stringContaining("Sync failed"),
    );
  });

  it("returns early when workspace not found", async () => {
    const mockFetch = vi.fn();
    const ctx = makeContext({
      fetch: mockFetch,
      gitStatuses: [],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Sync",
      getAttribute: (_a) => "99",
    };
    await ctx.syncWorkspace(btn);
    expect(mockFetch).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// rebaseOnMain
// ---------------------------------------------------------------------------
describe("rebaseOnMain", () => {
  it("calls requestGitWorkspaceMutation with rebase route", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 204,
      ok: true,
      text: vi.fn().mockResolvedValue(""),
    });
    const ctx = makeContext({
      fetch: mockFetch,
      gitStatuses: [{ name: "repo", path: "/repo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Rebase",
      getAttribute: (_a) => "0",
    };
    await ctx.rebaseOnMain(btn);
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/git/rebase-on-main",
      expect.objectContaining({
        method: "POST",
      }),
    );
  });

  it("shows conflict alert for 409 with blocking_tasks", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 409,
      ok: false,
      text: vi.fn().mockResolvedValue(
        JSON.stringify({
          error: "Blocked",
          blocking_tasks: [
            { id: "t1", title: "Running task", status: "in_progress" },
          ],
        }),
      ),
    });
    const mockAlert = vi.fn();
    const ctx = makeContext({
      fetch: mockFetch,
      showAlert: mockAlert,
      gitStatuses: [{ name: "repo", path: "/repo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Rebase",
      getAttribute: (_a) => "0",
    };
    await ctx.rebaseOnMain(btn);
    expect(mockAlert).toHaveBeenCalledWith(
      expect.stringContaining("Rebase blocked"),
    );
  });

  it("shows rebase conflict alert", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 500,
      ok: false,
      text: vi.fn().mockResolvedValue('{"error":"rebase conflict in file.go"}'),
    });
    const mockAlert = vi.fn();
    const ctx = makeContext({
      fetch: mockFetch,
      showAlert: mockAlert,
      gitStatuses: [{ name: "myrepo", path: "/myrepo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Rebase",
      getAttribute: (_a) => "0",
    };
    await ctx.rebaseOnMain(btn);
    expect(mockAlert).toHaveBeenCalledWith(expect.stringContaining("conflict"));
    expect(mockAlert).toHaveBeenCalledWith(expect.stringContaining("myrepo"));
    expect(mockAlert).toHaveBeenCalledWith(expect.stringContaining("Resolve"));
  });

  it("shows generic error for other failures", async () => {
    const mockFetch = vi.fn().mockResolvedValue({
      status: 500,
      ok: false,
      text: vi.fn().mockResolvedValue('{"error":"something else"}'),
    });
    const mockAlert = vi.fn();
    const ctx = makeContext({
      fetch: mockFetch,
      showAlert: mockAlert,
      gitStatuses: [{ name: "repo", path: "/repo" }],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Rebase",
      getAttribute: (_a) => "0",
    };
    await ctx.rebaseOnMain(btn);
    expect(mockAlert).toHaveBeenCalledWith(
      expect.stringContaining("Rebase failed"),
    );
  });

  it("returns early when workspace not found", async () => {
    const mockFetch = vi.fn();
    const ctx = makeContext({
      fetch: mockFetch,
      gitStatuses: [],
    });
    loadScript(ctx);

    const btn = {
      disabled: false,
      textContent: "Rebase",
      getAttribute: (_a) => "99",
    };
    await ctx.rebaseOnMain(btn);
    expect(mockFetch).not.toHaveBeenCalled();
  });
});

// ---------------------------------------------------------------------------
// startGitStream
// ---------------------------------------------------------------------------
describe("startGitStream", () => {
  it("renders workspaces and returns early when no active workspaces", () => {
    const el = { innerHTML: "" };
    const ctx = makeContext({
      elements: [["workspace-git-bar", el]],
      activeWorkspaces: [],
      gitStatuses: null,
    });
    loadScript(ctx);

    ctx.startGitStream();
    // gitStatusSource should remain null (no EventSource created)
    expect(ctx.gitStatusSource).toBe(null);
  });

  it("closes existing source before creating new one", () => {
    const closeFn = vi.fn();
    const mockES = function (_url) {
      this.onmessage = null;
      this.onerror = null;
      this.readyState = 1;
      this.close = vi.fn();
    };
    const ctx = makeContext({
      activeWorkspaces: ["/repo"],
      gitStatusSource: { close: closeFn },
      EventSource: mockES,
    });
    loadScript(ctx);

    ctx.startGitStream();
    expect(closeFn).toHaveBeenCalled();
  });

  it("fetches initial git status as follower tab", () => {
    const mockApi = vi.fn().mockResolvedValue([{ name: "repo" }]);
    const ctx = makeContext({
      activeWorkspaces: ["/repo"],
      _sseIsLeader: () => false,
      _sseOnFollowerEvent: vi.fn(),
      api: mockApi,
    });
    loadScript(ctx);

    ctx.startGitStream();
    expect(mockApi).toHaveBeenCalledWith("/api/git/status");
    expect(ctx.gitStatusSource).toBe(null);
  });

  it("creates EventSource as leader tab", () => {
    let createdUrl = null;
    const mockES = function (url) {
      createdUrl = url;
      this.onmessage = null;
      this.onerror = null;
      this.readyState = 1;
      this.close = vi.fn();
    };
    mockES.CLOSED = 2;
    const ctx = makeContext({
      activeWorkspaces: ["/repo"],
      _sseIsLeader: () => true,
      EventSource: mockES,
    });
    loadScript(ctx);

    ctx.startGitStream();
    expect(createdUrl).toBe("/api/git/stream");
    expect(ctx.gitStatusSource).not.toBe(null);
  });
});

// ---------------------------------------------------------------------------
// closeBranchDropdown
// ---------------------------------------------------------------------------
describe("closeBranchDropdown", () => {
  it("removes existing branch dropdown", () => {
    const removeFn = vi.fn();
    const ctx = makeContext({
      querySelector: (sel) => {
        if (sel === ".branch-dropdown") return { remove: removeFn };
        return null;
      },
    });
    loadScript(ctx);

    ctx.closeBranchDropdown();
    expect(removeFn).toHaveBeenCalled();
  });

  it("does nothing when no dropdown exists", () => {
    const ctx = makeContext();
    loadScript(ctx);
    // Should not throw
    ctx.closeBranchDropdown();
  });
});
