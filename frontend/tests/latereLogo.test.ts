import { describe, expect, it } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const logoBaseUrl = 'https://latere.ai/static';

describe('Latere footer logo', () => {
  it('uses the canonical Latere-hosted PNG assets for the Latere mark and favicon', () => {
    const root = process.cwd();
    const footer = readFileSync(resolve(root, 'src/components/SiteFooter.vue'), 'utf8');
    const index = readFileSync(resolve(root, 'index.html'), 'utf8');
    // app.css is a barrel; the logo rules live in its navbar partial. Read
    // both so the assertion stays robust to internal reshuffling.
    const styles =
      readFileSync(resolve(root, 'src/styles/app.css'), 'utf8') +
      '\n' +
      readFileSync(resolve(root, 'src/styles/app/navbar-auth.css'), 'utf8');

    // The footer now comes from the shared latere-ui package (inline mark).
    // (index.html still preloads the hosted PNG; it is now unused — a dead
    // preload left for a follow-up, asserted below to document current state.)
    expect(footer).toContain("from 'latere-ui'");
    expect(footer).not.toContain('<svg class="logo-icon"');
    expect(footer).not.toContain('latere-logo-light.png');
    expect(index).toContain(`href="${logoBaseUrl}/latere-logo-light.png" media="(prefers-color-scheme: light)"`);
    expect(index).toContain(`href="${logoBaseUrl}/latere-logo-dark.png" media="(prefers-color-scheme: dark)"`);
    expect(index).not.toContain('/static/favicon.svg');
    expect(styles).toContain('.logo-mark-img');

    expect(existsSync(resolve(root, 'public/static/latere-logo-light.png'))).toBe(false);
    expect(existsSync(resolve(root, 'public/static/latere-logo-dark.png'))).toBe(false);
  });
});
