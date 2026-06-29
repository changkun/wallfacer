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

// Workspace management is unified onto the first-class registry and now lives
// only in the sidebar popover (the Settings → Workspace tab is retired). The
// legacy path-based PUT /api/workspaces switch and config workspace_groups
// writes are gone; group/workspace actions route through the id-based registry
// store, and active state derives from isActive() (config workspace_id), never a
// folder-key/DTO comparison. Per-workspace settings (rename, folders, limits,
// delete) are edited in the WorkspaceEditModal, opened by id from the popover.
describe('sidebar unified on the workspace registry', () => {
  const root = process.cwd();
  const sidebar = readFileSync(resolve(root, 'src/components/Sidebar.vue'), 'utf8');

  it('sidebar drops the legacy path-based switch and activates by id', () => {
    // No legacy path-based PUT switch and no config workspace_groups write.
    expect(sidebar).not.toMatch(/'PUT',\s*'\/api\/workspaces'/);
    expect(sidebar).not.toMatch(/'PUT',\s*'\/api\/config'/);
    // switchToGroup goes through the registry store's activate(id).
    expect(sidebar).toMatch(/wsStore\.activate\(g\.id\)/);
    // Delete routes through the id-based registry endpoint.
    expect(sidebar).toMatch(/wsStore\.remove\(g\.id\)/);
  });

  it('sidebar active marker uses isActive(g.id), not only folder-key comparison', () => {
    expect(sidebar).toMatch(/wsStore\.isActive\(g\.id\)/);
  });

  it('sidebar opens the full workspace editor by id (rename pencil retired)', () => {
    // The popover row's Edit affordance raises the registry-backed edit modal.
    expect(sidebar).toMatch(/ui\.openWorkspaceEdit\(g\.id\)/);
  });

  it('the Settings → Workspace tab is removed from the settings registry', () => {
    const settingsPage = readFileSync(resolve(root, 'src/views/SettingsPage.vue'), 'utf8');
    expect(settingsPage).not.toMatch(/SettingsTabWorkspace/);
    expect(settingsPage).not.toMatch(/key:\s*'workspace'/);
  });
});
