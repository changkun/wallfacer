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
async function scene(ctx, name, fn) {
  const pageErrors = [];
  const consoleErrors = [];
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
  },
};

// Lightweight smoke for the remaining routed surfaces: they must render a
// non-empty app-main with no uncaught error.
const SMOKE_ROUTES = { settings: '/settings', plan: '/plan', analytics: '/analytics', agents: '/agents', flows: '/flows' };
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
const BOOT = { mode: 'local', serverApiKey: '', version: 'dev' };
const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, deviceScaleFactor: 1 });
await ctx.addInitScript((boot) => { window.__WALLFACER__ = boot; }, BOOT);

for (const name of names) {
  if (!SCENES[name]) { fail(name, 'unknown scene'); continue; }
  await scene(ctx, name, SCENES[name]);
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
