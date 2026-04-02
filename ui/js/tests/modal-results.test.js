/**
 * Tests for modal-results.js — result type detection and rendering.
 */
import { describe, it, expect, beforeAll } from "vitest";
import { readFileSync } from "fs";
import { fileURLToPath } from "url";
import { dirname, join } from "path";
import vm from "vm";
import { loadLibDeps } from "./lib-deps.js";

const __dirname = dirname(fileURLToPath(import.meta.url));
const jsDir = join(__dirname, "..");

function makeContext(extra = {}) {
  return vm.createContext({ console, Math, Date, ...extra });
}

function loadScript(filename, ctx) {
  loadLibDeps(filename, ctx);
  const code = readFileSync(join(jsDir, filename), "utf8");
  vm.runInContext(code, ctx, { filename: join(jsDir, filename) });
  return ctx;
}

// ---------------------------------------------------------------------------
// detectResultType
// ---------------------------------------------------------------------------
describe("detectResultType", () => {
  let ctx;
  beforeAll(() => {
    ctx = makeContext();
    loadScript("modal-results.js", ctx);
  });

  it('returns "result" for empty string', () => {
    expect(ctx.detectResultType("")).toBe("result");
  });

  it('returns "result" for null/falsy', () => {
    expect(ctx.detectResultType(null)).toBe("result");
    expect(ctx.detectResultType(undefined)).toBe("result");
  });

  it('returns "result" for plain text with no plan keywords', () => {
    expect(ctx.detectResultType("The task is complete. All tests pass.")).toBe(
      "result",
    );
  });

  it('returns "plan" for text with ## Implementation Plan heading', () => {
    const text = "## Implementation Plan\n1. Do this\n2. Do that";
    expect(ctx.detectResultType(text)).toBe("plan");
  });

  it('returns "plan" for "implementation plan" phrase (case insensitive)', () => {
    expect(
      ctx.detectResultType("Here is the Implementation Plan we will follow"),
    ).toBe("plan");
    expect(
      ctx.detectResultType("This is our implementation plan for the feature"),
    ).toBe("plan");
  });

  it('returns "plan" for heading containing "plan" (case insensitive)', () => {
    expect(ctx.detectResultType("# My Plan\nDetails here")).toBe("plan");
    expect(ctx.detectResultType("## Migration Plan\nSteps")).toBe("plan");
    expect(ctx.detectResultType("### PLAN\n...")).toBe("plan");
  });

  it('returns "plan" for heading containing "phase N"', () => {
    expect(ctx.detectResultType("## Phase 1: Setup\nDo stuff")).toBe("plan");
    expect(ctx.detectResultType("# Phase 2\nMore stuff")).toBe("plan");
  });

  it('returns "plan" for heading containing "step N"', () => {
    expect(ctx.detectResultType("## Step 1: Initialize\nDo this")).toBe("plan");
    expect(ctx.detectResultType("# Step 3\nDo that")).toBe("plan");
  });

  it('returns "plan" for heading containing "approach"', () => {
    expect(ctx.detectResultType("## Approach\nWe will...")).toBe("plan");
  });

  it('returns "plan" for heading containing "proposal"', () => {
    expect(ctx.detectResultType("## Proposal\nHere is my proposal")).toBe(
      "plan",
    );
  });

  it('returns "plan" for heading containing "design"', () => {
    expect(ctx.detectResultType("# System Design\n...")).toBe("plan");
  });

  it('returns "plan" for heading containing "architecture"', () => {
    expect(ctx.detectResultType("## Architecture Overview\n...")).toBe("plan");
  });

  it('returns "plan" for heading containing "strategy"', () => {
    expect(ctx.detectResultType("## Strategy\nWe will adopt...")).toBe("plan");
  });

  it('does not match "plan" keyword in body text (not a heading)', () => {
    // "plan" in body text without being a heading should not match /^#{1,3}\s+.*\bplan\b/im
    // but "implementation plan" inline does match the separate pattern
    expect(ctx.detectResultType("We have a plan to fix this bug.")).toBe(
      "result",
    );
  });

  it("is case-insensitive for headings", () => {
    expect(ctx.detectResultType("## IMPLEMENTATION PLAN\nDetails")).toBe(
      "plan",
    );
    expect(ctx.detectResultType("## My Design Approach\n...")).toBe("plan");
  });

  it("matches headings of depth 1, 2, and 3", () => {
    expect(ctx.detectResultType("# Plan\nDetails")).toBe("plan");
    expect(ctx.detectResultType("## Plan\nDetails")).toBe("plan");
    expect(ctx.detectResultType("### Plan\nDetails")).toBe("plan");
  });

  it("does not match headings of depth 4+ as plan patterns", () => {
    // The regex uses {1,3} so #### would not match
    expect(ctx.detectResultType("#### Plan\nDetails")).toBe("result");
  });
});

