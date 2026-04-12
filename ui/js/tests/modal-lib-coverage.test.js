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
    toggle: (c, force) => (force ? set.add(c) : set.delete(c)),
    _set: set,
  };
}

function makeElement(overrides = {}) {
  return {
    classList: makeClassList(),
    style: { display: "" },
    textContent: "",
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    ...overrides,
  };
}

function makeContext() {
  const ctx = {
    document: {
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    },
  };
  return vm.createContext(ctx);
}

function loadScript(ctx) {
  const code = readFileSync(join(jsDir, "build/lib/modal.js"), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, "build/lib/modal.js") });
  return ctx;
}

describe("lib/modal.js", () => {
  describe("openModalPanel", () => {
    it("shows modal by removing hidden class", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const modal = makeElement();
      modal.classList.add("hidden");
      ctx.openModalPanel(modal);
      expect(modal.classList._set.has("hidden")).toBe(false);
      expect(modal.style.display).toBe("flex");
    });

    it("handles null gracefully", () => {
      const ctx = makeContext();
      loadScript(ctx);
      ctx.openModalPanel(null); // should not throw
    });
  });

  describe("closeModalPanel", () => {
    it("hides modal by adding hidden class", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const modal = makeElement();
      ctx.closeModalPanel(modal);
      expect(modal.classList._set.has("hidden")).toBe(true);
      expect(modal.style.display).toBe("");
    });

    it("handles null gracefully", () => {
      const ctx = makeContext();
      loadScript(ctx);
      ctx.closeModalPanel(null);
    });
  });

  describe("bindModalDismiss", () => {
    it("returns unbind function", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const modal = makeElement();
      const onClose = vi.fn();
      const unbind = ctx.bindModalDismiss(modal, onClose);
      expect(typeof unbind).toBe("function");
      expect(modal.addEventListener).toHaveBeenCalledWith(
        "click",
        expect.any(Function),
      );
      expect(ctx.document.addEventListener).toHaveBeenCalledWith(
        "keydown",
        expect.any(Function),
      );
    });

    it("calls onClose on backdrop click", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const modal = makeElement();
      const onClose = vi.fn();
      ctx.bindModalDismiss(modal, onClose);
      // Get the click handler
      const clickHandler = modal.addEventListener.mock.calls.find(
        (c) => c[0] === "click",
      )[1];
      // Simulate clicking the backdrop (target === modal)
      clickHandler({ target: modal });
      expect(onClose).toHaveBeenCalled();
    });

    it("does not call onClose when clicking inside modal", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const modal = makeElement();
      const onClose = vi.fn();
      ctx.bindModalDismiss(modal, onClose);
      const clickHandler = modal.addEventListener.mock.calls.find(
        (c) => c[0] === "click",
      )[1];
      clickHandler({ target: {} }); // not the modal
      expect(onClose).not.toHaveBeenCalled();
    });

    it("calls onClose on Escape key", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const modal = makeElement();
      const onClose = vi.fn();
      ctx.bindModalDismiss(modal, onClose);
      const keyHandler = ctx.document.addEventListener.mock.calls.find(
        (c) => c[0] === "keydown",
      )[1];
      keyHandler({ key: "Escape" });
      expect(onClose).toHaveBeenCalled();
    });

    it("unbind removes listeners", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const modal = makeElement();
      const unbind = ctx.bindModalDismiss(modal, vi.fn());
      unbind();
      expect(modal.removeEventListener).toHaveBeenCalledWith(
        "click",
        expect.any(Function),
      );
      expect(ctx.document.removeEventListener).toHaveBeenCalledWith(
        "keydown",
        expect.any(Function),
      );
    });

    it("returns noop when modal is null", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const unbind = ctx.bindModalDismiss(null, vi.fn());
      expect(typeof unbind).toBe("function");
      unbind(); // should not throw
    });

    it("returns noop when onClose is not a function", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const unbind = ctx.bindModalDismiss(makeElement(), "not-a-fn");
      expect(typeof unbind).toBe("function");
    });
  });

  describe("createModalStateController", () => {
    it("shows loading state", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const loading = makeElement();
      const error = makeElement();
      const empty = makeElement();
      const content = makeElement();
      const setState = ctx.createModalStateController({
        loadingEl: loading,
        errorEl: error,
        emptyEl: empty,
        contentEl: content,
      });
      setState("loading");
      expect(loading.style.display).toBe("flex");
      expect(error.classList._set.has("hidden")).toBe(true);
      expect(empty.classList._set.has("hidden")).toBe(true);
      expect(content.classList._set.has("hidden")).toBe(true);
    });

    it("shows error state with message", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const loading = makeElement();
      const error = makeElement();
      const content = makeElement();
      const setState = ctx.createModalStateController({
        loadingEl: loading,
        errorEl: error,
        contentEl: content,
      });
      setState("error", "Something failed");
      expect(loading.style.display).toBe("none");
      expect(error.classList._set.has("hidden")).toBe(false);
      expect(error.textContent).toBe("Something failed");
    });

    it("shows empty state", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const empty = makeElement();
      const content = makeElement();
      const setState = ctx.createModalStateController({
        emptyEl: empty,
        contentEl: content,
      });
      setState("empty");
      expect(empty.classList._set.has("hidden")).toBe(false);
      expect(content.classList._set.has("hidden")).toBe(true);
    });

    it("shows content state", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const loading = makeElement();
      const content = makeElement();
      const setState = ctx.createModalStateController({
        loadingEl: loading,
        contentEl: content,
      });
      setState("content");
      expect(loading.style.display).toBe("none");
      expect(content.classList._set.has("hidden")).toBe(false);
    });

    it("supports custom content state name", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const content = makeElement();
      const setState = ctx.createModalStateController({
        contentEl: content,
        contentState: "table",
      });
      setState("table");
      expect(content.classList._set.has("hidden")).toBe(false);
    });

    it("shows error default message", () => {
      const ctx = makeContext();
      loadScript(ctx);
      const error = makeElement();
      const setState = ctx.createModalStateController({ errorEl: error });
      setState("error");
      expect(error.textContent).toBe("Unknown error");
    });
  });
});
