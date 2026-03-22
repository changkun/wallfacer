import { describe, it, expect } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import { readAllCSS } from "./read-css.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, "..", "..", "..");

describe("settings modal layout", () => {
  it("keeps the sandbox configuration inside a scrollable tab panel", () => {
    const html = readFileSync(
      join(repoRoot, "ui/partials/settings-modal.html"),
      "utf8",
    );
    const sandboxHtml = readFileSync(
      join(repoRoot, "ui/partials/settings-tab-sandbox.html"),
      "utf8",
    );
    const css = readAllCSS(join(repoRoot, "ui/css/styles.css"));

    expect(html).toContain('data-settings-tab="sandbox"');
    expect(sandboxHtml).toContain("Sandbox Configuration");
    expect(css).toContain(".settings-layout");
    expect(css).toContain("align-items: stretch;");
    expect(css).toContain(".settings-tab-content-wrap");
    expect(css).toContain("align-self: stretch;");
    expect(css).toContain(".settings-tab-content.active");
    expect(css).toContain("height: 100%;");
    expect(css).toContain("overflow-y: auto;");
  });
});