// ---------------------------------------------------------------------------
// renderResultsFromEvents — using DOM stubs
// ---------------------------------------------------------------------------
describe("renderResultsFromEvents", () => {
  let ctx;
  let elements;

  beforeAll(() => {
    elements = {};
    const makeEl = (id) => {
      const el = {
        id,
        innerHTML: "",
        classList: {
          _classes: new Set(["hidden"]),
          add(c) {
            this._classes.add(c);
          },
          remove(c) {
            this._classes.delete(c);
          },
          contains(c) {
            return this._classes.has(c);
          },
          toggle(c, force) {
            if (force !== undefined) {
              force ? this._classes.add(c) : this._classes.delete(c);
            } else {
              this._classes.has(c)
                ? this._classes.delete(c)
                : this._classes.add(c);
            }
          },
        },
        textContent: "",
      };
      elements[id] = el;
      return el;
    };

    ctx = makeContext({
      escapeHtml: (s) =>
        String(s ?? "")
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")
          .replace(/>/g, "&gt;"),
      renderMarkdown: (s) => `<p>${s}</p>`,
      document: {
        getElementById: (id) => {
          if (!elements[id]) makeEl(id);
          return elements[id];
        },
        querySelectorAll: () => ({ forEach: () => {} }),
      },
    });
    loadScript("modal-results.js", ctx);
  });

  function resetElements() {
    Object.keys(elements).forEach((k) => delete elements[k]);
  }

  it("hides the tab and summary when results are empty", () => {
    resetElements();
    ctx.renderResultsFromEvents([]);
    const tab = elements["left-tab-implementation"];
    if (tab) {
      expect(tab.classList.contains("hidden")).toBe(true);
    }
  });

  it("renders a single result entry", () => {
    resetElements();
    ctx.renderResultsFromEvents(["The implementation is complete."]);
    const list = elements["modal-results-list"];
    expect(list).toBeDefined();
    expect(list.innerHTML).toContain("result-entry");
  });

  it("renders multiple results in reverse order (newest first)", () => {
    resetElements();
    ctx.renderResultsFromEvents(["first turn", "second turn", "third turn"]);
    const list = elements["modal-results-list"];
    const html = list.innerHTML;
    // Turn 3 should appear before Turn 1 (newest first)
    const idx3 = html.indexOf("Turn 3");
    const idx1 = html.indexOf("Turn 1");
    expect(idx3).toBeLessThan(idx1);
  });

  it("shows turn labels when there are multiple results", () => {
    resetElements();
    ctx.renderResultsFromEvents(["turn A", "turn B"]);
    const list = elements["modal-results-list"];
    expect(list.innerHTML).toContain("result-turn-label");
    expect(list.innerHTML).toContain("Turn 1");
    expect(list.innerHTML).toContain("Turn 2");
  });

  it("does not show turn label when there is a single result", () => {
    resetElements();
    ctx.renderResultsFromEvents(["only turn"]);
    const list = elements["modal-results-list"];
    expect(list.innerHTML).not.toContain("result-turn-label");
  });

  it("shows plan badge for plan-type results", () => {
    resetElements();
    ctx.renderResultsFromEvents(["## Implementation Plan\nStep 1. Do this"]);
    const list = elements["modal-results-list"];
    expect(list.innerHTML).toContain("result-type-plan");
    expect(list.innerHTML).toContain("Plan");
  });

  it("does not show plan badge for non-plan results", () => {
    resetElements();
    ctx.renderResultsFromEvents(["The fix is applied and tests pass."]);
    const list = elements["modal-results-list"];
    expect(list.innerHTML).not.toContain("result-type-plan");
  });

  it("wraps newest result in div, older results in details elements", () => {
    resetElements();
    ctx.renderResultsFromEvents(["old", "newer", "newest"]);
    const list = elements["modal-results-list"];
    // Newest (index 2) shown first as <div>, older as <details>
    const firstDiv = list.innerHTML.indexOf('<div class="result-entry"');
    const firstDetails = list.innerHTML.indexOf(
      '<details class="result-entry"',
    );
    expect(firstDiv).toBeLessThan(firstDetails);
  });

  it("uses custom tabId and listId from opts", () => {
    resetElements();
    ctx.renderResultsFromEvents(["test result"], {
      tabId: "left-tab-testing",
      listId: "modal-test-results-list",
      entryPrefix: "test-result-entry-",
    });
    expect(elements["modal-test-results-list"]).toBeDefined();
    expect(elements["modal-test-results-list"].innerHTML).toContain(
      "result-entry",
    );
  });

  it("unhides the summary section when results are present", () => {
    resetElements();
    // Pre-create summary element as hidden
    const summary = {
      classList: {
        _classes: new Set(["hidden"]),
        add(c) {
          this._classes.add(c);
        },
        remove(c) {
          this._classes.delete(c);
        },
        contains(c) {
          return this._classes.has(c);
        },
        toggle(c, f) {
          f !== undefined
            ? f
              ? this._classes.add(c)
              : this._classes.delete(c)
            : this._classes.has(c)
              ? this._classes.delete(c)
              : this._classes.add(c);
        },
      },
    };
    elements["modal-summary-section"] = summary;

    ctx.renderResultsFromEvents(["some result"]);
    expect(summary.classList.contains("hidden")).toBe(false);
  });
});

