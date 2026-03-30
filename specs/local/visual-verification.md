---
title: Visual Verification for UI Changes
status: drafted
depends_on: []
affects:
  - playwright.config.ts
  - ui/tests/visual/
effort: medium
created: 2026-03-21
updated: 2026-03-30
author: changkun
dispatched_task_id: null
---

# Plan: Visual Verification for UI Changes

## Problem

UI changes are merged without visual verification. CSS and layout issues
(broken flex containers, overflowing menus, misaligned elements) ship
because the test suite only covers logic — there are no browser-based
tests, no screenshot baselines, and no way for CI or an agent to confirm
a change looks correct.

## Current State (as of 2026-03-21)

- **Frontend tests**: 33 Vitest files running in a Node.js VM context.
  They mock browser APIs (`document`, `EventSource`, `fetch`) and assert
  on innerHTML / state. They cannot detect visual regressions.
- **Backend tests**: 109 Go test files. No browser integration.
- **UI serving**: All assets are embedded in the Go binary via
  `//go:embed ui`. The Go server renders `ui/index.html` as a template
  and serves JS/CSS from the embedded FS. There is no separate dev
  server (no Vite/Webpack).
- **CI**: GitHub Actions runs `go test ./...` and `npx vitest run`.
  No browser-based step.
- **No**: Playwright, Cypress, Puppeteer, Storybook, or any visual
  regression tooling.

---

## Approach: Playwright Screenshot Tests

Playwright is the best fit because it:
- Launches real Chromium against the actual Go server
- Supports `toHaveScreenshot()` with pixel-diff baselines
- Runs headless in CI (GitHub Actions has `ubuntu-latest` with browser
  deps pre-installed)
- Needs no framework changes — the Go server already serves everything

### Architecture

```
┌────────────────────────────────────────────────────────┐
│ Playwright test process                                │
│                                                        │
│  1. Build Go binary (go build -o wallfacer .)          │
│  2. Start server (./wallfacer run -no-browser -addr    │
│     :0 -no-workspaces) → capture actual port           │
│  3. For each test:                                     │
│     a. Seed state via API (POST /api/tasks, PUT        │
│        /api/config, etc.)                              │
│     b. Navigate browser to page / interact             │
│     c. await page.screenshot() or                      │
│        expect(locator).toHaveScreenshot()              │
│  4. Kill server                                        │
│                                                        │
│  Baselines stored in: ui/tests/screenshots/            │
│  Diffs on failure in: ui/tests/test-results/           │
└────────────────────────────────────────────────────────┘
```

---

## Phase 1: Infrastructure Setup

### 1.1 Install Playwright

```bash
npm install -D @playwright/test
npx playwright install chromium
```

Add to `package.json`:

```json
{
  "scripts": {
    "test:visual": "npx playwright test",
    "test:visual:update": "npx playwright test --update-snapshots"
  }
}
```

### 1.2 Playwright Config

Create `playwright.config.ts`:

```ts
import { defineConfig } from '@playwright/test';

export default defineConfig({
  testDir: 'ui/tests/visual',
  snapshotDir: 'ui/tests/screenshots',
  outputDir: 'ui/tests/test-results',
  timeout: 30_000,
  retries: 0,
  use: {
    baseURL: `http://localhost:${process.env.WALLFACER_PORT || 18080}`,
    screenshot: 'only-on-failure',
    viewport: { width: 1440, height: 900 },
    colorScheme: 'dark',
  },
  projects: [
    { name: 'chromium', use: { browserName: 'chromium' } },
  ],
  webServer: {
    command: 'go build -o wallfacer . && ./wallfacer run -no-browser -addr :18080 -no-workspaces',
    port: 18080,
    reuseExistingServer: !process.env.CI,
    timeout: 30_000,
  },
});
```

### 1.3 Gitignore

Add to `.gitignore`:

```
ui/tests/test-results/
```

Baseline screenshots (`ui/tests/screenshots/`) are committed.

### Files

- `playwright.config.ts` (new)
- `package.json` (add devDependency + scripts)
- `.gitignore` (add test-results)

---

## Phase 2: Core Visual Tests

### 2.1 Test Helpers

Create `ui/tests/visual/helpers.ts`:

```ts
import { Page } from '@playwright/test';

// Seed a workspace group via the API so the board renders.
export async function seedWorkspace(page: Page, paths: string[]) {
  await page.request.put('/api/workspaces', {
    data: { workspaces: paths },
  });
}

// Create a task via the API.
export async function createTask(page: Page, prompt: string, priority = 50) {
  const res = await page.request.post('/api/tasks', {
    data: { prompt, priority },
  });
  return res.json();
}

// Wait for the board to finish rendering.
export async function waitForBoard(page: Page) {
  await page.waitForSelector('.kanban-col', { timeout: 5000 });
}
```

### 2.2 Test Scenarios

Create `ui/tests/visual/header.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

test.describe('header', () => {
  test('empty board — no workspace tabs', async ({ page }) => {
    await page.goto('/');
    const header = page.locator('.app-header');
    await expect(header).toHaveScreenshot('header-empty.png');
  });

  test('with workspace groups — tabs render inline', async ({ page }) => {
    // Seed two workspace groups via API, then reload.
    await page.goto('/');
    const tabs = page.locator('.workspace-group-tabs');
    await expect(tabs).toHaveScreenshot('header-tabs.png');
  });
});
```

Create `ui/tests/visual/board.spec.ts`:

```ts
import { test, expect } from '@playwright/test';
import { createTask, waitForBoard } from './helpers';

