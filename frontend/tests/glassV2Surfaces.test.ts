import { describe, expect, it } from 'vitest';
import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';

// Regression guard for the Liquid Glass "native v2" adoption. Two contracts:
//   1. The chrome surfaces wallfacer restyled consume the SHARED latere-ui glass
//      ladder tokens (never hand-rolled blur/opacity/radii) so every Latere site
//      renders the same material.
//   2. The hard constraint: glass is floating chrome ONLY — no backdrop-filter is
//      ever painted over content (diffs, code, terminals, prose, request streams).
//
// CSS is read from disk (not the DOM) so the assertions stay fast and independent
// of a full render; mirrors the tests/latereLogo.test.ts harness.

const root = process.cwd();
const read = (rel: string) => readFileSync(resolve(root, rel), 'utf8');

describe('Liquid Glass v2 — shared-token chrome surfaces', () => {
  it('landing navbar is a floating thin-glass capsule with pill links', () => {
    const css = read('src/styles/app/navbar-auth.css');
    // The capsule material lives on .nav-container (the .site-header wrapper
    // stays transparent to detach the pill from the top edge). Thin-tier
    // material from the shared ladder, not hand-rolled blur().
    expect(css).toMatch(/\.nav-container[^}]*var\(--glass-bg-thin\)/s);
    expect(css).toMatch(/\.nav-container[^}]*blur\(var\(--glass-blur-thin\)\)/s);
    expect(css).toMatch(/\.nav-container[^}]*var\(--radius-pill\)/s);
    expect(css).not.toMatch(/\.site-header[^}]*saturate\(180%\) blur\(20px\)/s);
    // Nav links are capsules whose active/hover fill comes from --glass-pill-fill.
    expect(css).toMatch(/\.nav-link[^}]*var\(--radius-pill\)/s);
    expect(css).toContain('--glass-pill-fill');
    expect(css).toContain('--glass-edge-top');
  });

  it('console board header consumes the shared glass ladder, not --glass-filter', () => {
    const css = read('src/styles/header/content-header.css');
    expect(css).toMatch(/\.app-header[^}]*blur\(var\(--glass-blur-thin\)\)/s);
    expect(css).toMatch(/\.app-header[^}]*var\(--glass-edge-top\)/s);
    // The old hand-rolled shorthand must be gone from the header band.
    expect(css).not.toMatch(/\.app-header\b[^}]*backdrop-filter: var\(--glass-filter\)/s);
  });

  it('landing CTA is a smoked-ink glass capsule scoped to the marketing shell', () => {
    const css = read('src/styles/app/buttons-hero.css');
    expect(css).toMatch(/\.wallfacer-page \.btn-lg[^}]*var\(--radius-pill\)/s);
    expect(css).toMatch(/\.wallfacer-page \.btn-lg[^}]*var\(--glass-smoke-strong\)/s);
    expect(css).toMatch(/\.wallfacer-page \.btn-lg[^}]*var\(--shadow-glass\)/s);
  });
});

describe('Liquid Glass v2 — no glass over content', () => {
  // Every stylesheet that paints CONTENT (tables, code, diffs, terminals, docs
  // prose, live request/agent streams) must be free of backdrop-filter: glass is
  // chrome only. Adding a frosted layer over any of these tanks legibility + perf
  // and breaks the contract, so this fails the moment one slips in.
  const contentStylesheets = [
    'src/styles/diffs.css',
    'src/styles/syntax.css',
    'src/styles/task-detail.css',
    'src/styles/multi-turn.css',
    'src/styles/agents.css',
    'src/styles/board.css',
    'src/styles/docs.css',
    'src/styles/whiteboard.css',
    // The docked terminal / xterm surface — content the spec names explicitly.
    'src/styles/dock.css',
    // Mermaid diagrams + the aggregated oversight logs/stream view — rendered
    // content, never floating chrome.
    'src/styles/mermaid.css',
    'src/styles/oversight.css',
    // Spec mode paints chat prose, the spec tree, and the reading-view TOC.
    // spec-mode.css is a barrel of @imports, so guard the partials that
    // actually paint content — the barrel itself carries no rules.
    'src/styles/spec-mode/chat-pane.css',
    'src/styles/spec-mode/chat-bubbles.css',
    'src/styles/spec-mode/prose-toc.css',
    'src/styles/spec-mode/explorer-tree.css',
  ];

  it.each(contentStylesheets)('%s carries no backdrop-filter', (file) => {
    expect(read(file)).not.toMatch(/backdrop-filter/);
  });
});
