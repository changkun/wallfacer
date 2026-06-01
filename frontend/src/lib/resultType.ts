// Heuristic for distinguishing a "plan" output (architecture / proposal / phased
// rollout) from a "result" output (implementation summary, file diff, etc).
// Mirrors ui/js/modal-results.js detectResultType — used by the Results tab
// to surface a small Plan / Result chip per turn entry.

const PLAN_PATTERNS: RegExp[] = [
  /^#{1,3}\s+.*\bplan\b/im,
  /^#{1,3}\s+.*\bphase\s*\d/im,
  /^#{1,3}\s+.*\bstep\s*\d/im,
  /\bimplementation plan\b/i,
  /^#{1,3}\s+.*\bapproach\b/im,
  /^#{1,3}\s+.*\bproposal\b/im,
  /^#{1,3}\s+.*\bdesign\b/im,
  /^#{1,3}\s+.*\barchitecture\b/im,
  /^#{1,3}\s+.*\bstrategy\b/im,
];

export type ResultType = 'plan' | 'result';

export function detectResultType(text: string): ResultType {
  if (!text) return 'result';
  return PLAN_PATTERNS.some((p) => p.test(text)) ? 'plan' : 'result';
}
