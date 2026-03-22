/**
 * Tests for modal-ansi.js — ANSI terminal escape code rendering.
 */
import { describe, it, expect, beforeAll } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext(extra = {}) {
  return vm.createContext({ console, Math, Date, ...extra });
}

function loadScript(filename, ctx) {
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

describe("ansiToHtml", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("modal-ansi.js", ctx);
  });

  it("returns plain text unchanged when there are no ANSI codes", () => {
    expect(ctx.ansiToHtml("hello world")).toBe("hello world");
  });

  it("escapes HTML special characters in plain text (& < > but not quotes)", () => {
    // The internal esc() only escapes & < > — not " — since this is terminal
    // output where quote-escaping is not needed for HTML safety.
    expect(ctx.ansiToHtml("<b>&</b>")).toBe("&lt;b&gt;&amp;&lt;/b&gt;");
  });

  it("wraps bold text in a span with font-weight:bold", () => {
    const result = ctx.ansiToHtml("\x1b[1mBold\x1b[0m");
    expect(result).toContain('<span style="font-weight:bold;">Bold</span>');
  });

  it("wraps dim text in a span with opacity", () => {
    const result = ctx.ansiToHtml("\x1b[2mDim\x1b[0m");
    expect(result).toContain('<span style="opacity:0.6;">Dim</span>');
  });

  it("wraps italic text in a span with font-style:italic", () => {
    const result = ctx.ansiToHtml("\x1b[3mItalic\x1b[0m");
    expect(result).toContain('<span style="font-style:italic;">Italic</span>');
  });

  it("wraps underlined text in a span with text-decoration:underline", () => {
    const result = ctx.ansiToHtml("\x1b[4mUnder\x1b[0m");
    expect(result).toContain(
      '<span style="text-decoration:underline;">Under</span>',
    );
  });

  it("applies standard foreground color code 31 (red)", () => {
    const result = ctx.ansiToHtml("\x1b[31mred\x1b[0m");
    expect(result).toContain("color:#ff7b72");
    expect(result).toContain("red");
  });

  it("applies standard foreground color code 32 (green)", () => {
    const result = ctx.ansiToHtml("\x1b[32mgreen\x1b[0m");
    expect(result).toContain("color:#3fb950");
    expect(result).toContain("green");
  });

  it("applies bright foreground color code 91 (bright red)", () => {
    const result = ctx.ansiToHtml("\x1b[91mbright\x1b[0m");
    expect(result).toContain("color:#ffa198");
    expect(result).toContain("bright");
  });

  it("applies all 8 standard foreground colors (30-37)", () => {
    const ANSI_FG = [
      "#484f58",
      "#ff7b72",
      "#3fb950",
      "#e3b341",
      "#79c0ff",
      "#ff79c6",
      "#39c5cf",
      "#b1bac4",
    ];
    for (let i = 0; i < 8; i++) {
      const result = ctx.ansiToHtml(`\x1b[${30 + i}mtext\x1b[0m`);
      expect(result).toContain(`color:${ANSI_FG[i]}`);
    }
  });

  it("applies all 8 bright foreground colors (90-97)", () => {
    const ANSI_FG_BRIGHT = [
      "#6e7681",
      "#ffa198",
      "#56d364",
      "#f8e3ad",
      "#cae8ff",
      "#fecfe8",
      "#b3f0ff",
      "#ffffff",
    ];
    for (let i = 0; i < 8; i++) {
      const result = ctx.ansiToHtml(`\x1b[${90 + i}mtext\x1b[0m`);
      expect(result).toContain(`color:${ANSI_FG_BRIGHT[i]}`);
    }
  });

  it("applies RGB color via 38;2;r;g;b sequence", () => {
    const result = ctx.ansiToHtml("\x1b[38;2;100;200;50mcolored\x1b[0m");
    expect(result).toContain("color:rgb(100,200,50)");
    expect(result).toContain("colored");
  });

  it("collapses carriage returns, keeping only the last overwrite per line", () => {
    // Simulates terminal spinner: "loading\r done" should show only " done"
    const result = ctx.ansiToHtml("loading\r done");
    expect(result).toBe(" done");
    expect(result).not.toContain("loading");
  });

  it("handles multiple carriage returns, keeping the last segment", () => {
    const result = ctx.ansiToHtml("a\rb\rc");
    expect(result).toBe("c");
  });

  it("preserves newlines across carriage-return collapsing", () => {
    const result = ctx.ansiToHtml("line1\nspinner\rdone\nline3");
    expect(result).toContain("line1");
    expect(result).toContain("done");
    expect(result).toContain("line3");
    expect(result).not.toContain("spinner");
  });

  it("closes open spans when reset code (0) is encountered", () => {
    const result = ctx.ansiToHtml("\x1b[31mred\x1b[0m normal");
    // All spans must be closed
    const opens = (result.match(/<span/g) || []).length;
    const closes = (result.match(/<\/span>/g) || []).length;
    expect(opens).toBe(closes);
  });

  it("closes all open spans at end of string without explicit reset", () => {
    const result = ctx.ansiToHtml("\x1b[1mno reset at end");
    const opens = (result.match(/<span/g) || []).length;
    const closes = (result.match(/<\/span>/g) || []).length;
    expect(opens).toBe(closes);
  });

  it("ignores non-SGR ANSI sequences like cursor movement", () => {
    // \x1b[2J = erase screen, \x1b[H = cursor home — should be silently dropped
    const result = ctx.ansiToHtml("\x1b[2J\x1b[Hhello");
    expect(result).toBe("hello");
  });

  it("handles empty string input", () => {
    expect(ctx.ansiToHtml("")).toBe("");
  });

  it("handles reset code without prior style (no spurious spans)", () => {
    const result = ctx.ansiToHtml("\x1b[0mplain");
    expect(result).toBe("plain");
    expect(result).not.toContain("<span");
  });

  it("handles combined style codes in a single sequence", () => {
    // Bold + red in one sequence
    const result = ctx.ansiToHtml("\x1b[1;31mbold-red\x1b[0m");
    expect(result).toContain("font-weight:bold");
    expect(result).toContain("color:#ff7b72");
  });
});

describe("ANSI_FG and ANSI_FG_BRIGHT constants", () => {
  // const declarations in vm scripts are not global-object properties, so we
  // use vm.runInContext to read them from within the script's lexical scope.
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("modal-ansi.js", ctx);
  });

  it("ANSI_FG has 8 entries", () => {
    const fg = vm.runInContext("ANSI_FG", ctx);
    expect(fg.length).toBe(8);
  });

  it("ANSI_FG_BRIGHT has 8 entries", () => {
    const fgb = vm.runInContext("ANSI_FG_BRIGHT", ctx);
    expect(fgb.length).toBe(8);
  });

  it("ANSI_FG entries are hex color strings", () => {
    const fg = vm.runInContext("ANSI_FG", ctx);
    for (const color of fg) {
      expect(color).toMatch(/^#[0-9a-f]{6}$/i);
    }
  });

  it("ANSI_FG_BRIGHT entries are hex color strings", () => {
    const fgb = vm.runInContext("ANSI_FG_BRIGHT", ctx);
    for (const color of fgb) {
      expect(color).toMatch(/^#[0-9a-f]{6}$/i);
    }
  });
});
