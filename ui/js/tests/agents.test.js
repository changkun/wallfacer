import { describe, it, expect, vi, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

// Minimal DOM polyfill covering the nodes the split-pane agents
// module touches. Children are plain arrays; classList / dataset
// are objects with the methods vanilla code uses.
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
    rows: 0,
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
      // Support the editor's [name="..."] lookups used by
      // readEditorPayload. Walks the children tree.
      const m = sel.match(/^\[name="([^"]+)"\]$/);
      if (m) {
        return findByName(this, m[1]);
      }
      return null;
    },
    querySelectorAll(sel) {
      const m = sel.match(/^\[name="([^"]+)"\](?::checked)?$/);
      if (!m) return [];
      const wantChecked = sel.includes(":checked");
      const results = [];
      collectByName(this, m[1], wantChecked, results);
      return results;
    },
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

function collectByName(node, name, wantChecked, out) {
  if (node.name === name && (!wantChecked || node.checked)) out.push(node);
  for (const child of node.children || []) {
    collectByName(child, name, wantChecked, out);
  }
}

function makeContext(overrides = {}) {
  const rail = makeElement("div");
  rail.id = "agents-rail-list";
  const detail = makeElement("section");
  detail.id = "agents-detail";
  const search = makeElement("input");
  search.id = "agents-rail-search";
  const defaultHarness = makeElement("span");
  defaultHarness.id = "agents-mode-default-harness";

  const byId = {
    "agents-rail-list": rail,
    "agents-detail": detail,
    "agents-rail-search": search,
    "agents-mode-default-harness": defaultHarness,
  };

  const doc = {
    getElementById(id) {
      return byId[id] || null;
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
    clearTimeout: vi.fn(),
    encodeURIComponent,
    fetch: vi.fn().mockResolvedValue({
      ok: true,
      json: () => Promise.resolve([]),
    }),
    document: doc,
    window: {},
    defaultSandbox: "claude",
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
  ctx._rail = rail;
  ctx._detail = detail;
  vm.createContext(ctx);
  return ctx;
}

function loadAgents(ctx) {
  const code = readFileSync(join(jsDir, "agents.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "agents.js") });
  return ctx;
}

describe("agents.js (split-pane)", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
    loadAgents(ctx);
  });

  it("exposes loadAgents and the test hook on window", () => {
    expect(typeof ctx.window.loadAgents).toBe("function");
    expect(typeof ctx.window.__agents_test.renderRail).toBe("function");
  });

  it("renderRail groups built-in and user-authored agents", () => {
    ctx.window.__agents_test._setState({
      agents: [
        { slug: "impl", title: "Implementation", builtin: true },
        { slug: "impl-codex", title: "Impl Codex", builtin: false },
      ],
    });
    ctx.window.__agents_test.renderRail();
    // Headers + two rail items = at least 4 children (2 headers + 2 items).
    const children = ctx._rail.children;
    const headers = children.filter((c) => c.tagName === "DIV");
    expect(headers.length).toBeGreaterThanOrEqual(1);
    const buttons = children.filter((c) => c.tagName === "BUTTON");
    expect(buttons.length).toBe(2);
    // User-authored row gets the user modifier.
    const userRow = buttons.find(
      (b) => b.attributes["data-slug"] === "impl-codex",
    );
    expect(userRow.classList.contains("agents-rail__item--user")).toBe(true);
  });

  it("openNewEditor stages a draft and renders the editor", () => {
    ctx.window.__agents_test.openNewEditor();
    // After openNewEditor, renderDetail runs synchronously and the
    // detail pane should contain a form with a Create button.
    const detail = ctx._detail;
    const form = findByTag(detail, "FORM");
    expect(form).toBeTruthy();
    const submit = findByTag(form, "BUTTON", (b) => b.type === "submit");
    expect(submit.textContent).toBe("Create");
  });

  it("startClone pre-fills the editor from a built-in", () => {
    const role = {
      slug: "impl",
      title: "Implementation",
      harness: "",
      capabilities: ["workspace.write"],
      multiturn: true,
      builtin: true,
      prompt_tmpl: "hello {{.Prompt}}",
    };
    ctx.window.__agents_test.startClone(role);
    const form = findByTag(ctx._detail, "FORM");
    expect(form).toBeTruthy();
    // Slug should be suggested with -copy suffix.
    const slugInput = form.querySelector('[name="slug"]');
    expect(slugInput.value).toBe("impl-copy");
    // Submit says Create (not Save) because this is a new draft.
    const submit = findByTag(form, "BUTTON", (b) => b.type === "submit");
    expect(submit.textContent).toBe("Create");
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
});

function findByTag(node, tag, pred) {
  if (!node) return null;
  if (node.tagName === tag && (!pred || pred(node))) return node;
  for (const child of node.children || []) {
    const r = findByTag(child, tag, pred);
    if (r) return r;
  }
  return null;
}
