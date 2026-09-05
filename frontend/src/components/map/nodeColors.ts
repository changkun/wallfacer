// State to colour, shared by the canvas (node disc fill) and the inspector
// legend so the two never drift. Every value is a CSS colour expression on
// the ramp, so the map follows the palette and the theme. Spec lifecycle and
// task status share one map; their value sets do not collide, and states
// that share a ramp hue are mixed toward a neighbour so no two legend rows
// look alike.
export const STATE_COLORS: Record<string, string> = {
  // task status
  backlog: 'var(--ink-4)',
  in_progress: 'var(--run)',
  waiting: 'var(--warn)',
  committing: 'color-mix(in srgb, var(--run) 55%, var(--bg))',
  done: 'var(--ok)',
  failed: 'var(--err)',
  cancelled: 'color-mix(in srgb, var(--ink-4) 55%, var(--bg))',
  // spec lifecycle
  vague: 'var(--rule-2)',
  drafted: 'color-mix(in srgb, var(--warn) 70%, var(--err))',
  validated: 'var(--purple)',
  testing: 'color-mix(in srgb, var(--purple) 55%, var(--run))',
  complete: 'color-mix(in srgb, var(--ok) 65%, var(--run))',
  stale: 'color-mix(in srgb, var(--err) 55%, var(--warn))',
  archived: 'color-mix(in srgb, var(--ink-4) 40%, var(--bg))',
};

export function stateColor(status: string): string {
  return STATE_COLORS[status] ?? STATE_COLORS.backlog;
}
