// Regression guard: ban raw browser dialogs (alert/confirm/prompt) in UI
// source. Use the in-app showAlert / showConfirm / showPrompt helpers from
// utils.js instead so warnings render inside the app's modal system.
//
// Biome 1.9.4 (the version pinned by `make lint`) has no equivalent rule —
// noAlert landed in Biome 2.x — so this vitest case is the single source
// of truth for enforcement and runs as part of `make test`.

import { readdirSync, readFileSync, statSync } from "node:fs";
import { join, relative } from "node:path";
import { describe, expect, it } from "vitest";

const JS_DIR = new URL("..", import.meta.url).pathname.replace(/\/$/, "");

// Directories that are allowed to keep raw calls (tests mock them, vendor
// bundles are third-party, generated/build output is derived).
const SKIP_DIRS = new Set(["tests", "vendor", "generated", "build"]);

// Matches `alert(`, `confirm(`, `prompt(`, `window.alert(`, etc., but not
// method calls on other objects (e.g. `foo.confirm(`). Comments are stripped
// before matching so English prose like "the task prompt (...)" is ignored.
const BANNED = /(?:^|[^.\w$])(alert|confirm|prompt)\s*\(/;

function listJsFiles(dir) {
  const out = [];
  for (const entry of readdirSync(dir)) {
    const full = join(dir, entry);
    const s = statSync(full);
    if (s.isDirectory()) {
      if (SKIP_DIRS.has(entry)) continue;
      out.push(...listJsFiles(full));
    } else if (entry.endsWith(".js")) {
      out.push(full);
    }
  }
  return out;
}

function stripComments(src) {
  // Remove /* ... */ block comments and // ... line comments. Good enough
  // for this repo's vanilla JS — no JSX, no regex-literal ambiguity to
  // worry about for the banned-token check.
  return src
    .replace(/\/\*[\s\S]*?\*\//g, "")
    .replace(/(^|[^:])\/\/[^\n]*/g, "$1");
}

describe("no raw browser dialogs in UI source", () => {
  const files = listJsFiles(JS_DIR);

  it("discovers source files to scan", () => {
    expect(files.length).toBeGreaterThan(0);
  });

  for (const file of files) {
    const rel = relative(JS_DIR, file);
    it(`${rel} uses the in-app dialog helpers`, () => {
      const src = stripComments(readFileSync(file, "utf8"));
      const lines = src.split("\n");
      const offenders = [];
      for (let i = 0; i < lines.length; i++) {
        const line = lines[i];
        const m = line.match(BANNED);
        if (!m) continue;
        // Allow the wrapper definitions in utils.js to reference the
        // DOM ids like `alert-modal` — those don't match the regex — but
        // do block any real call.
        offenders.push(`${rel}:${i + 1}: ${line.trim()}`);
      }
      expect(
        offenders,
        `Use showAlert / showConfirm / showPrompt from utils.js instead of ` +
          `browser alert() / confirm() / prompt():\n${offenders.join("\n")}`,
      ).toEqual([]);
    });
  }
});
