/**
 * Tests for template manager helpers.
 */
import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function createClassList() {
  const set = new Set();
  return {
    add: (cls) => set.add(cls),
    remove: (cls) => set.delete(cls),
    contains: (cls) => set.has(cls),
  };
}

function createElement(overrides = {}) {
  return {
    classList: createClassList(),
    style: {},
    appendChild: vi.fn(),
    textContent: "",
    innerHTML: "",
    onclick: null,
    onmouseenter: null,
    onmouseleave: null,
    ...overrides,
  };
}

function makeContext(overrides = {}) {
  const elements = new Map(overrides.elements || []);
  const ctx = {
    console: {
      error: vi.fn(),
      log: () => {},
      warn: () => {},
    },
    Date,
    Math,
    Promise,
    alert: vi.fn(),
    showAlert: vi.fn(),
    window: {
      alert: vi.fn(),
    },
    document: {
      getElementById: (id) => elements.get(id) || null,
      createElement: () => createElement(),
      body: { appendChild: () => {} },
      addEventListener: () => {},
      querySelector: () => null,
    },
    ...overrides,
  };
  return vm.createContext(ctx);
}

function loadScript(ctx, filename) {
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

describe("openTemplatesManagerFromSettings", () => {
  it("closes settings, prevents default, and opens templates manager", async () => {
    const settingsModal = createElement({ id: "settings-modal" });
    const calls = [];
    const ctx = makeContext({
      elements: [["settings-modal", settingsModal]],
    });
    loadScript(ctx, "templates.js");

    // Rebind these helpers after load so we can assert call order.
    vm.runInContext(
      `closeSettings = function() {
         calls.push('close');
         return;
       };
       openTemplatesManager = function() {
         calls.push('open');
         return Promise.resolve();
       };`,
      Object.assign(ctx, { calls }),
    );

    vm.runInContext(
      'openTemplatesManagerFromSettings({ preventDefault: function() { calls.push("pd"); } });',
      ctx,
    );
    await Promise.resolve();

    expect(calls).toEqual(["pd", "close", "open"]);
  });

  it("alerts and logs when opening templates manager fails", async () => {
    const calls = [];
    const settingsModal = createElement({ id: "settings-modal" });
    const ctx = makeContext({
      elements: [["settings-modal", settingsModal]],
      closeSettings: vi.fn(() => calls.push("close")),
    });
    loadScript(ctx, "templates.js");
    vm.runInContext(
      `openTemplatesManager = function() {
         calls.push('open');
         return Promise.reject(new Error('network down'));
       };`,
      Object.assign(ctx, { calls }),
    );
    ctx.openTemplatesManagerFromSettings();
    // Flush microtasks: rejection + .catch() handler need multiple ticks.
    await new Promise((r) => setTimeout(r, 10));

    expect(calls).toEqual(["close", "open"]); // sync path executes both
    expect(ctx.closeSettings).toHaveBeenCalledTimes(1);
    expect(ctx.showAlert).toHaveBeenCalledWith(
      "Failed to open Templates: network down",
    );
    expect(ctx.console.error).toHaveBeenCalledWith(
      "Failed to open templates manager:",
      expect.anything(),
    );
    const loggedError = ctx.console.error.mock.calls[0][1];
    expect(String((loggedError && loggedError.message) || loggedError)).toBe(
      "network down",
    );
  });

  it("opens templates manager even when closeSettings is unavailable", async () => {
    const settingsModal = createElement({ id: "settings-modal" });
    const calls = [];
    const ctx = makeContext({
      elements: [["settings-modal", settingsModal]],
      alert: vi.fn(),
    });
    loadScript(ctx, "templates.js");
    vm.runInContext(
      `openTemplatesManager = function() {
         calls.push('open');
         return Promise.resolve();
       };`,
      Object.assign(ctx, { calls }),
    );

    ctx.openTemplatesManagerFromSettings();
    await Promise.resolve();

    expect(calls).toEqual(["open"]);
  });
});

describe("closeTemplatesPicker", () => {
  it("removes picker element and cleans up listeners", () => {
    const ctx = makeContext({
      document: {
        getElementById: () => null,
        createElement: () => createElement(),
        body: { appendChild: () => {} },
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        querySelector: () => null,
      },
    });
    loadScript(ctx, "templates.js");
    // Initially no picker, should be safe to call
    ctx.closeTemplatesPicker();

    // Set up a mock picker element
    const mockPicker = { remove: vi.fn() };
    vm.runInContext(
      "_templatesPickerEl = mockPicker;",
      Object.assign(ctx, { mockPicker }),
    );
    ctx.closeTemplatesPicker();
    expect(mockPicker.remove).toHaveBeenCalled();
  });
});

describe("closeTemplatesManager", () => {
  it("hides the modal", () => {
    const modal = createElement({ id: "templates-manager-modal" });
    modal.style = { display: "flex" };
    const ctx = makeContext({
      elements: [["templates-manager-modal", modal]],
    });
    loadScript(ctx, "templates.js");
    ctx.closeTemplatesManager();
    expect(modal.classList.contains("hidden")).toBe(true);
    expect(modal.style.display).toBe("");
  });

  it("does nothing when modal is missing", () => {
    const ctx = makeContext({ elements: [] });
    loadScript(ctx, "templates.js");
    ctx.closeTemplatesManager(); // should not throw
  });
});

describe("saveNewTemplate", () => {
  it("validates name and body are required", async () => {
    const nameInput = { value: "" };
    const bodyInput = { value: "" };
    const statusEl = { textContent: "" };
    const ctx = makeContext({
      JSON,
      setTimeout: vi.fn(),
      api: vi.fn(),
      showConfirm: vi.fn(),
      elements: [
        ["tmpl-new-name", nameInput],
        ["tmpl-new-body", bodyInput],
        ["tmpl-add-status", statusEl],
      ],
    });
    loadScript(ctx, "templates.js");
    await ctx.saveNewTemplate();
    expect(statusEl.textContent).toBe("Name and body are required.");
    expect(ctx.api).not.toHaveBeenCalled();
  });

  it("saves template via API on valid input", async () => {
    const nameInput = { value: "My Template" };
    const bodyInput = { value: "Template body" };
    const statusEl = { textContent: "" };
    const listEl = createElement();
    listEl.innerHTML = "";
    const apiMock = vi
      .fn()
      .mockResolvedValueOnce({}) // save
      .mockResolvedValueOnce([]); // refresh list
    const ctx = makeContext({
      JSON,
      setTimeout: vi.fn(),
      api: apiMock,
      showConfirm: vi.fn(),
      elements: [
        ["tmpl-new-name", nameInput],
        ["tmpl-new-body", bodyInput],
        ["tmpl-add-status", statusEl],
        ["tmpl-list", listEl],
      ],
    });
    loadScript(ctx, "templates.js");
    await ctx.saveNewTemplate();
    expect(apiMock).toHaveBeenCalledWith("/api/templates", {
      method: "POST",
      body: JSON.stringify({ name: "My Template", body: "Template body" }),
    });
    expect(nameInput.value).toBe("");
    expect(bodyInput.value).toBe("");
    expect(statusEl.textContent).toBe("Saved.");
  });

  it("shows error on API failure", async () => {
    const nameInput = { value: "Name" };
    const bodyInput = { value: "Body" };
    const statusEl = { textContent: "" };
    const ctx = makeContext({
      JSON,
      setTimeout: vi.fn(),
      api: vi.fn().mockRejectedValue(new Error("server error")),
      showConfirm: vi.fn(),
      elements: [
        ["tmpl-new-name", nameInput],
        ["tmpl-new-body", bodyInput],
        ["tmpl-add-status", statusEl],
      ],
    });
    loadScript(ctx, "templates.js");
    await ctx.saveNewTemplate();
    expect(statusEl.textContent).toBe("Error: server error");
  });
});

describe("_deleteTemplate", () => {
  it("deletes template on confirm", async () => {
    const listEl = createElement();
    listEl.innerHTML = "";
    const apiMock = vi
      .fn()
      .mockResolvedValueOnce({}) // delete
      .mockResolvedValueOnce([]); // refresh
    const ctx = makeContext({
      JSON,
      setTimeout: vi.fn(),
      api: apiMock,
      showConfirm: vi.fn().mockResolvedValue(true),
      elements: [["tmpl-list", listEl]],
    });
    loadScript(ctx, "templates.js");
    await ctx._deleteTemplate("tmpl-123");
    expect(apiMock).toHaveBeenCalledWith("/api/templates/tmpl-123", {
      method: "DELETE",
    });
  });

  it("does nothing when user cancels", async () => {
    const ctx = makeContext({
      JSON,
      setTimeout: vi.fn(),
      api: vi.fn(),
      showConfirm: vi.fn().mockResolvedValue(false),
    });
    loadScript(ctx, "templates.js");
    await ctx._deleteTemplate("tmpl-123");
    expect(ctx.api).not.toHaveBeenCalled();
  });
});

describe("_refreshTemplatesList", () => {
  it("renders template rows", async () => {
    const listEl = createElement();
    listEl.innerHTML = "";
    const children = [];
    listEl.appendChild = vi.fn((el) => children.push(el));
    const ctx = makeContext({
      JSON,
      setTimeout: vi.fn(),
      api: vi.fn().mockResolvedValue([
        { id: "1", name: "Template 1", body: "Body 1" },
        { id: "2", name: "Template 2", body: "Body 2" },
      ]),
      showConfirm: vi.fn(),
      elements: [["tmpl-list", listEl]],
    });
    loadScript(ctx, "templates.js");
    await ctx._refreshTemplatesList();
    expect(children.length).toBe(2);
  });

  it("shows empty message when no templates", async () => {
    const listEl = createElement();
    listEl.innerHTML = "";
    const ctx = makeContext({
      JSON,
      setTimeout: vi.fn(),
      api: vi.fn().mockResolvedValue([]),
      showConfirm: vi.fn(),
      elements: [["tmpl-list", listEl]],
    });
    loadScript(ctx, "templates.js");
    await ctx._refreshTemplatesList();
    expect(listEl.innerHTML).toContain("No templates yet");
  });

  it("shows error on API failure", async () => {
    const listEl = createElement();
    listEl.innerHTML = "";
    const ctx = makeContext({
      JSON,
      setTimeout: vi.fn(),
      api: vi.fn().mockRejectedValue(new Error("fail")),
      showConfirm: vi.fn(),
      elements: [["tmpl-list", listEl]],
    });
    loadScript(ctx, "templates.js");
    await ctx._refreshTemplatesList();
    expect(listEl.innerHTML).toContain("Error loading");
  });
});

describe("openTemplatesPicker", () => {
  it("fetches templates and renders picker", async () => {
    const textarea = {
      getBoundingClientRect: () => ({ bottom: 100, left: 50 }),
      focus: vi.fn(),
      value: "",
    };
    const createdEls = [];
    const ctx = makeContext({
      JSON,
      setTimeout: vi.fn(),
      api: vi.fn().mockResolvedValue([{ name: "T1", body: "body1" }]),
      showConfirm: vi.fn(),
      elements: [["new-prompt", textarea]],
      window: { scrollY: 0, scrollX: 0 },
      document: {
        getElementById: (id) => {
          if (id === "new-prompt") return textarea;
          return null;
        },
        createElement: () => {
          const el = createElement({
            appendChild: vi.fn(),
            focus: vi.fn(),
            addEventListener: vi.fn(),
          });
          el.style = { cssText: "" };
          el.type = "";
          el.placeholder = "";
          el.className = "";
          el.id = "";
          el.textContent = "";
          el.innerHTML = "";
          el.onmouseenter = null;
          el.onmouseleave = null;
          el.onclick = null;
          createdEls.push(el);
          return el;
        },
        body: { appendChild: vi.fn() },
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        querySelector: () => null,
      },
    });
    loadScript(ctx, "templates.js");
    const onSelect = vi.fn();
    await ctx.openTemplatesPicker(onSelect);
    expect(ctx.api).toHaveBeenCalledWith("/api/templates");
  });
});
