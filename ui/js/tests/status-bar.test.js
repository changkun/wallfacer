import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { readAllCSS } from "./read-css.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, "..", "..", "..");

// ---------------------------------------------------------------------------
// Layout tests (readFileSync approach)
// ---------------------------------------------------------------------------

describe("status-bar layout", () => {
  it("HTML partial contains required elements", () => {
    const html = readFileSync(
      join(repoRoot, "ui/partials/status-bar.html"),
      "utf8",
    );
    expect(html).toContain('id="status-bar"');
    expect(html).toContain('class="status-bar"');
    expect(html).toContain('id="status-bar-panel"');
    expect(html).toContain('id="status-bar-panel-resize"');
    expect(html).toContain('class="status-bar__left"');
    expect(html).toContain('class="status-bar__right"');
    expect(html).toContain('id="status-bar-conn-dot"');
    expect(html).toContain('id="status-bar-in-progress"');
    expect(html).toContain('id="status-bar-waiting"');
    expect(html).toContain('id="status-bar-depgraph-btn"');
    expect(html).toContain("switchMode('depgraph'");
    expect(html).toContain('id="status-bar-terminal-btn"');
    expect(html).toContain("toggleTerminalPanel");
    expect(html).toContain('id="status-bar-shortcuts-btn"');
    expect(html).toContain("openKeyboardShortcuts");
  });

  it("status-bar is included in index.html", () => {
    const html = readFileSync(join(repoRoot, "ui/index.html"), "utf8");
    expect(html).toContain('{{template "status-bar.html"}}');
  });

  it("status-bar.js script is included in scripts.html", () => {
    const html = readFileSync(
      join(repoRoot, "ui/partials/scripts.html"),
      "utf8",
    );
    expect(html).toContain('src="/js/status-bar.js"');
  });

  it("CSS defines required status bar selectors", () => {
    const css = readAllCSS(join(repoRoot, "ui/css/styles.css"));
    expect(css).toContain(".status-bar");
    expect(css).toContain(".status-bar-panel");
    expect(css).toContain(".status-bar-panel-resize");
    expect(css).toContain(".status-bar-conn-dot");
    expect(css).toContain(".status-bar-conn-dot--ok");
    expect(css).toContain(".status-bar-conn-dot--reconnecting");
    expect(css).toContain(".status-bar-conn-dot--closed");
    expect(css).toContain(".status-bar__left");
    expect(css).toContain(".status-bar__right");
    expect(css).toContain(".status-bar-btn");
    expect(css).toContain(".status-bar-count");
  });

  it("CSS hides status bar on mobile via media query", () => {
    const css = readAllCSS(join(repoRoot, "ui/css/styles.css"));
    // Some @media (max-width: 768px) block should include #status-bar
    const re = /@media\s*\(max-width:\s*768px\)\s*\{[^}]*#status-bar/;
    expect(css).toMatch(re);
  });
});

// ---------------------------------------------------------------------------
// Logic tests (VM context approach)
// ---------------------------------------------------------------------------

