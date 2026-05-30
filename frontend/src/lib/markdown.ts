import MarkdownIt from 'markdown-it';
import anchor from 'markdown-it-anchor';

function escapeHtml(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

const md = new MarkdownIt({
  html: true,
  linkify: true,
  typographer: false,
}).use(anchor, {
  permalink: false,
  slugify: (s: string) =>
    s.toLowerCase().replace(/[^\w\s-]/g, '').replace(/\s+/g, '-').replace(/-+/g, '-').trim(),
});

// Custom fence renderer: mermaid fences become a placeholder div consumed
// by enhanceMarkdown(); other fences get the standard <pre><code class="language-X">.
const defaultFence = md.renderer.rules.fence!;
md.renderer.rules.fence = (tokens, idx, options, env, self) => {
  const token = tokens[idx];
  const lang = (token.info ?? '').trim();
  if (lang === 'mermaid') {
    return (
      '<div class="mermaid-block" data-mermaid="' +
      escapeHtml(token.content) +
      '"><pre class="mermaid-src"><code>' +
      escapeHtml(token.content) +
      '</code></pre></div>\n'
    );
  }
  return defaultFence(tokens, idx, options, env, self);
};

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
