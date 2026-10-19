// Derive the sidebar presence list: one entry per in-progress task
// ("agent-XXXX"). Ports the old UI's presence row.

export interface PresenceEntry {
  id: string;
  label: string;
}

export function derivePresence(inProgress: { id: string }[]): PresenceEntry[] {
  return inProgress.map((t) => ({
    id: t.id,
    label: `agent-${t.id.slice(0, 4)}`,
  }));
}