function makeStatusBarContext(extra = {}) {
  // Build minimal DOM stubs needed by status-bar.js at load time
  const elements = {};

  function makeEl(id) {
    return {
      id,
      className: "",
      textContent: "",
      offsetHeight: 260,
      style: { display: "", height: "" },
      _ariaLabel: "",
      _ariaExpanded: "",
      _listeners: {},
      classList: {
        _classes: new Set(),
        contains(c) {
          return this._classes.has(c);
        },
        add(c) {
          this._classes.add(c);
        },
        remove(c) {
          this._classes.delete(c);
        },
        toggle(c, force) {
          if (force === undefined) {
            if (this._classes.has(c)) this._classes.delete(c);
            else this._classes.add(c);
          } else if (force) {
            this._classes.add(c);
          } else {
            this._classes.delete(c);
          }
        },
      },
      addEventListener(event, handler) {
        if (!this._listeners[event]) this._listeners[event] = [];
        this._listeners[event].push(handler);
      },
      removeEventListener() {},
      setAttribute(attr, val) {
        if (attr === "aria-label") this._ariaLabel = val;
        if (attr === "aria-expanded") this._ariaExpanded = val;
        this["_attr_" + attr] = val;
      },
      getAttribute(attr) {
        if (attr === "contenteditable") return null;
        return this["_attr_" + attr] || null;
      },
    };
  }

  const ids = [
    "status-bar-conn-dot",
    "status-bar-conn-label",
    "status-bar-workspace",
    "status-bar-in-progress",
    "status-bar-waiting",
    "status-bar-panel",
    "status-bar-panel-resize",
    "status-bar-terminal-btn",
  ];
  ids.forEach((id) => {
    elements[id] = makeEl(id);
    // Pre-add 'hidden' to panel and resize handle
    if (id === "status-bar-panel" || id === "status-bar-panel-resize") {
      elements[id].classList._classes.add("hidden");
    }
  });

  const _storage = {};
  const ctx = {
    document: {
      getElementById(id) {
        return elements[id] || null;
      },
      addEventListener() {},
      removeEventListener() {},
      activeElement: { tagName: "BODY", getAttribute: () => null },
      readyState: "complete",
      body: { style: {} },
    },
    window: {},
    localStorage: {
      getItem(k) {
        return _storage[k] || null;
      },
      setItem(k, v) {
        _storage[k] = String(v);
      },
    },
    // Default global state
    tasks: [],
    tasksSource: null,
    _sseConnState: "closed",
    activeWorkspaces: [],
    workspaceGroups: [],
    console,
    ...extra,
  };
  return { ctx: vm.createContext(ctx), elements };
}

function loadStatusBar(ctx) {
  const code = readFileSync(join(__dirname, "..", "status-bar.js"), "utf8");
  vm.runInContext(code, ctx);
}

describe("updateStatusBar logic", () => {
  it("sets conn dot to --closed when _sseConnState is 'closed'", () => {
    const { ctx, elements } = makeStatusBarContext({ _sseConnState: "closed" });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-conn-dot"].className).toContain(
      "status-bar-conn-dot--closed",
    );
    expect(elements["status-bar-conn-label"].textContent).toBe("Disconnected");
  });

  it("sets conn dot to --closed when _sseConnState is undefined", () => {
    const { ctx, elements } = makeStatusBarContext();
    delete ctx._sseConnState;
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-conn-dot"].className).toContain(
      "status-bar-conn-dot--closed",
    );
    expect(elements["status-bar-conn-label"].textContent).toBe("Disconnected");
  });

  it("sets conn dot to --ok when _sseConnState is 'ok'", () => {
    const { ctx, elements } = makeStatusBarContext({
      _sseConnState: "ok",
    });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-conn-dot"].className).toContain(
      "status-bar-conn-dot--ok",
    );
    expect(elements["status-bar-conn-label"].textContent).toBe("Connected");
  });

  it("sets conn dot to --reconnecting when _sseConnState is 'reconnecting'", () => {
    const { ctx, elements } = makeStatusBarContext({
      _sseConnState: "reconnecting",
    });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-conn-dot"].className).toContain(
      "status-bar-conn-dot--reconnecting",
    );
    expect(elements["status-bar-conn-label"].textContent).toBe("Reconnecting…");
  });

  it("shows 0 in-progress and 0 waiting when tasks is empty", () => {
    const { ctx, elements } = makeStatusBarContext({ tasks: [] });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-in-progress"].textContent).toBe("0");
    expect(elements["status-bar-waiting"].textContent).toBe("0");
  });

  it("counts in_progress and committing tasks as in-progress", () => {
    const { ctx, elements } = makeStatusBarContext({
      tasks: [
        { status: "in_progress" },
        { status: "committing" },
        { status: "backlog" },
        { status: "waiting" },
      ],
    });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-in-progress"].textContent).toBe("2");
    expect(elements["status-bar-waiting"].textContent).toBe("1");
  });

  it("counts waiting and failed tasks in the waiting count", () => {
    const { ctx, elements } = makeStatusBarContext({
      tasks: [{ status: "waiting" }, { status: "failed" }, { status: "done" }],
    });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-in-progress"].textContent).toBe("0");
    expect(elements["status-bar-waiting"].textContent).toBe("2");
  });

  it("shows workspace basename from activeWorkspaces", () => {
    const { ctx, elements } = makeStatusBarContext({
      activeWorkspaces: ["/home/user/my-project"],
      workspaceGroups: [],
    });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-workspace"].textContent).toBe("my-project");
  });

  it("prefers workspace group name over basename", () => {
    const { ctx, elements } = makeStatusBarContext({
      activeWorkspaces: ["/home/user/my-project"],
      workspaceGroups: [
        { name: "My Group", workspaces: ["/home/user/my-project"] },
      ],
    });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-workspace"].textContent).toBe("My Group");
  });

  it("hides workspace pill when activeWorkspaces is empty", () => {
    const { ctx, elements } = makeStatusBarContext({
      activeWorkspaces: [],
      workspaceGroups: [],
    });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-workspace"].style.display).toBe("none");
  });
});

