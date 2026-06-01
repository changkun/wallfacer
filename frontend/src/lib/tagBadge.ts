export interface RenderedTag {
  rawTag: string;
  label: string;
  cls: string;
  styled: boolean;
}

// Mirrors ui/js/render.js's renderTaskTagBadge taxonomy. Brainstorm category
// tags (sourced from /api/config ideation_categories) and idea-agent get
// dedicated badge classes; priority:* / impact:* / spawned-by:* keep their
// own; everything else falls back to the hue-coloured generic chip.
export function classifyTag(rawTag: string, categories?: Set<string>): RenderedTag {
  const lower = rawTag.toLowerCase();
  if (categories && categories.has(rawTag)) {
    return { rawTag, label: rawTag, cls: 'badge badge-category', styled: false };
  }
  if (lower === 'idea-agent') {
    return { rawTag, label: rawTag, cls: 'badge badge-idea-agent', styled: false };
  }
  if (lower.startsWith('priority:')) {
    return { rawTag, label: rawTag.slice('priority:'.length).trim() || 'priority', cls: 'badge badge-priority', styled: false };
  }
  if (lower.startsWith('impact:')) {
    return { rawTag, label: `impact ${rawTag.slice('impact:'.length).trim()}`, cls: 'badge badge-impact', styled: false };
  }
  if (lower.startsWith('spawned-by:')) {
    return { rawTag, label: rawTag, cls: 'tag-chip badge-routine-spawn', styled: false };
  }
  return { rawTag, label: rawTag, cls: 'tag-chip', styled: true };
}
