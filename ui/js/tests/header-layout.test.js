import { describe, it, expect } from 'vitest';
import { readFileSync } from 'fs';
import { fileURLToPath } from 'url';
import { dirname, join } from 'path';

const __dirname = dirname(fileURLToPath(import.meta.url));
const repoRoot = join(__dirname, '..', '..', '..');

describe('header layout', () => {
  it('places automation toggles inside a dropdown menu in the primary header', () => {
    const html = readFileSync(join(repoRoot, 'ui/partials/initial-layout.html'), 'utf8');

    expect(html).toContain('class="app-header"');
    expect(html).toContain('class="app-header__primary"');
    expect(html).toContain('id="automation-menu-btn"');
    expect(html).toContain('id="automation-menu"');
    expect(html).toContain('class="header-toggle-strip"');
    expect(html.match(/class="header-toggle-chip"/g) || []).toHaveLength(8);
    expect(html).toContain('id="autopilot-toggle"');
    expect(html).toContain('id="autotest-toggle"');
    expect(html).toContain('id="autosubmit-toggle"');
    expect(html).toContain('id="dep-graph-toggle"');
    expect(html).not.toContain('class="app-header__secondary"');
  });

  it('defines automation menu and toggle chip styles', () => {
    const css = readFileSync(join(repoRoot, 'ui/css/styles.css'), 'utf8');

    expect(css).toContain('.automation-menu');
    expect(css).toContain('.automation-menu-wrap');
    expect(css).toContain('.automation-active-count');
    expect(css).toContain('.header-toggle-chip');
    expect(css).toContain('.header-toggle-chip__track');
    expect(css).toContain('.app-header__primary');
    expect(css).toContain('@media (max-width: 768px)');
    expect(css).toContain('.app-header__button-row');
    expect(css).toContain('align-content: flex-start;');
    expect(css).toContain('flex: 0 0 100%;');
  });
});
