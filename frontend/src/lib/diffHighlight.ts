import hljs from 'highlight.js/lib/common';
import type { DiffFile, DiffLineKind } from './diff';

// Map a filename to a highlight.js language name (ported 1:1 from
// ui/js/modal-diff.js extToLang). Returns null when unknown.
const LANG_BY_EXT: Record<string, string> = {
  js: 'javascript', mjs: 'javascript', cjs: 'javascript', jsx: 'javascript',
  ts: 'typescript', tsx: 'typescript', py: 'python', pyi: 'python', go: 'go',
  rs: 'rust', java: 'java', kt: 'kotlin', kts: 'kotlin', swift: 'swift',
  c: 'c', h: 'c', cpp: 'cpp', cc: 'cpp', cxx: 'cpp', hpp: 'cpp', cs: 'csharp',
  rb: 'ruby', php: 'php', css: 'css', scss: 'scss', sass: 'scss',
  html: 'xml', htm: 'xml', svg: 'xml', xml: 'xml', json: 'json',
  yaml: 'yaml', yml: 'yaml', toml: 'ini', sh: 'bash', bash: 'bash', zsh: 'bash',
  md: 'markdown', mdx: 'markdown', sql: 'sql', r: 'r', lua: 'lua', pl: 'perl',
  proto: 'protobuf',
};

export function extToLang(filename: string): string | null {
  const basename = (filename.split('/').pop() || '').toLowerCase();
  if (basename === 'dockerfile') return 'dockerfile';
  if (basename === 'makefile') return 'makefile';
  const ext = basename.includes('.') ? basename.split('.').pop()! : '';
  return LANG_BY_EXT[ext] || null;
}

// Split a highlight.js HTML output string into per-line HTML, closing and
// reopening <span> tags at line boundaries so each line is self-contained.
export function splitHighlightedLines(html: string): string[] {
  const lines: string[] = [];
  let current = '';
  const openSpans: string[] = [];
  let i = 0;
  while (i < html.length) {
    if (html[i] === '<') {
      const end = html.indexOf('>', i);
      if (end === -1) { current += html.slice(i); break; }
      const tag = html.slice(i, end + 1);
      if (tag.startsWith('<span')) { openSpans.push(tag); current += tag; }
      else if (tag === '</span>') { openSpans.pop(); current += tag; }
      else { current += tag; }
      i = end + 1;
    } else if (html[i] === '\n') {
      current += openSpans.map(() => '</span>').join('');
      lines.push(current);
      current = openSpans.join('');
      i++;
    } else {
      current += html[i];
      i++;
    }
  }
  if (current) lines.push(current);
  return lines;
}

export interface HighlightedDiffLine {
  kind: DiffLineKind;
  prefix: string;
  html: string;
}

// Strip the leading +/- (or context space) so the highlighter sees clean code.
function codeOf(kind: DiffLineKind, text: string): string {
  if (kind === 'header' || kind === 'hunk') return '';
  if (kind === 'add' || kind === 'del') return text.slice(1);
  return text.length > 0 ? text.slice(1) : '';
}

// Produce per-line highlighted HTML for a diff file, or null when the language
// is unknown or highlighting fails (caller falls back to plain text). The HTML
// is hljs output (token spans only) — safe to render via v-html.
export function highlightDiffFile(file: DiffFile): HighlightedDiffLine[] | null {
  const lang = extToLang(file.filename);
  if (!lang) return null;
  try {
    const codeOnly = file.lines.map((l) => codeOf(l.kind, l.text));
    const highlighted = hljs.highlight(codeOnly.join('\n'), { language: lang }).value;
    const hl = splitHighlightedLines(highlighted);
    return file.lines.map((l, i) => ({
      kind: l.kind,
      prefix: l.kind === 'add' ? '+' : l.kind === 'del' ? '-' : (l.kind === 'ctx' ? ' ' : ''),
      html: l.kind === 'header' || l.kind === 'hunk' ? '' : (hl[i] || ''),
    }));
  } catch {
    return null;
  }
}
