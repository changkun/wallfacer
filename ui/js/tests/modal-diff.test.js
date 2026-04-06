/**
 * Tests for modal-diff.js — diff parsing and rendering helpers.
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

function makeDiffContext() {
  const ctx = makeContext({
    escapeHtml: (s) =>
      String(s ?? "")
        .replace(/&/g, "&amp;")
        .replace(/</g, "&lt;")
        .replace(/>/g, "&gt;")
        .replace(/"/g, "&quot;"),
  });
  loadScript("modal-diff.js", ctx);
  return ctx;
}

const SAMPLE_DIFF = `diff --git a/foo.js b/foo.js
index abc..def 100644
--- a/foo.js
+++ b/foo.js
@@ -1,3 +1,4 @@
 const x = 1;
-const y = 2;
+const y = 3;
+const z = 4;
`;

const SAMPLE_DIFF_TWO_FILES = `diff --git a/a.go b/a.go
index 111..222 100644
--- a/a.go
+++ b/a.go
@@ -1 +1 @@
-old
+new
diff --git a/b.go b/b.go
index 333..444 100644
--- a/b.go
+++ b/b.go
@@ -1 +1 @@
-x
+y
`;

// ---------------------------------------------------------------------------
// parseDiffByFile
// ---------------------------------------------------------------------------
describe("parseDiffByFile", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeDiffContext();
  });

  it("parses a single-file diff", () => {
    const files = ctx.parseDiffByFile(SAMPLE_DIFF);
    expect(files.length).toBe(1);
    expect(files[0].filename).toBe("foo.js");
  });

  it("counts additions and deletions correctly", () => {
    const files = ctx.parseDiffByFile(SAMPLE_DIFF);
    expect(files[0].adds).toBe(2); // +const y = 3; and +const z = 4;
    expect(files[0].dels).toBe(1); // -const y = 2;
  });

  it("does not count +++ or --- as additions/deletions", () => {
    const files = ctx.parseDiffByFile(SAMPLE_DIFF);
    // +++ and --- lines are header, not diffs
    expect(files[0].adds).toBe(2);
    expect(files[0].dels).toBe(1);
  });

  it("parses a two-file diff", () => {
    const files = ctx.parseDiffByFile(SAMPLE_DIFF_TWO_FILES);
    expect(files.length).toBe(2);
    expect(files[0].filename).toBe("a.go");
    expect(files[1].filename).toBe("b.go");
  });

  it("returns an empty array for empty diff string", () => {
    expect(ctx.parseDiffByFile("")).toEqual([]);
  });

  it("extracts workspace from === name === separator lines", () => {
    const diffWithWs = `=== repo1 ===\ndiff --git a/x.ts b/x.ts\nindex 0..1 100644\n--- a/x.ts\n+++ b/x.ts\n@@ -1 +1 @@\n-old\n+new\n`;
    const files = ctx.parseDiffByFile(diffWithWs);
    expect(files.length).toBe(1);
    expect(files[0].workspace).toBe("repo1");
  });

  it("propagates workspace to subsequent files until changed", () => {
    const diffWithWs =
      `=== ws1 ===\n` +
      `diff --git a/a.js b/a.js\nindex 0..1 100644\n--- a/a.js\n+++ b/a.js\n@@ -1 +1 @@\n-a\n+b\n` +
      `diff --git a/c.js b/c.js\nindex 0..1 100644\n--- a/c.js\n+++ b/c.js\n@@ -1 +1 @@\n-c\n+d\n`;
    const files = ctx.parseDiffByFile(diffWithWs);
    expect(files[0].workspace).toBe("ws1");
    expect(files[1].workspace).toBe("ws1");
  });

  it("tracks content field for each file", () => {
    const files = ctx.parseDiffByFile(SAMPLE_DIFF);
    expect(files[0].content).toContain("diff --git");
    expect(files[0].content).toContain("foo.js");
  });

  it("splits concatenated diffs where headers are on their own lines", () => {
    // Regression: if backend joins trimmed diffs without newlines, the second
    // "diff --git" header would be mid-line and not detected as a file boundary.
    const tracked =
      "diff --git a/tracked.go b/tracked.go\nindex 0..1 100644\n--- a/tracked.go\n+++ b/tracked.go\n@@ -1 +1 @@\n-old\n+new";
    const untracked =
      "diff --git a/untracked.go b/untracked.go\nnew file mode 100644\n--- /dev/null\n+++ b/untracked.go\n@@ -0,0 +1 @@\n+content";
    const combined = tracked + "\n" + untracked;
    const files = ctx.parseDiffByFile(combined);
    expect(files.length).toBe(2);
    expect(files[0].filename).toBe("tracked.go");
    expect(files[1].filename).toBe("untracked.go");
  });

  it("ignores blocks that lack a diff --git header", () => {
    const noDiff = "just some text\nno diff header here\n";
    const files = ctx.parseDiffByFile(noDiff);
    expect(files).toEqual([]);
  });
});

// ---------------------------------------------------------------------------
// renderDiffLine
// ---------------------------------------------------------------------------
describe("renderDiffLine", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeDiffContext();
  });

  it("renders addition lines with diff-add class", () => {
    const result = ctx.renderDiffLine("+added line");
    expect(result).toContain("diff-add");
    expect(result).toContain("+added line");
  });

  it("renders deletion lines with diff-del class", () => {
    const result = ctx.renderDiffLine("-removed line");
    expect(result).toContain("diff-del");
    expect(result).toContain("-removed line");
  });

  it("renders hunk headers (@@ lines) with diff-hunk class", () => {
    const result = ctx.renderDiffLine("@@ -1,3 +1,4 @@");
    expect(result).toContain("diff-hunk");
    expect(result).toContain("@@ -1,3 +1,4 @@");
  });

  it("renders diff header lines (diff --git) with diff-header class", () => {
    const result = ctx.renderDiffLine("diff --git a/foo b/foo");
    expect(result).toContain("diff-header");
  });

  it("renders --- lines with diff-header class", () => {
    const result = ctx.renderDiffLine("--- a/foo.js");
    expect(result).toContain("diff-header");
  });

  it("renders +++ lines with diff-header class", () => {
    const result = ctx.renderDiffLine("+++ b/foo.js");
    expect(result).toContain("diff-header");
  });

  it("does not classify +++ as addition", () => {
    const result = ctx.renderDiffLine("+++ b/foo.js");
    expect(result).not.toContain("diff-add");
  });

  it("does not classify --- as deletion", () => {
    const result = ctx.renderDiffLine("--- a/foo.js");
    expect(result).not.toContain("diff-del");
  });

  it("renders index lines with diff-header class", () => {
    const result = ctx.renderDiffLine("index abc123..def456 100644");
    expect(result).toContain("diff-header");
  });

  it("renders Binary diff lines with diff-header class", () => {
    const result = ctx.renderDiffLine(
      "Binary files a/image.png and b/image.png differ",
    );
    expect(result).toContain("diff-header");
  });

  it("renders context lines with just diff-line class (no special subclass)", () => {
    const result = ctx.renderDiffLine(" context line");
    expect(result).toContain("diff-line");
    expect(result).not.toContain("diff-add");
    expect(result).not.toContain("diff-del");
    expect(result).not.toContain("diff-hunk");
    expect(result).not.toContain("diff-header");
  });

  it("HTML-escapes special characters in diff lines", () => {
    const result = ctx.renderDiffLine('+<script>alert("xss")</script>');
    expect(result).toContain("&lt;script&gt;");
    expect(result).not.toContain("<script>");
  });
});

// ---------------------------------------------------------------------------
// extToLang
// ---------------------------------------------------------------------------
describe("extToLang", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeDiffContext();
  });

  it("returns javascript for .js", () => {
    expect(ctx.extToLang("src/app.js")).toBe("javascript");
  });
  it("returns typescript for .ts", () => {
    expect(ctx.extToLang("src/app.ts")).toBe("typescript");
  });
  it("returns typescript for .tsx", () => {
    expect(ctx.extToLang("src/app.tsx")).toBe("typescript");
  });
  it("returns python for .py", () => {
    expect(ctx.extToLang("main.py")).toBe("python");
  });
  it("returns go for .go", () => {
    expect(ctx.extToLang("internal/foo.go")).toBe("go");
  });
  it("returns rust for .rs", () => {
    expect(ctx.extToLang("src/main.rs")).toBe("rust");
  });
  it("returns yaml for .yml", () => {
    expect(ctx.extToLang(".github/ci.yml")).toBe("yaml");
  });
  it("returns json for .json", () => {
    expect(ctx.extToLang("package.json")).toBe("json");
  });
  it("returns bash for .sh", () => {
    expect(ctx.extToLang("scripts/run.sh")).toBe("bash");
  });
  it("returns dockerfile for Dockerfile (no extension)", () => {
    expect(ctx.extToLang("Dockerfile")).toBe("dockerfile");
  });
  it("returns makefile for Makefile (no extension)", () => {
    expect(ctx.extToLang("Makefile")).toBe("makefile");
  });
  it("returns null for unknown extensions", () => {
    expect(ctx.extToLang("archive.tar")).toBeNull();
  });
  it("returns null for files with no extension", () => {
    expect(ctx.extToLang("README")).toBeNull();
  });
  it("is case-insensitive for extensions", () => {
    expect(ctx.extToLang("App.JS")).toBe("javascript");
  });
});

// ---------------------------------------------------------------------------
// splitHighlightedLines
// ---------------------------------------------------------------------------
describe("splitHighlightedLines", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeDiffContext();
  });

  it("splits plain text by newlines", () => {
    const lines = ctx.splitHighlightedLines("a\nb\nc");
    expect(lines).toEqual(["a", "b", "c"]);
  });

  it("returns a single-element array for text without newlines", () => {
    expect(ctx.splitHighlightedLines("hello")).toEqual(["hello"]);
  });

  it("returns empty array for empty string", () => {
    expect(ctx.splitHighlightedLines("")).toEqual([]);
  });

  it("preserves self-contained spans within a line", () => {
    const html = '<span class="hljs-keyword">const</span> x = 1;';
    const lines = ctx.splitHighlightedLines(html);
    expect(lines.length).toBe(1);
    expect(lines[0]).toBe(html);
  });

  it("closes and reopens spans across line boundaries", () => {
    // Simulate a multi-line string token: opened on line 1, closed on line 2
    const html = '<span class="hljs-string">line1\nline2</span>';
    const lines = ctx.splitHighlightedLines(html);
    expect(lines.length).toBe(2);
    expect(lines[0]).toBe('<span class="hljs-string">line1</span>');
    expect(lines[1]).toBe('<span class="hljs-string">line2</span>');
  });

  it("handles multiple nested spans across lines", () => {
    const html = '<span class="a"><span class="b">x\ny</span></span>';
    const lines = ctx.splitHighlightedLines(html);
    expect(lines.length).toBe(2);
    expect(lines[0]).toBe('<span class="a"><span class="b">x</span></span>');
    expect(lines[1]).toBe('<span class="a"><span class="b">y</span></span>');
  });

  it("produces correct line count for highlighted multi-line code", () => {
    const html = "line1\nline2\nline3";
    expect(ctx.splitHighlightedLines(html).length).toBe(3);
  });
});

// ---------------------------------------------------------------------------
// renderDiffFiles — DOM-mocked
// ---------------------------------------------------------------------------
describe("renderDiffFiles", () => {
  let ctx;

  beforeAll(() => {
    ctx = makeDiffContext();
  });

  function makeContainer() {
    let innerHTML = "";
    return {
      get innerHTML() {
        return innerHTML;
      },
      set innerHTML(v) {
        innerHTML = v;
      },
    };
  }

  it('sets "No changes" message when diff is null', () => {
    const container = makeContainer();
    ctx.renderDiffFiles(container, null);
    expect(container.innerHTML).toContain("No changes");
  });

  it('sets "No changes" message when diff is empty string', () => {
    const container = makeContainer();
    ctx.renderDiffFiles(container, "");
    expect(container.innerHTML).toContain("No changes");
  });

  it("renders a diff file as a details element", () => {
    const container = makeContainer();
    ctx.renderDiffFiles(container, SAMPLE_DIFF);
    expect(container.innerHTML).toContain("<details");
    expect(container.innerHTML).toContain("foo.js");
  });

  it("shows addition stats in the output", () => {
    const container = makeContainer();
    ctx.renderDiffFiles(container, SAMPLE_DIFF);
    expect(container.innerHTML).toContain("+2");
  });

  it("shows deletion stats in the output", () => {
    const container = makeContainer();
    ctx.renderDiffFiles(container, SAMPLE_DIFF);
    // &minus; is used instead of - for negative stats
    expect(container.innerHTML).toContain("&minus;1");
  });

  it("renders multiple files", () => {
    const container = makeContainer();
    ctx.renderDiffFiles(container, SAMPLE_DIFF_TWO_FILES);
    expect(container.innerHTML).toContain("a.go");
    expect(container.innerHTML).toContain("b.go");
  });

  it("renders workspace header when workspace changes", () => {
    const diffWithWs =
      `=== myrepo ===\n` +
      `diff --git a/file.ts b/file.ts\nindex 0..1 100644\n--- a/file.ts\n+++ b/file.ts\n@@ -1 +1 @@\n-old\n+new\n`;
    const container = makeContainer();
    ctx.renderDiffFiles(container, diffWithWs);
    expect(container.innerHTML).toContain("diff-workspace-header");
    expect(container.innerHTML).toContain("myrepo");
  });

  it("HTML-escapes filename in output", () => {
    const evilDiff = `diff --git a/<evil>.js b/<evil>.js\nindex 0..1 100644\n--- a/<evil>.js\n+++ b/<evil>.js\n@@ -1 +1 @@\n-a\n+b\n`;
    const container = makeContainer();
    ctx.renderDiffFiles(container, evilDiff);
    expect(container.innerHTML).not.toContain("<evil>");
    expect(container.innerHTML).toContain("&lt;evil&gt;");
  });

  it("does not show stats span when there are no adds or dels", () => {
    // A diff with only header lines, no actual +/- content lines
    const headerOnlyDiff = `diff --git a/x.js b/x.js\nindex abc..def 100644\n--- a/x.js\n+++ b/x.js\n`;
    const container = makeContainer();
    ctx.renderDiffFiles(container, headerOnlyDiff);
    // No +N or &minus;N stats
    expect(container.innerHTML).not.toMatch(/\+\d+/);
    expect(container.innerHTML).not.toContain("&minus;");
  });
});