describe("toggleTerminalPanel", () => {
  it("removes 'hidden' class from panel and resize handle when initially hidden", () => {
    const { ctx, elements } = makeStatusBarContext();
    loadStatusBar(ctx);
    expect(elements["status-bar-panel"].classList.contains("hidden")).toBe(
      true,
    );
    expect(
      elements["status-bar-panel-resize"].classList.contains("hidden"),
    ).toBe(true);
    ctx.toggleTerminalPanel();
    expect(elements["status-bar-panel"].classList.contains("hidden")).toBe(
      false,
    );
    expect(
      elements["status-bar-panel-resize"].classList.contains("hidden"),
    ).toBe(false);
    expect(elements["status-bar-terminal-btn"]._ariaExpanded).toBe("true");
  });

  it("adds 'hidden' class to panel and resize handle when visible", () => {
    const { ctx, elements } = makeStatusBarContext();
    loadStatusBar(ctx);
    ctx.toggleTerminalPanel();
    expect(elements["status-bar-panel"].classList.contains("hidden")).toBe(
      false,
    );
    ctx.toggleTerminalPanel();
    expect(elements["status-bar-panel"].classList.contains("hidden")).toBe(
      true,
    );
    expect(
      elements["status-bar-panel-resize"].classList.contains("hidden"),
    ).toBe(true);
    expect(elements["status-bar-terminal-btn"]._ariaExpanded).toBe("false");
  });
});

// ---------------------------------------------------------------------------
// Terminal integration tests
// ---------------------------------------------------------------------------

describe("terminal integration", () => {
  it("_showTerminalPanel calls connectTerminal", () => {
    const connectCalled = { value: false };
    const { ctx, elements } = makeStatusBarContext({
      connectTerminal: () => {
        connectCalled.value = true;
      },
      terminalEnabled: true,
    });
    loadStatusBar(ctx);
    ctx.toggleTerminalPanel();
    expect(connectCalled.value).toBe(true);
  });

  it("_hideTerminalPanel does not call disconnectTerminal", () => {
    const disconnectCalled = { value: false };
    const { ctx } = makeStatusBarContext({
      connectTerminal: () => {},
      disconnectTerminal: () => {
        disconnectCalled.value = true;
      },
      terminalEnabled: true,
    });
    loadStatusBar(ctx);
    ctx.toggleTerminalPanel(); // open
    ctx.toggleTerminalPanel(); // close
    expect(disconnectCalled.value).toBe(false);
  });

  it("terminal button hidden when terminalEnabled is false", () => {
    const { ctx, elements } = makeStatusBarContext({
      terminalEnabled: false,
    });
    loadStatusBar(ctx);
    ctx.applyTerminalVisibility();
    expect(
      elements["status-bar-terminal-btn"].classList.contains("hidden"),
    ).toBe(true);
  });

  it("terminal button visible when terminalEnabled is true", () => {
    const { ctx, elements } = makeStatusBarContext({
      terminalEnabled: true,
    });
    loadStatusBar(ctx);
    ctx.applyTerminalVisibility();
    expect(
      elements["status-bar-terminal-btn"].classList.contains("hidden"),
    ).toBe(false);
  });

  it("toggleTerminalPanel is no-op when terminalEnabled is false", () => {
    const { ctx, elements } = makeStatusBarContext({
      terminalEnabled: false,
    });
    loadStatusBar(ctx);
    ctx.toggleTerminalPanel();
    // Panel should remain hidden
    expect(elements["status-bar-panel"].classList.contains("hidden")).toBe(
      true,
    );
  });
});

