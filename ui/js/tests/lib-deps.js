// Shared lib dependency map for test infrastructure.
// When a test loads a script via vm.runInContext, it must first load any
// lib/ modules that script depends on (since the browser loads them via
// <script> tags in scripts.html, but tests don't have that mechanism).

import { readFileSync } from "fs";
import { join } from "path";
import vm from "vm";
import { fileURLToPath } from "url";
import { dirname } from "path";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

const LIB_DEPS = {
  "utils.js": ["build/lib/formatting.js", "build/lib/modal.js"],
  "markdown.js": ["build/lib/clipboard.js", "build/lib/toggle.js"],
  "modal.js": ["build/lib/tab-switcher.js", "lib/markdown-render.js"],
  "modal-core.js": ["build/lib/tab-switcher.js", "lib/markdown-render.js"],
  "modal-logs.js": ["build/lib/scheduling.js", "build/lib/tab-switcher.js"],
  "modal-results.js": [
    "build/lib/formatting.js",
    "build/lib/clipboard.js",
    "build/lib/toggle.js",
    "build/lib/tab-switcher.js",
    "lib/markdown-render.js",
  ],
  "render.js": ["build/lib/scheduling.js"],
  "refine.js": ["build/lib/scheduling.js"],
  "workspace.js": ["build/lib/modal.js"],
  "keyboard-shortcuts.js": [
    "build/lib/modal.js",
    "build/lib/modal-controller.js",
  ],
  "instructions.js": ["build/lib/modal.js", "build/lib/modal-controller.js"],
  "containers.js": ["build/lib/modal.js", "build/lib/modal-controller.js"],
  "docs.js": [
    "build/lib/modal.js",
    "build/lib/modal-controller.js",
    "lib/markdown-render.js",
    "lib/floating-toc.js",
  ],
  "api.js": ["lib/config-toggle.js"],
};

/**
 * Load lib dependencies for a script into a VM context.
 * Call this before vm.runInContext for the script itself.
 * Safe to call multiple times — already-loaded libs are skipped.
 *
 * @param {string} filename  The script filename (e.g. "utils.js").
 * @param {Object} ctx       The VM context.
 */
export function loadLibDeps(filename, ctx) {
  const deps = LIB_DEPS[filename];
  if (!deps) return;
  if (!ctx._loadedLibs) ctx._loadedLibs = new Set();
  for (const dep of deps) {
    if (ctx._loadedLibs.has(dep)) continue;
    ctx._loadedLibs.add(dep);
    const code = readFileSync(join(jsDir, dep), "utf8");
    vm.runInContext(code, ctx, { filename: join(jsDir, dep) });
  }
}
