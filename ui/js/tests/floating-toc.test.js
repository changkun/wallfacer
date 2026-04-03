/**
 * Tests for floating-toc.js — buildFloatingToc and teardownFloatingToc.
 */
import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "lib", "floating-toc.js"), "utf8");

function makeEl(tag, overrides = {}) {
  const _classList = new Set();
  const _attrs = {};
  const _listeners = {};
  const _children = [];
  let _style = {};
  let _textContent = "";
  let _innerHTML = "";

  const el = {
    tagName: tag.toUpperCase(),
    get id() {
      return _attrs.id || "";
    },
    set id(v) {
      _attrs.id = v;
    },
    get textContent() {
      return _textContent;
    },
    set textContent(v) {
      _textContent = v;
    },
    get innerHTML() {
      return _innerHTML;
    },
    set innerHTML(v) {
      _innerHTML = v;
    },
    style: _style,
    className: "",
    classList: {
      add(c) {
        _classList.add(c);
      },
      remove(c) {
        _classList.delete(c);
      },
      toggle(c, force) {
        if (force) _classList.add(c);
        else _classList.delete(c);
      },
      contains(c) {
        return _classList.has(c);
      },
    },
    setAttribute(name, val) {
      _attrs[name] = val;
    },
    getAttribute(name) {
      return _attrs[name] ?? null;
    },
    addEventListener(type, fn, opts) {
      if (!_listeners[type]) _listeners[type] = [];
      _listeners[type].push(fn);
    },
    removeEventListener(type, fn) {
      if (_listeners[type]) {
        _listeners[type] = _listeners[type].filter((f) => f !== fn);
      }
    },
    appendChild(child) {
      _children.push(child);
    },
    remove() {
      el._removed = true;
    },
    querySelectorAll() {
      return [];
    },
    getBoundingClientRect() {
      return {
        top: 0,
        bottom: 100,
        left: 0,
        right: 500,
        width: 500,
        height: 100,
      };
    },
    scrollIntoView() {},
    get scrollTop() {
      return 0;
    },
    get clientWidth() {
      return 500;
    },
    get offsetWidth() {
      return 150;
    },
    _children,
    _listeners,
    _classList,
    _removed: false,
    ...overrides,
  };
  return el;
}

function makeHeading(tag, text, id) {
  const h = makeEl(tag);
  h.textContent = text;
  if (id) h.id = id;
  return h;
}

