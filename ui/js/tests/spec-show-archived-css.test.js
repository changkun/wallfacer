/**
 * Regression test for the spec "Show archived" toggle CSS override.
 *
 * `.spec-show-archived { display: inline-flex; }` has the same specificity
 * as tailwind's `.hidden { display: none; }`. Since spec-mode.css loads
 * after tailwind.css, the bare rule won the cascade and the toggle label
 * leaked into the board mode's file explorer. The fix is a
 * higher-specificity `.spec-show-archived.hidden { display: none }` override.
 */
import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const css = readFileSync(
  join(__dirname, "..", "..", "css", "spec-mode.css"),
  "utf8",
);

describe("spec-show-archived CSS", () => {
  it("declares display: inline-flex on the bare class", () => {
    expect(css).toMatch(/\.spec-show-archived\s*\{[^}]*display:\s*inline-flex/);
  });

  it("has a higher-specificity override so .hidden still hides it", () => {
    expect(css).toMatch(
      /\.spec-show-archived\.hidden\s*\{[^}]*display:\s*none/,
    );
  });
});