// ---------------------------------------------------------------------------
// Sign-in badge (cloud mode)
// ---------------------------------------------------------------------------

function makeSigninContext(fetchImpl) {
  // A lightweight DOM stub that supports the subset of APIs renderSigninBadge
  // touches: createElement, appendChild, textContent, innerHTML, attributes.
  function makeEl(tag) {
    return {
      tagName: String(tag || "div").toUpperCase(),
      children: [],
      _attrs: {},
      style: {},
      className: "",
      textContent: "",
      _innerHTML: "",
      get innerHTML() {
        // Reconstruct from children so tests can check either side.
        if (this._innerHTML !== "") return this._innerHTML;
        return this.children
          .map((c) => {
            if (typeof c === "string") return c;
            if (c.tagName === "A")
              return `<a href="${c._attrs.href || ""}">${c.textContent}</a>`;
            return c.tagName ? `<${c.tagName.toLowerCase()}>` : "";
          })
          .join("");
      },
      set innerHTML(v) {
        this._innerHTML = v;
        this.children = [];
      },
      appendChild(c) {
        this._innerHTML = "";
        this.children.push(c);
        return c;
      },
      setAttribute(k, v) {
        this._attrs[k] = v;
        if (k === "src") this.src = v;
        if (k === "href") this.href = v;
      },
      getAttribute(k) {
        return this._attrs[k] ?? null;
      },
      get src() {
        return this._attrs.src || "";
      },
      set src(v) {
        this._attrs.src = v;
      },
      get href() {
        return this._attrs.href || "";
      },
      set href(v) {
        this._attrs.href = v;
      },
      get alt() {
        return this._attrs.alt || "";
      },
      set alt(v) {
        this._attrs.alt = v;
      },
      get name() {
        return this._attrs.name || "";
      },
      set name(v) {
        this._attrs.name = v;
      },
      classList: {
        _s: new Set(),
        contains(c) {
          return this._s.has(c);
        },
        add(c) {
          this._s.add(c);
        },
        remove(c) {
          this._s.delete(c);
        },
      },
      addEventListener() {},
      removeEventListener() {},
    };
  }

  const signinEl = makeEl("div");
  signinEl.id = "sidebar-signin";

  const peersEl = makeEl("div");
  peersEl.id = "sidebar-peers";

  // The full stub set the main status-bar tests use, plus signin + peers.
  const ids = [
    "status-bar-conn-dot",
    "status-bar-conn-label",
    "status-bar-workspace",
    "status-bar-in-progress",
    "status-bar-waiting",
    "status-bar-panel",
    "status-bar-panel-resize",
    "status-bar-terminal-btn",
  ];
  const elements = {
    "sidebar-signin": signinEl,
    "sidebar-peers": peersEl,
  };
  ids.forEach((id) => {
    elements[id] = makeEl("div");
    elements[id].id = id;
  });

  const ctx = {
    document: {
      getElementById(id) {
        return elements[id] || null;
      },
      createElement: makeEl,
      addEventListener() {},
      removeEventListener() {},
      activeElement: { tagName: "BODY", getAttribute: () => null },
      readyState: "complete",
      body: { style: {} },
    },
    window: {},
    localStorage: { getItem: () => null, setItem: () => {} },
    fetch: fetchImpl,
    tasks: [],
    _sseConnState: "closed",
    activeWorkspaces: [],
    workspaceGroups: [],
    console,
    Promise,
    setTimeout,
    clearTimeout,
  };
  return { ctx: vm.createContext(ctx), elements, signinEl, peersEl };
}

function loadStatusBarInCtx(ctx) {
  const code = readFileSync(join(__dirname, "..", "status-bar.js"), "utf8");
  vm.runInContext(code, ctx);
}

// wait for any pending fetch().then() callbacks to flush so assertions
// observe the post-render state. 10 iterations covers the deepest
// chain in status-bar.js: renderSigninBadge (fetch → then → then) +
// _fetchAndRenderOrgSwitcher (fetch → then → then) runs back-to-back,
// so later tests need extra microtask turns to settle.
async function flushPromises() {
  for (let i = 0; i < 10; i++) {
    await Promise.resolve();
  }
}

