/**
 * Tests for lib/markdown-render.js — unified markdown post-processor.
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeEl(tag = "div") {
  const children = [];
  const attrs = {};
  const el = {
    tagName: tag.toUpperCase(),
    innerHTML: "",
    textContent: "",
    className: "",
    style: {},
    title: "",
    dataset: {},
    children,
    childNodes: children,
    classList: {
      _classes: new Set(),
      add(c) {
        this._classes.add(c);
      },
      remove(c) {
        this._classes.delete(c);
      },
      contains(c) {
        return this._classes.has(c);
      },
      toggle(c, force) {
        if (force === undefined) {
          if (this._classes.has(c)) this._classes.delete(c);
          else this._classes.add(c);
        } else if (force) this._classes.add(c);
        else this._classes.delete(c);
      },
    },
    setAttribute(k, v) {
      attrs[k] = v;
    },
    getAttribute(k) {
      return attrs[k] ?? null;
    },
    removeAttribute(k) {
      delete attrs[k];
    },
    appendChild(child) {
      children.push(child);
    },
    replaceWith() {},
    remove() {},
    addEventListener() {},
    removeEventListener() {},
    querySelectorAll() {
      return [];
    },
    querySelector() {
      return null;
    },
    cloneNode() {
      return makeEl(tag);
    },
    getBoundingClientRect() {
      return { width: 0, height: 0, top: 0, left: 0 };
    },
    get firstElementChild() {
      return children[0] || null;
    },
  };
  return el;
}

function makeContext(extra = {}) {
  const ctx = vm.createContext({
    console,
    Date,
    Math,
    Promise,
    setTimeout: (fn) => fn(),
    clearTimeout() {},
    requestAnimationFrame: (fn) => fn(),
    getComputedStyle: () => ({
      getPropertyValue: () => "#000",
    }),
    document: {
      createElement: (tag) => makeEl(tag),
      head: { appendChild() {} },
      body: { appendChild() {} },
      documentElement: { setAttribute() {} },
      getElementById: () => null,
      querySelector: () => null,
      querySelectorAll: () => [],
      addEventListener() {},
      removeEventListener() {},
    },
    window: {
      addEventListener() {},
      removeEventListener() {},
    },
    escapeHtml: (s) =>
      String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
    ...extra,
  });
  const code = readFileSync(join(jsDir, "lib/markdown-render.js"), "utf8");
  vm.runInContext(code, ctx, {
    filename: join(jsDir, "lib/markdown-render.js"),
  });
  return ctx;
}

describe("_mdRender._resolvePath", () => {
  it("resolves a simple relative path", () => {
    const ctx = makeContext();
    expect(ctx._mdRender._resolvePath("guide/", "getting-started.md")).toBe(
      "guide/getting-started.md",
    );
  });

  it("resolves parent directory references", () => {
    const ctx = makeContext();
    expect(ctx._mdRender._resolvePath("guide/sub/", "../other.md")).toBe(
      "guide/other.md",
    );
  });

  it("resolves dot references", () => {
    const ctx = makeContext();
    expect(ctx._mdRender._resolvePath("docs/", "./readme.md")).toBe(
      "docs/readme.md",
    );
  });

  it("handles multiple parent traversals", () => {
    const ctx = makeContext();
    expect(ctx._mdRender._resolvePath("a/b/c/", "../../x.md")).toBe("a/x.md");
  });

  it("handles empty base", () => {
    const ctx = makeContext();
    expect(ctx._mdRender._resolvePath("", "file.md")).toBe("file.md");
  });
});

describe("enhanceMarkdown", () => {
  it("returns a promise", () => {
    const ctx = makeContext();
    const container = makeEl("div");
    const result = ctx._mdRender.enhanceMarkdown(container);
    expect(result).toBeDefined();
    expect(typeof result.then).toBe("function");
  });

  it("is a no-op for null container", async () => {
    const ctx = makeContext();
    // Should not throw.
    await ctx._mdRender.enhanceMarkdown(null);
  });

  it("skips mermaid when opts.mermaid is false", async () => {
    const ctx = makeContext();
    const container = makeEl("div");
    // Should resolve immediately without trying to load mermaid.
    await ctx._mdRender.enhanceMarkdown(container, { mermaid: false });
  });

  it("does not rewrite links by default", async () => {
    const ctx = makeContext();
    const linkEl = makeEl("a");
    linkEl.setAttribute("href", "foo.go");
    const container = makeEl("div");
    container.querySelectorAll = (sel) => {
      if (sel === ".mermaid-block") return [];
      if (sel === "pre code.language-mermaid, pre code.mermaid") return [];
      if (sel === "pre code") return [];
      if (sel === "a[href]") return [linkEl];
      return [];
    };
    // Mermaid disabled to avoid async CDN load.
    await ctx._mdRender.enhanceMarkdown(container, {
      mermaid: false,
      links: false,
    });
    // Link should remain unchanged.
    expect(linkEl.getAttribute("href")).toBe("foo.go");
  });

  it("rewrites file links to explorer when links enabled", async () => {
    const ctx = makeContext();
    const linkEl = makeEl("a");
    linkEl.setAttribute("href", "internal/runner/exec.go");
    linkEl.onclick = null;
    const container = makeEl("div");
    container.querySelectorAll = (sel) => {
      if (sel === "a[href]") return [linkEl];
      return [];
    };
    await ctx._mdRender.enhanceMarkdown(container, {
      mermaid: false,
      links: true,
      linkHandler: "explorer",
    });
    expect(linkEl.getAttribute("href")).toBe("#");
    expect(typeof linkEl.onclick).toBe("function");
  });

  it("rewrites .md links for docs handler", async () => {
    const ctx = makeContext();
    const linkEl = makeEl("a");
    linkEl.setAttribute("href", "getting-started.md");
    linkEl.onclick = null;
    const container = makeEl("div");
    container.querySelectorAll = (sel) => {
      if (sel === "a[href]") return [linkEl];
      return [];
    };
    await ctx._mdRender.enhanceMarkdown(container, {
      mermaid: false,
      links: true,
      linkHandler: "docs",
      basePath: "guide/usage",
    });
    expect(linkEl.getAttribute("href")).toBe("#");
    expect(linkEl.getAttribute("data-doc-slug")).toBe("guide/getting-started");
  });

  it("rewrites .md links for spec handler", async () => {
    const ctx = makeContext();
    const linkEl = makeEl("a");
    linkEl.setAttribute("href", "../shared/auth.md");
    linkEl.onclick = null;
    const container = makeEl("div");
    container.querySelectorAll = (sel) => {
      if (sel === "a[href]") return [linkEl];
      return [];
    };
    await ctx._mdRender.enhanceMarkdown(container, {
      mermaid: false,
      links: true,
      linkHandler: "spec",
      basePath: "specs/local/live-serve.md",
      workspace: "/home/user/project",
    });
    expect(linkEl.getAttribute("href")).toBe("#");
    expect(typeof linkEl.onclick).toBe("function");
  });

  it("skips http links", async () => {
    const ctx = makeContext();
    const linkEl = makeEl("a");
    linkEl.setAttribute("href", "https://example.com");
    const container = makeEl("div");
    container.querySelectorAll = (sel) => {
      if (sel === "a[href]") return [linkEl];
      return [];
    };
    await ctx._mdRender.enhanceMarkdown(container, {
      mermaid: false,
      links: true,
    });
    // Should not be rewritten.
    expect(linkEl.getAttribute("href")).toBe("https://example.com");
  });

  it("skips anchor-only links", async () => {
    const ctx = makeContext();
    const linkEl = makeEl("a");
    linkEl.setAttribute("href", "#section");
    const container = makeEl("div");
    container.querySelectorAll = (sel) => {
      if (sel === "a[href]") return [linkEl];
      return [];
    };
    await ctx._mdRender.enhanceMarkdown(container, {
      mermaid: false,
      links: true,
    });
    expect(linkEl.getAttribute("href")).toBe("#section");
  });

  it("accepts a custom link handler function", async () => {
    const handler = vi.fn();
    const ctx = makeContext();
    const linkEl = makeEl("a");
    linkEl.setAttribute("href", "path/to/file.go");
    linkEl.onclick = null;
    const container = makeEl("div");
    container.querySelectorAll = (sel) => {
      if (sel === "a[href]") return [linkEl];
      return [];
    };
    // Pass handler as a JS function via the context.
    ctx._testHandler = handler;
    vm.runInContext(
      `_testResult = _mdRender.enhanceMarkdown(
        { querySelectorAll: function(s) {
            if (s === "a[href]") return [_testLink];
            return [];
          }
        },
        { mermaid: false, links: true, linkHandler: _testHandler }
      )`,
      Object.assign(ctx, { _testLink: linkEl }),
    );
    expect(linkEl.getAttribute("href")).toBe("#");
    expect(typeof linkEl.onclick).toBe("function");
  });
});
