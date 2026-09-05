// How a task tag renders on a card. priority:* and impact:* are structured
// tags that lead the card's meta line; spawned-by:* names the routine that
// created the task; everything else is a label the reader can filter by.
// Tags are one mono meta line under the title, never a row of tinted chips:
// the card's colour budget is spent on state (specs/shared/console-redesign/board.md).

export type TagKind = 'priority' | 'impact' | 'spawned' | 'label';

// The priority levels that earn a colour. Everything else is ink.
export type TagTone = 'warn' | 'err' | '';

export interface RenderedTag {
  rawTag: string;
  kind: TagKind;
  label: string;
  tone: TagTone;
}

export function classifyTag(rawTag: string): RenderedTag {
  const lower = rawTag.toLowerCase();
  if (lower.startsWith('priority:')) {
    const level = rawTag.slice('priority:'.length).trim() || 'priority';
    const l = level.toLowerCase();
    const tone: TagTone = l === 'critical' || l === 'urgent' ? 'err' : l === 'high' ? 'warn' : '';
    return { rawTag, kind: 'priority', label: level, tone };
  }
  if (lower.startsWith('impact:')) {
    return { rawTag, kind: 'impact', label: `impact ${rawTag.slice('impact:'.length).trim()}`, tone: '' };
  }
  if (lower.startsWith('spawned-by:')) {
    return { rawTag, kind: 'spawned', label: rawTag, tone: '' };
  }
  return { rawTag, kind: 'label', label: rawTag, tone: '' };
}

// Card order: priority first, then impact, then labels, then provenance.
const ORDER: Record<TagKind, number> = { priority: 0, impact: 1, label: 2, spawned: 3 };

export function orderTags(tags: readonly string[]): RenderedTag[] {
  return tags.map(classifyTag).sort((a, b) => ORDER[a.kind] - ORDER[b.kind]);
}
