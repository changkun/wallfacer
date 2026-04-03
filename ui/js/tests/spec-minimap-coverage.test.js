import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeClassList() {
  const set = new Set();
  return {
    add: (c) => set.add(c),
    remove: (c) => set.delete(c),
    contains: (c) => set.has(c),
    _set: set,
  };
}

function makeElement(overrides = {}) {
  return {
    classList: makeClassList(),
    innerHTML: "",
    textContent: "",
    style: {},
    setAttribute: vi.fn(),
    getAttribute: vi.fn(),
    appendChild: vi.fn(),
    addEventListener: vi.fn(),
    querySelectorAll: vi.fn().mockReturnValue([]),
    querySelector: vi.fn().mockReturnValue(null),
    getBoundingClientRect: vi.fn().mockReturnValue({ width: 200, height: 100 }),
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Math,
    String,
    Array,
    parseInt,
    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: () => makeElement(),
      createElementNS: () => makeElement(),
      addEventListener: vi.fn(),
    },
    escapeHtml: (s) => String(s),
    focusSpec: vi.fn(),
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "spec-minimap.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "spec-minimap.js") });
  return ctx;
}

describe("spec-minimap.js", () => {
  describe("_normalizeDep", () => {
    it("strips specs/ prefix", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._normalizeDep("specs/local/foo.md")).toBe("local/foo.md");
    });

    it("returns path unchanged without specs/ prefix", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._normalizeDep("local/foo.md")).toBe("local/foo.md");
    });

    it("handles exact specs/ path", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._normalizeDep("specs/")).toBe("");
    });
  });

  describe("buildReverseDeps", () => {
    it("builds reverse index from nodes", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const nodes = [
        { path: "local/a.md", spec: { depends_on: ["specs/shared/b.md"] } },
        {
          path: "local/c.md",
          spec: { depends_on: ["specs/shared/b.md", "specs/local/a.md"] },
        },
      ];
      const result = ctx.buildReverseDeps(nodes);
      expect(result["shared/b.md"]).toEqual(["local/a.md", "local/c.md"]);
      expect(result["local/a.md"]).toEqual(["local/c.md"]);
    });

    it("returns empty object for nodes without dependencies", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const nodes = [
        { path: "a.md", spec: {} },
        { path: "b.md", spec: { depends_on: null } },
      ];
      const result = ctx.buildReverseDeps(nodes);
      expect(result).toEqual({});
    });

    it("handles empty nodes array", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx.buildReverseDeps([])).toEqual({});
    });
  });

  describe("_hideMinimap / _showMinimap", () => {
    it("adds hidden class to minimap and handle", () => {
      const container = makeElement();
      const handle = makeElement();
      const ctx = makeContext({
        elements: [
          ["spec-minimap", container],
          ["spec-minimap-resize", handle],
        ],
      });
      loadScript(ctx);
      ctx._hideMinimap();
      expect(container.classList._set.has("hidden")).toBe(true);
      expect(handle.classList._set.has("hidden")).toBe(true);
    });

    it("removes hidden class from minimap and handle", () => {
      const container = makeElement();
      container.classList.add("hidden");
      const handle = makeElement();
      handle.classList.add("hidden");
      const ctx = makeContext({
        elements: [
          ["spec-minimap", container],
          ["spec-minimap-resize", handle],
        ],
      });
      loadScript(ctx);
      ctx._showMinimap();
      expect(container.classList._set.has("hidden")).toBe(false);
      expect(handle.classList._set.has("hidden")).toBe(false);
    });

    it("handles missing elements gracefully", () => {
      const ctx = makeContext({ elements: [] });
      loadScript(ctx);
      ctx._hideMinimap(); // should not throw
      ctx._showMinimap();
    });
  });

  describe("renderMinimap", () => {
    it("hides minimap when treeData is null", () => {
      const container = makeElement();
      const svg = makeElement();
      const handle = makeElement();
      const ctx = makeContext({
        elements: [
          ["spec-minimap", container],
          ["spec-minimap-svg", svg],
          ["spec-minimap-resize", handle],
        ],
      });
      loadScript(ctx);
      ctx.renderMinimap("some/path", null);
      expect(container.classList._set.has("hidden")).toBe(true);
    });

    it("hides minimap when specPath is empty", () => {
      const container = makeElement();
      const svg = makeElement();
      const handle = makeElement();
      const ctx = makeContext({
        elements: [
          ["spec-minimap", container],
          ["spec-minimap-svg", svg],
          ["spec-minimap-resize", handle],
        ],
      });
      loadScript(ctx);
      ctx.renderMinimap("", { nodes: [] });
      expect(container.classList._set.has("hidden")).toBe(true);
    });

    it("hides minimap when focused node not found", () => {
      const container = makeElement();
      const svg = makeElement();
      const handle = makeElement();
      const ctx = makeContext({
        elements: [
          ["spec-minimap", container],
          ["spec-minimap-svg", svg],
          ["spec-minimap-resize", handle],
        ],
      });
      loadScript(ctx);
      ctx.renderMinimap("missing/path", {
        nodes: [{ path: "other/path", spec: {} }],
      });
      expect(container.classList._set.has("hidden")).toBe(true);
    });

    it("returns early when container is missing", () => {
      const ctx = makeContext({ elements: [] });
      loadScript(ctx);
      ctx.renderMinimap("path", { nodes: [] }); // should not throw
    });

    it("renders minimap for node with dependencies", () => {
      const container = makeElement();
      const svg = makeElement();
      svg.innerHTML = "";
      const handle = makeElement();
      const ctx = makeContext({
        elements: [
          ["spec-minimap", container],
          ["spec-minimap-svg", svg],
          ["spec-minimap-resize", handle],
        ],
      });
      loadScript(ctx);
      const treeData = {
        nodes: [
          {
            path: "local/a.md",
            spec: {
              title: "Spec A",
              status: "drafted",
              depends_on: ["specs/shared/b.md"],
            },
          },
          {
            path: "shared/b.md",
            spec: { title: "Spec B", status: "complete", depends_on: [] },
          },
        ],
      };
      ctx.renderMinimap("local/a.md", treeData);
      // Should have shown minimap (hidden class removed)
      expect(container.classList._set.has("hidden")).toBe(false);
    });

    it("renders minimap for node with reverse dependencies", () => {
      const container = makeElement();
      const svg = makeElement();
      svg.innerHTML = "";
      const handle = makeElement();
      const ctx = makeContext({
        elements: [
          ["spec-minimap", container],
          ["spec-minimap-svg", svg],
          ["spec-minimap-resize", handle],
        ],
      });
      loadScript(ctx);
      const treeData = {
        nodes: [
          {
            path: "shared/base.md",
            spec: { title: "Base", status: "complete", depends_on: [] },
          },
          {
            path: "local/child.md",
            spec: {
              title: "Child",
              status: "drafted",
              depends_on: ["specs/shared/base.md"],
            },
          },
        ],
      };
      ctx.renderMinimap("shared/base.md", treeData);
      expect(container.classList._set.has("hidden")).toBe(false);
    });
  });

  describe("_minimapStatusColors", () => {
    it("has expected status color mappings", () => {
      const ctx = makeContext();
      loadScript(ctx);
      expect(ctx._minimapStatusColors.complete).toBe("#d4edda");
      expect(ctx._minimapStatusColors.validated).toBe("#cce5ff");
      expect(ctx._minimapStatusColors.drafted).toBe("#fff3cd");
      expect(ctx._minimapStatusColors.vague).toBe("#e2e3e5");
      expect(ctx._minimapStatusColors.stale).toBe("#f8d7da");
    });
  });
});