describe("renderSigninBadge", () => {
  it("renders nothing when config.cloud is false", async () => {
    let called = false;
    const { ctx, signinEl } = makeSigninContext(() => {
      called = true;
      return Promise.resolve({ status: 204 });
    });
    loadStatusBarInCtx(ctx);
    ctx.renderSigninBadge({ cloud: false });
    await flushPromises();
    expect(signinEl.innerHTML).toBe("");
    expect(called).toBe(false); // must NOT hit /api/auth/me in local mode
  });

  it('renders "Sign in" link when cloud && /api/auth/me → 204', async () => {
    const { ctx, signinEl } = makeSigninContext(() =>
      Promise.resolve({ status: 204 }),
    );
    loadStatusBarInCtx(ctx);
    ctx.renderSigninBadge({ cloud: true });
    await flushPromises();
    expect(signinEl.innerHTML).toContain('href="/login"');
    expect(signinEl.innerHTML).toContain("Sign in");
    // No iframe in the signed-out branch.
    const iframeChild = signinEl.children.find((c) => c.tagName === "IFRAME");
    expect(iframeChild).toBeUndefined();
  });

  it("renders avatar, name, and front-channel iframe when cloud && /api/auth/me → 200", async () => {
    const user = {
      sub: "u-123",
      email: "alice@example.com",
      name: "Alice",
      picture: "https://cdn/a.png",
    };
    const { ctx, signinEl } = makeSigninContext(() =>
      Promise.resolve({
        status: 200,
        json: () => Promise.resolve(user),
      }),
    );
    loadStatusBarInCtx(ctx);
    ctx.renderSigninBadge({
      cloud: true,
      auth_url: "https://auth.latere.ai",
    });
    await flushPromises();

    // Find the avatar img among nested children.
    const wrap = signinEl.children.find(
      (c) => c.className === "sb-signin__user",
    );
    expect(wrap).toBeTruthy();
    const avatar = wrap.children.find((c) => c.tagName === "IMG");
    expect(avatar).toBeTruthy();
    expect(avatar.src).toBe("https://cdn/a.png");
    expect(avatar.getAttribute("referrerpolicy")).toBe("no-referrer");

    const nameSpan = wrap.children.find(
      (c) => c.className === "sb-signin__name",
    );
    expect(nameSpan.textContent).toBe("Alice");

    const logout = wrap.children.find(
      (c) => c.className === "sb-signin__logout",
    );
    expect(logout.href).toBe("/logout");

    const iframe = signinEl.children.find((c) => c.tagName === "IFRAME");
    expect(iframe).toBeTruthy();
    expect(iframe.src).toBe("https://auth.latere.ai/logout");
    expect(iframe.name).toBe("latere-logout-iframe");
  });

  it("falls back to email when name is empty", async () => {
    const user = {
      sub: "u-123",
      email: "bob@example.com",
      name: "",
      picture: "",
    };
    const { ctx, signinEl } = makeSigninContext(() =>
      Promise.resolve({
        status: 200,
        json: () => Promise.resolve(user),
      }),
    );
    loadStatusBarInCtx(ctx);
    ctx.renderSigninBadge({ cloud: true });
    await flushPromises();

    const wrap = signinEl.children.find(
      (c) => c.className === "sb-signin__user",
    );
    const nameSpan = wrap.children.find(
      (c) => c.className === "sb-signin__name",
    );
    expect(nameSpan.textContent).toBe("bob@example.com");
  });

  it("renders user-controlled strings as text, not markup", async () => {
    const user = {
      sub: "u-1",
      email: "e@e",
      name: "<script>alert(1)</script>",
      picture: "",
    };
    const { ctx, signinEl } = makeSigninContext(() =>
      Promise.resolve({
        status: 200,
        json: () => Promise.resolve(user),
      }),
    );
    loadStatusBarInCtx(ctx);
    ctx.renderSigninBadge({ cloud: true });
    await flushPromises();

    const wrap = signinEl.children.find(
      (c) => c.className === "sb-signin__user",
    );
    const nameSpan = wrap.children.find(
      (c) => c.className === "sb-signin__name",
    );
    // textContent receives the raw string — DOM would not interpret it.
    expect(nameSpan.textContent).toBe("<script>alert(1)</script>");
    // And there's no real <script> element in the tree.
    const scriptChild = wrap.children.find((c) => c.tagName === "SCRIPT");
    expect(scriptChild).toBeUndefined();
  });
});

