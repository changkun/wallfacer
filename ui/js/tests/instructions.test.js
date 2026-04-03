import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeElement(overrides = {}) {
  return { textContent: "", value: "", style: {}, ...overrides };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const mockCtrl = {
    open: vi.fn(),
    close: vi.fn(),
  };
  const ctx = {
    console,
    JSON,
    setTimeout: vi.fn(),
    api: overrides.api || vi.fn().mockResolvedValue({}),
    showAlert: vi.fn(),
    showConfirm: overrides.showConfirm || vi.fn().mockResolvedValue(true),
    closeSettings: vi.fn(),
    switchEditTab: vi.fn(),
    createModalController: vi.fn().mockReturnValue(mockCtrl),
    _mockCtrl: mockCtrl,
    document: {
      getElementById: (id) => elements.get(id) || null,
      addEventListener: vi.fn(),
    },
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "instructions.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "instructions.js") });
  return ctx;
}

describe("instructions.js", () => {
  describe("showInstructionsEditor", () => {
    it("opens modal and loads instructions from API", async () => {
      const textarea = makeElement();
      const pathEl = makeElement();
      const statusEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["instructions-content", textarea],
          ["instructions-path", pathEl],
          ["instructions-status", statusEl],
        ],
        api: vi
          .fn()
          .mockResolvedValueOnce({
            instructions_path: "/home/.wallfacer/instructions/abc.md",
          })
          .mockResolvedValueOnce({ content: "# My Instructions" }),
      });
      loadScript(ctx);
      await ctx.showInstructionsEditor(null, null);
      expect(ctx._mockCtrl.open).toHaveBeenCalled();
      expect(textarea.value).toBe("# My Instructions");
      expect(pathEl.textContent).toBe("/home/.wallfacer/instructions/abc.md");
      expect(ctx.switchEditTab).toHaveBeenCalledWith("instructions", "preview");
    });

    it("uses preloaded content when provided", async () => {
      const textarea = makeElement();
      const pathEl = makeElement();
      const statusEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["instructions-content", textarea],
          ["instructions-path", pathEl],
          ["instructions-status", statusEl],
        ],
        api: vi.fn().mockResolvedValue({}),
      });
      loadScript(ctx);
      await ctx.showInstructionsEditor(null, "preloaded text");
      expect(textarea.value).toBe("preloaded text");
      expect(statusEl.textContent).toBe("Re-initialized.");
    });

    it("stops event propagation when event provided", async () => {
      const event = { stopPropagation: vi.fn() };
      const ctx = makeContext({
        elements: [
          ["instructions-content", makeElement()],
          ["instructions-path", makeElement()],
          ["instructions-status", makeElement()],
        ],
        api: vi.fn().mockResolvedValue({}),
      });
      loadScript(ctx);
      await ctx.showInstructionsEditor(event, "content");
      expect(event.stopPropagation).toHaveBeenCalled();
    });

    it("shows error status on API failure", async () => {
      const statusEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["instructions-content", makeElement()],
          ["instructions-path", makeElement()],
          ["instructions-status", statusEl],
        ],
        api: vi
          .fn()
          .mockResolvedValueOnce({}) // config
          .mockRejectedValueOnce(new Error("network")),
      });
      loadScript(ctx);
      await ctx.showInstructionsEditor(null, null);
      expect(statusEl.textContent).toContain("Error loading");
    });
  });

  describe("closeInstructionsEditor", () => {
    it("calls controller close", () => {
      const ctx = makeContext();
      loadScript(ctx);
      ctx.closeInstructionsEditor();
      expect(ctx._mockCtrl.close).toHaveBeenCalled();
    });
  });

  describe("saveInstructions", () => {
    it("saves content via API", async () => {
      const textarea = makeElement({ value: "updated content" });
      const statusEl = makeElement();
      const apiMock = vi.fn().mockResolvedValue({});
      const ctx = makeContext({
        elements: [
          ["instructions-content", textarea],
          ["instructions-status", statusEl],
        ],
        api: apiMock,
      });
      loadScript(ctx);
      await ctx.saveInstructions();
      expect(apiMock).toHaveBeenCalledWith("/api/instructions", {
        method: "PUT",
        body: JSON.stringify({ content: "updated content" }),
      });
      expect(statusEl.textContent).toBe("Saved.");
    });

    it("shows error on save failure", async () => {
      const statusEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["instructions-content", makeElement({ value: "x" })],
          ["instructions-status", statusEl],
        ],
        api: vi.fn().mockRejectedValue(new Error("save failed")),
      });
      loadScript(ctx);
      await ctx.saveInstructions();
      expect(statusEl.textContent).toBe("Error: save failed");
    });
  });

  describe("reinitInstructionsFromEditor", () => {
    it("reinitializes and updates textarea on confirm", async () => {
      const textarea = makeElement();
      const statusEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["instructions-content", textarea],
          ["instructions-status", statusEl],
        ],
        api: vi.fn().mockResolvedValue({ content: "new default" }),
        showConfirm: vi.fn().mockResolvedValue(true),
      });
      loadScript(ctx);
      await ctx.reinitInstructionsFromEditor();
      expect(ctx.api).toHaveBeenCalledWith("/api/instructions/reinit", {
        method: "POST",
      });
      expect(textarea.value).toBe("new default");
      expect(statusEl.textContent).toBe("Re-initialized.");
    });

    it("does nothing when user cancels", async () => {
      const apiMock = vi.fn();
      const ctx = makeContext({
        api: apiMock,
        showConfirm: vi.fn().mockResolvedValue(false),
      });
      loadScript(ctx);
      await ctx.reinitInstructionsFromEditor();
      expect(apiMock).not.toHaveBeenCalled();
    });

    it("shows error on API failure", async () => {
      const statusEl = makeElement();
      const ctx = makeContext({
        elements: [
          ["instructions-content", makeElement()],
          ["instructions-status", statusEl],
        ],
        api: vi.fn().mockRejectedValue(new Error("reinit failed")),
        showConfirm: vi.fn().mockResolvedValue(true),
      });
      loadScript(ctx);
      await ctx.reinitInstructionsFromEditor();
      expect(statusEl.textContent).toBe("Error: reinit failed");
    });
  });
});
