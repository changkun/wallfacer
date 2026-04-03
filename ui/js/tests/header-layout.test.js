import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import { readAllCSS } from "./read-css.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, "..", "..", "..");

describe("header layout", () => {
  it("places automation toggles inside a dropdown menu in the content header", () => {
    const headerHtml = readFileSync(
      join(repoRoot, "ui/partials/content-header.html"),
      "utf8",
    );
    const automationHtml = readFileSync(
      join(repoRoot, "ui/partials/automation-menu.html"),
      "utf8",
    );

    // Content header includes the automation-menu partial via Go template
    expect(headerHtml).toContain('class="app-header"');
    expect(headerHtml).toContain('{{template "automation-menu.html"}}');

    // Sidebar partial exists and contains navigation
    const sidebarHtml = readFileSync(
      join(repoRoot, "ui/partials/sidebar.html"),
      "utf8",
    );
    expect(sidebarHtml).toContain('id="app-sidebar"');
    expect(sidebarHtml).toContain('id="sidebar-nav-board"');
    expect(sidebarHtml).toContain('id="sidebar-nav-spec"');

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
  });

  it("defines sidebar and automation menu styles", () => {
    const css = readAllCSS(join(repoRoot, "ui/css/styles.css"));

    expect(css).toContain(".app-sidebar");
    expect(css).toContain(".sidebar-nav");
    expect(css).toContain(".automation-menu");
    expect(css).toContain(".automation-menu-wrap");
    expect(css).toContain(".automation-active-count");
    expect(css).toContain(".header-toggle-chip");
    expect(css).toContain(".header-toggle-chip__track");
    expect(css).toContain(".app-header__button-row");
  });
});
