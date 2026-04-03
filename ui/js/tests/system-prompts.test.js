import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeElement(overrides = {}) {
  const el = {
    textContent: "",
    innerHTML: "",
    value: "",
    disabled: false,
    style: {
      cssText: "",
      background: "",
      borderColor: "",
      opacity: "",
      color: "",
      display: "",
    },
    classList: {
      _set: new Set(["hidden"]),
      contains: (c) => el.classList._set.has(c),
      add: (c) => el.classList._set.add(c),
      remove: (c) => el.classList._set.delete(c),
    },
    dataset: {},
    type: "",
    appendChild: vi.fn(),
    querySelectorAll: vi.fn().mockReturnValue([]),
    querySelector: vi.fn().mockReturnValue(null),
    ...overrides,
  };
  return el;
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const clickListeners = [];

  const ctx = {
    console: { error: vi.fn(), log: vi.fn() },
    JSON,
    Array,
    String,
    encodeURIComponent,
    setTimeout: vi.fn(),
    api: overrides.api || vi.fn().mockResolvedValue({}),
    escapeHtml: (s) => String(s),
    closeSettings: vi.fn(),
    bindModalDismiss: vi.fn().mockReturnValue(vi.fn()),
    switchEditTab: vi.fn(),
    showConfirm: overrides.showConfirm || vi.fn().mockResolvedValue(true),
    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: (tag) => {
        const el = makeElement({ tagName: tag });
        el.appendChild = vi.fn();
        return el;
      },
      addEventListener: vi.fn((type, fn) => {
        if (type === "click") clickListeners.push(fn);
      }),
    },
    _clickListeners: clickListeners,
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "system-prompts.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "system-prompts.js") });
  return ctx;
}

