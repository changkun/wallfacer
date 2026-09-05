// The pill tone for a task status: one ramp colour per board column. Shared by
// every surface that shows a status capsule outside the card (the task sheet,
// the command palette, the trash dialog) so the same status never reads in two
// colours.
export function statusPill(status: string | undefined): string {
  switch (status) {
    case 'in_progress':
    case 'committing': return 'pill-run';
    case 'waiting':
    case 'cancelling': return 'pill-warn';
    case 'done': return 'pill-ok';
    case 'failed': return 'pill-err';
    case 'cancelled': return 'pill-pub';
    default: return 'pill-neutral';
  }
}
