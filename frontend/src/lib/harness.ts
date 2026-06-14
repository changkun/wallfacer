// Display metadata for the Tier-A harnesses. The canonical id is the
// lowercase harness key used by the backend (harness.ID); the label is the
// brand-cased name shown in the UI.
export const HARNESS_LABELS: Record<string, string> = {
  claude: 'Claude',
  codex: 'Codex',
  cursor: 'Cursor',
  opencode: 'OpenCode',
  pi: 'Pi',
};

// harnessLabel returns the brand-cased display name for a harness id,
// falling back to a capitalized form for ids not in the table.
export function harnessLabel(id: string): string {
  const key = (id || '').toLowerCase();
  return HARNESS_LABELS[key] ?? (key ? key.charAt(0).toUpperCase() + key.slice(1) : '');
}
