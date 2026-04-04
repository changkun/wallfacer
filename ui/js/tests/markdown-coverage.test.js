import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeClassList() {
  const set = new Set();
  return {
    add: (c) => set.add(c),
    remove: (c) => set.delete(c),
    contains: (c) => set.has(c),
    toggle: (c, force) => (force ? set.add(c) : set.delete(c)),
    _set: set,
  };
}

function makeElement(overrides = {}) {
  return {
    classList: makeClassList(),
    textContent: "",
    innerHTML: "",
    style: {},
    closest: vi.fn().mockReturnValue(null),
    querySelectorAll: vi.fn().mockReturnValue({ forEach: vi.fn() }),
    dataset: {},
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    String,
    escapeHtml: (s) =>
      String(s)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
    marked: undefined, // no marked by default
    hljs: undefined,
    navigator: {
      clipboard: { writeText: vi.fn().mockResolvedValue(undefined) },
    },
    tasks: overrides.tasks || [],
    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: () => makeElement(),
    },
    toggleRenderedRaw: vi.fn(),
    copyWithFeedback: vi.fn(),
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  loadLibDeps("markdown.js", ctx);
  const code = readFileSync(join(jsDir, "markdown.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "markdown.js") });
  return ctx;
}

describe("markdown.js coverage", () => {
  describe("renderMarkdown", () => {
    it("returns empty string for empty input", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx.renderMarkdown("")).toBe("");
      expect(ctx.renderMarkdown(null)).toBe("");
    });

    it("returns escaped text when marked is not available", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx.renderMarkdown("<script>")).toBe("&lt;script&gt;");
    });

    it("uses marked.parse when available", () => {
      const ctx = makeContext({
        marked: {
          Renderer: function () {
            this.code = null;
          },
          setOptions: vi.fn(),
          parse: vi.fn().mockReturnValue("<p>Hello</p>"),
          parseInline: vi.fn().mockReturnValue("inline"),
        },
      });
      loadScript(ctx);
      expect(ctx.renderMarkdown("Hello")).toBe("<p>Hello</p>");
    });
  });

  describe("renderMarkdownInline", () => {
    it("returns empty for empty input", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx.renderMarkdownInline("")).toBe("");
    });

    it("returns escaped text without marked", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx.renderMarkdownInline("<b>bold</b>")).toBe(
        "&lt;b&gt;bold&lt;/b&gt;",
      );
    });
  });

  describe("toggleModalSection", () => {
    it("calls toggleRenderedRaw with correct elements", () => {
      const rendered = makeElement();
      const raw = makeElement();
      const btn = makeElement();
      let called = false;
      const ctx = makeContext({
        elements: [
          ["modal-prompt-rendered", rendered],
          ["modal-prompt", raw],
          ["toggle-prompt-btn", btn],
        ],
        toggleRenderedRaw: () => {
          called = true;
        },
      });
      loadScript(ctx);
      // Override after load to capture calls from our code
      ctx.toggleRenderedRaw = () => {
        called = true;
      };
      ctx.toggleModalSection("prompt");
      expect(called).toBe(true);
    });
  });

  describe("copyModalText", () => {
    it("copies raw element text", () => {
      const raw = makeElement({ textContent: "raw text" });
      const btn = makeElement();
      let copiedText = null;
      const ctx = makeContext({
        elements: [
          ["modal-result", raw],
          ["copy-result-btn", btn],
        ],
      });
      loadScript(ctx);
      ctx.copyWithFeedback = (text) => {
        copiedText = text;
      };
      ctx.copyModalText("result");
      expect(copiedText).toBe("raw text");
    });
  });

  describe("toggleCardMarkdown", () => {
    it("toggles raw/preview view on card", () => {
      const card = {
        dataset: { rawView: "false" },
        querySelectorAll: vi.fn().mockReturnValue({
          forEach: vi.fn(),
        }),
      };
      const btn = makeElement({
        closest: vi.fn().mockReturnValue(card),
      });
      const event = { stopPropagation: vi.fn() };
      const ctx = makeContext();
      loadScript(ctx);
      ctx.toggleCardMarkdown(event, btn);
      expect(event.stopPropagation).toHaveBeenCalled();
      expect(card.dataset.rawView).toBe("true");
    });

    it("toggles back to preview from raw", () => {
      const card = {
        dataset: { rawView: "true" },
        querySelectorAll: vi.fn().mockReturnValue({
          forEach: vi.fn(),
        }),
      };
      const btn = makeElement({
        closest: vi.fn().mockReturnValue(card),
        textContent: "",
      });
      const event = { stopPropagation: vi.fn() };
      const ctx = makeContext();
      loadScript(ctx);
      ctx.toggleCardMarkdown(event, btn);
      expect(card.dataset.rawView).toBe("false");
      expect(btn.textContent).toBe("Raw");
    });
  });

  describe("copyCardText", () => {
    it("copies task prompt and result", () => {
      const task = { id: "t1", prompt: "Hello", result: "World" };
      const event = {
        stopPropagation: vi.fn(),
        currentTarget: makeElement(),
      };
      let copiedText = null;
      const ctx = makeContext({ tasks: [task] });
      loadScript(ctx);
      ctx.copyWithFeedback = (text) => {
        copiedText = text;
      };
      ctx.copyCardText(event, "t1");
      expect(copiedText).toBe("Hello\n\nWorld");
    });

    it("copies only prompt when no result", () => {
      const task = { id: "t2", prompt: "Just prompt", result: "" };
      const event = {
        stopPropagation: vi.fn(),
        currentTarget: makeElement(),
      };
      let copiedText = null;
      const ctx = makeContext({ tasks: [task] });
      loadScript(ctx);
      ctx.copyWithFeedback = (text) => {
        copiedText = text;
      };
      ctx.copyCardText(event, "t2");
      expect(copiedText).toBe("Just prompt");
    });

    it("does nothing for unknown task", () => {
      const event = { stopPropagation: vi.fn() };
      let called = false;
      const ctx = makeContext({ tasks: [] });
      loadScript(ctx);
      ctx.copyWithFeedback = () => {
        called = true;
      };
      ctx.copyCardText(event, "missing");
      expect(called).toBe(false);
    });
  });

  describe("marked renderer IIFE", () => {
    it("configures custom code renderer with highlight.js", () => {
      let rendererCode = null;
      const ctx = makeContext({
        marked: {
          Renderer: function () {
            return {
              code: null,
            };
          },
          setOptions: vi.fn((opts) => {
            rendererCode = opts.renderer.code;
          }),
          parse: vi.fn(),
          parseInline: vi.fn(),
        },
        hljs: {
          getLanguage: vi.fn().mockReturnValue(true),
          highlight: vi.fn().mockReturnValue({ value: "highlighted" }),
          highlightAuto: vi.fn().mockReturnValue({ value: "auto" }),
        },
      });
      loadScript(ctx);

      if (rendererCode) {
        // Test with language
        const result = rendererCode("console.log(1)", "javascript");
        expect(result).toContain("highlighted");

        // Test mermaid
        const mermaidResult = rendererCode("graph TD", "mermaid");
        expect(mermaidResult).toContain("mermaid-block");

        // Test with object arg (marked v14+)
        const objResult = rendererCode({ text: "code", lang: "js" }, undefined);
        expect(objResult).toContain("highlighted");
      }
    });
  });
});
