/**
 * Tests for utility alert and layout helpers.
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function createElement(overrides = {}) {
  return {
    classList: {
      add: vi.fn(),
      remove: vi.fn(),
    },
    style: {},
    textContent: "",
    focus: vi.fn(),
    scrollIntoView: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  return vm.createContext({
    console,
    Promise,
    document: {
      getElementById: (id) => elements.get(id) || null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    },
    ...overrides,
  });
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

describe("utils alerts", () => {
  it("opens alert modal with message and closes it", () => {
    const alertMessage = createElement();
    const alertModal = createElement();
    const okButton = createElement();
    const ctx = makeContext({
      elements: [
        ["alert-message", alertMessage],
        ["alert-modal", alertModal],
        ["alert-ok-btn", okButton],
      ],
    });
    loadScript(ctx, "utils.js");

    ctx.showAlert("Need attention");
    expect(alertMessage.textContent).toBe("Need attention");
    expect(alertModal.classList.add).toHaveBeenCalledWith("flex");
    expect(okButton.focus).toHaveBeenCalledTimes(1);
    expect(alertModal.classList.remove).toHaveBeenCalledWith("hidden");

    ctx.closeAlert();
    expect(alertModal.classList.add).toHaveBeenCalledWith("hidden");
    expect(alertModal.classList.remove).toHaveBeenCalledWith("flex");
  });
});

describe("scrollToColumn", () => {
  it("scrolls the column target into view when it exists", () => {
    const target = createElement({
      scrollIntoView: vi.fn(),
    });
    const ctx = makeContext({
      elements: [["col-wrapper-backlog", target]],
    });
    loadScript(ctx, "utils.js");

    ctx.scrollToColumn("col-wrapper-backlog");
    expect(target.scrollIntoView).toHaveBeenCalledWith({
      behavior: "smooth",
      block: "nearest",
      inline: "start",
    });
  });

  it("does nothing when target is missing", () => {
    const ctx = makeContext();
    loadScript(ctx, "utils.js");
    expect(() => ctx.scrollToColumn("missing")).not.toThrow();
  });
});

describe("showConfirm", () => {
  function makeConfirmContext() {
    const confirmMessage = createElement();
    const confirmModal = createElement();
    const confirmOkBtn = createElement();
    const confirmCancelBtn = createElement();
    return makeContext({
      elements: [
        ["confirm-message", confirmMessage],
        ["confirm-modal", confirmModal],
        ["confirm-ok-btn", confirmOkBtn],
        ["confirm-cancel-btn", confirmCancelBtn],
      ],
    });
  }

  it("resolves true when confirm button is clicked", async () => {
    const ctx = makeConfirmContext();
    loadScript(ctx, "utils.js");

    const promise = ctx.showConfirm("Are you sure?");

    // Simulate clicking the confirm button.
    const okBtn = ctx.document.getElementById("confirm-ok-btn");
    okBtn.onclick();

    const result = await promise;
    expect(result).toBe(true);

    const modal = ctx.document.getElementById("confirm-modal");
    expect(modal.classList.add).toHaveBeenCalledWith("hidden");
  });

  it("resolves false when cancel button is clicked", async () => {
    const ctx = makeConfirmContext();
    loadScript(ctx, "utils.js");

    const promise = ctx.showConfirm("Are you sure?");

    const cancelBtn = ctx.document.getElementById("confirm-cancel-btn");
    cancelBtn.onclick();

    const result = await promise;
    expect(result).toBe(false);
  });

  it("sets the message text", async () => {
    const ctx = makeConfirmContext();
    loadScript(ctx, "utils.js");

    const promise = ctx.showConfirm("Delete everything?");

    const msg = ctx.document.getElementById("confirm-message");
    expect(msg.textContent).toBe("Delete everything?");

    // Clean up.
    const cancelBtn = ctx.document.getElementById("confirm-cancel-btn");
    cancelBtn.onclick();
    await promise;
  });
});

describe("showPrompt", () => {
  function makePromptContext() {
    const promptMessage = createElement();
    const promptInput = createElement({ value: "", select: vi.fn() });
    const promptModal = createElement();
    const promptOkBtn = createElement();
    const promptCancelBtn = createElement();
    return makeContext({
      elements: [
        ["prompt-message", promptMessage],
        ["prompt-input", promptInput],
        ["prompt-modal", promptModal],
        ["prompt-ok-btn", promptOkBtn],
        ["prompt-cancel-btn", promptCancelBtn],
      ],
    });
  }

  it("resolves with input value when OK is clicked", async () => {
    const ctx = makePromptContext();
    loadScript(ctx, "utils.js");

    const promise = ctx.showPrompt("Enter name:", "default");

    const input = ctx.document.getElementById("prompt-input");
    expect(input.value).toBe("default");
    input.value = "my-value";

    const okBtn = ctx.document.getElementById("prompt-ok-btn");
    okBtn.onclick();

    const result = await promise;
    expect(result).toBe("my-value");
  });

  it("resolves null when cancel is clicked", async () => {
    const ctx = makePromptContext();
    loadScript(ctx, "utils.js");

    const promise = ctx.showPrompt("Enter name:");

    const cancelBtn = ctx.document.getElementById("prompt-cancel-btn");
    cancelBtn.onclick();

    const result = await promise;
    expect(result).toBe(null);
  });

  it("resolves with input value on Enter key", async () => {
    const ctx = makePromptContext();
    loadScript(ctx, "utils.js");

    const promise = ctx.showPrompt("Enter value:", "hello");

    const input = ctx.document.getElementById("prompt-input");
    input.value = "submitted";

    // Simulate Enter keydown.
    input.onkeydown({ key: "Enter", preventDefault: vi.fn() });

    const result = await promise;
    expect(result).toBe("submitted");
  });

  it("sets the message text and focuses input", async () => {
    const ctx = makePromptContext();
    loadScript(ctx, "utils.js");

    const promise = ctx.showPrompt("What is your name?", "Alice");

    const msg = ctx.document.getElementById("prompt-message");
    expect(msg.textContent).toBe("What is your name?");

    const input = ctx.document.getElementById("prompt-input");
    expect(input.focus).toHaveBeenCalled();
    expect(input.select).toHaveBeenCalled();

    // Clean up.
    const cancelBtn = ctx.document.getElementById("prompt-cancel-btn");
    cancelBtn.onclick();
    await promise;
  });
});