describe("renderPresence", () => {
  it("renders nothing when local mode and no active tasks", () => {
    const { ctx, peersEl } = makeSigninContext(() =>
      Promise.resolve({ status: 204 }),
    );
    loadStatusBarInCtx(ctx);
    ctx.renderPresence();
    // Local mode + idle board: :empty CSS hides it. We assert the DOM
    // contract — no children — since ":empty" is enforced by CSS.
    expect(peersEl.children.length).toBe(0);
  });

  it("renders one row per active task (in_progress / committing / waiting)", () => {
    const { ctx, peersEl } = makeSigninContext(() =>
      Promise.resolve({ status: 204 }),
    );
    ctx.tasks = [
      { id: "aaaa-1111", status: "in_progress", sandbox: "claude" },
      { id: "bbbb-2222", status: "committing", sandbox: "codex" },
      { id: "cccc-3333", status: "waiting", sandbox: "claude" },
      { id: "dddd-4444", status: "done", sandbox: "claude" }, // excluded
      { id: "eeee-5555", status: "backlog", sandbox: "claude" }, // excluded
      { id: "ffff-6666", status: "failed", sandbox: "claude" }, // excluded
    ];
    loadStatusBarInCtx(ctx);
    ctx.renderPresence();

    // Header row + 3 peer rows.
    const peerRows = peersEl.children.filter((c) => c.className === "sb-peer");
    expect(peerRows.length).toBe(3);

    // Dot classes must reflect status: on/on/idle in that order.
    const dotClasses = peerRows.map((r) => {
      const dot = r.children.find((c) => c.className.includes("pd "));
      return dot.className;
    });
    expect(dotClasses[0]).toContain("on");
    expect(dotClasses[1]).toContain("on");
    expect(dotClasses[2]).toContain("idle");
  });

  it("shows signed-in user as the first peer", () => {
    const { ctx, peersEl } = makeSigninContext(() =>
      Promise.resolve({ status: 204 }),
    );
    ctx.tasks = [];
    loadStatusBarInCtx(ctx);
    ctx._presenceSelf = { name: "Alice", email: "a@b.com" };
    ctx.renderPresence();

    const peerRows = peersEl.children.filter((c) => c.className === "sb-peer");
    expect(peerRows.length).toBe(1);
    const name = peerRows[0].children.find((c) => c.className === "pn");
    expect(name.textContent).toBe("Alice");
    // Self row is always "on".
    const dot = peerRows[0].children.find((c) => c.className.includes("pd "));
    expect(dot.className).toContain("on");
  });

  it("caps the rendered peer list so the sidebar doesn't grow unbounded", () => {
    const { ctx, peersEl } = makeSigninContext(() =>
      Promise.resolve({ status: 204 }),
    );
    ctx.tasks = Array.from({ length: 20 }, (_, i) => ({
      id: `t-${i}`,
      status: "in_progress",
      sandbox: "claude",
    }));
    loadStatusBarInCtx(ctx);
    ctx.renderPresence();
    const peerRows = peersEl.children.filter((c) => c.className === "sb-peer");
    expect(peerRows.length).toBeLessThanOrEqual(8);
  });
});

// ---------------------------------------------------------------------------
// Org switcher (cloud mode, multi-org users)
// ---------------------------------------------------------------------------

// routedFetch returns a fetch impl that dispatches by URL. Used by the
// org-switcher tests so /api/auth/me and /api/auth/orgs can be stubbed
// independently without ordering assumptions.
function routedFetch(routes) {
  return (url, opts) => {
    const path = typeof url === "string" ? url : url.url;
    const route = routes[path];
    if (!route) {
      return Promise.reject(new Error(`no stub for ${path}`));
    }
    return route(opts);
  };
}

