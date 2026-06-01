import type { PromptTemplate } from '../api/types';

// Case-insensitive filter over template name + body, mirroring the legacy
// ui/js/templates.js picker. Empty query returns all.
export function filterTemplates(templates: readonly PromptTemplate[], query: string): PromptTemplate[] {
  const q = (query || '').trim().toLowerCase();
  if (!q) return templates.slice();
  return templates.filter(
    (t) => t.name.toLowerCase().includes(q) || t.body.toLowerCase().includes(q),
  );
}
