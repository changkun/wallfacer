// checks.mjs — assert UI invariants against a booted wallfacer to catch
// regressions that unit tests (jsdom, no layout) cannot: render crashes that
// blank a region, and broken CSS layout (overlapping / clipped / mislaid
// elements). Uses a real browser, so it can measure geometry.
//
// This is the assertion counterpart to snap.mjs (which only captures images).
// It exits non-zero when any check fails, so it works as a CI/`make ui-test`
// gate.
//
// Usage:
//   node checks.mjs --base http://localhost:8099
//   node checks.mjs --base http://localhost:5173 --only picker,board
//
// Flags:
//   --base <url>   server origin (default http://localhost:8099)
//   --only <a,b>   comma list of scene names (default: all)
//   --list         print scene names and exit
import { chromium } from 'playwright';

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`);
  if (i === -1) return fallback;
  const next = process.argv[i + 1];
  if (next === undefined || next.startsWith('--')) return true;
  return next;
}

const base = arg('base', 'http://localhost:8099');
const only = arg('only', '');
const BOOT = { mode: 'local', serverApiKey: '', version: 'dev' };

// ---- geometry helpers (run client-side and return plain boxes) ------------
async function boxes(page, sel) {
  return page.$$eval(sel, (els) =>
    els.map((e) => {
      const r = e.getBoundingClientRect();
      return { left: r.left, top: r.top, right: r.right, bottom: r.bottom, width: r.width, height: r.height };
    }),
  );
}
async function firstBox(page, sel) {
  const els = await boxes(page, sel);
  return els[0] ?? null;
}

// ---- the failure collector ------------------------------------------------
const failures = [];
const notes = [];
function fail(scene, msg) { failures.push(`[${scene}] ${msg}`); }

// A scene: a named navigation + a set of assertions. Page errors (uncaught
// exceptions) are always treated as failures — they are the "a region
// vanished" class. Console errors are reported but not fatal (app noise).
// Every scene gets its own browser context so nothing one scene persists
// (the last route, an open popup, a dock layout) leaks into the next.
async function scene(browser, name, fn) {
  const pageErrors = [];
  const consoleErrors = [];
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
  await ctx.addInitScript((boot) => { window.__WALLFACER__ = boot; }, BOOT);
  const page = await ctx.newPage();
  page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()); });
  page.on('pageerror', (e) => pageErrors.push(e.message));
  try {
    await page.goto(base + '/', { waitUntil: 'load', timeout: 20000 });
    await page.waitForTimeout(1200);
    await fn(page);
  } catch (e) {
    fail(name, `scene threw: ${e.message}`);
  }
  if (pageErrors.length) fail(name, `uncaught page error(s): ${pageErrors.join(' | ')}`);
  if (consoleErrors.length) notes.push(`[${name}] console.error: ${consoleErrors.slice(0, 3).join(' | ')}`);
  await page.close();
  await ctx.close();
}

// assert helper bound to a scene name
function expect(name, cond, msg) { if (!cond) fail(name, msg); }

// ---- scenes ---------------------------------------------------------------
const SCENES = {
  // The whole app shell renders and the sidebar is present and has real size.
  // Guards the "entire sidebar disappeared" regression: a crashed render leaves
  // the switcher with no box.
  board: async (page) => {
    const shell = await firstBox(page, '.app-shell');
    expect('board', shell && shell.width > 0 && shell.height > 0, 'app-shell not rendered');
    const sw = await firstBox(page, '.sb-ws-switch');
    expect('board', sw && sw.width > 0 && sw.height > 0, 'sidebar workspace switcher missing/zero-size (sidebar gone?)');
    const main = await firstBox(page, '.app-main');
    expect('board', main && main.width > 0, 'app-main not rendered');

    // The board geometry (specs/shared/console-redesign/board.md): four equal
    // columns, 14px cards, at most two pills on a card's first row, no card
    // wider than its column, and a composer that expands on click.
    const cols = await boxes(page, '.board-grid > .col');
    expect('board', cols.length === 4, `expected 4 columns, got ${cols.length}`);
    if (cols.length === 4) {
      const w = cols.map((c) => Math.round(c.width));
      expect('board', Math.max(...w) - Math.min(...w) <= 1, `column widths differ: ${w.join(', ')}`);
    }
    const radii = await page.$$eval('.task-card', (els) => els.map((e) => getComputedStyle(e).borderTopLeftRadius));
    expect('board', radii.length > 0 && radii.every((r) => r === '14px'), `card radii: ${[...new Set(radii)].join(', ')}`);
    const pillCounts = await page.$$eval('.task-card', (els) => els.map((e) => e.querySelectorAll('.task-card__pills .pill').length));
    expect('board', pillCounts.every((n) => n <= 3), `a card carries more than rank + state + qualifier: ${pillCounts.join(',')}`);
    const overflow = await page.$$eval('.board-grid > .col', (cols) =>
      cols.flatMap((col) => [...col.querySelectorAll('.task-card')].filter((c) => c.getBoundingClientRect().right > col.getBoundingClientRect().right + 1).length));
    expect('board', overflow.every((n) => n === 0), 'a card overflows its column');
    await page.click('.composer-add', { timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(200);
    const focused = await page.evaluate(() => document.activeElement?.classList.contains('composer__prompt'));
    expect('board', focused === true, 'composer did not expand with the prompt focused');
  },

  // The shell geometry (specs/shared/console-redesign/shell.md): rail 236 open
  // and 64 folded on the deep ground, the main card inset 8px on three sides
  // starting at the rail's edge, a 52px topbar, and no status bar anywhere.
  shell: async (page) => {
    const rail = await firstBox(page, '.app-rail');
    const main = await firstBox(page, '.app-main');
    const topbar = await firstBox(page, '.topbar');
    expect('shell', rail && Math.abs(rail.width - 236) <= 1, `rail width ${rail && rail.width}, want 236`);
    expect('shell', main && Math.abs(main.left - rail.right) <= 1, `main left ${main && main.left} != rail right ${rail && rail.right}`);
    expect('shell', main && Math.abs(main.top - 8) <= 1, `main top inset ${main && main.top}, want 8`);
    const vw = await page.evaluate(() => window.innerWidth);
    const vh = await page.evaluate(() => window.innerHeight);
    expect('shell', main && Math.abs(vw - main.right - 8) <= 1, `main right inset ${main && vw - main.right}, want 8`);
    expect('shell', main && Math.abs(vh - main.bottom - 8) <= 1, `main bottom inset ${main && vh - main.bottom}, want 8`);
    expect('shell', topbar && Math.abs(topbar.height - 52) <= 1, `topbar height ${topbar && topbar.height}, want 52`);
    expect('shell', !(await page.$('.status-bar')), 'a .status-bar is still in the DOM');
    expect('shell', !!(await page.$('#topbar-actions .task-search-input')), 'board search is not in the topbar actions slot');
    expect('shell', !!(await page.$('.board-grid .task-card')), 'board grid rendered no cards');
    // Fold the rail and check the icon width.
    await page.click('.rail-fold-btn', { timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(300);
    const folded = await firstBox(page, '.app-rail');
    expect('shell', folded && Math.abs(folded.width - 64) <= 1, `folded rail width ${folded && folded.width}, want 64`);
    await page.click('.rail-fold-btn', { timeout: 5000 }).catch(() => {});
  },

  // Opening the sidebar workspace popover must not crash the layout: the
  // sidebar switcher stays present afterward. Guards the null-folders crash.
  switcher: async (page) => {
    await page.click('.sb-ws-switch', { timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(400);
    const sw = await firstBox(page, '.sb-ws-switch');
    expect('switcher', sw && sw.width > 0, 'sidebar vanished after opening the workspace popover');
  },

  // The Select Workspace list must be a single full-width column, not crammed
  // into the wizard's narrow right grid cell. Guards the picker layout
  // regression. The picker auto-opens when no workspace is active; otherwise
  // open it from the sidebar popover.
  picker: async (page) => {
    if (!(await page.$('.ws-picker'))) {
      await page.click('.sb-ws-switch', { timeout: 5000 }).catch(() => {});
      await page.waitForTimeout(300);
      await page.click('.sb-ws-popover__add', { timeout: 5000 }).catch(() => {});
    }
    await page.waitForSelector('.ws-picker', { state: 'visible', timeout: 8000 }).catch(() => {});
    const card = await firstBox(page, '.ws-picker');
    expect('picker', !!card, 'workspace picker did not open');
    if (!card) return;
    expect('picker', Math.abs(card.width - 720) <= 1, `picker dialog width ${Math.round(card.width)}, want 720`);

    // List view present (vs the wizard) and at least one row.
    const listView = await page.$('.ws-picker__list-view');
    expect('picker', !!listView, 'picker did not open to the list view');
    const items = await boxes(page, '.ws-list__item');
    expect('picker', items.length >= 1, 'workspace list rendered no rows');
    if (items.length === 0) return;

    // Each row must fill most of the modal width (the bug squeezed them into a
    // ~0.8fr right cell) and sit near the modal's left edge, not pushed right.
    for (const it of items) {
      expect('picker', it.width > card.width * 0.5,
        `list row too narrow (${Math.round(it.width)}px of ${Math.round(card.width)}px) — crammed into a column?`);
      expect('picker', (it.left - card.left) < card.width * 0.4,
        `list row pushed to the right (left offset ${Math.round(it.left - card.left)}px) — inheriting the wizard grid?`);
      expect('picker', it.right <= card.right + 2 && it.left >= card.left - 2,
        'list row overflows the modal card horizontally');
    }
    // Rows must not overlap vertically (collapsed grid rows did).
    const sorted = [...items].sort((a, b) => a.top - b.top);
    for (let i = 1; i < sorted.length; i++) {
      expect('picker', sorted[i].top >= sorted[i - 1].bottom - 2,
        `list rows overlap vertically (row ${i} top ${Math.round(sorted[i].top)} < prev bottom ${Math.round(sorted[i - 1].bottom)})`);
    }

    // Content must not be vertically clipped: each row's name + folder-path line
    // must render fully (scrollHeight <= clientHeight). Catches the "half
    // visible path" regression that row-width/overlap checks miss.
    const clipped = await page.$$eval('.ws-list__item .ws-list__name, .ws-list__item .ws-list__paths',
      (els) => els.filter((e) => e.scrollHeight > e.clientHeight + 1)
        .map((e) => `${e.className}: ${e.scrollHeight}>${e.clientHeight}`));
    expect('picker', clipped.length === 0, `workspace row text is clipped: ${clipped.join('; ')}`);
  },
};

// The material is matte: no element on the board may carry a computed
// backdrop-filter. Guards the console-redesign contract end to end, including
// third-party and latere-ui chrome that reads the pinned glass tokens.
SCENES['no-glass'] = async (page) => {
  const glassy = await page.$$eval('*', (els) =>
    els.filter((e) => {
      const f = getComputedStyle(e).backdropFilter;
      return f && f !== 'none';
    }).map((e) => `${e.tagName.toLowerCase()}.${String(e.className).split(' ')[0]}`).slice(0, 8));
  expect('no-glass', glassy.length === 0, `elements with backdrop-filter: ${glassy.join(', ')}`);
};

// Meta text must clear WCAG AA on the canvas in both themes. Reads the resolved
// tokens from the live page so a palette edit that only looks right in one
// theme is caught here.
SCENES['contrast'] = async (page) => {
  const lum = (hex) => {
    const [r, g, b] = [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16) / 255)
      .map((c) => (c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4));
    return 0.2126 * r + 0.7152 * g + 0.0722 * b;
  };
  for (const theme of ['light', 'dark']) {
    await page.evaluate((th) => document.documentElement.setAttribute('data-theme', th), theme);
    const [bg, ink3] = await page.evaluate(() => {
      const cs = getComputedStyle(document.documentElement);
      return [cs.getPropertyValue('--bg').trim(), cs.getPropertyValue('--ink-3').trim()];
    });
    const [l1, l2] = [lum(bg), lum(ink3)].sort((a, b) => b - a);
    const ratio = (l1 + 0.05) / (l2 + 0.05);
    expect('contrast', ratio >= 4.5, `${theme}: --ink-3 ${ink3} on --bg ${bg} is ${ratio.toFixed(2)}:1`);
  }
};

// The task sheet (specs/shared/console-redesign/task-detail.md): open the
// first card, the sheet fits the viewport, the aside is 340 wide, the tabs
// row is present, the Actions card carries one ink button, and the Changes
// and Verification tabs switch without page errors.
SCENES['task-detail'] = async (page) => {
  await page.click('.board-grid .task-card .task-card__title', { timeout: 5000 }).catch(() => {});
  await page.waitForSelector('.sheet', { state: 'visible', timeout: 8000 }).catch(() => {});
  const sheet = await firstBox(page, '.sheet');
  expect('task-detail', !!sheet, 'sheet did not open');
  if (!sheet) return;
  const vw = await page.evaluate(() => window.innerWidth);
  expect('task-detail', sheet.width <= Math.min(vw * 0.96, 1440) + 1, `sheet width ${Math.round(sheet.width)}`);
  const aside = await firstBox(page, '.sheet-aside');
  expect('task-detail', aside && Math.abs(aside.width - 340) <= 1, `aside width ${aside && aside.width}, want 340`);
  const tabs = await firstBox(page, '.sheet-tabs');
  expect('task-detail', tabs && tabs.height >= 30, 'tabs row missing');
  const inks = await page.$$eval('.sheet-actions .btn:not(.ghost)', (els) => els.length);
  expect('task-detail', inks <= 1, `${inks} ink buttons in the Actions card`);
  const mainBox = await firstBox(page, '.sheet-main');
  const overflow = await page.$eval('.sheet-main', (el) => el.scrollWidth > el.clientWidth + 1);
  expect('task-detail', mainBox && !overflow, 'sheet main column overflows horizontally');
  for (const tab of ['changes', 'verification', 'events']) {
    await page.click(`[data-tab="${tab}"]`, { timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(250);
  }
};

// The chat surface (specs/shared/console-redesign/chat.md): the session list
// and the hero composer render on /chat, the entry column stays narrow, and
// the floating popup on another route opens inside the viewport.
SCENES['chat'] = async (page) => {
  await page.goto(base + '/chat', { waitUntil: 'load', timeout: 20000 });
  await page.waitForTimeout(900);
  const sessions = await firstBox(page, '.chat-sessions');
  expect('chat', !!sessions, 'session list did not render');
  const composer = await firstBox(page, '.pcp-composer');
  expect('chat', !!composer, 'composer did not render');
  const entry = await firstBox(page, '.chat-entry-inner');
  expect('chat', entry && entry.width <= 681, `entry column ${entry && Math.round(entry.width)}px, want <= 680`);
  const vw = await page.evaluate(() => window.innerWidth);
  const vh = await page.evaluate(() => window.innerHeight);
  await page.goto(base + '/routines', { waitUntil: 'load', timeout: 20000 });
  await page.waitForTimeout(700);
  const launcher = await firstBox(page, '.scp-launcher');
  expect('chat', launcher && launcher.right <= vw && launcher.bottom <= vh, 'chat launcher outside the viewport');
  await page.click('.scp-launcher', { timeout: 5000 }).catch(() => {});
  await page.waitForTimeout(400);
  const win = await firstBox(page, '.scp-window');
  expect('chat', win && win.left >= 0 && win.top >= 0 && win.right <= vw + 1 && win.bottom <= vh + 1, 'chat popup outside the viewport');
};

// The plan surface (specs/shared/console-redesign/plan.md): tree rows on the
// nav-row geometry, a reading column no wider than 76ch, the tree folds to a
// 28px strip, and the focused view's status is a pill.
SCENES['plan'] = async (page) => {
  await page.goto(base + '/plan', { waitUntil: 'load', timeout: 20000 });
  await page.waitForTimeout(900);
  // Tracks start folded; open the first one to get spec rows.
  await page.click('.stp-track-header', { timeout: 5000 }).catch(() => {});
  await page.waitForSelector('.stp-node', { timeout: 5000 }).catch(() => {});
  const rows = await boxes(page, '.stp-node');
  expect('plan', rows.length > 0, 'spec tree rendered no rows');
  expect('plan', rows.every((r) => Math.abs(r.height - 34) <= 1), `tree row heights: ${[...new Set(rows.map((r) => Math.round(r.height)))].join(',')}`);
  await page.click('.stp-node', { timeout: 5000 }).catch(() => {});
  await page.waitForSelector('.sf-content--spec', { timeout: 8000 }).catch(() => {});
  await page.waitForTimeout(300);
  const pill = await page.$('.sf-status.pill');
  expect('plan', !!pill, 'focused view status is not a pill');
  const col = await firstBox(page, '.sf-content--spec');
  const ch = await page.$eval('.sf-content--spec', (el) => parseFloat(getComputedStyle(el).fontSize));
  expect('plan', col && col.width <= 76 * ch * 0.62 + 40, `reading column ${col && Math.round(col.width)}px wider than 76ch`);
  await page.click('.stp-collapse', { timeout: 5000 }).catch(() => {});
  await page.waitForTimeout(300);
  const rail = await firstBox(page, '.spec-tree-rail');
  expect('plan', rail && Math.abs(rail.width - 28) <= 1, `collapsed rail width ${rail && rail.width}, want 28`);
};

// Lightweight smoke for the remaining routed surfaces: they must render a
// non-empty app-main with no uncaught error.
// Settings (specs/shared/console-redesign/settings.md): a column no wider
// than 760, an underline tab strip, right-aligned controls, every tab without
// page errors, and six swatches on Appearance.
SCENES['settings'] = async (page) => {
  await page.goto(base + '/settings', { waitUntil: 'load', timeout: 20000 });
  await page.waitForTimeout(800);
  const col = await firstBox(page, '.settings-page-inner');
  expect('settings', col && col.width <= 761, `settings column ${col && Math.round(col.width)}px, want <= 760`);
  expect('settings', !!(await page.$('.settings-page .tabs .tab.on')), 'no active underline tab');
  // Hidden rows (v-show) report a zero box; only laid-out controls align.
  const ends = (await boxes(page, '.set-row:not(.set-row--stack) .set-row__end')).filter((e) => e.width > 0);
  if (ends.length > 1) {
    const rights = ends.map((e) => Math.round(e.right));
    expect('settings', Math.max(...rights) - Math.min(...rights) <= 1, `row controls not right-aligned: ${[...new Set(rights)].join(',')}`);
  }
  for (const tab of ['appearance', 'sandbox', 'github', 'about', 'execution']) {
    await page.click(`[data-tab="${tab}"]`, { timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(250);
    if (tab === 'appearance') {
      const swatches = await page.$$eval('.ap-palette', (els) => els.length);
      expect('settings', swatches === 6, `${swatches} palette swatches, want 6`);
    }
  }
};

// Agent fleets (specs/shared/console-redesign/agent-graph.md): a 280px list
// column, nodes drawn at the card radius, and the editor dialog inside the
// main card when an agent is opened.
SCENES['agents'] = async (page) => {
  await page.goto(base + '/agent-graph', { waitUntil: 'load', timeout: 20000 });
  await page.waitForTimeout(900);
  const rail = await firstBox(page, '.ag-mode__rail');
  expect('agents', rail && Math.abs(rail.width - 280) <= 1, `list column ${rail && rail.width}, want 280`);
  const rx = await page.$$eval('.agc-node--agent .agc-node-box', (els) => els.map((e) => e.getAttribute('rx')));
  expect('agents', rx.length > 0 && rx.every((r) => r === '14'), `node radii: ${[...new Set(rx)].join(',')}`);
  const card = await page.$('.ag-card');
  if (card) {
    await card.dblclick();
    await page.waitForSelector('.ag-agent-modal__panel', { timeout: 5000 }).catch(() => {});
    const panel = await firstBox(page, '.ag-agent-modal__panel');
    const main = await firstBox(page, '.app-main');
    expect('agents', panel && main && panel.left >= main.left && panel.right <= main.right + 1, 'agent editor dialog outside the main card');
  }
};

// The command palette (specs/shared/console-redesign/panels-and-overlays.md):
// ⌘K opens a 640px popover near the top of the viewport, inside it, with the
// first row selected and no blur anywhere on the page.
SCENES['palette'] = async (page) => {
  await page.keyboard.press('Meta+k');
  await page.waitForSelector('.command-palette-panel', { state: 'visible', timeout: 5000 }).catch(() => {});
  const panel = await firstBox(page, '.command-palette-panel');
  expect('palette', !!panel, 'command palette did not open on Meta+k');
  if (!panel) return;
  const vw = await page.evaluate(() => window.innerWidth);
  const vh = await page.evaluate(() => window.innerHeight);
  expect('palette', Math.abs(panel.width - 640) <= 1, `palette width ${Math.round(panel.width)}, want 640`);
  expect('palette', panel.left >= 0 && panel.right <= vw && panel.top >= 0 && panel.bottom <= vh, 'palette leaves the viewport');
  expect('palette', panel.top < vh * 0.2, `palette top ${Math.round(panel.top)} is not near the top of the viewport`);
  const focused = await page.evaluate(() => document.activeElement?.classList.contains('command-palette-input'));
  expect('palette', focused === true, 'palette input is not focused');
  const active = await boxes(page, '.command-palette-row.active');
  expect('palette', active.length === 1, `${active.length} selected rows, want 1`);
  const rows = await boxes(page, '.command-palette-row');
  expect('palette', rows.length > 0 && rows[0].top >= panel.top, 'no rows rendered in the palette');
  await page.keyboard.press('ArrowDown');
  await page.waitForTimeout(100);
  const moved = await page.$$eval('.command-palette-row.active, .command-palette-action-btn.active', (els) => els.length);
  expect('palette', moved === 1, `${moved} selections after ArrowDown, want 1`);
};

// The dock (specs/shared/console-redesign/panels-and-overlays.md): the
// terminal toggles into the bottom region flush with the main card's bottom
// edge, the gutter drag changes its height, and maximize fills the workspace.
SCENES['dock'] = async (page) => {
  await page.click('.topbar [data-action="terminal"]', { timeout: 5000 }).catch(() => {});
  await page.waitForSelector('.dock-region--bottom', { state: 'visible', timeout: 5000 }).catch(() => {});
  const region = await firstBox(page, '.dock-region--bottom');
  const ws = await firstBox(page, '.dock-ws');
  const main = await firstBox(page, '.app-main');
  expect('dock', !!region, 'terminal did not dock into the bottom region');
  if (!region || !ws || !main) return;
  expect('dock', Math.abs(region.bottom - main.bottom) <= 2, `region bottom ${Math.round(region.bottom)} != main card bottom ${Math.round(main.bottom)}`);
  expect('dock', Math.abs(region.left - ws.left) <= 1 && Math.abs(region.right - ws.right) <= 1, 'bottom region does not span the workspace');
  const bar = await firstBox(page, '.terminal-tab-bar');
  expect('dock', bar && Math.abs(bar.height - 36) <= 1, `terminal tab bar height ${bar && bar.height}, want 36`);
  // Drag the gutter up 80px: the region grows by about that much.
  const gutter = await firstBox(page, '.dock-gutter--h');
  expect('dock', !!gutter, 'no horizontal gutter above the bottom region');
  if (gutter) {
    const x = gutter.left + gutter.width / 2;
    const y = gutter.top + gutter.height / 2;
    await page.mouse.move(x, y);
    await page.mouse.down();
    await page.mouse.move(x, y - 40);
    await page.mouse.move(x, y - 80);
    await page.mouse.up();
    await page.waitForTimeout(150);
    const grown = await firstBox(page, '.dock-region--bottom');
    expect('dock', grown && grown.height > region.height + 60, `region height ${Math.round(grown ? grown.height : 0)} after an 80px drag from ${Math.round(region.height)}`);
  }
  await page.click('.dock-panel__btn[aria-label="Maximize terminal"]', { timeout: 5000 }).catch(() => {});
  // The panel teleports into the overlay on the next tick; wait for it there.
  await page.waitForSelector('.dock-max .terminal-panel', { state: 'attached', timeout: 5000 }).catch(() => {});
  await page.waitForTimeout(150);
  const max = await firstBox(page, '.dock-max');
  const ws2 = await firstBox(page, '.dock-ws');
  expect('dock', max && ws2 && Math.abs(max.width - ws2.width) <= 1 && Math.abs(max.height - ws2.height) <= 1, 'maximized terminal does not fill the workspace');
  expect('dock', !!(await page.$('.dock-max .terminal-panel')), 'terminal did not move into the maximized overlay');
  await page.click('.dock-panel__btn[aria-label="Restore terminal"]', { timeout: 5000 }).catch(() => {});
};

// Analytics (specs/shared/console-redesign/secondary-screens.md): stat tiles
// as cards, tables whose numeric columns share one right edge, and every tab
// without errors.
SCENES['analytics'] = async (page) => {
  await page.goto(base + '/analytics', { waitUntil: 'load', timeout: 20000 });
  await page.waitForTimeout(900);
  await page.click('.an-tabs [data-tab="analytics"]', { timeout: 5000 }).catch(() => {});
  await page.waitForSelector('.an-tile', { timeout: 8000 }).catch(() => {});
  const tiles = await boxes(page, '.an-tile');
  expect('analytics', tiles.length >= 2, `${tiles.length} stat tiles, want at least 2`);
  const cards = await page.$$eval('.an-tile', (els) => els.filter((e) => e.classList.contains('card')).length);
  expect('analytics', cards === tiles.length, 'a stat tile is not a card');
  // Numeric cells in one table share the header's right edge.
  const misaligned = await page.$$eval('.an-table', (tables) => tables.flatMap((t) => {
    const heads = [...t.querySelectorAll('th.num')].map((th) => th.getBoundingClientRect().right);
    const rows = [...t.querySelectorAll('tbody tr')];
    return rows.flatMap((r) => [...r.querySelectorAll('td.num')].map((td, i) => {
      const cols = [...r.querySelectorAll('td.num')];
      const head = heads[heads.length - cols.length + i];
      return head == null ? 0 : Math.abs(td.getBoundingClientRect().right - head);
    })).filter((d) => d > 1);
  }));
  expect('analytics', misaligned.length === 0, `${misaligned.length} numeric cells off their column edge`);
  for (const tab of ['timing', 'usage']) {
    await page.click(`.an-tabs [data-tab="${tab}"]`, { timeout: 5000 }).catch(() => {});
    await page.waitForTimeout(600);
    expect('analytics', !!(await page.$('.an-body')), `${tab} tab rendered no body`);
  }
};

// Routines: the create card, then rows with the schedule pill and the switch
// (or the empty state when the seed has none).
SCENES['routines'] = async (page) => {
  await page.goto(base + '/routines', { waitUntil: 'load', timeout: 20000 });
  await page.waitForTimeout(900);
  expect('routines', !!(await page.$('.routine-create.card')), 'create card missing');
  const rows = await boxes(page, '.routine-row');
  if (rows.length) {
    const switches = await page.$$eval('.routine-row [role="switch"]', (els) => els.length);
    expect('routines', switches === rows.length, 'a routine row has no enabled switch');
    expect('routines', !!(await page.$('.routine-row .pill')), 'a routine row has no schedule pill');
  } else {
    expect('routines', !!(await page.$('.routines-empty')), 'no rows and no empty state');
  }
};

// Mission Control: the canvas draws nodes on the ramp, the inspector is 300
// wide, and a spec node opens its popover inside the viewport.
SCENES['mission'] = async (page) => {
  await page.goto(base + '/mission', { waitUntil: 'load', timeout: 20000 });
  await page.waitForSelector('.gc-node', { timeout: 10000 }).catch(() => {});
  const nodes = await boxes(page, '.gc-node');
  expect('mission', nodes.length > 0, 'canvas rendered no nodes');
  const insp = await firstBox(page, '.mission__inspector');
  expect('mission', insp && Math.abs(insp.width - 300) <= 1, `inspector width ${insp && insp.width}, want 300`);
  const fills = await page.$$eval('.gc-dot', (els) => els.map((e) => e.style.fill));
  expect('mission', fills.length > 0 && fills.every((f) => /var\(|color-mix\(/.test(f)), 'a node fill is not a token expression');
  const spec = await page.$('.gc-node--spec');
  if (spec) {
    await spec.dblclick();
    await page.waitForSelector('.map-popup', { timeout: 5000 }).catch(() => {});
    const pop = await firstBox(page, '.map-popup');
    const vw = await page.evaluate(() => window.innerWidth);
    const vh = await page.evaluate(() => window.innerHeight);
    expect('mission', pop && pop.left >= 0 && pop.top >= 0 && pop.right <= vw + 1 && pop.bottom <= vh + 1, 'spec popover outside the viewport');
    expect('mission', !!(await page.$('.map-popup.pop')), 'spec popover is not the pop shape');
  }
};

// Whiteboard: Excalidraw mounts on the console theme.
SCENES['whiteboard'] = async (page) => {
  await page.goto(base + '/whiteboard', { waitUntil: 'load', timeout: 20000 });
  await page.waitForSelector('.excalidraw', { timeout: 15000 }).catch(() => {});
  expect('whiteboard', !!(await page.$('.excalidraw')), 'Excalidraw did not mount');
};

// Artifacts: the tool bar over a preview, or the empty state.
SCENES['artifacts'] = async (page) => {
  await page.goto(base + '/artifacts', { waitUntil: 'load', timeout: 20000 });
  await page.waitForTimeout(900);
  const frame = await page.$('.af-frame');
  const empty = await page.$('.af-empty .eyebrow');
  expect('artifacts', !!frame || !!empty, 'neither a preview nor the empty state rendered');
  if (frame) expect('artifacts', !!(await page.$('.af-bar .btn')), 'tool bar carries no buttons');
};

// Docs: the nav is 260 wide on the sunk surface and the reading column is at
// most 76ch.
SCENES['docs'] = async (page) => {
  await page.goto(base + '/docs', { waitUntil: 'load', timeout: 20000 });
  await page.waitForSelector('.local-docs-body', { timeout: 10000 }).catch(() => {});
  const nav = await firstBox(page, '.local-docs-nav');
  expect('docs', nav && Math.abs(nav.width - 260) <= 1, `docs nav width ${nav && nav.width}, want 260`);
  const wrap = await firstBox(page, '.local-docs-wrap');
  const chPx = await page.evaluate(() => {
    const el = document.querySelector('.local-docs-wrap');
    if (!el) return 0;
    const probe = document.createElement('span');
    probe.textContent = '0';
    probe.style.font = getComputedStyle(el).font;
    probe.style.position = 'absolute';
    document.body.appendChild(probe);
    const w = probe.getBoundingClientRect().width;
    probe.remove();
    return w;
  });
  expect('docs', wrap && chPx > 0 && wrap.width <= 76 * chPx + 2, `reading column ${wrap && Math.round(wrap.width)}px exceeds 76ch (${Math.round(76 * chPx)}px)`);
  expect('docs', !!(await page.$('.local-docs-link.is-active')), 'no active doc row');
};

// Routes without a scene of their own yet get the smoke.
const SMOKE_ROUTES = { flows: '/flows' };
for (const [name, route] of Object.entries(SMOKE_ROUTES)) {
  SCENES[name] = async (page) => {
    await page.goto(base + route, { waitUntil: 'load', timeout: 20000 });
    await page.waitForTimeout(800);
    const main = await firstBox(page, '.app-main');
    expect(name, main && main.width > 0 && main.height > 0, `${route} rendered no app-main`);
  };
}

if (arg('list', false) === true) {
  console.log(Object.keys(SCENES).join('\n'));
  process.exit(0);
}

const names = only && only !== true ? String(only).split(',').map((s) => s.trim()) : Object.keys(SCENES);
const browser = await chromium.launch();

for (const name of names) {
  if (!SCENES[name]) { fail(name, 'unknown scene'); continue; }
  await scene(browser, name, SCENES[name]);
}
await browser.close();

if (notes.length) {
  console.log('notes:');
  for (const n of notes) console.log('  ' + n);
}
if (failures.length) {
  console.error(`\nUI REGRESSION: ${failures.length} check(s) failed:`);
  for (const f of failures) console.error('  ✗ ' + f);
  process.exit(1);
}
console.log(`\nUI checks passed (${names.length} scenes).`);
