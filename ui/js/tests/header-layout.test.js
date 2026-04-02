import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import { readAllCSS } from "./read-css.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, "..", "..", "..");

describe("header layout", () => {
  it("places automation toggles inside a dropdown menu in the primary header", () => {
    const layoutHtml = readFileSync(
      join(repoRoot, "ui/partials/initial-layout.html"),
      "utf8",
    );
    const automationHtml = readFileSync(
      join(repoRoot, "ui/partials/automation-menu.html"),
      "utf8",
    );

    // Layout includes the automation-menu partial via Go template
    expect(layoutHtml).toContain('class="app-header"');
    expect(layoutHtml).toContain('class="app-header__primary"');
    expect(layoutHtml).toContain('{{template "automation-menu.html"}}');

    // Automation menu partial contains the toggle controls
    expect(automationHtml).toContain('id="automation-menu-btn"');
    expect(automationHtml).toContain('id="automation-menu"');
    expect(automationHtml).toContain('class="header-toggle-strip"');
    expect(
      automationHtml.match(/class="header-toggle-chip"/g) || [],
    ).toHaveLength(7);
    expect(automationHtml).toContain('id="autopilot-toggle"');
    expect(automationHtml).toContain('id="autotest-toggle"');
    expect(automationHtml).toContain('id="autosubmit-toggle"');
    expect(layoutHtml).not.toContain('class="app-header__secondary"');
  });

  it("defines automation menu and toggle chip styles", () => {
    const css = readAllCSS(join(repoRoot, "ui/css/styles.css"));

    expect(css).toContain(".automation-menu");
    expect(css).toContain(".automation-menu-wrap");
    expect(css).toContain(".automation-active-count");
    expect(css).toContain(".header-toggle-chip");
    expect(css).toContain(".header-toggle-chip__track");
    expect(css).toContain(".app-header__primary");
    expect(css).toContain("@media (max-width: 768px)");
    expect(css).toContain(".app-header__button-row");
    expect(css).toContain("align-content: flex-start;");
    expect(css).toContain("flex: 0 0 100%;");
  });
});
