/**
 * Regression test for the archived-banner CSS override.
 *
 * `.spec-archived-banner { display: flex; }` has the same specificity as the
 * `.hidden { display: none; }` utility in tailwind.css. Since spec-mode.css
 * loads after tailwind.css, the banner rule was winning the cascade and the
 * banner stayed visible even with the `hidden` class applied. The fix is a
 * higher-specificity `.spec-archived-banner.hidden { display: none }` override.
 *
 * Vitest does not run a real browser, so this test reads the CSS source and
 * asserts the override rule is present — it would fail if someone removes it.
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

describe("spec-archived-banner CSS", () => {
  it("declares display: flex on the bare class", () => {
    expect(css).toMatch(/\.spec-archived-banner\s*\{[^}]*display:\s*flex/);
  });

  it("has a higher-specificity override so .hidden still hides it", () => {
    expect(css).toMatch(
      /\.spec-archived-banner\.hidden\s*\{[^}]*display:\s*none/,
    );
  });
});
