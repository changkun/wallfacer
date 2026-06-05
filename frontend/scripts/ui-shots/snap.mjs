// snap.mjs — screenshot named wallfacer UI surfaces at retina 2x.
//
// Self-contained Playwright capture (does not depend on .parity/, which is
// throwaway). Injects local-mode boot config so the SPA renders the board.
//
// Usage:
//   node snap.mjs --base http://localhost:8099 --out /tmp/wf-shots
//   node snap.mjs --base http://localhost:5173 --only board,palette,settings
//   node snap.mjs --list
//
// Flags:
//   --base <url>      server origin to shoot (default http://localhost:8099)
//   --out  <dir>      output dir (default /tmp/wf-shots)
//   --only <a,b,c>    comma list of surface names (default: all)
//   --list            print surface names and exit
//   --width/--height  viewport (default 1440x900); deviceScaleFactor is 2x
//
// Prints JSON: [{name, file, errors}] so callers can detect page errors.
import { chromium } from 'playwright';
import { mkdirSync } from 'node:fs';

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`);
  if (i === -1) return fallback;
  const next = process.argv[i + 1];
  if (next === undefined || next.startsWith('--')) return true;
  return next;
}

// Surface table: route + optional pre-shot steps. Selectors cribbed from the
// working .parity check scripts. Steps run in order before the screenshot.
const SURFACES = {
  board: { route: '/' },
  palette: { route: '/', steps: [{ key: 'Meta+k' }, { wait: 700 }] },
  'task-detail': {
    route: '/',
    // Open the first board card (.card is the root) to render the detail drawer.
    steps: [{ click: '.card' }, { wait: 800 }],
  },
  settings: { route: '/settings', steps: [{ wait: 600 }] },
  analytics: { route: '/analytics', steps: [{ wait: 900 }] },
  plan: { route: '/plan', steps: [{ wait: 800 }] },
  routines: { route: '/routines', steps: [{ wait: 800 }] },
  agents: { route: '/agents', steps: [{ wait: 800 }] },
  flows: { route: '/flows', steps: [{ wait: 800 }] },
  docs: { route: '/docs', steps: [{ wait: 800 }] },
};

const base = arg('base', 'http://localhost:8099');
const outDir = arg('out', '/tmp/wf-shots');
const width = Number(arg('width', 1440));
const height = Number(arg('height', 900));
const only = arg('only', '');

if (arg('list', false) === true) {
  console.log(Object.keys(SURFACES).join('\n'));
  process.exit(0);
}

const names = only && only !== true ? String(only).split(',').map((s) => s.trim()) : Object.keys(SURFACES);
mkdirSync(outDir, { recursive: true });

const BOOT = { mode: 'local', serverApiKey: '', version: 'dev' };
const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width, height }, deviceScaleFactor: 2 });
await ctx.addInitScript((b) => { window.__WALLFACER__ = b; }, BOOT);

const results = [];
for (const name of names) {
  const surface = SURFACES[name];
  if (!surface) {
    results.push({ name, error: 'unknown surface' });
    continue;
  }
  const errors = [];
  const page = await ctx.newPage();
  page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()); });
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message));

  // 'load' not 'networkidle' — the board holds SSE connections open forever.
  await page
    .goto(base + surface.route, { waitUntil: 'load', timeout: 20000 })
    .catch((e) => errors.push('GOTO: ' + e.message));
  await page.waitForTimeout(1200);

  for (const step of surface.steps || []) {
    try {
      if (step.key) await page.keyboard.press(step.key);
      else if (step.click) await page.click(step.click, { timeout: 5000 });
      else if (step.waitFor) await page.waitForSelector(step.waitFor, { state: 'visible', timeout: 8000 });
      else if (step.wait != null) await page.waitForTimeout(step.wait);
    } catch (e) {
      errors.push(`STEP ${JSON.stringify(step)}: ${e.message}`);
    }
  }

  const file = `${outDir}/${name}.png`;
  await page.screenshot({ path: file });
  await page.close();
  results.push({ name, file, errors });
}

await browser.close();
console.log(JSON.stringify(results, null, 2));
