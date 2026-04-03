import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeClassList() {
  const set = new Set();
  return {
    add: (c) => set.add(c),
    remove: (c) => set.delete(c),
    contains: (c) => set.has(c),
    toggle: (c, force) => (force ? set.add(c) : set.delete(c)),
    _set: set,
  };
}

function makeElement(overrides = {}) {
  return {
    classList: makeClassList(),
    innerHTML: "",
    textContent: "",
    value: "",
    style: { cssText: "", display: "" },
    focus: vi.fn(),
    select: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    appendChild: vi.fn(),
    querySelector: vi.fn().mockReturnValue(null),
    onclick: null,
    onkeydown: null,
    scrollIntoView: vi.fn(),
    dataset: {},
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console,
    Promise,
    String,
    Array,
    JSON,
    parseInt,
    IntersectionObserver: class {
      constructor() {}
      observe() {}
    },
    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: (tag) => makeElement({ tagName: tag }),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      readyState: "complete",
      querySelectorAll: vi.fn().mockReturnValue({ forEach: vi.fn() }),
    },
    fetch: vi.fn().mockResolvedValue({ json: vi.fn().mockResolvedValue({}) }),
    apiGet: vi.fn().mockResolvedValue({}),
    escapeHtml: (s) => String(s),
    bindModalDismiss: vi.fn().mockReturnValue(vi.fn()),
    getComputedStyle: () => ({ getPropertyValue: () => "" }),
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  loadLibDeps("utils.js", ctx);
  const code = readFileSync(join(jsDir, "utils.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "utils.js") });
  return ctx;
}

describe("utils.js additional coverage", () => {
  describe("showConfirm", () => {
    it("resolves true when OK clicked", async () => {
      const modal = makeElement();
      const confirmBtn = makeElement();
      const cancelBtn = makeElement();
      const msgEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["confirm-modal", modal],
          ["confirm-message", msgEl],
          ["confirm-ok-btn", confirmBtn],
          ["confirm-cancel-btn", cancelBtn],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      const promise = ctx.showConfirm("Are you sure?");
      expect(msgEl.textContent).toBe("Are you sure?");
      // Simulate OK click
      confirmBtn.onclick();
      const result = await promise;
      expect(result).toBe(true);
      expect(modal.classList._set.has("hidden")).toBe(true);
    });

    it("resolves false when Cancel clicked", async () => {
      const modal = makeElement();
      const confirmBtn = makeElement();
      const cancelBtn = makeElement();
      const ctx = makeContext({
        elements: [
          ["confirm-modal", modal],
          ["confirm-message", makeElement()],
          ["confirm-ok-btn", confirmBtn],
          ["confirm-cancel-btn", cancelBtn],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      const promise = ctx.showConfirm("Sure?");
      cancelBtn.onclick();
      expect(await promise).toBe(false);
    });
  });

  describe("showPrompt", () => {
    it("resolves with input value on OK", async () => {
      const modal = makeElement();
      const input = makeElement({ value: "hello" });
      const msgEl = makeElement();
      const okBtn = makeElement();
      const cancelBtn = makeElement();
      const ctx = makeContext({
        elements: [
          ["prompt-modal", modal],
          ["prompt-message", msgEl],
          ["prompt-input", input],
          ["prompt-ok-btn", okBtn],
          ["prompt-cancel-btn", cancelBtn],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      const promise = ctx.showPrompt("Enter name:", "default");
      expect(input.value).toBe("default");
      input.value = "typed";
      okBtn.onclick();
      expect(await promise).toBe("typed");
    });

    it("resolves null on cancel", async () => {
      const modal = makeElement();
      const input = makeElement();
      const cancelBtn = makeElement();
      const ctx = makeContext({
        elements: [
          ["prompt-modal", modal],
          ["prompt-message", makeElement()],
          ["prompt-input", input],
          ["prompt-ok-btn", makeElement()],
          ["prompt-cancel-btn", cancelBtn],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      const promise = ctx.showPrompt("Enter:");
      cancelBtn.onclick();
      expect(await promise).toBeNull();
    });

    it("resolves on Enter key in input", async () => {
      const modal = makeElement();
      const input = makeElement({ value: "val" });
      const ctx = makeContext({
        elements: [
          ["prompt-modal", modal],
          ["prompt-message", makeElement()],
          ["prompt-input", input],
          ["prompt-ok-btn", makeElement()],
          ["prompt-cancel-btn", makeElement()],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      const promise = ctx.showPrompt("Enter:");
      // Set value after showPrompt (which resets it to defaultValue)
      input.value = "val";
      input.onkeydown({ key: "Enter", preventDefault: vi.fn() });
      expect(await promise).toBe("val");
    });
  });

  describe("loadJsonEndpoint", () => {
    it("calls setState loading then onSuccess", async () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
        apiGet: vi.fn().mockResolvedValue({ data: 1 }),
      });
      loadScript(ctx);
      const setState = vi.fn();
      const onSuccess = vi.fn();
      await ctx.loadJsonEndpoint("/test", onSuccess, setState);
      expect(setState).toHaveBeenCalledWith("loading");
      expect(onSuccess).toHaveBeenCalledWith({ data: 1 });
    });

    it("calls setState error on failure", async () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
        apiGet: vi.fn().mockRejectedValue(new Error("fail")),
      });
      loadScript(ctx);
      const setState = vi.fn();
      await ctx.loadJsonEndpoint("/test", vi.fn(), setState);
      expect(setState).toHaveBeenCalledWith("error", expect.any(String));
    });
  });

  describe("createHoverRow", () => {
    it("creates tr with td cells", () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      const tr = ctx.createHoverRow([
        { text: "A", style: "padding:4px;" },
        { html: "<b>B</b>" },
        { text: null },
      ]);
      expect(tr.appendChild).toHaveBeenCalledTimes(3);
    });
  });

  describe("appendRowsToTbody", () => {
    it("appends hover rows to tbody element", () => {
      const tbody = makeElement();
      const ctx = makeContext({
        elements: [
          ["my-tbody", tbody],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      ctx.appendRowsToTbody("my-tbody", [[{ text: "A" }], [{ text: "B" }]]);
      expect(tbody.appendChild).toHaveBeenCalledTimes(2);
    });

    it("accepts element directly", () => {
      const tbody = makeElement();
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      ctx.appendRowsToTbody(tbody, [[{ text: "X" }]]);
      expect(tbody.appendChild).toHaveBeenCalled();
    });
  });

  describe("appendNoDataRow", () => {
    it("adds a row with message", () => {
      const tbody = makeElement();
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      ctx.appendNoDataRow(tbody, 5, "No items");
      expect(tbody.appendChild).toHaveBeenCalled();
    });
  });

  describe("closeFirstVisibleModal", () => {
    it("closes first visible modal", () => {
      const modal = makeElement();
      modal.classList._set.delete("hidden");
      const closeFn = vi.fn();
      const ctx = makeContext({
        elements: [
          ["my-modal", modal],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      const result = ctx.closeFirstVisibleModal([
        { id: "my-modal", close: closeFn },
      ]);
      expect(result).toBe(true);
      expect(closeFn).toHaveBeenCalled();
    });

    it("returns false when all modals hidden", () => {
      const modal = makeElement();
      modal.classList.add("hidden");
      const ctx = makeContext({
        elements: [
          ["my-modal", modal],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      const result = ctx.closeFirstVisibleModal([
        { id: "my-modal", close: vi.fn() },
      ]);
      expect(result).toBe(false);
    });
  });

  describe("taskDisplayPrompt", () => {
    it("returns execution_prompt for idea-agent", () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      expect(
        ctx.taskDisplayPrompt({
          kind: "idea-agent",
          execution_prompt: "exec",
          prompt: "orig",
        }),
      ).toBe("exec");
    });

    it("returns prompt for regular tasks", () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      expect(ctx.taskDisplayPrompt({ prompt: "hello" })).toBe("hello");
    });
  });

  describe("getTaskAccessibleTitle", () => {
    it("returns title when present", () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      expect(
        ctx.getTaskAccessibleTitle({ title: "My Task", prompt: "p" }),
      ).toBe("My Task");
    });

    it("returns truncated prompt when no title", () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      const longPrompt = "x".repeat(80);
      const result = ctx.getTaskAccessibleTitle({ prompt: longPrompt });
      expect(result).toHaveLength(61); // 60 chars + ellipsis
    });

    it("returns id as fallback", () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      expect(ctx.getTaskAccessibleTitle({ id: "abc-123" })).toBe("abc-123");
    });
  });

  describe("formatTaskStatusLabel", () => {
    it("replaces underscores with spaces", () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      expect(ctx.formatTaskStatusLabel("in_progress")).toBe("in progress");
    });

    it("handles null", () => {
      const ctx = makeContext({
        elements: [
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      expect(ctx.formatTaskStatusLabel(null)).toBe("");
    });
  });

  describe("announceBoardStatus", () => {
    it("sets announcer text", () => {
      const announcer = makeElement();
      const ctx = makeContext({
        elements: [
          ["board-announcer", announcer],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      ctx.announceBoardStatus("Task created");
      expect(announcer.textContent).toBe("Task created");
    });
  });

  describe("scrollToColumn", () => {
    it("scrolls element into view", () => {
      const el = makeElement();
      const ctx = makeContext({
        elements: [
          ["col-wrapper-backlog", el],
          ["alert-modal", makeElement()],
          ["alert-message", makeElement()],
          ["alert-ok-btn", makeElement()],
        ],
      });
      loadScript(ctx);
      ctx.scrollToColumn("col-wrapper-backlog");
      expect(el.scrollIntoView).toHaveBeenCalled();
    });
  });
});
