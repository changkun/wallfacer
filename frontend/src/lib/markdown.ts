import MarkdownIt from 'markdown-it';
import anchor from 'markdown-it-anchor';

const md = new MarkdownIt({
  html: true,
  linkify: true,
  typographer: false,
}).use(anchor, {
  permalink: false,
  slugify: (s: string) =>
    s.toLowerCase().replace(/[^\w\s-]/g, '').replace(/\s+/g, '-').replace(/-+/g, '-').trim(),
});

export function renderMarkdown(src: string): string {
  return md.render(src);
}

export function stripFirstHeading(src: string): string {
  const lines = src.split('\n');
  if (lines.length > 0 && lines[0].startsWith('# ')) {
    return lines.slice(1).join('\n');
  }
  return src;
}
