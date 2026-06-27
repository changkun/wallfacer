// State → color, shared by the canvas (node disc fill) and the inspector
// legend so the two never drift. Spec lifecycle and task status share one map;
// their value sets don't collide.
// Each state gets its own hue. Spec-lifecycle and task-status values are kept
// visually distinct from one another (e.g. validated indigo vs in_progress
// blue, complete teal vs done green) so no two legend rows look alike.
export const STATE_COLORS: Record<string, string> = {
  // task status
  backlog: '#8e8a80', // warm gray
  in_progress: '#2f6fb0', // blue
  waiting: '#d99a2b', // amber
  committing: '#7aa0d4', // light blue
  done: '#3f7a4a', // green
  failed: '#c0493d', // red
  cancelled: '#9a9488', // dim gray
  // spec lifecycle
  vague: '#c6c0b2', // pale sand
  drafted: '#d98f3a', // orange
  validated: '#5a4fc4', // indigo
  testing: '#8a5cc4', // purple
  complete: '#2e9e7a', // teal
  stale: '#b5703a', // rust
  archived: '#bdb4a4', // light gray
};

export function stateColor(status: string): string {
  return STATE_COLORS[status] ?? '#8e8a80';
}
