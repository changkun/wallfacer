import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// Regression: when the rail is collapsed the workspace popover flies out to the
// right of the trigger. The base `.sb-ws-popover--inline` rule pins both
// `left: 0` and `right: 0` to span the full-width trigger; the collapsed
// override re-anchors `left` but must also clear `right` and set an explicit
// width — otherwise the two opposing anchors squeeze the popover to a ~10px
// sliver peeking out from behind the rail (no layout in jsdom, so assert the
// rule source rather than computed geometry).
describe('collapsed workspace popover', () => {
  it('clears the inherited right anchor and sets a width so it is not squished', () => {
    const root = process.cwd();
    const sidebar = readFileSync(resolve(root, 'src/components/Sidebar.vue'), 'utf8');

    const rule = sidebar
      .match(/\.sb-ws-switch-wrap--collapsed \.sb-ws-popover--inline\)\s*\{([^}]*)\}/)?.[1];
    expect(rule, 'collapsed inline popover rule must exist').toBeTruthy();
    expect(rule).toMatch(/left:\s*calc\(100%/);
    // Without these two the left/right anchors collapse the width to a sliver.
    expect(rule).toMatch(/right:\s*auto/);
    expect(rule).toMatch(/width:\s*\d/);
  });
});
