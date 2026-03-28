import { describe, it, expect, beforeEach } from "vitest";
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
    expect(html).toContain("toggleDependencyGraph");
    expect(html).toContain('id="status-bar-terminal-btn"');
    expect(html).toContain("toggleTerminalPanel");
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
