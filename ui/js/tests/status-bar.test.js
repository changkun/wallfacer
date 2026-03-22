import { describe, it, expect, beforeEach } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

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
    expect(html).toContain('class="status-bar__left"');
    expect(html).toContain('class="status-bar__right"');
    expect(html).toContain('id="status-bar-conn-dot"');
    expect(html).toContain('id="status-bar-in-progress"');
    expect(html).toContain('id="status-bar-waiting"');
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
    const css = readFileSync(join(repoRoot, "ui/css/styles.css"), "utf8");
    expect(css).toContain(".status-bar");
    expect(css).toContain(".status-bar-panel");
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
    const css = readFileSync(join(repoRoot, "ui/css/styles.css"), "utf8");
    // The @media (max-width: 768px) block should include #status-bar
    const mediaIdx = css.lastIndexOf("@media (max-width: 768px)");
    expect(mediaIdx).toBeGreaterThan(-1);
    const afterMedia = css.slice(mediaIdx);
    expect(afterMedia).toContain("#status-bar");
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
      style: { display: "" },
      _ariaLabel: "",
      _ariaExpanded: "",
      classList: {
        _classes: new Set(),
        contains(c) { return this._classes.has(c); },
        add(c) { this._classes.add(c); },
        remove(c) { this._classes.delete(c); },
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
    "status-bar-terminal-btn",
  ];
  ids.forEach((id) => {
    elements[id] = makeEl(id);
    // Pre-add 'hidden' to panel
    if (id === "status-bar-panel") {
      elements[id].classList._classes.add("hidden");
    }
  });

  const ctx = {
    document: {
      getElementById(id) { return elements[id] || null; },
      addEventListener() {},
      activeElement: { tagName: "BODY", getAttribute: () => null },
      readyState: "complete",
    },
    window: {},
    // Default global state
    tasks: [],
    tasksSource: null,
    activeWorkspaces: [],
    workspaceGroups: [],
    console,
    ...extra,
  };
  return { ctx: vm.createContext(ctx), elements };
}

function loadStatusBar(ctx) {
  const code = readFileSync(
    join(__dirname, "..", "status-bar.js"),
    "utf8",
  );
  vm.runInContext(code, ctx);
}

describe("updateStatusBar logic", () => {
  it("sets conn dot to --closed when tasksSource is null", () => {
    const { ctx, elements } = makeStatusBarContext({ tasksSource: null });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-conn-dot"].className).toContain(
      "status-bar-conn-dot--closed",
    );
    expect(elements["status-bar-conn-label"].textContent).toBe("Disconnected");
  });

  it("sets conn dot to --ok when tasksSource.readyState === 1 (OPEN)", () => {
    const { ctx, elements } = makeStatusBarContext({
      tasksSource: { readyState: 1 },
    });
    loadStatusBar(ctx);
    ctx.updateStatusBar();
    expect(elements["status-bar-conn-dot"].className).toContain(
      "status-bar-conn-dot--ok",
    );
    expect(elements["status-bar-conn-label"].textContent).toBe("Connected");
  });

  it("sets conn dot to --reconnecting when tasksSource.readyState === 0 (CONNECTING)", () => {
    const { ctx, elements } = makeStatusBarContext({
      tasksSource: { readyState: 0 },
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
      tasks: [
        { status: "waiting" },
        { status: "failed" },
        { status: "done" },
      ],
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
  it("removes 'hidden' class from panel when initially hidden", () => {
    const { ctx, elements } = makeStatusBarContext();
    loadStatusBar(ctx);
    // panel starts with hidden class
    expect(elements["status-bar-panel"].classList.contains("hidden")).toBe(true);
    ctx.toggleTerminalPanel();
    expect(elements["status-bar-panel"].classList.contains("hidden")).toBe(false);
    expect(elements["status-bar-terminal-btn"]._ariaExpanded).toBe("true");
  });

  it("adds 'hidden' class when panel is visible", () => {
    const { ctx, elements } = makeStatusBarContext();
    loadStatusBar(ctx);
    // Open it first
    ctx.toggleTerminalPanel();
    expect(elements["status-bar-panel"].classList.contains("hidden")).toBe(false);
    // Close it
    ctx.toggleTerminalPanel();
    expect(elements["status-bar-panel"].classList.contains("hidden")).toBe(true);
    expect(elements["status-bar-terminal-btn"]._ariaExpanded).toBe("false");
  });
});