describe("system-prompts.js", () => {
  describe("openSystemPromptsFromSettings", () => {
    it("closes settings and shows editor", async () => {
      const modal = makeElement();
      const list = makeElement();
      const ctx = makeContext({
        elements: [
          ["system-prompts-modal", modal],
          ["system-prompts-list", list],
          ["system-prompts-dir", makeElement()],
        ],
        api: vi
          .fn()
          .mockResolvedValueOnce({ prompts_dir: "/home/.wallfacer/prompts" })
          .mockResolvedValueOnce([]),
      });
      loadScript(ctx);
      const event = { preventDefault: vi.fn(), stopPropagation: vi.fn() };
      // openSystemPromptsFromSettings is async (calls showSystemPromptsEditor)
      await ctx.openSystemPromptsFromSettings(event);
      expect(event.preventDefault).toHaveBeenCalled();
      expect(event.stopPropagation).toHaveBeenCalled();
      expect(ctx.closeSettings).toHaveBeenCalled();
    });
  });

  describe("closeSystemPromptsEditor", () => {
    it("hides modal and resets state", () => {
      const modal = makeElement();
      const list = makeElement();
      const ctx = makeContext({
        elements: [
          ["system-prompts-modal", modal],
          ["system-prompts-list", list],
        ],
      });
      loadScript(ctx);
      ctx.closeSystemPromptsEditor();
      expect(modal.classList._set.has("hidden")).toBe(true);
      expect(list.innerHTML).toBe("");
    });
  });

  describe("selectSystemPrompt", () => {
    it("updates name label and content textarea", async () => {
      const modal = makeElement();
      const list = makeElement();
      const nameLabel = makeElement();
      const textarea = makeElement();
      const statusEl = makeElement();
      const resetBtn = makeElement();

      // Simulate buttons in the list
      const btn1 = makeElement({ dataset: { name: "title" } });
      const btn2 = makeElement({ dataset: { name: "commit" } });
      list.querySelectorAll = vi.fn().mockReturnValue([btn1, btn2]);

      const ctx = makeContext({
        elements: [
          ["system-prompts-modal", modal],
          ["system-prompts-list", list],
          ["system-prompts-dir", makeElement()],
          ["system-prompt-name-label", nameLabel],
          ["system-prompt-content", textarea],
          ["system-prompt-status", statusEl],
          ["system-prompt-reset-btn", resetBtn],
        ],
        api: vi
          .fn()
          .mockResolvedValueOnce({}) // config
          .mockResolvedValueOnce([
            { name: "title", has_override: true, content: "title template" },
            { name: "commit", has_override: false, content: "commit template" },
          ]),
      });
      loadScript(ctx);

      // Load prompts first
      await ctx.showSystemPromptsEditor();

      // Now select a prompt
      ctx.selectSystemPrompt("title");
      expect(nameLabel.textContent).toContain("title");
      expect(nameLabel.textContent).toContain("override active");
      expect(textarea.value).toBe("title template");
      expect(resetBtn.disabled).toBe(false);
    });
  });

  describe("saveSystemPrompt", () => {
    it("saves prompt override via API", async () => {
      const modal = makeElement();
      const list = makeElement();
      const nameLabel = makeElement();
      const textarea = makeElement({ value: "new content" });
      const statusEl = makeElement();
      const resetBtn = makeElement();

      const apiMock = vi
        .fn()
        .mockResolvedValueOnce({}) // config
        .mockResolvedValueOnce([
          { name: "title", has_override: false, content: "old" },
        ])
        .mockResolvedValueOnce({}) // save
        .mockResolvedValueOnce([
          { name: "title", has_override: true, content: "new content" },
        ]); // reload

      const ctx = makeContext({
        elements: [
          ["system-prompts-modal", modal],
          ["system-prompts-list", list],
          ["system-prompts-dir", makeElement()],
          ["system-prompt-name-label", nameLabel],
          ["system-prompt-content", textarea],
          ["system-prompt-status", statusEl],
          ["system-prompt-reset-btn", resetBtn],
        ],
        api: apiMock,
      });
      loadScript(ctx);

      await ctx.showSystemPromptsEditor();
      ctx.selectSystemPrompt("title");
      // Override textarea after selection (selectSystemPrompt sets it to template content)
      textarea.value = "new content";
      await ctx.saveSystemPrompt();

      expect(apiMock).toHaveBeenCalledWith("/api/system-prompts/title", {
        method: "PUT",
        body: JSON.stringify({ content: "new content" }),
      });
    });

    it("does nothing when no prompt selected", async () => {
      const ctx = makeContext();
      loadScript(ctx);
      await ctx.saveSystemPrompt();
      expect(ctx.api).not.toHaveBeenCalled();
    });
  });

  describe("resetSystemPromptToDefault", () => {
    it("deletes override on confirm", async () => {
      const modal = makeElement();
      const list = makeElement();
      const nameLabel = makeElement();
      const textarea = makeElement();
      const statusEl = makeElement();
      const resetBtn = makeElement();

      const apiMock = vi
        .fn()
        .mockResolvedValueOnce({}) // config
        .mockResolvedValueOnce([
          { name: "title", has_override: true, content: "custom" },
        ])
        .mockResolvedValueOnce({}) // delete
        .mockResolvedValueOnce([
          { name: "title", has_override: false, content: "default" },
        ]); // reload

      const ctx = makeContext({
        elements: [
          ["system-prompts-modal", modal],
          ["system-prompts-list", list],
          ["system-prompts-dir", makeElement()],
          ["system-prompt-name-label", nameLabel],
          ["system-prompt-content", textarea],
          ["system-prompt-status", statusEl],
          ["system-prompt-reset-btn", resetBtn],
        ],
        api: apiMock,
        showConfirm: vi.fn().mockResolvedValue(true),
      });
      loadScript(ctx);

      await ctx.showSystemPromptsEditor();
      ctx.selectSystemPrompt("title");
      await ctx.resetSystemPromptToDefault();

      expect(apiMock).toHaveBeenCalledWith("/api/system-prompts/title", {
        method: "DELETE",
      });
    });

    it("does nothing when user cancels", async () => {
      const modal = makeElement();
      const list = makeElement();
      const nameLabel = makeElement();
      const textarea = makeElement();
      const statusEl = makeElement();
      const resetBtn = makeElement();

      const apiMock = vi
        .fn()
        .mockResolvedValueOnce({}) // config
        .mockResolvedValueOnce([
          { name: "title", has_override: true, content: "custom" },
        ]);

      const ctx = makeContext({
        elements: [
          ["system-prompts-modal", modal],
          ["system-prompts-list", list],
          ["system-prompts-dir", makeElement()],
          ["system-prompt-name-label", nameLabel],
          ["system-prompt-content", textarea],
          ["system-prompt-status", statusEl],
          ["system-prompt-reset-btn", resetBtn],
        ],
        api: apiMock,
        showConfirm: vi.fn().mockResolvedValue(false),
      });
      loadScript(ctx);

      await ctx.showSystemPromptsEditor();
      ctx.selectSystemPrompt("title");
      await ctx.resetSystemPromptToDefault();

      // Only the config + loadSystemPrompts calls, no DELETE
      expect(apiMock).toHaveBeenCalledTimes(2);
    });
  });

  describe("document click dismiss", () => {
    it("registers a click listener for outside-click dismiss", () => {
      const ctx = makeContext({
        elements: [["system-prompts-modal", makeElement()]],
      });
      loadScript(ctx);
      expect(ctx.document.addEventListener).toHaveBeenCalledWith(
        "click",
        expect.any(Function),
      );
    });
  });
});
