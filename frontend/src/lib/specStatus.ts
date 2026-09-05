// The ramp colour a spec's lifecycle status carries in the tree and the
// focused view. Statuses rank themselves by tone: drafted and vague are ink,
// validated is live work (run), testing is attention (warn), complete is ok,
// stale is err, archived and frontmatter-less docs are muted.

export type SpecTone = 'neutral' | 'run' | 'warn' | 'ok' | 'err' | 'muted';

export function specStatusTone(status: string | undefined, doc = false): SpecTone {
  if (doc) return 'muted';
  switch (status) {
    case 'validated': return 'run';
    case 'testing': return 'warn';
    case 'complete': return 'ok';
    case 'stale': return 'err';
    case 'archived': return 'muted';
    default: return 'neutral';
  }
}

// The pill class for the focused view's status chip.
export function specStatusPill(status: string | undefined): string {
  const tone = specStatusTone(status);
  return tone === 'muted' ? 'pill-neutral' : `pill-${tone}`;
}
