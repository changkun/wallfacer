import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeClassList() {
  const set = new Set();
  return {
    add: (c) => set.add(c),
    remove: (c) => set.delete(c),
    contains: (c) => set.has(c),
    _set: set,
  };
}

function makeElement(overrides = {}) {
  return {
    classList: makeClassList(),
    style: { display: "" },
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    document: {
      getElementById: (id) => elements.get(id) || null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    },
    openModalPanel: vi.fn(),
    closeModalPanel: vi.fn(),
    bindModalDismiss: vi.fn().mockReturnValue(vi.fn()),
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "build/lib/modal-controller.js"), "utf8");
  vm.runInContext(code, ctx);
  return ctx;
}

describe("lib/modal-controller.js", () => {
  describe("createModalController", () => {
    it("returns open and close functions", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const ctrl = ctx.createModalController("test-modal");
      expect(typeof ctrl.open).toBe("function");
      expect(typeof ctrl.close).toBe("function");
    });

    it("open shows modal and binds dismiss", () => {
      const modal = makeElement();
      const ctx = makeContext({ elements: [["test-modal", modal]] });
      loadScript(ctx);
      const ctrl = ctx.createModalController("test-modal");
      ctrl.open();
      expect(ctx.openModalPanel).toHaveBeenCalledWith(modal);
      expect(ctx.bindModalDismiss).toHaveBeenCalledWith(
        modal,
        expect.any(Function),
      );
    });

    it("close hides modal and calls dismiss", () => {
      const modal = makeElement();
      const dismiss = vi.fn();
      const ctx = makeContext({
        elements: [["test-modal", modal]],
        bindModalDismiss: vi.fn().mockReturnValue(dismiss),
      });
      loadScript(ctx);
      const ctrl = ctx.createModalController("test-modal");
      ctrl.open();
      ctrl.close();
      expect(ctx.closeModalPanel).toHaveBeenCalledWith(modal);
      expect(dismiss).toHaveBeenCalled();
    });

    it("open does nothing when modal not found", () => {
      const ctx = makeContext({ elements: [] });
      loadScript(ctx);
      const ctrl = ctx.createModalController("missing-modal");
      ctrl.open(); // should not throw
      expect(ctx.openModalPanel).not.toHaveBeenCalled();
    });

    it("calls onOpen callback", () => {
      const modal = makeElement();
      const onOpen = vi.fn();
      const ctx = makeContext({ elements: [["test-modal", modal]] });
      loadScript(ctx);
      const ctrl = ctx.createModalController("test-modal", { onOpen });
      ctrl.open();
      expect(onOpen).toHaveBeenCalled();
    });

    it("calls onClose callback", () => {
      const modal = makeElement();
      const onClose = vi.fn();
      const ctx = makeContext({ elements: [["test-modal", modal]] });
      loadScript(ctx);
      const ctrl = ctx.createModalController("test-modal", { onClose });
      ctrl.close();
      expect(onClose).toHaveBeenCalled();
    });

    it("re-binds dismiss on second open", () => {
      const modal = makeElement();
      const dismiss1 = vi.fn();
      const dismiss2 = vi.fn();
      const ctx = makeContext({
        elements: [["test-modal", modal]],
        bindModalDismiss: vi
          .fn()
          .mockReturnValueOnce(dismiss1)
          .mockReturnValueOnce(dismiss2),
      });
      loadScript(ctx);
      const ctrl = ctx.createModalController("test-modal");
      ctrl.open();
      ctrl.open();
      expect(dismiss1).toHaveBeenCalled(); // previous dismiss cleaned up
    });
  });
});
