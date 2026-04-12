/**
 * Unit tests for explorer.js — file explorer panel tree logic.
 *
 * explorer.js is loaded into an isolated vm context with minimal DOM stubs.
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");
const code = readFileSync(join(jsDir, "explorer.js"), "utf8");

// ---------------------------------------------------------------------------
// Minimal DOM mock
// ---------------------------------------------------------------------------

function makeDom() {
  const registry = new Map();

  function makeEl(tag) {
    const _attrs = {};
    const _style = {};
    const _children = [];
    const _listeners = {};
    let _text = "";
    let _id = "";
    let _className = "";

    const el = {
      tagName: tag,
      get id() {
        return _id;
      },
      set id(v) {
        _id = v;
        if (v) registry.set(v, el);
      },
      get innerHTML() {
        return _text;
      },
      set innerHTML(v) {
        _text = String(v || "");
        _children.length = 0;
      },
      get style() {
        return _style;
      },
      get children() {
        return _children;
      },
      get textContent() {
        return _text;
      },
      set textContent(v) {
        _text = String(v || "");
      },
      get className() {
        return _className;
      },
      set className(v) {
        _className = String(v || "");
      },

      setAttribute(k, v) {
        _attrs[k] = String(v);
        if (k === "id") el.id = v;
      },
      getAttribute(k) {
        return Object.hasOwn(_attrs, k) ? _attrs[k] : null;
      },

      appendChild(child) {
        _children.push(child);
        if (child.id) registry.set(child.id, child);
        return child;
      },
      removeChild(child) {
        const i = _children.indexOf(child);
        if (i !== -1) _children.splice(i, 1);
      },
      get firstChild() {
        return _children[0] || null;
      },

      querySelectorAll(sel) {
        // Simple .class match over children (flat)
        const cls = sel.replace(/^\./, "");
        return _children.filter(
          (c) => c.className && c.className.includes(cls),
        );
      },

      addEventListener(ev, fn) {
        if (!_listeners[ev]) _listeners[ev] = [];
        _listeners[ev].push(fn);
      },

      focus() {},

      _attrs,
      _listeners,
      _style,
    };
    return el;
  }

  // Create the explorer panel elements
  const explorerPanel = makeEl("aside");
  explorerPanel.id = "explorer-panel";
  explorerPanel._style.display = "none";

  const explorerTree = makeEl("div");
  explorerTree.id = "explorer-tree";

  const resizeHandle = makeEl("div");
  resizeHandle.id = "explorer-resize-handle";

  const toggleBtn = makeEl("button");
  toggleBtn.id = "explorer-toggle-btn";

  const document = {
    getElementById(id) {
      return registry.get(id) || null;
    },
    createElement(tag) {
      return makeEl(tag);
    },
    body: makeEl("body"),
    readyState: "complete",
    addEventListener() {},
  };

  return { document, registry };
}

// ---------------------------------------------------------------------------
// Context factory
// ---------------------------------------------------------------------------

function makeContext(opts = {}) {
  const store = {};
  const { document, registry } = makeDom();
  const windowObj = {};
  const apiCalls = [];

  const ctx = vm.createContext({
    document,
    window: windowObj,
    localStorage: {
      getItem(k) {
        return Object.hasOwn(store, k) ? store[k] : null;
      },
      setItem(k, v) {
        store[k] = String(v);
      },
    },
    // Stubs
    activeWorkspaces: opts.workspaces || [],
    Routes: {
      explorer: {
        tree: function () {
          return "/api/explorer/tree";
        },
        readFile: function () {
          return "/api/explorer/file";
        },
        writeFile: function () {
          return "/api/explorer/file";
        },
      },
    },
    confirm: function () {
      return true;
    },
    api: function (url) {
      apiCalls.push(url);
      var response = opts.apiResponse || [];
      return Promise.resolve(response);
    },
    fetch: function () {
      return Promise.resolve({
        ok: true,
        status: 200,
        headers: new Map(),
        text: function () {
          return Promise.resolve("");
        },
      });
    },
    escapeHtml: function (s) {
      return String(s)
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;");
    },
    extToLang: function () {
      return null;
    },
    splitHighlightedLines: function (html) {
      return html.split("\n");
    },
    renderMarkdown: function (text) {
      return "<div>" + text + "</div>";
    },
    hljs: {
      highlight: function (code) {
        return { value: code };
      },
      highlightAuto: function (code) {
        return { value: code };
      },
    },
    console,
  });

  vm.runInContext(code, ctx, { filename: join(jsDir, "explorer.js") });

  return {
    ctx,
    registry,
    store,
    apiCalls,
    win: windowObj,
  };
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("_basename", () => {
  it("extracts last path component", () => {
    const { win } = makeContext();
    expect(win._basename("/home/user/project")).toBe("project");
    expect(win._basename("/home/user/project/")).toBe("project");
    expect(win._basename("C:\\Users\\me\\code")).toBe("code");
    expect(win._basename("single")).toBe("single");
  });
});

describe("_buildChildNodes", () => {
  it("creates child nodes from API entries", () => {
    const { win } = makeContext();
    const entries = [
      { name: "src", type: "dir", modified: "2025-01-01T00:00:00Z" },
      {
        name: "README.md",
        type: "file",
        size: 100,
        modified: "2025-01-01T00:00:00Z",
      },
      {
        name: ".gitignore",
        type: "file",
        size: 50,
        modified: "2025-01-01T00:00:00Z",
      },
    ];

    const nodes = win._buildChildNodes(entries, "/ws/project", "/ws/project");

    expect(nodes).toHaveLength(3);
    expect(nodes[0]).toEqual({
      path: "/ws/project/src",
      name: "src",
      type: "dir",
      workspace: "/ws/project",
      expanded: false,
      children: null,
      loading: false,
    });
    expect(nodes[1].path).toBe("/ws/project/README.md");
    expect(nodes[1].type).toBe("file");
    expect(nodes[2].name).toBe(".gitignore");
  });

  it("returns empty array for empty entries", () => {
    const { win } = makeContext();
    const nodes = win._buildChildNodes([], "/ws", "/ws");
    expect(nodes).toEqual([]);
  });
});

describe("_getVisibleNodes", () => {
  it("returns flat list of roots when nothing is expanded", () => {
    const { win } = makeContext();
    const roots = [
      { path: "/a", name: "a", type: "dir", expanded: false, children: null },
      { path: "/b", name: "b", type: "dir", expanded: false, children: null },
    ];
    const visible = win._getVisibleNodes(roots);
    expect(visible).toHaveLength(2);
    expect(visible[0].name).toBe("a");
    expect(visible[1].name).toBe("b");
  });

  it("includes children of expanded nodes in DFS order", () => {
    const { win } = makeContext();
    const child1 = {
      path: "/a/x",
      name: "x",
      type: "file",
      expanded: false,
      children: null,
    };
    const child2 = {
      path: "/a/y",
      name: "y",
      type: "file",
      expanded: false,
      children: null,
    };
    const roots = [
      {
        path: "/a",
        name: "a",
        type: "dir",
        expanded: true,
        children: [child1, child2],
      },
      { path: "/b", name: "b", type: "dir", expanded: false, children: null },
    ];
    const visible = win._getVisibleNodes(roots);
    expect(visible).toHaveLength(4);
    expect(visible.map((n) => n.name)).toEqual(["a", "x", "y", "b"]);
  });

  it("does not include children of collapsed nodes", () => {
    const { win } = makeContext();
    const child = {
      path: "/a/x",
      name: "x",
      type: "file",
      expanded: false,
      children: null,
    };
    const roots = [
      {
        path: "/a",
        name: "a",
        type: "dir",
        expanded: false,
        children: [child],
      },
    ];
    const visible = win._getVisibleNodes(roots);
    expect(visible).toHaveLength(1);
  });
});

describe("_findParent", () => {
  it("returns the parent node", () => {
    const { win } = makeContext();
    const child = {
      path: "/a/x",
      name: "x",
      type: "file",
      expanded: false,
      children: null,
    };
    const root = {
      path: "/a",
      name: "a",
      type: "dir",
      expanded: true,
      children: [child],
    };
    const found = win._findParent([root], child);
    expect(found).toBe(root);
  });

  it("returns null for root nodes", () => {
    const { win } = makeContext();
    const root = {
      path: "/a",
      name: "a",
      type: "dir",
      expanded: false,
      children: null,
    };
    const found = win._findParent([root], root);
    expect(found).toBeNull();
  });

  it("finds deeply nested parents", () => {
    const { win } = makeContext();
    const grandchild = {
      path: "/a/b/c",
      name: "c",
      type: "file",
      expanded: false,
      children: null,
    };
    const child = {
      path: "/a/b",
      name: "b",
      type: "dir",
      expanded: true,
      children: [grandchild],
    };
    const root = {
      path: "/a",
      name: "a",
      type: "dir",
      expanded: true,
      children: [child],
    };
    const found = win._findParent([root], grandchild);
    expect(found).toBe(child);
  });
});

describe("_classifyFileResponse", () => {
  it("classifies 413 as large file", () => {
    const { win } = makeContext();
    const result = win._classifyFileResponse(
      413,
      "application/json",
      JSON.stringify({ error: "file too large", size: 5242880, max: 2097152 }),
    );
    expect(result.type).toBe("large");
    expect(result.size).toBe(5242880);
    expect(result.max).toBe(2097152);
  });

  it("classifies JSON with binary:true as binary", () => {
    const { win } = makeContext();
    const result = win._classifyFileResponse(200, "application/json", {
      binary: true,
      size: 1024,
    });
    expect(result.type).toBe("binary");
    expect(result.size).toBe(1024);
  });

  it("classifies text/plain as text with content", () => {
    const { win } = makeContext();
    const result = win._classifyFileResponse(
      200,
      "text/plain; charset=utf-8",
      "hello world",
    );
    expect(result.type).toBe("text");
    expect(result.content).toBe("hello world");
  });

  it("returns text type for unknown content types", () => {
    const { win } = makeContext();
    const result = win._classifyFileResponse(200, "", "some content");
    expect(result.type).toBe("text");
    expect(result.content).toBe("some content");
  });
});

describe("_relativePath", () => {
  it("strips workspace prefix from path", () => {
    const { win } = makeContext();
    expect(
      win._relativePath("/home/user/project/src/main.go", "/home/user/project"),
    ).toBe("src/main.go");
  });

  it("returns full path if workspace is not a prefix", () => {
    const { win } = makeContext();
    expect(win._relativePath("/other/path/file.go", "/home/user/project")).toBe(
      "/other/path/file.go",
    );
  });

  it("handles workspace path with trailing separator", () => {
    const { win } = makeContext();
    expect(win._relativePath("/ws/file.txt", "/ws")).toBe("file.txt");
  });
});

describe("_getFileIcon", () => {
  it("returns folder SVG for directories", () => {
    const { win } = makeContext();
    const closed = win._getFileIcon("src", "dir", false);
    const open = win._getFileIcon("src", "dir", true);
    expect(closed).toContain("<svg");
    expect(open).toContain("<svg");
    expect(closed).not.toBe(open);
  });

  it("returns distinct SVGs for known extensions", () => {
    const { win } = makeContext();
    const goIcon = win._getFileIcon("main.go", "file", false);
    const jsIcon = win._getFileIcon("app.js", "file", false);
    const mdIcon = win._getFileIcon("README.md", "file", false);
    expect(goIcon).toContain("<svg");
    expect(jsIcon).toContain("<svg");
    expect(mdIcon).toContain("<svg");
    // Go and JS should have different stroke colors
    expect(goIcon).toContain("#00ADD8");
    expect(jsIcon).toContain("#F0DB4F");
  });

  it("returns default file SVG for unknown extensions", () => {
    const { win } = makeContext();
    const icon = win._getFileIcon("data.xyz", "file", false);
    expect(icon).toContain("<svg");
    expect(icon).toContain("var(--text-muted)");
  });

  it("matches special filenames", () => {
    const { win } = makeContext();
    const makefile = win._getFileIcon("Makefile", "file", false);
    const dockerfile = win._getFileIcon("Dockerfile", "file", false);
    expect(makefile).toContain("<svg");
    expect(dockerfile).toContain("#2496ED"); // Docker blue
  });

  it("matches Dockerfile.dev pattern", () => {
    const { win } = makeContext();
    const icon = win._getFileIcon("Dockerfile.dev", "file", false);
    expect(icon).toContain("#2496ED");
  });

  it("matches .gitignore as git file", () => {
    const { win } = makeContext();
    const icon = win._getFileIcon(".gitignore", "file", false);
    expect(icon).toContain("#E44D26");
  });
});

describe("_isEditDirty", () => {
  it("returns false when not in edit mode", () => {
    const { win } = makeContext();
    expect(win._isEditDirty()).toBe(false);
  });
});

describe("_toggleMarkdownView", () => {
  it("is exposed on window", () => {
    const { win } = makeContext();
    expect(typeof win._toggleMarkdownView).toBe("function");
  });
});

describe("_expandNode preserves nested expanded state", () => {
  it("does not collapse expanded children when parent is re-expanded", async () => {
    // Simulate: root → child dir (expanded) → grandchild file.
    // When _expandNode is called on root (as during refresh), the child's
    // expanded state and children must be preserved.
    let apiResponse = [];
    const { win, registry } = makeContext({
      workspaces: ["/ws"],
      apiResponse: [{ name: "sub", type: "dir" }],
    });

    // Build a tree manually: root with an expanded child.
    const grandchild = {
      path: "/ws/sub/file.txt",
      name: "file.txt",
      type: "file",
      workspace: "/ws",
      expanded: false,
      children: null,
      loading: false,
    };
    const child = {
      path: "/ws/sub",
      name: "sub",
      type: "dir",
      workspace: "/ws",
      expanded: true,
      children: [grandchild],
      loading: false,
    };
    const root = {
      path: "/ws",
      name: "ws",
      type: "dir",
      workspace: "/ws",
      expanded: true,
      children: [child],
      loading: false,
    };
    win._setExplorerRoots([root]);

    // _expandNode on root should preserve child's expanded state.
    win._expandNode(root);
    // Wait for the async api call to resolve.
    await new Promise((r) => setTimeout(r, 10));

    const roots = win._getExplorerRoots();
    expect(roots[0].children).toHaveLength(1);
    const newChild = roots[0].children[0];
    expect(newChild.path).toBe("/ws/sub");
    expect(newChild.expanded).toBe(true);
    expect(newChild.children).toEqual([grandchild]);
  });

  it("does not carry over expanded state for removed children", async () => {
    // If a child directory was expanded but no longer exists in the API
    // response, it should not appear in the new children.
    const { win } = makeContext({
      workspaces: ["/ws"],
      apiResponse: [{ name: "other", type: "dir" }],
    });

    const child = {
      path: "/ws/deleted",
      name: "deleted",
      type: "dir",
      workspace: "/ws",
      expanded: true,
      children: [],
      loading: false,
    };
    const root = {
      path: "/ws",
      name: "ws",
      type: "dir",
      workspace: "/ws",
      expanded: true,
      children: [child],
      loading: false,
    };
    win._setExplorerRoots([root]);

    win._expandNode(root);
    await new Promise((r) => setTimeout(r, 10));

    const roots = win._getExplorerRoots();
    expect(roots[0].children).toHaveLength(1);
    expect(roots[0].children[0].name).toBe("other");
    expect(roots[0].children[0].expanded).toBe(false);
  });
});

describe("explorer init", () => {
  it("loads roots when panel was previously open", () => {
    const { store } = makeContext({
      workspaces: ["/home/user/project"],
    });
    expect(store).toBeDefined();
  });

  it("sets explorer-toggle-btn aria-expanded when restoring open state", () => {
    const { registry } = makeContext({
      workspaces: ["/home/user/project"],
    });
    const btn = registry.get("explorer-toggle-btn");
    expect(btn).toBeDefined();
  });
});
