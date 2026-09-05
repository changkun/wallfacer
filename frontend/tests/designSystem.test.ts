import { describe, expect, it } from 'vitest';
import { existsSync, readFileSync, readdirSync, statSync } from 'node:fs';
import { join, resolve } from 'node:path';

// Regression guard for the console design system
// (specs/shared/console-redesign.md). Four contracts:
//   1. The material is matte: no backdrop-filter anywhere in src/styles, and
//      (as surface specs land) in any component style block. `none` is allowed:
//      it is how a shared latere-ui surface's own filter is switched off.
//   2. Palettes set surfaces, ink, rules, accent and the semantic ramp only.
//      Tint pairs, shadows, terminal and glass tokens derive in tokens.css.
//   3. Every palette keeps meta text readable (ink-3 on bg >= 4.5:1) and body
//      text off glare (bg vs ink >= 12:1) in both themes.
//   4. The primitive classes every surface composes from exist, and the glass
//      runtime is no longer imported.
//
// CSS is read from disk (not the DOM) so the assertions stay fast and
// independent of a full render.

const root = process.cwd();
const read = (rel: string) => {
  const abs = resolve(root, rel);
  if (!existsSync(abs)) throw new Error(`${rel} no longer exists; drop it from this guard's list`);
  return readFileSync(abs, 'utf8');
};
function walk(dir: string, ext: string, out: string[] = []): string[] {
  for (const name of readdirSync(dir)) {
    const p = join(dir, name);
    if (statSync(p).isDirectory()) walk(p, ext, out);
    else if (p.endsWith(ext)) out.push(p);
  }
  return out;
}
const rel = (abs: string) => abs.slice(root.length + 1);

// ---- colour maths -----------------------------------------------------------
function hexToRgb(hex: string): [number, number, number] {
  const h = hex.replace('#', '');
  const n = h.length === 3 ? h.split('').map((c) => c + c).join('') : h;
  return [0, 2, 4].map((i) => parseInt(n.slice(i, i + 2), 16)) as [number, number, number];
}
function luminance(hex: string): number {
  const [r, g, b] = hexToRgb(hex).map((v) => {
    const c = v / 255;
    return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
  });
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
}
function contrast(a: string, b: string): number {
  const [l1, l2] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (l1 + 0.05) / (l2 + 0.05);
}

