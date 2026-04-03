import { describe, it, expect, vi } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

describe("keyboard-shortcuts.js", () => {
  it("creates modal controller and exposes open/close functions", () => {
    const mockCtrl = { open: vi.fn(), close: vi.fn() };
    const ctx = vm.createContext({
      createModalController: vi.fn().mockReturnValue(mockCtrl),
    });
    const code = readFileSync(join(jsDir, "keyboard-shortcuts.js"), "utf8");
    vm.runInContext(code, ctx);

    expect(ctx.createModalController).toHaveBeenCalledWith(
      "keyboard-shortcuts-modal",
    );
    expect(ctx.openKeyboardShortcuts).toBe(mockCtrl.open);
    expect(ctx.closeKeyboardShortcuts).toBe(mockCtrl.close);
  });

  it("openKeyboardShortcuts calls controller open", () => {
    const mockCtrl = { open: vi.fn(), close: vi.fn() };
    const ctx = vm.createContext({
      createModalController: vi.fn().mockReturnValue(mockCtrl),
    });
    const code = readFileSync(join(jsDir, "keyboard-shortcuts.js"), "utf8");
    vm.runInContext(code, ctx);
    ctx.openKeyboardShortcuts();
    expect(mockCtrl.open).toHaveBeenCalled();
  });

  it("closeKeyboardShortcuts calls controller close", () => {
    const mockCtrl = { open: vi.fn(), close: vi.fn() };
    const ctx = vm.createContext({
      createModalController: vi.fn().mockReturnValue(mockCtrl),
    });
    const code = readFileSync(join(jsDir, "keyboard-shortcuts.js"), "utf8");
    vm.runInContext(code, ctx);
    ctx.closeKeyboardShortcuts();
    expect(mockCtrl.close).toHaveBeenCalled();
  });
});
