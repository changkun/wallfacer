// Global type declarations for the wallfacer frontend.
//
// Files under ui/js/ run in the browser's global scope (no ES modules).
// As files migrate to TypeScript, their top-level function and variable
// declarations need ambient declarations here so other files that
// reference them as globals still type-check.
//
// Grow this file as migration progresses. When every module has migrated,
// this file is the complete catalog of the frontend's global surface.

export {};

declare global {
  // --- ui/js/lib/clipboard.ts ---
  function copyWithFeedback(
    text: string,
    btn: HTMLElement | null,
    feedback?: string,
    duration?: number,
  ): void;
}
