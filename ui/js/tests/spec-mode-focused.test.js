/**
 * Unit tests for focused markdown view in spec-mode.js.
 */
import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "spec-mode.js"), "utf8");

function makeContext() {
  const registry = new Map();
  const storage = new Map();

  function makeEl(tag, id) {
    const _classList = new Set();
    const _style = {};
    const el = {
      tagName: tag,
      style: _style,
      textContent: "",
      innerHTML: "",
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
    };
    if (id) registry.set(id, el);
    return el;
  }

  const ids = [
    "mode-tab-board",
    "mode-tab-spec",
    "board",
    "spec-mode-container",
    "spec-focused-title",
    "spec-focused-status",
    "spec-focused-body",
    "spec-dispatch-btn",
    "spec-summarize-btn",
  ];
  for (const id of ids) makeEl("DIV", id);
  registry.get("mode-tab-board").classList.add("active");
  registry.get("spec-mode-container").style.display = "none";

  const ctx = {
    document: {
      getElementById(id) {
        return registry.get(id) || null;
      },
      addEventListener() {},
    },
    localStorage: {
      getItem(k) {
        return storage.get(k) ?? null;
      },
      setItem(k, v) {
        storage.set(k, v);
      },
    },
    // Stub fetch — not needed for frontmatter parsing tests.
    fetch: () => Promise.reject(new Error("stubbed")),
    // Stub globals that spec-mode.js references.
    Routes: { explorer: { readFile: () => "/api/explorer/file" } },
    withBearerHeaders: () => ({}),
    renderMarkdown: (text) => "<p>" + text + "</p>",
    setInterval: () => 42,
    clearInterval: () => {},
    console,
    registry,
  };
  vm.createContext(ctx);
  vm.runInContext(code, ctx);
  return ctx;
}

describe("parseSpecFrontmatter", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
  });

  it("parses valid frontmatter", () => {
    const text =
      "---\ntitle: My Spec\nstatus: validated\neffort: small\n---\n# Body\n\nContent here.";
    const result = ctx.parseSpecFrontmatter(text);
    expect(result.frontmatter.title).toBe("My Spec");
    expect(result.frontmatter.status).toBe("validated");
    expect(result.frontmatter.effort).toBe("small");
    expect(result.body).toBe("# Body\n\nContent here.");
  });

  it("returns full text as body when no frontmatter", () => {
    const text = "# Just markdown\n\nNo frontmatter here.";
    const result = ctx.parseSpecFrontmatter(text);
    expect(result.frontmatter).toEqual({});
    expect(result.body).toBe(text);
  });

  it("handles empty input", () => {
    const result = ctx.parseSpecFrontmatter("");
    expect(result.frontmatter).toEqual({});
    expect(result.body).toBe("");
  });

  it("handles null input", () => {
    const result = ctx.parseSpecFrontmatter(null);
    expect(result.frontmatter).toEqual({});
    expect(result.body).toBe("");
  });

  it("skips list values in frontmatter", () => {
    const text =
      "---\ntitle: Test\ndepends_on:\n  - specs/foo.md\nstatus: drafted\n---\nBody";
    const result = ctx.parseSpecFrontmatter(text);
    expect(result.frontmatter.title).toBe("Test");
    expect(result.frontmatter.status).toBe("drafted");
    // depends_on is a list, should be skipped (starts with -)
    expect(result.frontmatter.depends_on).toBeUndefined();
  });
});

describe("focusSpec state", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
  });

  it("getFocusedSpecPath returns null initially", () => {
    expect(ctx.getFocusedSpecPath()).toBeNull();
  });

  it("focusSpec sets the focused path", () => {
    // focusSpec triggers fetch which is stubbed to reject, but state is set.
    ctx.focusSpec("specs/local/foo.md", "/workspace");
    expect(ctx.getFocusedSpecPath()).toBe("specs/local/foo.md");
  });
});

describe("spec refresh polling", () => {
  let ctx;

  beforeEach(() => {
    ctx = makeContext();
  });

  it("stops polling when switching to board mode", () => {
    let cleared = false;
    ctx.clearInterval = () => {
      cleared = true;
    };
    ctx.setInterval = () => 99;

    ctx.focusSpec("specs/foo.md", "/ws");
    // switchMode to spec first (since we start in board mode)
    ctx.switchMode("spec");
    ctx.switchMode("board");
    expect(cleared).toBe(true);
  });
});
