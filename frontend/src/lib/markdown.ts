import MarkdownIt from 'markdown-it';
import anchor from 'markdown-it-anchor';

import { escapeHtml } from './html';


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