test.describe('board', () => {
  test('kanban columns with tasks', async ({ page }) => {
    await createTask(page, 'Test task alpha');
    await createTask(page, 'Test task beta');
    await page.goto('/');
    await waitForBoard(page);
    await expect(page).toHaveScreenshot('board-with-tasks.png', {
      maxDiffPixelRatio: 0.01,
    });
  });
});
```

Create `ui/tests/visual/settings.spec.ts`:

```ts
import { test, expect } from '@playwright/test';

test.describe('settings modal', () => {
  test('opens and renders tabs', async ({ page }) => {
    await page.goto('/');
    await page.click('[title="Settings"]');
    const modal = page.locator('.modal-card');
    await expect(modal).toHaveScreenshot('settings-modal.png');
  });
});
```

### Priority Scenarios

| Test | What it catches |
|------|-----------------|
| Header empty | Brand + search + buttons layout |
| Header with tabs | Tab strip alignment, workspace chips inline |
| Header mobile (viewport 390×844) | Responsive wrap, tab overflow |
| Board empty | Column headers, spacing |
| Board with tasks | Card rendering, status colors |
| Settings modal | Modal layout, tab strip, form controls |
| Task detail modal | Logs, oversight, diff sections |
| Workspace picker modal | Browser panel, selection panel |

### Files

- `ui/tests/visual/helpers.ts` (new)
- `ui/tests/visual/header.spec.ts` (new)
- `ui/tests/visual/board.spec.ts` (new)
- `ui/tests/visual/settings.spec.ts` (new)

---

## Phase 3: CI Integration

### 3.1 GitHub Actions

Add a job to `.github/workflows/test.yml`:

```yaml
  visual:
    runs-on: ubuntu-latest
    needs: [build]
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.25'
      - uses: actions/setup-node@v4
        with:
          node-version: '22'
      - run: npm ci
      - run: npx playwright install --with-deps chromium
      - run: npm run test:visual
      - uses: actions/upload-artifact@v4
        if: failure()
        with:
          name: visual-test-results
          path: ui/tests/test-results/
          retention-days: 7
```

On failure, the diff images are uploaded as artifacts so reviewers can
see exactly what changed.

### 3.2 Makefile

```makefile
test-visual:
	npx playwright test

test-visual-update:
	npx playwright test --update-snapshots
```

---

## Phase 4: Responsive and Theme Coverage

### 4.1 Multiple Viewports

Add Playwright projects for common breakpoints:

```ts
projects: [
  {
    name: 'desktop',
    use: { viewport: { width: 1440, height: 900 } },
  },
  {
    name: 'tablet',
    use: { viewport: { width: 768, height: 1024 } },
  },
  {
    name: 'mobile',
    use: { viewport: { width: 390, height: 844 } },
  },
],
```

Each test generates separate baselines per project
(`header-tabs-desktop.png`, `header-tabs-mobile.png`).

### 4.2 Dark / Light Theme

The app uses CSS custom properties for theming. Add a second color
scheme project:

```ts
{
  name: 'desktop-light',
  use: { viewport: { width: 1440, height: 900 }, colorScheme: 'light' },
},
```

---

## Phase 5: Agent-Friendly Workflow

### 5.1 Programmatic Verification

An AI agent making UI changes can run:

```bash
npx playwright test --reporter=line 2>&1
```

If tests fail, the agent receives the diff output and can inspect the
actual screenshot at `ui/tests/test-results/` to understand what broke.

### 5.2 Updating Baselines

After intentional visual changes:

```bash
npx playwright test --update-snapshots
git add ui/tests/screenshots/
```

The updated baselines are committed alongside the code change so
reviewers can inspect the visual diff in the PR.

### 5.3 Selective Runs

Run only header-related visual tests during header work:

```bash
npx playwright test header.spec.ts
```

---

## Implementation Order

```
Phase 1 (Infrastructure)  — Playwright setup, config, gitignore
Phase 2 (Core tests)       — Header, board, settings, modals
Phase 3 (CI)               — GitHub Actions job, Makefile targets
Phase 4 (Coverage)         — Responsive viewports, theme variants
Phase 5 (Agent workflow)   — Documentation for programmatic use
```

---

## Risk Areas

1. **Flaky screenshots** — Timestamps, animations, and async rendering
   cause non-deterministic pixels. Mitigate with:
   - `maxDiffPixelRatio: 0.01` tolerance
   - `await page.waitForLoadState('networkidle')` before screenshots
   - CSS `* { animation: none !important; }` injected via
     `page.addStyleTag()` during tests
   - Mock or freeze timestamps in test seeds

2. **Server startup time** — The Go server must build and start before
   tests run. Playwright's `webServer` config handles this with a
   timeout, but slow CI machines may need a higher value.

3. **Baseline maintenance** — Screenshot baselines are committed and
   must be updated when visual changes are intentional. Forgetting to
   update causes false failures. The `test:visual:update` script
   makes this a one-liner.

4. **Cross-platform rendering** — Chromium renders slightly differently
   on macOS vs Linux. CI baselines must be generated on the same OS as
   CI (Ubuntu). Developers on macOS should run with
   `--update-snapshots` locally and let CI be the source of truth.

---

## Verification

1. `npx playwright test` passes locally with baseline screenshots
2. CI job runs green on a clean checkout
3. Intentional CSS breakage (e.g. removing flex from `.app-header`)
   causes a clear, readable test failure with diff image
4. Agent can run `npm run test:visual` and interpret the output
