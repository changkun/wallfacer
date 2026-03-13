import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, '..', '..', '..');

describe('header layout', () => {
  it('uses compact toggle chips in the secondary header strip', () => {
    const html = readFileSync(join(repoRoot, 'ui/partials/initial-layout.html'), 'utf8');
    const primaryMatch = html.match(/<div class="app-header__primary">([\s\S]*?)<\/div>\s*<div class="app-header__secondary">/);

    expect(html).toContain('class="app-header"');
    expect(primaryMatch && primaryMatch[1]).toContain('class="app-header__actions"');
    expect(html).toContain('class="app-header__secondary"');
    expect(html).toContain('class="header-toggle-strip"');
    expect(html.match(/class="header-toggle-chip"/g) || []).toHaveLength(4);
    expect(html).toContain('id="autopilot-toggle"');
    expect(html).toContain('id="autotest-toggle"');
    expect(html).toContain('id="autosubmit-toggle"');
    expect(html).toContain('id="dep-graph-toggle"');
  });

  it('defines dedicated mobile header rules for the compact toolbar', () => {
    const css = readFileSync(join(repoRoot, 'ui/css/styles.css'), 'utf8');

    expect(css).toContain('.app-header__secondary');
    expect(css).toContain('.header-toggle-chip');
    expect(css).toContain('.header-toggle-chip__track');
    expect(css).toContain('.app-header__primary');
    expect(css).toContain('@media (max-width: 768px)');
    expect(css).toContain('.header-toggle-strip::-webkit-scrollbar');
    expect(css).toContain('.app-header__button-row');
    expect(css).toContain('align-content: flex-start;');
    expect(css).toContain('flex: 0 0 100%;');
  });
});
