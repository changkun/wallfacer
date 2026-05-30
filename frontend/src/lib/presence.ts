// Derive the sidebar presence list: one entry per in-progress task ("agent-XXXX")
// plus the signed-in user (cloud mode). Ports the old UI's presence row.

export interface PresenceEntry {
  id: string;
  label: string;
  kind: 'agent' | 'self';
}

export function derivePresence(
  inProgress: { id: string }[],
  me: { name?: string; email?: string } | null,
): PresenceEntry[] {
  const out: PresenceEntry[] = inProgress.map((t) => ({
    id: t.id,
    label: `agent-${t.id.slice(0, 4)}`,
    kind: 'agent' as const,
  }));
  if (me) {
    out.push({ id: 'self', label: me.name || me.email || 'you', kind: 'self' });
  }
  return out;
}
