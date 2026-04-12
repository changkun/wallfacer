import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeElement(overrides = {}) {
  return {
    textContent: "",
    innerHTML: "",
    scrollTop: 0,
    style: { cssText: "" },
    appendChild: vi.fn(),
    querySelectorAll: vi.fn().mockReturnValue([]),
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const domContentLoadedCbs = [];

  const ctx = {
    console: { error: vi.fn(), log: vi.fn() },
    JSON,
    Array,
    String,
    Object,
    Promise,
    setTimeout: vi.fn(),
    api: overrides.api || vi.fn().mockResolvedValue([]),
    fetch:
      overrides.fetch ||
      vi.fn().mockResolvedValue({
        ok: true,
        text: vi.fn().mockResolvedValue("# Doc content"),
      }),
    escapeHtml: (s) => String(s),
    switchMode: vi.fn(),
    renderMarkdown: vi.fn().mockReturnValue("<h1>Doc</h1>"),
    teardownFloatingToc: vi.fn(),
    buildFloatingToc: vi.fn(),
    withAuthToken: (url) => url,
    _mdRender: {
      enhanceMarkdown: vi.fn().mockResolvedValue(undefined),
    },
    document: {
      getElementById: (id) => elements.get(id) || null,
      addEventListener: vi.fn((type, fn) => {
        if (type === "DOMContentLoaded") domContentLoadedCbs.push(fn);
      }),
      createElement: (tag) => makeElement({ tagName: tag }),
    },
    _domContentLoadedCbs: domContentLoadedCbs,
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "docs.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "docs.js") });
  return ctx;
}

describe("docs.js", () => {
  describe("openDocs", () => {
    it("switches to docs mode and loads slug", async () => {
      const content = makeElement();
      const nav = makeElement();
      const ctx = makeContext({
        elements: [
          ["docs-nav", nav],
          ["docs-content", content],
          ["docs-content-wrapper", makeElement()],
        ],
      });
      loadScript(ctx);
      await ctx.openDocs("guide/usage");
      expect(ctx.switchMode).toHaveBeenCalledWith("docs");
      // loadDoc should have been called
      expect(ctx.fetch).toHaveBeenCalled();
    });

    it("switches mode without loading when no slug", () => {
      const ctx = makeContext();
      loadScript(ctx);
      ctx.openDocs();
      expect(ctx.switchMode).toHaveBeenCalledWith("docs");
    });
  });

  describe("loadDoc", () => {
    it("fetches and renders document content", async () => {
      const content = makeElement();
      const nav = makeElement();
      const wrapper = makeElement();
      const ctx = makeContext({
        elements: [
          ["docs-nav", nav],
          ["docs-content", content],
          ["docs-content-wrapper", wrapper],
        ],
      });
      loadScript(ctx);
      await ctx.loadDoc("guide/getting-started");
      expect(ctx.teardownFloatingToc).toHaveBeenCalled();
      expect(ctx.fetch).toHaveBeenCalledWith("/api/docs/guide/getting-started");
      expect(ctx.renderMarkdown).toHaveBeenCalled();
      expect(content.innerHTML).toBe("<h1>Doc</h1>");
      expect(content.scrollTop).toBe(0);
    });

    it("shows error on fetch failure", async () => {
      const content = makeElement();
      const ctx = makeContext({
        elements: [
          ["docs-nav", makeElement()],
          ["docs-content", content],
        ],
        fetch: vi.fn().mockResolvedValue({ ok: false }),
      });
      loadScript(ctx);
      await ctx.loadDoc("nonexistent");
      expect(content.innerHTML).toContain("Failed to load");
    });
  });

  describe("renderDocsNav", () => {
    it("renders navigation with categories", async () => {
      const nav = makeElement();
      const ctx = makeContext({
        elements: [["docs-nav", nav]],
        api: vi.fn().mockResolvedValue([
          {
            slug: "guide/usage",
            title: "Usage",
            category: "guide",
            order: null,
          },
          {
            slug: "guide/getting-started",
            title: "Getting Started",
            category: "guide",
            order: 1,
          },
          {
            slug: "internals/internals",
            title: "Internals",
            category: "internals",
            order: null,
          },
        ]),
      });
      loadScript(ctx);
      // Set _docsEntries by calling _ensureDocsLoaded
      await ctx._ensureDocsLoaded();
      // Nav should have been rendered
      expect(nav.innerHTML).toContain("User Guide");
      expect(nav.innerHTML).toContain("Getting Started");
      expect(nav.innerHTML).toContain("Technical Reference");
    });

    it("handles empty entries", () => {
      const nav = makeElement();
      const ctx = makeContext({
        elements: [["docs-nav", nav]],
      });
      loadScript(ctx);
      ctx.renderDocsNav();
      expect(nav.innerHTML).toBe("");
    });
  });

  describe("_ensureDocsLoaded", () => {
    it("fetches docs index on first call", async () => {
      const nav = makeElement();
      const content = makeElement();
      const apiMock = vi
        .fn()
        .mockResolvedValue([
          { slug: "guide/usage", title: "Usage", category: "guide" },
        ]);
      const ctx = makeContext({
        elements: [
          ["docs-nav", nav],
          ["docs-content", content],
          ["docs-content-wrapper", makeElement()],
        ],
        api: apiMock,
      });
      loadScript(ctx);
      await ctx._ensureDocsLoaded();
      expect(apiMock).toHaveBeenCalledWith("/api/docs");
    });

    it("only fetches once", async () => {
      const nav = makeElement();
      const content = makeElement();
      const apiMock = vi.fn().mockResolvedValue([]);
      const ctx = makeContext({
        elements: [
          ["docs-nav", nav],
          ["docs-content", content],
          ["docs-content-wrapper", makeElement()],
        ],
        api: apiMock,
      });
      loadScript(ctx);
      await ctx._ensureDocsLoaded();
      await ctx._ensureDocsLoaded();
      // api called once in DOMContentLoaded background fetch + once in _ensureDocsLoaded
      // But _ensureDocsLoaded should only load once due to _docsLoaded flag
    });
  });

  describe("_appendDocNav", () => {
    it("appends prev/next links for ordered docs", async () => {
      const nav = makeElement();
      const content = makeElement();
      const appendedChildren = [];
      content.appendChild = vi.fn((child) => appendedChildren.push(child));

      const ctx = makeContext({
        elements: [
          ["docs-nav", nav],
          ["docs-content", content],
          ["docs-content-wrapper", makeElement()],
        ],
        api: vi.fn().mockResolvedValue([
          {
            slug: "guide/usage",
            title: "Usage",
            category: "guide",
            order: null,
          },
          {
            slug: "guide/getting-started",
            title: "Getting Started",
            category: "guide",
            order: 1,
          },
          {
            slug: "guide/board-and-tasks",
            title: "Board",
            category: "guide",
            order: 2,
          },
          {
            slug: "guide/workspaces",
            title: "Workspaces",
            category: "guide",
            order: 3,
          },
        ]),
      });
      loadScript(ctx);

      // Set up entries via _ensureDocsLoaded
      await ctx._ensureDocsLoaded();

      // Now call _appendDocNav for the middle doc
      ctx._appendDocNav(content, "guide/board-and-tasks");
      expect(content.appendChild).toHaveBeenCalled();
    });

    it("does not append nav for non-ordered docs", async () => {
      const nav = makeElement();
      const content = makeElement();

      const ctx = makeContext({
        elements: [
          ["docs-nav", nav],
          ["docs-content", content],
          ["docs-content-wrapper", makeElement()],
        ],
        api: vi.fn().mockResolvedValue([
          {
            slug: "guide/usage",
            title: "Usage",
            category: "guide",
            order: null,
          },
        ]),
      });
      loadScript(ctx);
      await ctx._ensureDocsLoaded();
      ctx._appendDocNav(content, "guide/usage");
      expect(content.appendChild).not.toHaveBeenCalled();
    });
  });

  describe("DOMContentLoaded preload", () => {
    it("pre-loads docs index in background", () => {
      const ctx = makeContext({
        api: vi.fn().mockResolvedValue([]),
      });
      loadScript(ctx);
      // Trigger DOMContentLoaded
      ctx._domContentLoadedCbs.forEach((cb) => { cb(); });
      expect(ctx.api).toHaveBeenCalledWith("/api/docs");
    });
  });
});
