// System-managed routine *cards* (e.g. the ideation routine) live behind the
// Automation settings surface, not on the board. The instance tasks a system
// routine spawns inherit the system:* tag but must stay visible — they are the
// actual work, not the schedule template — so the filter is scoped to
// kind=routine. Mirrors ui/js/render.js grouping.
export function isSystemRoutineCard(t: { kind?: string; tags?: string[] }): boolean {
  return (
    t.kind === 'routine' &&
    Array.isArray(t.tags) &&
    t.tags.some((tag) => tag.startsWith('system:'))
  );
}
