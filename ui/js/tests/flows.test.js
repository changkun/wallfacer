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
    textContent: "",
    innerHTML: "",
    hidden: false,
    disabled: false,
    type: "",
    title: "",
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
    scrollIntoView() {},
  };
  return el;
}

function makeContext(overrides = {}) {
  const rootList = makeElement("div");
  rootList.id = "flows-list";

  const doc = {
    getElementById(id) {
      if (id === "flows-list") return rootList;
      return null;
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
    encodeURIComponent,
    fetch: vi.fn(),
    document: doc,
    window: { CSS: { escape: (s) => s }, setTimeout: (fn) => fn() },
    ...overrides,
  };
  ctx.window.apiRoutes = {
    flows: {
      list: () => "/api/flows",
      get: () => "/api/flows/{slug}",
    },
  };
  ctx._rootList = rootList;
  vm.createContext(ctx);
  return ctx;
}

function loadFlows(ctx) {
  const code = readFileSync(join(jsDir, "flows.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "flows.js") });
  return ctx;
}

describe("flows.js", () => {
  let ctx;
  beforeEach(() => {
    ctx = makeContext();
    loadFlows(ctx);
  });

  it("exposes loadFlows on window", () => {
    expect(typeof ctx.window.loadFlows).toBe("function");
  });

  it("groupParallel collapses transitive mutual references into one group", () => {
    // Matches the implement flow's terminal triple.
    const groups = ctx.window.__flows_test.groupParallel([
      { agent_slug: "impl" },
      {
        agent_slug: "commit-msg",
        run_in_parallel_with: ["title", "oversight"],
      },
      {
        agent_slug: "title",
        run_in_parallel_with: ["commit-msg", "oversight"],
      },
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

  it("buildChain inserts ‖ between parallel siblings and → between groups", () => {
    const chain = ctx.window.__flows_test.buildChain([
      { agent_slug: "impl", agent_name: "Implementation" },
      {
        agent_slug: "commit-msg",
        agent_name: "Commit",
        run_in_parallel_with: ["title"],
      },
      {
        agent_slug: "title",
        agent_name: "Title",
        run_in_parallel_with: ["commit-msg"],
      },
    ]);
    // Expected sequence: chip, arrow(→), chip, arrow(‖), chip
    const seps = chain.children.filter(
      (c) => c.classList && c.classList.contains("flows-chain__sep"),
    );
    expect(seps.map((s) => s.textContent)).toEqual(["→", "‖"]);
  });

  it("chip click switches to the agents tab", () => {
    const switchMode = vi.fn();
    ctx.window.switchMode = switchMode;
    const chain = ctx.window.__flows_test.buildChain([
      { agent_slug: "impl", agent_name: "Implementation" },
    ]);
    const chip = chain.children[0];
    (chip.listeners.click || []).forEach((fn) => fn({ stopPropagation() {} }));
    expect(switchMode).toHaveBeenCalledWith(
      "agents",
      expect.objectContaining({ persist: true }),
    );
  });

  it("optional steps render with a trailing ?", () => {
    const chain = ctx.window.__flows_test.buildChain([
      { agent_slug: "refine", agent_name: "Refine", optional: true },
    ]);
    expect(chain.children[0].textContent).toBe("Refine?");
  });

  it("loadFlows fetches /api/flows and renders one card per flow", async () => {
    ctx.fetch.mockResolvedValue({
      ok: true,
      json: () =>
        Promise.resolve([
          {
            slug: "implement",
            name: "Implement",
            builtin: true,
            steps: [{ agent_slug: "impl", agent_name: "Implementation" }],
          },
          {
            slug: "brainstorm",
            name: "Brainstorm",
            builtin: true,
            steps: [{ agent_slug: "ideate", agent_name: "Ideate" }],
          },
        ]),
    });
    ctx.window.loadFlows();
    await new Promise((resolve) => setImmediate(resolve));
    expect(ctx.fetch).toHaveBeenCalledWith(
      "/api/flows",
      expect.objectContaining({}),
    );
    expect(ctx._rootList.children.length).toBe(2);
    expect(ctx._rootList.children[0].attributes["data-slug"]).toBe("implement");
  });
});
