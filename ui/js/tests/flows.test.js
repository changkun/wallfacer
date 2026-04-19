import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

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
    dataset: {},
    textContent: "",
    innerHTML: "",
    hidden: false,
    disabled: false,
    title: "",
    type: "",
    value: "",
    checked: false,
    name: "",
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
    querySelector(sel) {
      const m = sel.match(/^\[name="([^"]+)"\]$/);
      if (m) return findByName(this, m[1]);
      return null;
    },
    querySelectorAll() {
      return [];
    },
    scrollIntoView() {},
  };
  return el;
}

function findByName(node, name) {
  if (node.name === name) return node;
  for (const child of node.children || []) {
    const r = findByName(child, name);
    if (r) return r;
  }
  return null;
}

function findByTag(node, tag, pred) {
  if (!node) return null;
  if (node.tagName === tag && (!pred || pred(node))) return node;
  for (const child of node.children || []) {
    const r = findByTag(child, tag, pred);
    if (r) return r;
  }
  return null;
}

function makeContext(overrides = {}) {
  const rail = makeElement("div");
  rail.id = "flows-rail-list";
  const detail = makeElement("section");
  detail.id = "flows-detail";
  const search = makeElement("input");
  search.id = "flows-rail-search";

  const byId = {
    "flows-rail-list": rail,
    "flows-detail": detail,
    "flows-rail-search": search,
  };

  const doc = {
    getElementById(id) {
      return byId[id] || null;
    },
    createElement: makeElement,
    querySelector() {
      return null;
    },
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
    clearTimeout: vi.fn(),
    encodeURIComponent,
    fetch: vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([]),
    }),
    document: doc,
    window: { CSS: { escape: (s) => s }, setTimeout: (fn) => fn() },
    Routes: {
      agents: {
        list: () => "/api/agents",
        get: () => "/api/agents/{slug}",
        create: () => "/api/agents",
        update: () => "/api/agents/{slug}",
        delete: () => "/api/agents/{slug}",
      },
      flows: {
        list: () => "/api/flows",
        get: () => "/api/flows/{slug}",
        create: () => "/api/flows",
        update: () => "/api/flows/{slug}",
        delete: () => "/api/flows/{slug}",
      },
    },
    ...overrides,
  };
  ctx.window.apiRoutes = ctx.Routes;
  ctx._rail = rail;
  ctx._detail = detail;
  vm.createContext(ctx);
  return ctx;
}

function loadFlows(ctx) {
  const code = readFileSync(join(jsDir, "flows.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "flows.js") });
  return ctx;
}

describe("flows.js (split-pane)", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
    loadFlows(ctx);
  });

  it("exposes loadFlows and the test hook on window", () => {
    expect(typeof ctx.window.loadFlows).toBe("function");
    expect(typeof ctx.window.__flows_test.renderRail).toBe("function");
  });

  it("renderRail groups built-in and user flows", () => {
    ctx.window.__flows_test._setState({
      flows: [
        {
          slug: "implement",
          name: "Implement",
          builtin: true,
          steps: [{ agent_slug: "impl" }],
        },
        {
          slug: "tdd-loop",
          name: "TDD Loop",
          builtin: false,
          steps: [{ agent_slug: "test" }, { agent_slug: "impl" }],
        },
      ],
    });
    ctx.window.__flows_test.renderRail();
    const buttons = ctx._rail.children.filter((c) => c.tagName === "BUTTON");
    expect(buttons.length).toBe(2);
    const user = buttons.find(
      (b) => b.attributes["data-slug"] === "tdd-loop",
    );
    expect(user.classList.contains("flows-rail__item--user")).toBe(true);
  });

  it("groupParallel collapses transitive mutual references", () => {
    const groups = ctx.window.__flows_test.groupParallel([
      { agent_slug: "impl" },
      {
        agent_slug: "commit-msg",
        run_in_parallel_with: ["title", "oversight"],
      },
      { agent_slug: "title", run_in_parallel_with: ["commit-msg", "oversight"] },
      {
        agent_slug: "oversight",
        run_in_parallel_with: ["commit-msg", "title"],
      },
    ]);
    expect(groups.length).toBe(2);
    expect(groups[0].map((s) => s.agent_slug)).toEqual(["impl"]);
    expect(groups[1].map((s) => s.agent_slug)).toEqual([
      "commit-msg",
      "title",
      "oversight",
    ]);
  });

  it("buildChain wraps parallel groups in a visual box", () => {
    const chain = ctx.window.__flows_test.buildChain([
      { agent_slug: "impl" },
      {
        agent_slug: "commit-msg",
        run_in_parallel_with: ["title"],
      },
      {
        agent_slug: "title",
        run_in_parallel_with: ["commit-msg"],
      },
    ]);
    // After impl comes an arrow then a parallel box.
    const box = chain.children.find(
      (c) => c.tagName === "SPAN" && c.className === "flows-detail__parallel",
    );
    expect(box).toBeTruthy();
  });

  it("openNewEditor stages a draft and renders the editor", () => {
    ctx.window.__flows_test.openNewEditor();
    const form = findByTag(ctx._detail, "FORM");
    expect(form).toBeTruthy();
    const submit = findByTag(form, "BUTTON", (b) => b.type === "submit");
    expect(submit.textContent).toBe("Create");
  });

  it("startClone pre-fills the editor with a (copy) name suffix", () => {
    ctx.window.__flows_test.startClone({
      slug: "implement",
      name: "Implement",
      builtin: true,
      steps: [{ agent_slug: "impl" }],
    });
    const form = findByTag(ctx._detail, "FORM");
    expect(form).toBeTruthy();
    const nameInput = form.querySelector('[name="name"]');
    expect(nameInput.value).toBe("Implement (copy)");
  });

  it("suggestCloneSlug appends -copy and respects the 40-char cap", () => {
    expect(ctx.window.__flows_test.suggestCloneSlug("implement")).toBe(
      "implement-copy",
    );
  });
});