// ---- token block parsing ----------------------------------------------------
interface Block { selector: string; tokens: Record<string, string> }
function blocks(css: string): Block[] {
  const out: Block[] = [];
  const re = /([^{}]+)\{([^{}]*)\}/g;
  let m: RegExpExecArray | null;
  while ((m = re.exec(css))) {
    // The first block follows the @import line; keep only the selector itself.
    const selector = m[1].replace(/\/\*[\s\S]*?\*\//g, '').split(';').pop()!.trim();
    const body = m[2].replace(/\/\*[\s\S]*?\*\//g, '');
    const tokens: Record<string, string> = {};
    for (const decl of body.split(';')) {
      const i = decl.indexOf(':');
      if (i === -1) continue;
      const k = decl.slice(0, i).trim();
      if (k.startsWith('--')) tokens[k] = decl.slice(i + 1).trim();
    }
    if (Object.keys(tokens).length) out.push({ selector, tokens });
  }
  return out;
}
const tokensCss = read('src/styles/tokens.css');
const palettesCss = read('src/styles/palettes.css');
const defaultLight = blocks(tokensCss).find((b) => b.selector.startsWith(':root'))!;
const defaultDark = blocks(tokensCss).find((b) => b.selector === '[data-theme="dark"]')!;
if (!defaultLight || !defaultDark) throw new Error('tokens.css default blocks not found');
const paletteBlocks = blocks(palettesCss);

// A palette block may omit ink (the warm palettes inherit it); resolve against
// the default block for the same theme.
function resolved(b: Block): Record<string, string> {
  const dark = b.selector.includes('data-theme="dark"');
  return { ...defaultLight.tokens, ...(dark ? defaultDark.tokens : {}), ...b.tokens };
}

// ---- 1. matte ---------------------------------------------------------------
describe('material is matte', () => {
  it.each(walk(resolve(root, 'src/styles'), '.css').map(rel))('%s carries no backdrop-filter', (file) => {
    expect(read(file)).not.toMatch(/backdrop-filter\s*:(?!\s*none\b)/);
  });

  // Component style blocks: surface specs turn their files on as they land.
  // Files not yet rebuilt are listed under `pending` so the guard documents
  // what is left rather than passing vacuously.
  const pending: string[] = [];
  const vueFiles = walk(resolve(root, 'src'), '.vue').map(rel);
  it.each(vueFiles.filter((f) => !pending.includes(f)))('%s style block carries no backdrop-filter', (file) => {
    const style = read(file).split(/<style[^>]*>/).slice(1).join('\n');
    expect(style).not.toMatch(/backdrop-filter\s*:(?!\s*none\b)/);
  });

  it('the glass runtime is not imported', () => {
    expect(read('src/main.ts')).not.toContain('latere-ui/glass');
    expect(read('src/App.vue')).not.toContain('useLiquidGlass');
  });

  it('glass tokens the shared components still read are pinned opaque', () => {
    for (const k of ['--glass-blur', '--glass-blur-thin', '--glass-blur-thick']) {
      expect(defaultLight.tokens[k]).toBe('0');
    }
    expect(defaultLight.tokens['--glass-bg']).toBe('var(--bg-card)');
    expect(defaultLight.tokens['--glass-edge-top']).toBe('none');
  });
});

// ---- 2. palettes derive -----------------------------------------------------
describe('palettes set only what the system derives from', () => {
  const allowed = /^--(bg(-deep|-sunk|-elevated|-card|-hover|-active)?|ink(-[234])?|rule(-2)?|accent(-2|-soft|-tint|-line|-ring|-gradient)?|ok|warn|run|err|purple|col-backlog)$/;
  it.each(paletteBlocks.map((b) => [b.selector, b] as const))('%s', (_sel, b) => {
    for (const k of Object.keys(b.tokens)) expect(k, `${b.selector} sets ${k}`).toMatch(allowed);
  });

  it('every palette has a light and a dark block', () => {
    const names = new Set(paletteBlocks.map((b) => /data-palette="([a-z]+)"/.exec(b.selector)![1]));
    for (const n of names) {
      expect(paletteBlocks.some((b) => b.selector === `:root[data-palette="${n}"]`), `${n} light`).toBe(true);
      expect(paletteBlocks.some((b) => b.selector === `[data-palette="${n}"][data-theme="dark"]`), `${n} dark`).toBe(true);
    }
  });

  it('tint pairs derive from the ramp in the default block', () => {
    for (const [tint, state] of [['green', 'ok'], ['amber', 'warn'], ['blue', 'run'], ['red', 'err'], ['plum', 'purple']]) {
      expect(defaultLight.tokens[`--tint-${tint}`]).toContain(`var(--${state})`);
      expect(defaultLight.tokens[`--tint-${tint}-ink`]).toBe(`var(--${state})`);
    }
  });
});

// ---- 3. contrast ------------------------------------------------------------
describe('every palette keeps its contrast floors', () => {
  const cases: Array<[string, Record<string, string>]> = [
    ['default light', defaultLight.tokens],
    ['default dark', { ...defaultLight.tokens, ...defaultDark.tokens }],
    ...paletteBlocks.map((b) => [b.selector, resolved(b)] as [string, Record<string, string>]),
  ];
  it.each(cases)('%s: ink-3 on bg >= 4.5 and bg vs ink >= 12', (_name, t) => {
    expect(t['--bg']).toMatch(/^#/);
    expect(t['--ink']).toMatch(/^#/);
    expect(t['--ink-3']).toMatch(/^#/);
    expect(contrast(t['--ink-3'], t['--bg'])).toBeGreaterThanOrEqual(4.5);
    expect(contrast(t['--ink'], t['--bg'])).toBeGreaterThanOrEqual(12);
  });
});

// ---- 4. primitives ----------------------------------------------------------
describe('primitives.css defines the shared classes', () => {
  const css = read('src/styles/primitives.css');
  it.each([
    '.btn', '.btn.ghost', '.btn.danger:hover', '.btn.sm', '.btn.lg', '.btn.pending',
    '.icon-btn', '.icon-btn.danger:hover',
    '.pill', '.pill-neutral', '.pill-brand', '.pill-ok', '.pill-warn', '.pill-run', '.pill-err', '.pill-pub', '.pill-dot',
    '.card', '.card-pad', '.card-head', '.rows', '.row',
    '.seg', '.seg-btn', '.seg-btn.on',
    '.field', '.eyebrow', '.link', '.muted', '.tabular', '.tabs', '.tab',
  ])('defines %s', (sel) => {
    const esc = sel.replace(/[.:]/g, (c) => '\\' + c);
    expect(css).toMatch(new RegExp(`(^|[,\\s])${esc}(\\s|,|\\{)`, 'm'));
  });

  it('the primary button is ink on canvas and the pill is mono uppercase', () => {
    expect(css).toMatch(/\.btn \{[^}]*background: var\(--ink\);[^}]*color: var\(--bg\);/s);
    expect(css).toMatch(/\.pill \{[^}]*text-transform: uppercase;/s);
    expect(css).toMatch(/\.pill \{[^}]*var\(--font-mono\)/s);
  });

  // Surface stylesheets rebuilt on the system carry no hex literal and no
  // radius literal: colour and geometry come from tokens. Each surface spec
  // adds its files here as it lands.
  const tokenOnly = [
    'src/styles/board.css', 'src/styles/search.css', 'src/styles/rail.css', 'src/styles/topbar.css',
    'src/styles/modal.css', 'src/styles/task-detail.css', 'src/styles/diffs.css', 'src/styles/syntax.css', 'src/styles/mermaid.css',
    'src/components/TaskDetail.vue', 'src/components/TaskPrPanel.vue', 'src/components/AgentTrace.vue',
    'src/components/ReviewVerification.vue', 'src/components/SpanFlamegraph.vue', 'src/components/TaskCard.vue',
    'src/components/TaskComposer.vue', 'src/components/AppRail.vue', 'src/components/Topbar.vue', 'src/components/WorkspaceChip.vue',
    'src/views/ChatPage.vue', 'src/components/plan/SessionList.vue', 'src/components/plan/SpecChatPopup.vue', 'src/components/plan/ChatModelBadge.vue',
    'src/components/plan/ChatMessageList.css', 'src/components/plan/ChatComposer.css', 'src/components/plan/AgentChatPanel.css', 'src/styles/multi-turn.css',
    'src/views/PlanPage.vue', 'src/components/plan/SpecTreePanel.vue', 'src/components/plan/SpecFocusedView.vue', 'src/components/plan/SpecCommentsLayer.vue',
    'src/components/plan/FloatingToc.vue', 'src/styles/spec-mode/prose-toc.css',
    'src/styles/settings-page.css', 'src/views/SettingsPage.vue', 'src/components/settings/SettingsTabExecution.vue', 'src/components/settings/SettingsTabAppearance.vue',
    'src/components/settings/SettingsTabSandbox.vue', 'src/components/settings/SettingsTabGithub.vue', 'src/components/settings/SettingsTabAbout.vue',
    'src/components/settings/SettingToggle.vue', 'src/components/AppSelect.vue', 'src/components/HarnessSelect.vue',
    'src/views/AgentGraphPage.vue', 'src/components/AgentGraphCanvas.vue', 'src/components/AgentEditor.vue', 'src/styles/agents.css', 'src/components/SystemPromptsManager.vue',
  ];
  it.each(tokenOnly)('%s uses tokens only', (file) => {
    const whole = read(file);
    const src = file.endsWith('.vue') ? whole.split(/<style[^>]*>/).slice(1).join('\n') : whole;
    expect(src).not.toMatch(/#[0-9a-fA-F]{3,8}\b/);
    // 50% is a circle and 0 removes a radius; neither is a rung on the ladder.
    expect(src.replace(/border-radius:\s*(50%|0)(?=[;\s])/g, '')).not.toMatch(/border-radius:\s*\d/);
  });

  it('modal.css addresses the sheet by class, not id', () => {
    expect(read('src/styles/modal.css')).not.toMatch(/#modal-/);
  });

  it('the replaced stylesheets and shell components are gone', () => {
    for (const f of [
      'src/styles/buttons.css', 'src/styles/badges.css', 'src/styles/forms.css',
      'src/styles/status-bar.css', 'src/styles/header.css', 'src/styles/header', 'src/styles/settings-modal.css',
      'src/components/Sidebar.vue', 'src/components/StatusBar.vue',
    ]) {
      expect(existsSync(resolve(root, f)), f).toBe(false);
    }
  });

  it('the rail is wallfacer\'s own: no ConsoleSidebar or console stylesheet import', () => {
    for (const f of walk(resolve(root, 'src'), '.vue').concat(walk(resolve(root, 'src'), '.ts'))) {
      const src = readFileSync(f, 'utf8');
      expect(src, rel(f)).not.toMatch(/ConsoleSidebar|latere-ui\/console/);
    }
  });
});
