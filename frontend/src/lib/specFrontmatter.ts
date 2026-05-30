// Parses the YAML frontmatter at the top of a spec markdown file.
// Mirrors the relaxed line-by-line parser from ui/js/spec-mode.js: only
// the simple `key: value` shape is captured (lists and block scalars are
// skipped) since that's all the Plan-mode header reads.

export interface SpecFrontmatter {
  title?: string;
  status?: string;
  effort?: string;
  author?: string;
  created?: string;
  updated?: string;
  dispatched_task_id?: string;
  [key: string]: string | undefined;
}

export interface ParsedSpec {
  frontmatter: SpecFrontmatter;
  body: string;
}

export function parseSpecFrontmatter(text: string): ParsedSpec {
  if (!text) return { frontmatter: {}, body: '' };
  const match = text.match(/^---\n([\s\S]*?)\n---\n([\s\S]*)$/);
  if (!match) return { frontmatter: {}, body: text };

  const fm: SpecFrontmatter = {};
  for (const line of match[1].split('\n')) {
    const colon = line.indexOf(':');
    if (colon === -1) continue;
    const key = line.slice(0, colon).trim();
    const val = line.slice(colon + 1).trim();
    if (key && val && !val.startsWith('-') && val !== '|' && val !== '>') {
      fm[key] = val;
    }
  }
  return { frontmatter: fm, body: match[2] };
}
