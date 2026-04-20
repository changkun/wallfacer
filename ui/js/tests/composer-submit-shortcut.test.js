/**
 * Regression test for the cmd+Enter composer duplicate-submit bug.
 *
 * Before the fix, both ui/js/events.js and ui/js/tasks.js attached a keydown
 * listener to #new-prompt that called createTask() on Ctrl/Cmd+Enter, so a
 * single shortcut press created two identical tasks. The fix removes the
 * events.js binding; tasks.js owns the composer-scoped handler.
 *
 * This test asserts the structural absence: events.js must not contain a
 * keydown listener on #new-prompt. A grep-style guard is sufficient because
 * the bug class is "two modules binding the same event" and behavioral tests
 * would only detect it if they loaded both real modules together.
 */
import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { describe, it, expect } from "vitest";

const here = dirname(fileURLToPath(import.meta.url));
const eventsSrc = readFileSync(join(here, "..", "events.js"), "utf8");
const tasksSrc = readFileSync(join(here, "..", "tasks.js"), "utf8");

describe("composer cmd+Enter handler (duplicate-submit regression)", () => {
  it("events.js does not attach a keydown listener to #new-prompt", () => {
    const hasNewPromptKeydown =
      /getElementById\(['"]new-prompt['"]\)[\s\S]{0,120}addEventListener\(\s*['"]keydown['"]/.test(
        eventsSrc,
      );
    expect(hasNewPromptKeydown).toBe(false);
  });

  it("tasks.js still binds the composer's cmd+Enter submit path", () => {
    expect(tasksSrc).toMatch(/_composerKeysBound/);
    expect(tasksSrc).toMatch(/metaKey.*ctrlKey.*Enter|ctrlKey.*metaKey.*Enter/);
    expect(tasksSrc).toMatch(/createTask\(\)/);
  });
});
