import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// Minimal DOM node polyfill good enough to exercise agents.js row and
// panel construction. The real DOM is not involved — tests verify the
// tree structure the module builds.
function makeElement(tag) {
  const el = {
    tagName: tag.toUpperCase(),
    children: [],
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
      toggle(c, on) {
        if (on === undefined ? !this._classes.has(c) : on) this._classes.add(c);
        else this._classes.delete(c);
      },
    },
    attributes: {},
    listeners: {},
    style: {},
    textContent: "",
    innerHTML: "",
    hidden: false,
    disabled: false,
    type: "",
    setAttribute(k, v) {
      this.attributes[k] = v;
    },
    getAttribute(k) {
      return this.attributes[k];
    },
    removeAttribute(k) {
      delete this.attributes[k];
    },
    appendChild(child) {
      this.children.push(child);
      return child;
    },
    addEventListener(ev, fn) {
      (this.listeners[ev] = this.listeners[ev] || []).push(fn);
    },
    querySelector() {
      return null;
    },
  };
  return el;
}

function makeContext(overrides = {}) {
  const rootList = makeElement("div");
  rootList.id = "agents-list";

  const doc = {
    getElementById(id) {
      if (id === "agents-list") return rootList;
      return null;
    },
    createElement: makeElement,
  };

  const ctx = {
    console,
    Date,
    Math,
    Number,
    parseInt,
    String,
    JSON,
    Set,
    Array,
    Promise,
    setTimeout: vi.fn(),
    encodeURIComponent,
    fetch: vi.fn(),
    document: doc,
    window: {},
    Routes: {
      agents: {
        list: () => "/api/agents",
        get: () => "/api/agents/{slug}",
        create: () => "/api/agents",
        update: () => "/api/agents/{slug}",
        delete: () => "/api/agents/{slug}",
      },
    },
    ...overrides,
  };
  ctx.window.apiRoutes = ctx.Routes;
  ctx._rootList = rootList;
  vm.createContext(ctx);
  return ctx;
}

function loadAgents(ctx) {
  const code = readFileSync(join(jsDir, "agents.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "agents.js") });
  return ctx;
}

describe("agents.js", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
    loadAgents(ctx);
  });

  it("exposes loadAgents on window", () => {
    expect(typeof ctx.window.loadAgents).toBe("function");
  });

  it("renderRow builds a card with title, capabilities meta, and a Clone button for built-ins", () => {
    const row = ctx.window.__agents_test.renderRow({
      slug: "impl",
      title: "Implementation",
      description: "runs implementation",
      capabilities: ["workspace.write", "board.context"],
      multiturn: true,
      builtin: true,
    });
    expect(row.attributes["data-slug"]).toBe("impl");
    const header = row.children[0];
    expect(header.children[0].textContent).toBe("Implementation");
    expect(header.children[1].textContent).toContain("workspace write");
    expect(header.children[1].textContent).toContain("multi-turn");
    // The actions container holds the Clone button for built-ins.
    const actions = header.children[2];
    expect(actions.children[0].textContent).toBe("Clone");
    expect(actions.children[0].disabled).toBe(false);
  });

  it("user-authored rows get Edit + Delete buttons, no Clone", () => {
    const row = ctx.window.__agents_test.renderRow({
      slug: "impl-codex",
      title: "Impl Codex",
      builtin: false,
      harness: "codex",
    });
    const actions = row.children[0].children[2];
    const labels = actions.children.map((c) => c.textContent);
    expect(labels).toContain("Edit");
    expect(labels).toContain("Delete");
    expect(labels).not.toContain("Clone");
    expect(row.classList.contains("agents-row--user")).toBe(true);
  });

  it("suggestCloneSlug appends -copy and respects the 40-char cap", () => {
    const s = ctx.window.__agents_test.suggestCloneSlug("impl");
    expect(s).toBe("impl-copy");
    const long = ctx.window.__agents_test.suggestCloneSlug(
      "a-really-long-base-slug-that-pushes-the-limit",
    );
    expect(long.length).toBeLessThanOrEqual(40);
    expect(long.endsWith("-copy")).toBe(true);
  });

  it("loadAgents fetches /api/agents and renders rows", async () => {
    ctx.fetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve([
          {
            slug: "title",
            title: "Title",
            capabilities: [],
            multiturn: false,
          },
        ]),
    });
    ctx.window.loadAgents();
    // Wait for the fetch promise chain.
    await new Promise((resolve) => setImmediate(resolve));
    expect(ctx.fetch).toHaveBeenCalledWith(
      "/api/agents",
      expect.objectContaining({}),
    );
    expect(ctx._rootList.children.length).toBe(1);
    expect(ctx._rootList.children[0].attributes["data-slug"]).toBe("title");
  });

  it("expandAgent fetches the slug-specific URL", async () => {
    ctx.fetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve({
          slug: "title",
          title: "Title",
          capabilities: [],
          multiturn: false,
          prompt_tmpl: "hello {{.Prompt}}",
        }),
    });
    const card = makeElement("div");
    const body = makeElement("div");
    ctx.window.__agents_test.expandAgent("title", card, body);
    await new Promise((resolve) => setImmediate(resolve));
    expect(ctx.fetch).toHaveBeenCalledWith(
      "/api/agents/title",
      expect.objectContaining({}),
    );
    expect(card.classList.contains("agents-row--open")).toBe(true);
  });
});
