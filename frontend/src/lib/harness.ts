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

// FALLBACK_HARNESSES mirrors the backend harness registry (harness.All()).
// Used so harness pickers never render empty before /api/config loads its
// authoritative `sandboxes` list. Keep in sync with HARNESS_LABELS.
export const FALLBACK_HARNESSES = Object.keys(HARNESS_LABELS);

// supportedHarnesses returns the harness ids the server advertises, falling
// back to the full registry when config has not loaded yet (or returned an
// empty list). Single source of truth for every harness picker.
export function supportedHarnesses(sandboxes?: string[] | null): string[] {
  return sandboxes && sandboxes.length ? sandboxes : [...FALLBACK_HARNESSES];
}
