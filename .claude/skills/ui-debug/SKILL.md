---
name: ui-debug
description: Debug frontend issues interactively using Playwright. Builds the server, launches it on a temporary port, writes and runs Playwright scripts to reproduce and diagnose the issue, then applies the fix.
argument-hint: "<description of the UI bug>"
user-invocable: true
---

# UI Debug

Debug a frontend issue using Playwright automation. This skill builds the
server, launches a real instance, writes Playwright scripts to reproduce
and diagnose the problem, then fixes it.

## Step 0: Parse the bug description

The user describes a UI bug in `$ARGUMENTS` or in the conversation. Extract:

- **What happens** (e.g., "typing in the second terminal session triggers
  global shortcuts instead of going to the terminal")
- **Expected behavior** (e.g., "keystrokes should go to the terminal")
- **Repro steps** if provided (e.g., "open terminal, click +, type 'e'")

## Step 1: Read the relevant code

Before writing any Playwright script, read the source files involved in
the bug. Understand the code path that should handle the user's action.
Form a hypothesis about what might be wrong.

## Step 2: Build and start the server

1. Stash unrelated dirty files if needed so the build succeeds:
   ```bash
   git stash push -m "temp: ui-debug stash" -- $(git diff --name-only | grep -v '<relevant-files>')
   ```
2. Build the server binary to a temp location:
   ```bash
   go build -o /tmp/wallfacer-debug .
   ```
3. Start the server on a temporary port (18080) with no browser:
   ```bash
   /tmp/wallfacer-debug run -addr :18080 -no-browser &
   ```
4. Wait for it to be healthy:
   ```bash
   sleep 2 && curl -s http://localhost:18080/api/debug/health
   ```

## Step 3: Write a Playwright repro script

Write a script at `/tmp/ui-debug-repro.mjs` that:

- Uses `chromium` from `playwright` (already installed globally)
- Navigates to `http://localhost:18080`
- Uses `waitUntil: 'domcontentloaded'` (NOT `networkidle` — SSE streams
  keep the connection open and would timeout)
- Uses `waitForTimeout` for timing (not `waitForLoadState`)
- Reproduces the bug step by step
- Logs diagnostic information: `document.activeElement`, DOM state,
  event traces, CSS visibility, etc.

### Playwright tips for this project

- **Page load**: `await page.goto(url, { waitUntil: 'domcontentloaded' })`
  then `await page.waitForTimeout(3000)` for JS init and SSE setup.
- **Terminal panel**: Open via `await page.click('#status-bar-terminal-btn')`,
  then wait ~2s for WebSocket connect and shell prompt.
- **xterm.js focus**: The terminal uses a hidden `<textarea>` with class
  `xterm-helper-textarea`. Check focus with:
  ```js
  await page.evaluate(() => ({
    tag: document.activeElement?.tagName,
    class: document.activeElement?.className,
  }))
  ```
- **Terminal text**: Read via `.xterm-screen` textContent:
  ```js
  await page.evaluate(() =>
    document.querySelector('.xterm-screen')?.textContent || ''
  )
  ```
- **Type into terminal**: `await page.keyboard.type('echo hello\n')`
- **Tracing events**: Patch globals via `page.evaluate` to add logging:
  ```js
  await page.evaluate(() => {
    const orig = window.someFunction;
    window.someFunction = function() {
      console.log('called!', document.activeElement?.tagName);
      return orig.apply(this, arguments);
    };
  });
  ```
- **Real mouse clicks**: For focus-related bugs, use coordinate-based clicks
  to match real user behavior:
  ```js
  const box = await page.$eval('#btn', el => {
    const r = el.getBoundingClientRect();
    return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
  });
  await page.mouse.click(box.x, box.y);
  ```
- **Modals/panels**: Check visibility with
  `!el.classList.contains('hidden')`.
- **WebSocket messages**: Intercept via:
  ```js
  await page.evaluate(() => {
    const origSend = WebSocket.prototype.send;
    window._wsMsgs = [];
    WebSocket.prototype.send = function(data) {
      window._wsMsgs.push(data);
      return origSend.call(this, data);
    };
  });
  ```

## Step 4: Run the script and analyze

```bash
node /tmp/ui-debug-repro.mjs
```

Analyze the output. If the bug doesn't reproduce in headless Chromium,
try `headless: false` or investigate browser-specific differences
(the project's users primarily use Chrome/Safari on macOS).

If the first script doesn't pinpoint the issue, write follow-up scripts
that narrow down the cause:

- Add event listener traces to identify which handler fires
- Log `document.activeElement` at each step
- Patch functions to trace call stacks
- Check timing issues with multiple `waitForTimeout` checkpoints

## Step 5: Identify root cause

Based on the Playwright output, identify the root cause. Common
categories:

- **Focus theft**: clicking a UI element moves focus away from an input.
  Fix: `mousedown preventDefault`, `tabindex="-1"`, deferred focus.
- **Event ordering**: a global listener catches events before a local one.
  Fix: guard the global listener, use capture phase, or `stopPropagation`.
- **Blocking I/O**: a goroutine blocks on a read and can't respond to
  signals. Fix: use channels/select instead of blocking calls.
- **Timing**: async operations complete in unexpected order. Fix: use
  proper sequencing, callbacks, or state machines.
- **CSS/layout**: elements are hidden, overlapping, or zero-sized.
  Fix: inspect computed styles and box model.

## Step 6: Write a Playwright regression test

Before fixing, write a Playwright script at `/tmp/ui-debug-verify.mjs`
that:

1. Reproduces the bug
2. Asserts the expected (correct) behavior
3. Currently fails (confirms the bug)

This becomes the verification script for the fix.

## Step 7: Apply the fix

Fix the code. Keep changes minimal and focused.

## Step 8: Verify

1. Rebuild the server: `go build -o /tmp/wallfacer-debug .`
2. Restart: kill old server, start new one
3. Run the verification script: `node /tmp/ui-debug-verify.mjs`
4. Confirm it passes
5. Run existing tests:
   - `go test ./internal/handler/ -run TestTerminalWS -v` (if backend changed)
   - `cd ui && npx --yes vitest@2 run` (frontend tests)

## Step 9: Clean up

1. Kill the debug server: `kill $(lsof -ti:18080) 2>/dev/null`
2. Remove temp scripts: `rm -f /tmp/ui-debug-*.mjs /tmp/wallfacer-debug`
3. Pop the stash if one was created: `git stash pop`
4. Commit the fix with a descriptive message explaining the root cause

## Guidelines

- **Iterate quickly**: write small, focused Playwright scripts. Don't try
  to test everything in one script.
- **Log liberally**: console.log DOM state, focus, event traces at each
  step. More data is better than guessing.
- **Check assumptions**: if you think xterm has focus, verify it. If you
  think a handler runs, trace it.
- **Headless first**: headless is faster. Only use `headless: false` if
  you need to visually inspect or if headless doesn't reproduce.
- **Don't guess**: if the first hypothesis is wrong, write another script
  to test the next one. The Playwright round-trip is fast.
