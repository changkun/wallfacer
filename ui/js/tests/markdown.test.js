/**
 * Tests for markdown.js helpers and clipboard/card markdown toggles.
 */
import { describe, it, expect, beforeAll, afterAll, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function createClassList() {
  const set = new Set();
  return {
    add: (cls) => {
      set.add(cls);
    },
    remove: (cls) => {
      set.delete(cls);
    },
    contains: (cls) => set.has(cls),
    containsAll: () => false,
    _set: set,
  };
}

function createVisibilityNode() {
  return {
    classList: createClassList(),
    style: {},
    textContent: "",
    innerHTML: "",
    _eventHandlers: {},
    className: "",
    addEventListener() {},
    appendChild() {},
    remove() {},
  };
}

function makeContext(extra = {}) {
  const nodes = new Map(extra.nodes || []);
  const copyWriteText = vi.fn().mockResolvedValue(undefined);
  const ctx = {
    console,
    Date,
    Math,
    setTimeout,
    clearTimeout,
    escapeHtml: (s) =>
      String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
    navigator: {
      clipboard: {
        writeText: copyWriteText,
      },
    },
    tasks: [],
    document: {
      getElementById: (id) => nodes.get(id) || null,
      querySelector: () => null,
      querySelectorAll: () => ({ forEach: () => {} }),
      documentElement: { setAttribute: () => {} },
      readyState: "complete",
      addEventListener: () => {},
    },
    ...extra,
  };
  if (!ctx._clipboardWriteText) ctx._clipboardWriteText = copyWriteText;
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  // Auto-load lib dependencies required by the target script.
  const libDeps = {
    "markdown.js": ["lib/clipboard.js", "lib/toggle.js"],
  };
  const deps = libDeps[filename];
  if (deps) {
    for (const dep of deps) {
      const depCode = readFileSync(join(jsDir, dep), "utf8");
      vm.runInContext(depCode, ctx, { filename: join(jsDir, dep) });
    }
  }
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

describe("renderMarkdown", () => {
  it("returns empty string for empty input", () => {
    const ctx = makeContext();
    loadScript(ctx, "markdown.js");
    expect(ctx.renderMarkdown("")).toBe("");
    expect(ctx.renderMarkdownInline("")).toBe("");
  });

  it("falls back to escaping when marked is unavailable", () => {
    const ctx = makeContext({ marked: undefined });
    loadScript(ctx, "markdown.js");
    const raw = "<b>hello</b>";
    expect(ctx.renderMarkdown(raw)).toBe("&lt;b&gt;hello&lt;/b&gt;");
    expect(ctx.renderMarkdownInline(raw)).toBe("&lt;b&gt;hello&lt;/b&gt;");
  });

  it("uses marked.parse and parseInline when available", () => {
    const parse = vi.fn().mockReturnValue("MARKED");
    const parseInline = vi.fn().mockReturnValue("INLINE");
    const ctx = makeContext({
      marked: {
        parse,
        parseInline,
        setOptions: function () {},
        Renderer: function () {
          this.code = function () {
            return "";
          };
        },
      },
    });
    loadScript(ctx, "markdown.js");
    const input = "# title";
    expect(ctx.renderMarkdown(input)).toBe("MARKED");
    expect(parse).toHaveBeenCalledWith(input);
    expect(ctx.renderMarkdownInline(input)).toBe("INLINE");
    expect(parseInline).toHaveBeenCalledWith(input);
  });
});

describe("toggleModalSection", () => {
  it("reveals rendered tab when raw is currently visible", () => {
    const rendered = createVisibilityNode();
    const raw = createVisibilityNode();
    raw.classList.add("hidden");
    const button = {
      textContent: "",
    };
    const ctx = makeContext({
      nodes: [
        ["modal-notes-rendered", rendered],
        ["modal-notes", raw],
        ["toggle-notes-btn", button],
      ],
    });
    loadScript(ctx, "markdown.js");

    ctx.toggleModalSection("notes");

    expect(rendered.classList.contains("hidden")).toBe(true);
    expect(raw.classList.contains("hidden")).toBe(false);
    expect(button.textContent).toBe("Preview");
  });

  it("reveals raw tab when rendered is currently visible", () => {
    const rendered = createVisibilityNode();
    const raw = createVisibilityNode();
    const button = {
      textContent: "",
    };
    const ctx = makeContext({
      nodes: [
        ["modal-notes-rendered", rendered],
        ["modal-notes", raw],
        ["toggle-notes-btn", button],
      ],
    });
    loadScript(ctx, "markdown.js");

    ctx.toggleModalSection("notes");

    expect(rendered.classList.contains("hidden")).toBe(false);
    expect(raw.classList.contains("hidden")).toBe(true);
    expect(button.textContent).toBe("Raw");
  });
});

describe("toggleCardMarkdown and copy helpers", () => {
  beforeAll(() => {
    vi.useFakeTimers();
  });
  afterAll(() => {
    vi.useRealTimers();
  });

  it("toggles card markdown and raw view state", () => {
    const renderedNode = createVisibilityNode();
    const rawNode = createVisibilityNode();
    const card = {
      dataset: { rawView: "false" },
      closest: () => card,
      querySelectorAll: (selector) => {
        if (selector === ".card-md-rendered") return [renderedNode];
        if (selector === ".card-md-raw") return [rawNode];
        return [];
      },
    };
    const button = {
      textContent: "",
      closest: () => card,
    };
    const ctx = makeContext({ tasks: [] });
    loadScript(ctx, "markdown.js");

    ctx.toggleCardMarkdown({ stopPropagation: () => {} }, button);
    expect(card.dataset.rawView).toBe("true");
    expect(renderedNode.classList.contains("hidden")).toBe(true);
    expect(rawNode.classList.contains("hidden")).toBe(false);
    expect(button.textContent).toBe("Preview");

    ctx.toggleCardMarkdown({ stopPropagation: () => {} }, button);
    expect(card.dataset.rawView).toBe("false");
    expect(renderedNode.classList.contains("hidden")).toBe(false);
    expect(rawNode.classList.contains("hidden")).toBe(true);
    expect(button.textContent).toBe("Raw");
  });

  it("copies modal text and restores button label after timeout", async () => {
    const modal = { textContent: "hello" };
    const button = {
      innerHTML: "<span>Copy</span>",
      textContent: "",
    };
    const ctx = makeContext({
      nodes: [
        ["modal-logs", modal],
        ["copy-logs-btn", button],
      ],
      tasks: [],
    });
    loadScript(ctx, "markdown.js");

    await ctx.copyModalText("logs");
    expect(ctx._clipboardWriteText).toHaveBeenCalledWith("hello");
    expect(button.textContent).toBe("Copied!");
    vi.advanceTimersByTime(1500);
    await Promise.resolve();
    expect(button.innerHTML).toBe("<span>Copy</span>");
  });

  it("copies task text and appends task.result when present", async () => {
    const copyButton = {
      innerHTML: "<span>Copy</span>",
      textContent: "",
    };
    const ctx = makeContext({
      tasks: [{ id: "t1", prompt: "run tests", result: "PASS" }],
      nodes: [],
    });
    loadScript(ctx, "markdown.js");

    await ctx.copyCardText(
      { stopPropagation: () => {}, currentTarget: copyButton },
      "t1",
    );
    expect(ctx._clipboardWriteText).toHaveBeenCalledWith("run tests\n\nPASS");
    expect(copyButton.textContent).toBe("✓");
    vi.advanceTimersByTime(1500);
    await Promise.resolve();
    expect(copyButton.innerHTML).toBe("<span>Copy</span>");
  });
});