// ---------------------------------------------------------------------------
// renderTestResultsFromEvents
// ---------------------------------------------------------------------------
describe("renderTestResultsFromEvents", () => {
  let ctx;
  let elements;

  beforeAll(() => {
    elements = {};
    const makeEl = (id) => {
      const el = {
        id,
        innerHTML: "",
        classList: {
          _classes: new Set(["hidden"]),
          add(c) {
            this._classes.add(c);
          },
          remove(c) {
            this._classes.delete(c);
          },
          contains(c) {
            return this._classes.has(c);
          },
          toggle(c, force) {
            if (force !== undefined) {
              force ? this._classes.add(c) : this._classes.delete(c);
            } else {
              this._classes.has(c)
                ? this._classes.delete(c)
                : this._classes.add(c);
            }
          },
        },
        textContent: "",
      };
      elements[id] = el;
      return el;
    };

    ctx = makeContext({
      escapeHtml: (s) =>
        String(s ?? "")
          .replace(/&/g, "&amp;")
          .replace(/</g, "&lt;")
          .replace(/>/g, "&gt;"),
      renderMarkdown: (s) => `<p>${s}</p>`,
      document: {
        getElementById: (id) => {
          if (!elements[id]) makeEl(id);
          return elements[id];
        },
        querySelectorAll: () => ({ forEach: () => {} }),
      },
    });
    loadScript("modal-results.js", ctx);
  });

  it("delegates to renderResultsFromEvents with test-specific IDs", () => {
    ctx.renderTestResultsFromEvents(["test passed"]);
    const list = elements["modal-test-results-list"];
    expect(list).toBeDefined();
    expect(list.innerHTML).toContain("test-result-entry-0");
  });
});