function makeContext() {
  const registry = new Map();

  const ctx = {
    window: null, // set below
    document: {
      getElementById(id) {
        return registry.get(id) || null;
      },
      createElement(tag) {
        const el = makeEl(tag);
        return el;
      },
      querySelectorAll() {
        return [];
      },
    },
    requestAnimationFrame: (fn) => {
      fn();
      return 1;
    },
    cancelAnimationFrame: () => {},
    setTimeout: (fn, ms) => {
      return 1;
    },
    clearTimeout: () => {},
    getComputedStyle: () => ({
      paddingTop: "0",
      paddingLeft: "0",
      paddingRight: "0",
      fontWeight: "400",
      fontSize: "16px",
      fontFamily: "sans-serif",
      lineHeight: "24px",
    }),
    parseInt,
    parseFloat,
    isNaN,
    Math,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    console,
    registry,
  };
  ctx.window = ctx;
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

describe("floating-toc", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
  });

  describe("teardownFloatingToc", () => {
    it("is callable with no prior build", () => {
      // Should not throw when nothing has been built.
      ctx.teardownFloatingToc();
    });

    it("removes the TOC element from the DOM", () => {
      const tocEl = makeEl("div");
      tocEl.id = "floating-toc";
      ctx.registry.set("floating-toc", tocEl);

      ctx.teardownFloatingToc();
      expect(tocEl._removed).toBe(true);
    });
  });

  describe("buildFloatingToc with < 2 headings", () => {
    it("returns early with zero headings", () => {
      const bodyEl = makeEl("div", {
        querySelectorAll: () => [],
      });
      const scrollEl = makeEl("div");
      const anchorEl = makeEl("div");

      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);
      // No TOC should be appended.
      expect(anchorEl._children.length).toBe(0);
    });

    it("returns early with exactly one heading", () => {
      const h1 = makeHeading("H1", "Title");
      const bodyEl = makeEl("div", {
        querySelectorAll: () => [h1],
      });
      const scrollEl = makeEl("div");
      const anchorEl = makeEl("div");

      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);
      expect(anchorEl._children.length).toBe(0);
    });
  });

  describe("buildFloatingToc with headings", () => {
    function makeBuildArgs(headingCount) {
      const headings = [];
      for (let i = 0; i < headingCount; i++) {
        headings.push(makeHeading("H2", `Section ${i + 1}`));
      }

      const bodyEl = makeEl("div", {
        querySelectorAll: (sel) => headings,
      });

      const scrollEl = makeEl("div", {
        getBoundingClientRect: () => ({
          top: 0,
          bottom: 600,
          left: 0,
          right: 800,
        }),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        scrollTop: 0,
      });

      // bodyEl (innerEl) needs querySelectorAll(":scope > *") for _prepare
      bodyEl.querySelectorAll = (sel) => {
        if (sel === ":scope > *") return headings;
        return headings;
      };

      const anchorEl = makeEl("div");

      return { headings, bodyEl, scrollEl, anchorEl };
    }

    it("creates TOC with correct number of links for 3 headings", () => {
      const { bodyEl, scrollEl, anchorEl } = makeBuildArgs(3);
      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);

      expect(anchorEl._children.length).toBe(1);
      const toc = anchorEl._children[0];
      expect(toc.id).toBe("floating-toc");
      expect(toc.className).toBe("spec-toc");

      // Children: title div + 3 link elements
      expect(toc._children.length).toBe(4);
      expect(toc._children[0].className).toBe("spec-toc__title");
      expect(toc._children[0].textContent).toBe("Contents");
    });

    it("auto-generates heading IDs from text", () => {
      const { headings, bodyEl, scrollEl, anchorEl } = makeBuildArgs(2);

      // Headings should not have IDs initially.
      expect(headings[0].id).toBe("");
      expect(headings[1].id).toBe("");

      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);

      // After build, headings should have auto-generated IDs.
      expect(headings[0].id).toContain("toc-heading-");
      expect(headings[1].id).toContain("toc-heading-");
    });

    it("preserves existing heading IDs", () => {
      const { headings, bodyEl, scrollEl, anchorEl } = makeBuildArgs(2);
      headings[0].id = "my-custom-id";

      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);
      expect(headings[0].id).toBe("my-custom-id");
    });

    it("assigns correct level classes to links", () => {
      const h1 = makeHeading("H1", "Title One");
      const h3 = makeHeading("H3", "Sub Section");
      const headings = [h1, h3];

      const bodyEl = makeEl("div", {
        querySelectorAll: () => headings,
      });
      const scrollEl = makeEl("div", {
        getBoundingClientRect: () => ({
          top: 0,
          bottom: 600,
          left: 0,
          right: 800,
        }),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        scrollTop: 0,
      });
      bodyEl.querySelectorAll = (sel) => {
        if (sel === ":scope > *") return headings;
        return headings;
      };
      const anchorEl = makeEl("div");

      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);

      const toc = anchorEl._children[0];
      // Children[0] = title, [1] = h1 link, [2] = h3 link
      expect(toc._children[1].className).toContain("spec-toc__link--h1");
      expect(toc._children[2].className).toContain("spec-toc__link--h3");
    });

    it("sets up scroll spy on scrollEl", () => {
      const { bodyEl, scrollEl, anchorEl } = makeBuildArgs(2);
      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);

      // scrollEl.addEventListener should be called for scroll spy.
      expect(scrollEl.addEventListener).toHaveBeenCalled();
      const scrollCalls = scrollEl.addEventListener.mock.calls.filter(
        (c) => c[0] === "scroll",
      );
      expect(scrollCalls.length).toBeGreaterThanOrEqual(1);
    });

    it("teardown after build cleans up", () => {
      const { bodyEl, scrollEl, anchorEl } = makeBuildArgs(2);
      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);

      // Register the TOC in the registry so teardown can find it.
      const toc = anchorEl._children[0];
      ctx.registry.set("floating-toc", toc);

      ctx.teardownFloatingToc();
      expect(toc._removed).toBe(true);
    });

    it("accepts custom headingSelector option", () => {
      const h4a = makeHeading("H4", "Deep A");
      const h4b = makeHeading("H4", "Deep B");
      const headings = [h4a, h4b];

      let selectorUsed = "";
      const bodyEl = makeEl("div", {
        querySelectorAll: (sel) => {
          selectorUsed = sel;
          if (sel === ":scope > *") return headings;
          return headings;
        },
      });
      const scrollEl = makeEl("div", {
        getBoundingClientRect: () => ({
          top: 0,
          bottom: 600,
          left: 0,
          right: 800,
        }),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        scrollTop: 0,
      });
      const anchorEl = makeEl("div");

      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl, {
        headingSelector: "h4",
      });

      // The first call should use the custom selector.
      expect(selectorUsed).not.toBe("");
      expect(anchorEl._children.length).toBe(1);
    });

    it("accepts custom idPrefix option", () => {
      const { headings, bodyEl, scrollEl, anchorEl } = makeBuildArgs(2);
      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl, {
        idPrefix: "custom-prefix",
      });

      expect(headings[0].id).toContain("custom-prefix-");
    });

    it("rebuilds TOC by tearing down previous one first", () => {
      const { bodyEl, scrollEl, anchorEl } = makeBuildArgs(2);
      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);

      const firstToc = anchorEl._children[0];
      ctx.registry.set("floating-toc", firstToc);

      // Build again — should teardown the old one.
      const { bodyEl: b2, scrollEl: s2, anchorEl: a2 } = makeBuildArgs(3);
      ctx.buildFloatingToc(b2, s2, a2);

      expect(firstToc._removed).toBe(true);
    });

    it("link click handler calls scrollIntoView and highlights", () => {
      const { headings, bodyEl, scrollEl, anchorEl } = makeBuildArgs(2);
      ctx.buildFloatingToc(bodyEl, scrollEl, anchorEl);

      const toc = anchorEl._children[0];
      // links are children[1] and children[2]
      const link0 = toc._children[1];
      const link1 = toc._children[2];

      // Register heading by ID so document.getElementById can find it.
      const headingId = headings[0].id;
      const scrollIntoViewSpy = vi.fn();
      const headingEl = makeEl("h2");
      headingEl.scrollIntoView = scrollIntoViewSpy;
      ctx.registry.set(headingId, headingEl);

      // Simulate click.
      const clickHandlers = link0._listeners["click"];
      expect(clickHandlers).toBeDefined();
      expect(clickHandlers.length).toBeGreaterThan(0);

      clickHandlers[0]({ preventDefault: () => {} });
      expect(scrollIntoViewSpy).toHaveBeenCalledWith({
        behavior: "smooth",
        block: "start",
      });
    });
  });
});