// signedInUser is the fixture the /api/auth/me stub uses across the
// org-switcher tests — content doesn't matter beyond producing a
// signed-in badge that renders the org slot.
const signedInUser = {
  sub: "u-1",
  email: "alice@example.com",
  name: "Alice",
  picture: "",
};

describe("renderSigninBadge org switcher", () => {
  it("renders no <select> when /api/auth/orgs returns 204 (single-org)", async () => {
    const { ctx, signinEl } = makeSigninContext(
      routedFetch({
        "/api/auth/me": () =>
          Promise.resolve({
            status: 200,
            json: () => Promise.resolve(signedInUser),
          }),
        "/api/auth/orgs": () => Promise.resolve({ status: 204 }),
      }),
    );
    loadStatusBarInCtx(ctx);
    ctx.renderSigninBadge({ cloud: true });
    await flushPromises();

    const wrap = signinEl.children.find(
      (c) => c.className === "sb-signin__user",
    );
    const slot = wrap.children.find(
      (c) => c.className === "sb-signin__orgs",
    );
    // Slot exists (mounted for layout) but has no children in the
    // single-org case — the renderer bails on <2 orgs.
    expect(slot).toBeTruthy();
    expect(slot.children.length).toBe(0);
  });

  it("renders a <select> with one option per org when 2+ orgs", async () => {
    const orgsPayload = {
      orgs: [
        { id: "org-a", name: "Alice Inc" },
        { id: "org-b", name: "Bob Corp" },
      ],
      current_id: "org-b",
    };
    const { ctx, signinEl } = makeSigninContext(
      routedFetch({
        "/api/auth/me": () =>
          Promise.resolve({
            status: 200,
            json: () => Promise.resolve(signedInUser),
          }),
        "/api/auth/orgs": () =>
          Promise.resolve({
            status: 200,
            json: () => Promise.resolve(orgsPayload),
          }),
      }),
    );
    loadStatusBarInCtx(ctx);
    ctx.renderSigninBadge({ cloud: true });
    await flushPromises();

    const wrap = signinEl.children.find(
      (c) => c.className === "sb-signin__user",
    );
    const slot = wrap.children.find(
      (c) => c.className === "sb-signin__orgs",
    );
    const select = slot.children.find((c) => c.tagName === "SELECT");
    expect(select).toBeTruthy();

    const options = select.children.filter((c) => c.tagName === "OPTION");
    expect(options.length).toBe(2);
    expect(options[0].textContent).toBe("Alice Inc");
    expect(options[1].textContent).toBe("Bob Corp");
    // Option `value` is stored on the stub element directly.
    expect(options[0].value).toBe("org-a");
    expect(options[1].value).toBe("org-b");
  });

  it("skips /api/auth/orgs in local mode (cloud=false)", async () => {
    const calls = [];
    const { ctx } = makeSigninContext((url) => {
      calls.push(url);
      return Promise.resolve({ status: 204 });
    });
    loadStatusBarInCtx(ctx);
    ctx.renderSigninBadge({ cloud: false });
    await flushPromises();

    // Cloud-off short-circuits before any fetch. Orgs endpoint in
    // particular must never be touched (it would error because the
    // routed stub doesn't include it anyway).
    expect(calls).not.toContain("/api/auth/orgs");
  });

  it("does not render the switcher when /api/auth/me is 204 (signed out)", async () => {
    const orgsCalls = [];
    const { ctx, signinEl } = makeSigninContext(
      routedFetch({
        "/api/auth/me": () => Promise.resolve({ status: 204 }),
        "/api/auth/orgs": () => {
          orgsCalls.push("called");
          return Promise.resolve({ status: 204 });
        },
      }),
    );
    loadStatusBarInCtx(ctx);
    ctx.renderSigninBadge({ cloud: true });
    await flushPromises();

    // A signed-out user should see the plain Sign-in link, not any
    // org affordance. The renderer also should not fetch /api/auth/orgs
    // since the signed-in wrapper was never built.
    expect(signinEl.innerHTML).toContain('href="/login"');
    expect(orgsCalls.length).toBe(0);
  });
});
