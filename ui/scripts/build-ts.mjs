// Compile every .ts source under ui/js/ to a sibling .js file.
//
// Keeps the no-bundler, global-scope <script>-tag model: each .ts file
// transpiles to a .js file in place, preserving top-level bindings so
// other scripts on the page see the same globals they did before.
//
// Run via `make ui-ts`. Re-run after editing any .ts source.

import { build } from "esbuild";
import { readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = dirname(fileURLToPath(import.meta.url));
const uiRoot = resolve(__dirname, "..");
const jsRoot = join(uiRoot, "js");

const SKIP_DIRS = new Set(["vendor", "generated", "tests"]);

/**
 * Recursively collect .ts files under jsRoot, skipping generated/vendor
 * and test files. Tests are compiled by Vitest on the fly — we only
 * build the production source modules that the browser loads via
 * <script> tags (and that the vm-based tests read from disk).
 */
function collectTsFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    const rel = relative(jsRoot, full);
    const s = statSync(full);
    if (s.isDirectory()) {
      if (SKIP_DIRS.has(entry)) continue;
      out.push(...collectTsFiles(full));
      continue;
    }
    if (!entry.endsWith(".ts")) continue;
    if (entry.endsWith(".d.ts")) continue;
    // Skip test files even if placed outside js/tests/.
    if (entry.endsWith(".test.ts")) continue;
    out.push({ full, rel });
  }
  return out;
}

const files = collectTsFiles(jsRoot);

if (files.length === 0) {
  console.log("build-ts: no .ts sources under ui/js/ (nothing to compile)");
  process.exit(0);
}

// One esbuild invocation with multiple entry points is faster than N calls.
// `outdir` + `outbase` preserves directory layout: js/lib/foo.ts -> js/lib/foo.js.
// Notes on preserving global-scope semantics:
//
// The runtime loads each JS file via a plain <script> tag (no modules),
// so top-level `function foo(){}` and `var x = ...` MUST become globals.
// Passing `bundle: false` without a `format` field makes esbuild run as a
// pure transpiler — it strips TS syntax per file without wrapping the
// output in an IIFE or adding any module scaffolding. Verified on the
// clipboard.ts pilot: `function copyWithFeedback(...)` at the top of the
// source emits as a top-level function declaration in the output.
await build({
  entryPoints: files.map((f) => f.full),
  outdir: jsRoot,
  outbase: jsRoot,
  outExtension: { ".js": ".js" },
  bundle: false,
  target: "es2022",
  platform: "browser",
  sourcemap: false,
  logLevel: "info",
});

console.log(`build-ts: compiled ${files.length} file(s)`);
