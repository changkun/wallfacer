import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeElement(overrides = {}) {
  return {
    value: "",
    textContent: "",
    innerHTML: "",
    style: { display: "" },
    parentElement: { appendChild: vi.fn() },
    addEventListener: vi.fn(),
    blur: vi.fn(),
    focus: vi.fn(),
    querySelectorAll: vi.fn().mockReturnValue({ forEach: vi.fn() }),
    dataset: {},
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const docListeners = {};
  const ctx = {
    console,
    Array,
    String,
    Boolean,
    RegExp,
    JSON,
    parseInt,
    clearTimeout: vi.fn(),
    setTimeout: vi.fn((fn) => {
      fn();
      return 1;
    }),
    encodeURIComponent,
    filterQuery: "",
    escapeHtml: (s) => String(s).replace(/</g, "&lt;").replace(/>/g, "&gt;"),
    render: vi.fn(),
    openModal: vi.fn(),
    apiGet: overrides.apiGet || vi.fn().mockResolvedValue([]),
    getCurrentMode: vi.fn().mockReturnValue("board"),
    setSpecTextFilter: vi.fn(),
    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: (tag) => makeElement({ tagName: tag }),
      addEventListener: vi.fn((type, fn) => {
        if (!docListeners[type]) docListeners[type] = [];
        docListeners[type].push(fn);
      }),
      querySelector: () => null,
      activeElement: { tagName: "BODY" },
      readyState: "complete",
    },
    _docListeners: docListeners,
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "search.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "search.js") });
  return ctx;
}

describe("search.js", () => {
  describe("matchesFilter", () => {
    it("returns true when no filter query", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "";
      expect(ctx.matchesFilter({ title: "test", prompt: "hello" })).toBe(true);
    });

    it("matches by title", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "bug";
      expect(ctx.matchesFilter({ title: "Fix bug", prompt: "" })).toBe(true);
    });

    it("matches by prompt", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "refactor";
      expect(
        ctx.matchesFilter({ title: "", prompt: "Refactor the module" }),
      ).toBe(true);
    });

    it("is case insensitive", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "BUG";
      expect(ctx.matchesFilter({ title: "fix bug", prompt: "" })).toBe(true);
    });

    it("returns false when no match", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "xyz";
      expect(ctx.matchesFilter({ title: "hello", prompt: "world" })).toBe(
        false,
      );
    });

    it("supports multi-word queries (all tokens must match)", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "fix bug";
      expect(ctx.matchesFilter({ title: "Fix the bug", prompt: "" })).toBe(
        true,
      );
      expect(ctx.matchesFilter({ title: "Fix", prompt: "" })).toBe(false);
    });

    it("supports tag filtering with #", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "#urgent";
      expect(
        ctx.matchesFilter({ title: "test", prompt: "", tags: ["urgent"] }),
      ).toBe(true);
      expect(
        ctx.matchesFilter({ title: "test", prompt: "", tags: ["low"] }),
      ).toBe(false);
    });

    it("supports combined tag and text filter", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "#urgent fix";
      expect(
        ctx.matchesFilter({ title: "Fix bug", prompt: "", tags: ["urgent"] }),
      ).toBe(true);
      expect(
        ctx.matchesFilter({ title: "Fix bug", prompt: "", tags: ["low"] }),
      ).toBe(false);
    });

    it("handles missing tags gracefully", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "#tag";
      expect(ctx.matchesFilter({ title: "test", prompt: "" })).toBe(false);
    });

    it("matches tags in tag text", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.filterQuery = "urgent";
      expect(
        ctx.matchesFilter({ title: "", prompt: "", tags: ["urgent"] }),
      ).toBe(true);
    });
  });

  describe("highlightMatch", () => {
    it("returns escaped text when no query", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      expect(ctx.highlightMatch("hello <world>", "")).toBe(
        "hello &lt;world&gt;",
      );
    });

    it("returns escaped text when no match", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      expect(ctx.highlightMatch("hello", "xyz")).toBe("hello");
    });

    it("wraps match in mark tag", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      const result = ctx.highlightMatch("Fix bug here", "bug");
      expect(result).toContain('<mark class="search-highlight">bug</mark>');
      expect(result).toBe('Fix <mark class="search-highlight">bug</mark> here');
    });

    it("is case insensitive", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      const result = ctx.highlightMatch("Fix BUG here", "bug");
      expect(result).toContain('<mark class="search-highlight">BUG</mark>');
    });

    it("handles null text", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      expect(ctx.highlightMatch(null, "q")).toBe("null");
    });
  });

  describe("renderSearchPanel", () => {
    it("shows no-results message for empty results", () => {
      const panel = makeElement();
      const ctx = makeContext({
        elements: [
          ["task-search", makeElement()],
          ["search-results-panel", panel],
        ],
      });
      loadScript(ctx);
      ctx.renderSearchPanel([], "test");
      expect(panel.innerHTML).toContain("No results");
      expect(panel.innerHTML).toContain("test");
      expect(panel.style.display).toBe("block");
    });

    it("shows no-results for null results", () => {
      const panel = makeElement();
      const ctx = makeContext({
        elements: [
          ["task-search", makeElement()],
          ["search-results-panel", panel],
        ],
      });
      loadScript(ctx);
      ctx.renderSearchPanel(null, "query");
      expect(panel.innerHTML).toContain("No results");
    });

    it("renders result items with badges and snippets", () => {
      const items = [];
      const panel = makeElement({
        querySelectorAll: vi.fn().mockReturnValue({ forEach: vi.fn() }),
      });
      const ctx = makeContext({
        elements: [
          ["task-search", makeElement()],
          ["search-results-panel", panel],
        ],
      });
      loadScript(ctx);
      ctx.renderSearchPanel(
        [
          {
            id: "task-1",
            title: "Fix bug",
            matched_field: "title",
            snippet: "...fix <mark>bug</mark>...",
          },
        ],
        "bug",
      );
      expect(panel.innerHTML).toContain("search-result-item");
      expect(panel.innerHTML).toContain("Fix bug");
      expect(panel.innerHTML).toContain("search-field-badge--title");
      expect(panel.style.display).toBe("block");
    });

    it("does nothing when panel is missing", () => {
      const ctx = makeContext({
        elements: [["task-search", makeElement()]],
      });
      loadScript(ctx);
      ctx.renderSearchPanel(
        [{ id: "1", title: "t", matched_field: "title", snippet: "" }],
        "q",
      );
      // Should not throw
    });
  });

  describe("hideSearchPanel", () => {
    it("hides the panel", () => {
      const panel = makeElement();
      panel.style.display = "block";
      const ctx = makeContext({
        elements: [
          ["task-search", makeElement()],
          ["search-results-panel", panel],
        ],
      });
      loadScript(ctx);
      ctx.hideSearchPanel();
      expect(panel.style.display).toBe("none");
    });
  });

  describe("triggerServerSearch", () => {
    it("calls apiGet with query", async () => {
      const panel = makeElement({
        querySelectorAll: vi.fn().mockReturnValue({ forEach: vi.fn() }),
      });
      const apiGetMock = vi.fn().mockResolvedValue([]);
      const ctx = makeContext({
        elements: [
          ["task-search", makeElement()],
          ["search-results-panel", panel],
        ],
        apiGet: apiGetMock,
      });
      loadScript(ctx);
      ctx.triggerServerSearch("@test query");
      expect(apiGetMock).toHaveBeenCalledWith(
        "/api/tasks/search?q=test%20query",
      );
    });

    it("hides panel for short queries", () => {
      const panel = makeElement();
      panel.style.display = "block";
      const ctx = makeContext({
        elements: [
          ["task-search", makeElement()],
          ["search-results-panel", panel],
        ],
      });
      loadScript(ctx);
      ctx.triggerServerSearch("@a");
      expect(panel.style.display).toBe("none");
    });
  });

  describe("keyboard shortcut", () => {
    it("registers / shortcut to focus search", () => {
      const input = makeElement();
      const ctx = makeContext({
        elements: [["task-search", input]],
      });
      loadScript(ctx);
      // Find the keydown listener
      const keydownCalls = ctx.document.addEventListener.mock.calls.filter(
        (c) => c[0] === "keydown",
      );
      expect(keydownCalls.length).toBeGreaterThan(0);
      const handler = keydownCalls[keydownCalls.length - 1][1];
      handler({ key: "/", preventDefault: vi.fn() });
      expect(input.focus).toHaveBeenCalled();
    });

    it("ignores / when in input field", () => {
      const input = makeElement();
      const ctx = makeContext({
        elements: [["task-search", input]],
      });
      ctx.document.activeElement = { tagName: "INPUT" };
      loadScript(ctx);
      const keydownCalls = ctx.document.addEventListener.mock.calls.filter(
        (c) => c[0] === "keydown",
      );
      const handler = keydownCalls[keydownCalls.length - 1][1];
      handler({ key: "/", preventDefault: vi.fn() });
      expect(input.focus).not.toHaveBeenCalled();
    });
  });
});
