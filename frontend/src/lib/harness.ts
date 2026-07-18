// Display metadata for the Tier-A harnesses. The canonical id is the
// lowercase harness key used by the backend (harness.ID); the label is the
// brand-cased name shown in the UI.
export const HARNESS_LABELS: Record<string, string> = {
  claude: 'Claude',
  codex: 'Codex',
  cursor: 'Cursor',
  opencode: 'OpenCode',
  pi: 'Pi',
  topos: 'Topos',
};

// harnessLabel returns the brand-cased display name for a harness id,
// falling back to a capitalized form for ids not in the table.
export function harnessLabel(id: string): string {
  const key = (id || '').toLowerCase();
  return HARNESS_LABELS[key] ?? (key ? key.charAt(0).toUpperCase() + key.slice(1) : '');
}

// modelLabel returns a brand-cased short label for a harness-reported model
// id, e.g. "claude-opus-4-8[1m]" -> "Opus 4.8". The trailing context-window
// variant suffix (e.g. "[1m]") and any dated release snapshot (e.g. the
// "-20251001" in "claude-haiku-4-5-20251001", which is what the CLI reports on
// per-turn assistant lines) are dropped from the label; callers keep the raw id
// for the chip's title so nothing is hidden. Ids that do not
// match the canonical "claude-<family>-<version>" shape fall back to the id
// verbatim (minus the variant suffix) rather than a hardcoded guess.
export function modelLabel(raw: string): string {
  const id = (raw || '').trim();
  if (!id) return '';
  // Strip a trailing variant suffix like "[1m]".
  const base = id.replace(/\[[^\]]*\]$/, '');
  const m = base.match(/^claude-([a-z]+)-(\d+(?:-\d+)*)$/i);
  if (m) {
    const family = m[1].charAt(0).toUpperCase() + m[1].slice(1).toLowerCase();
    const segments = m[2].split('-');
    // Drop a trailing dated release snapshot (e.g. "20251001"): the CLI reports
    // the concrete dated model id on per-turn assistant lines, but a version is
    // one or two short numeric segments, never an 8-digit date.
    if (segments.length > 1 && /^\d{5,}$/.test(segments[segments.length - 1])) {
      segments.pop();
    }
    return `${family} ${segments.join('.')}`;
  }
  return base || id;
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
