import { describe, expect, it } from 'vitest';
import { existsSync, readFileSync } from 'node:fs';
import { resolve } from 'node:path';

const logoBaseUrl = 'https://latere.ai/static';

describe('Latere footer logo', () => {
  it('uses the shared Latere mark and a bundled local favicon', () => {
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
    expect(footer).toContain("from 'latere-ui'");
    expect(footer).not.toContain('<svg class="logo-icon"');
    expect(footer).not.toContain('latere-logo-light.png');

    // The favicon is a bundled local asset so the tab icon resolves offline
    // and in self-hosted deploys, instead of the external latere.ai PNGs.
    expect(index).toContain('rel="icon" type="image/png" href="/static/wallfacer-icon.png"');
    expect(index).not.toContain(`${logoBaseUrl}/latere-logo-light.png`);
    expect(index).not.toContain(`${logoBaseUrl}/latere-logo-dark.png`);
    expect(existsSync(resolve(root, 'public/static/wallfacer-icon.png'))).toBe(true);
    expect(styles).toContain('.logo-mark-img');
  });
});
