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
    ...overrides,
  };
  ctx.window.apiRoutes = {
    agents: {
      list: () => "/api/agents",
      get: () => "/api/agents/{slug}",
    },
  };
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

  it("renderRow builds a card with name, meta, and disabled Clone button", () => {
    const row = ctx.window.__agents_test.renderRow({
      slug: "impl",
      name: "impl",
      description: "runs implementation",
      activity: "implementation",
      mount_mode: "read-write",
      single_turn: false,
    });
    expect(row.attributes["data-slug"]).toBe("impl");
    // Header row has name, meta, and clone button.
    const header = row.children[0];
    expect(header.children[0].textContent).toBe("impl");
    expect(header.children[1].textContent).toContain("implementation");
    expect(header.children[1].textContent).toContain("multi-turn");
    const clone = header.children[2];
    expect(clone.textContent).toBe("Clone");
    expect(clone.disabled).toBe(true);
  });

  it("loadAgents fetches /api/agents and renders rows", async () => {
    ctx.fetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve([
          {
            slug: "title",
            name: "title",
            activity: "title",
            mount_mode: "none",
            single_turn: true,
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
          name: "title",
          activity: "title",
          mount_mode: "none",
          single_turn: true,
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
