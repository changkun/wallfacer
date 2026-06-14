// seed.mjs — write a deterministic demo data dir + isolated config home for
// UI screenshots, then point a booted wallfacer at them.
//
// Emits a scoped store layout matching the real on-disk schema:
//   <data>/<groupKey>/<uuid>/task.json   (+ traces/, oversight.json)
//
// The active group is a fixed demo workspace dir (default /tmp/wf-demo-ws).
// Its group key is sha256(sorted_paths joined by ":")[:8] — the same key the
// Go manager (prompts.InstructionsKey) computes. Tasks are seeded under that
// key so the board renders cards (an empty workspace set shows the workspace
// picker instead).
//
// To make a booted server activate that group, seed.mjs also writes an
// ISOLATED config home (default /tmp/wf-demo-home) containing:
//   <home>/.wallfacer/workspace-groups.json   (restores the demo group)
//   <home>/.wallfacer/.env                     (credentials placeholder)
// The config dir is $HOME/.wallfacer (not overridable by a flag), so the boot
// command must set HOME to the isolated home:
//
//   HOME=/tmp/wf-demo-home <repo>/wallfacer run \
//     -data /tmp/wf-demo-data -addr :8099 -no-browser
//
// Deterministic: fixed UUIDs and timestamps, regenerable on every run.
// Idempotent: the group dir is wiped and rewritten each run.
//
// NOTE on in_progress: startup recovery (internal/runner/recovery.go) reconciles
// any orphaned in_progress task — with no live process it moves to `waiting`
// (or `failed` if its worktree paths are missing). So a board card cannot be
// durably frozen in in_progress; the seeded in_progress task renders as waiting
// after boot. All other states (backlog/waiting/done/failed) are stable.
//
// Usage:
//   node seed.mjs [--data /tmp/wf-demo-data] [--home /tmp/wf-demo-home] [--ws /tmp/wf-demo-ws]
import { mkdirSync, writeFileSync, rmSync, cpSync, existsSync } from 'node:fs';
import { createHash } from 'node:crypto';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

function arg(name, fallback) {
  const i = process.argv.indexOf(`--${name}`);
  if (i === -1) return fallback;
  const next = process.argv[i + 1];
  if (next === undefined || next.startsWith('--')) return true;
  return next;
}

const dataDir = arg('data', '/tmp/wf-demo-data');
const homeDir = arg('home', '/tmp/wf-demo-home');
const wsDir = arg('ws', '/tmp/wf-demo-ws');

// The repo's own specs/ folder becomes the demo workspace's spec tree, so the
// Plan view renders real wallfacer specs (not a fixture). Resolved from this
// script's location: <repo>/frontend/scripts/ui-shots/seed.mjs.
const repoRoot = join(dirname(fileURLToPath(import.meta.url)), '..', '..', '..');
const specsSrc = arg('specs', join(repoRoot, 'specs'));

// Group key: sha256 of sorted, colon-joined workspace paths, first 8 bytes hex.
// Mirrors prompts.InstructionsKey in the Go server.
const groupKey = (paths) =>
  createHash('sha256').update([...paths].sort().join(':')).digest('hex').slice(0, 16);

const KEY = groupKey([wsDir]);
const groupDir = join(dataDir, KEY);

// Deterministic clock: everything is relative to a fixed "now", anchored a day
// before the project reference date so every derived time stays in the past
// against any real wall clock (otherwise the detail "elapsed" goes negative
// when the clock trails the anchor).
const NOW = new Date('2026-06-04T12:00:00Z');
const iso = (minsAgo) => new Date(NOW.getTime() - minsAgo * 60000).toISOString();

const usage = (i, o, cr, cc, cost) => ({
  input_tokens: i,
  output_tokens: o,
  cache_read_input_tokens: cr,
  cache_creation_input_tokens: cc,
  cost_usd: cost,
});

// Representative cards: one per state plus varied badges (tags, priority/impact,
// test pass/fail, dependencies). Fixed UUIDs keep dep wiring stable across runs.
const ID = {
  done1: '11111111-1111-4111-8111-111111111111',
  done2: '22222222-2222-4222-8222-222222222222',
  inprog: '33333333-3333-4333-8333-333333333333',
  waiting: '44444444-4444-4444-8444-444444444444',
  failed: '55555555-5555-4555-8555-555555555555',
  back1: '66666666-6666-4666-8666-666666666666',
  back2: '77777777-7777-4777-8777-777777777777',
};

// Rich content for the OAuth task — drives the task-detail Prompt tab and the
// Patch panel in the oversight screenshot. Kept realistic so the shot reads as
// a genuine engineering task, not a fixture.
const OAUTH_PROMPT = `Implement RFC 8628 device authorization for \`wallfacer auth login\` so
headless and remote installs can authenticate without a browser callback URL.

## Goal

Let a user run \`wallfacer auth login\` on a box with no loopback browser, read a
short code, and approve it from any other device.

## Requirements

- **Device authorization request.** POST to the authorization server's
  device endpoint; parse \`device_code\`, \`user_code\`, \`verification_uri\`,
  \`expires_in\`, and \`interval\`.
- **User prompt.** Print the \`user_code\` and \`verification_uri\` clearly, and
  open the URL when a browser is available (best-effort, never required).
- **Token polling.** Poll the token endpoint every \`interval\` seconds; honor
  \`authorization_pending\` and \`slow_down\` per spec, and stop at \`expires_in\`.
- **Persistence.** Store the resulting token via the existing credential store;
  never log the token or device code.

## Acceptance

1. \`wallfacer auth login\` completes end to end against the staging IdP.
2. \`slow_down\` backs the poll interval off by 5s and does not busy-loop.
3. Unit tests cover the pending / slow_down / expired transitions.`;

const OAUTH_RESULT = `Implemented the device-code flow end to end.

- Added \`internal/oauth/devicecode.go\` with the request + poll loop.
- Wired \`wallfacer auth login\` to the new flow with a browser-optional prompt.
- Covered pending / slow_down / expired transitions in \`devicecode_test.go\`.
- Documented the headless login path in the auth guide.

Verified against the staging IdP; \`slow_down\` correctly backs off by 5s.`;

const OAUTH_DIFF = `diff --git a/internal/oauth/devicecode.go b/internal/oauth/devicecode.go
new file mode 100644
index 0000000..1a2b3c4
--- /dev/null
+++ b/internal/oauth/devicecode.go
@@ -0,0 +1,68 @@
+package oauth
+
+import (
+\t"context"
+\t"errors"
+\t"net/url"
+\t"time"
+)
+
+// DeviceAuth holds the authorization-server response for a device-code grant
+// (RFC 8628 section 3.2).
+type DeviceAuth struct {
+\tDeviceCode      string
+\tUserCode        string
+\tVerificationURI string
+\tInterval        time.Duration
+\tExpiresAt       time.Time
+}
+
+// ErrAuthExpired is returned when the user_code lapses before approval.
+var ErrAuthExpired = errors.New("oauth: device code expired")
+
+// PollToken polls the token endpoint until the user approves the request,
+// honoring authorization_pending and slow_down per RFC 8628 section 3.5.
+func (c *Client) PollToken(ctx context.Context, da DeviceAuth) (*Token, error) {
+\tinterval := da.Interval
+\tfor {
+\t\tif time.Now().After(da.ExpiresAt) {
+\t\t\treturn nil, ErrAuthExpired
+\t\t}
+\t\ttok, err := c.requestToken(ctx, url.Values{
+\t\t\t"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
+\t\t\t"device_code": {da.DeviceCode},
+\t\t})
+\t\tswitch {
+\t\tcase err == nil:
+\t\t\treturn tok, nil
+\t\tcase errors.Is(err, errAuthorizationPending):
+\t\t\t// keep waiting at the current interval
+\t\tcase errors.Is(err, errSlowDown):
+\t\t\tinterval += 5 * time.Second
+\t\tdefault:
+\t\t\treturn nil, err
+\t\t}
+\t\tselect {
+\t\tcase <-ctx.Done():
+\t\t\treturn nil, ctx.Err()
+\t\tcase <-time.After(interval):
+\t\t}
+\t}
+}
diff --git a/internal/cli/auth.go b/internal/cli/auth.go
index 7f8e9d0..3c4b5a6 100644
--- a/internal/cli/auth.go
+++ b/internal/cli/auth.go
@@ -18,9 +18,21 @@ func runAuthLogin(ctx context.Context, c *oauth.Client) error {
-\ttok, err := c.AuthorizeCodeFlow(ctx)
-\tif err != nil {
-\t\treturn err
-\t}
+\tda, err := c.RequestDeviceAuth(ctx)
+\tif err != nil {
+\t\treturn fmt.Errorf("request device code: %w", err)
+\t}
+\tfmt.Printf("\\nTo continue, open %s\\n", da.VerificationURI)
+\tfmt.Printf("and enter code: %s\\n\\n", da.UserCode)
+\tbrowser.TryOpen(da.VerificationURI) // best-effort; never required
+\ttok, err := c.PollToken(ctx, da)
+\tif err != nil {
+\t\treturn err
+\t}
 \treturn creds.Save(tok)
 }
`;

const tasks = [
  {
    schema_version: 2,
    id: ID.done1,
    title: 'Add OAuth device-code flow to local CLI',
    prompt: OAUTH_PROMPT,
    status: 'done',
    session_id: 'sess-done-1',
    result: OAUTH_RESULT,
    stop_reason: 'end_turn',
    turns: 7,
    timeout: 900,
    usage: usage(1200, 4300, 88000, 120000, 1.84),
    usage_breakdown: {
      implementation: usage(900, 4000, 60000, 100000, 1.42),
      oversight: usage(300, 300, 28000, 20000, 0.42),
    },
    sandbox: 'claude',
    flow_id: 'implement',
    tags: ['priority:high', 'impact:5', 'backend'],
    last_test_result: 'pass',
    position: 0,
    branch_name: 'task/11111111',
    commit_message: 'internal/oauth: add device-code flow',
    // Non-git demo workspace: a stored snapshot diff drives the detail Patch
    // panel without a real worktree (see handler.TaskDiff non-git branch).
    worktree_paths: { [wsDir]: join(wsDir, '.worktrees', ID.done1) },
    snapshot_diffs: { [wsDir]: OAUTH_DIFF },
    created_at: iso(180),
    started_at: iso(178),
    updated_at: iso(120),
  },
  {
    schema_version: 2,
    id: ID.done2,
    title: 'Document the screenshot harness',
    prompt: 'Write a terse README for the ui-shots seed/snap flow.',
    status: 'done',
    session_id: 'sess-done-2',
    result: 'Added README.md with the one-command flow.',
    stop_reason: 'end_turn',
    turns: 3,
    timeout: 900,
    usage: usage(600, 1800, 30000, 40000, 0.61),
    sandbox: 'codex',
    flow_id: 'implement',
    tags: ['docs', 'impact:2'],
    last_test_result: 'fail',
    position: 1,
    branch_name: 'task/22222222',
    commit_message: 'docs: ui-shots README',
    created_at: iso(150),
    started_at: iso(149),
    updated_at: iso(90),
  },
  {
    schema_version: 2,
    id: ID.inprog,
    title: 'Refactor worktree sync to share rebase helper',
    prompt: 'Extract the rebase-onto-default logic into a shared gitutil helper.',
    // Seeded as in_progress; startup recovery reconciles it to waiting (no live
    // process). See the NOTE at the top of this file.
    status: 'in_progress',
    session_id: 'sess-inprog',
    result: null,
    stop_reason: null,
    turns: 2,
    timeout: 900,
    usage: usage(800, 1200, 40000, 60000, 0.9),
    sandbox: 'claude',
    flow_id: 'implement',
    tags: ['priority:medium', 'impact:4', 'refactor'],
    position: 0,
    branch_name: 'task/33333333',
    created_at: iso(40),
    started_at: iso(20),
    updated_at: iso(2),
  },
  {
    schema_version: 2,
    id: ID.waiting,
    title: 'Add analytics export endpoint',
    prompt: 'Expose GET /api/usage/export as CSV.',
    status: 'waiting',
    session_id: 'sess-waiting',
    result: 'I added the endpoint. Should the CSV include cache token columns?',
    stop_reason: '',
    turns: 4,
    timeout: 900,
    usage: usage(700, 2100, 35000, 50000, 0.78),
    sandbox: 'claude',
    flow_id: 'implement',
    tags: ['priority:low', 'impact:3', 'backend'],
    last_test_result: 'pass',
    position: 0,
    branch_name: 'task/44444444',
    created_at: iso(60),
    started_at: iso(55),
    updated_at: iso(10),
  },
  {
    schema_version: 2,
    id: ID.failed,
    title: 'Migrate frontend store to Pinia setup syntax',
    prompt: 'Convert option-store modules to setup stores.',
    status: 'failed',
    session_id: 'sess-failed',
    result: 'Build failed: type error in ui store.',
    stop_reason: 'end_turn',
    turns: 5,
    timeout: 900,
    usage: usage(900, 2400, 42000, 55000, 0.95),
    sandbox: 'claude',
    flow_id: 'implement',
    tags: ['priority:high', 'impact:4', 'frontend'],
    last_test_result: 'fail',
    failure_category: 'agent_error',
    position: 0,
    branch_name: 'task/55555555',
    created_at: iso(100),
    started_at: iso(98),
    updated_at: iso(30),
  },
  {
    schema_version: 2,
    id: ID.back1,
    title: 'Wire dependency badges on backlog cards',
    prompt: 'Show a dep-count badge and block promotion until deps are done.',
    status: 'backlog',
    result: null,
    stop_reason: null,
    turns: 0,
    timeout: 900,
    usage: usage(0, 0, 0, 0, 0),
    sandbox: 'claude',
    flow_id: 'implement',
    tags: ['priority:medium', 'impact:3', 'frontend'],
    // Depends on two done tasks to render a dependency badge.
    depends_on: [ID.done1, ID.done2],
    impact_score: 3,
    position: 0,
    created_at: iso(20),
    updated_at: iso(20),
  },
  {
    schema_version: 2,
    id: ID.back2,
    title: 'Add brainstorm routine for weekly cleanup',
    prompt: 'Schedule a routine that proposes tech-debt tasks every Monday.',
    status: 'backlog',
    result: null,
    stop_reason: null,
    turns: 0,
    timeout: 900,
    usage: usage(0, 0, 0, 0, 0),
    sandbox: 'codex',
    flow_id: 'brainstorm',
    tags: ['impact:2', 'chore'],
    impact_score: 2,
    position: 1,
    created_at: iso(15),
    updated_at: iso(15),
  },
];

// A minimal ready oversight summary, attached to terminal-state tasks so the
// task-detail oversight panel renders representative content.
const oversightFor = (t) => ({
  status: 'ready',
  generated_at: t.updated_at,
  phases: [
    {
      timestamp: t.started_at || t.created_at,
      title: 'Agent executed task',
      summary: `Worked on: ${t.title}.`,
      tools_used: ['Read', 'Edit', 'Bash'],
      actions: [`Touched files for: ${t.title}`],
    },
  ],
});

// A couple of trace events so the timeline/trace view is not empty.
const tracesFor = (t) => {
  const base = new Date(t.started_at || t.created_at);
  const at = (s) => new Date(base.getTime() + s * 1000).toISOString();
  return [
    {
      id: 1,
      task_id: t.id,
      event_type: 'state_change',
      data: JSON.stringify({ from: 'backlog', to: 'in_progress', trigger: 'user' }),
      created_at: at(0),
    },
    {
      id: 2,
      task_id: t.id,
      event_type: 'output',
      data: JSON.stringify({ text: `Starting work on ${t.title}` }),
      created_at: at(1),
    },
  ];
};

// Demo workspace dir (must exist so the saved group passes startup validation).
mkdirSync(wsDir, { recursive: true });

// Mirror the repo's real specs/ into the workspace so the Plan view renders the
// actual wallfacer spec tree. Wiped + recopied each run to stay in sync.
if (existsSync(specsSrc)) {
  rmSync(join(wsDir, 'specs'), { recursive: true, force: true });
  cpSync(specsSrc, join(wsDir, 'specs'), { recursive: true });
}

// Wipe and rewrite the group dir so state is idempotent/regenerable.
rmSync(groupDir, { recursive: true, force: true });
mkdirSync(groupDir, { recursive: true });

for (const t of tasks) {
  const dir = join(groupDir, t.id);
  const traces = join(dir, 'traces');
  mkdirSync(traces, { recursive: true });
  writeFileSync(join(dir, 'task.json'), JSON.stringify(t, null, 2));

  if (['done', 'failed', 'waiting'].includes(t.status)) {
    writeFileSync(join(dir, 'oversight.json'), JSON.stringify(oversightFor(t)));
    tracesFor(t).forEach((e, i) => {
      const seq = String(i + 1).padStart(4, '0');
      writeFileSync(join(traces, `${seq}.json`), JSON.stringify(e));
    });
  }
}

// Isolated config home: workspace-groups.json restores the demo group, and a
// placeholder .env keeps startup happy. HOME=<homeDir> on boot selects this.
const cfgDir = join(homeDir, '.wallfacer');
mkdirSync(cfgDir, { recursive: true });
writeFileSync(
  join(cfgDir, 'workspace-groups.json'),
  JSON.stringify([{ name: 'demo', workspaces: [wsDir] }], null, 2),
);
writeFileSync(join(cfgDir, '.env'), 'ANTHROPIC_API_KEY=sk-demo-placeholder\n');

console.log(
  JSON.stringify(
    {
      data: dataDir,
      home: homeDir,
      workspace: wsDir,
      group: KEY,
      tasks: tasks.length,
      states: [...new Set(tasks.map((t) => t.status))],
      boot: `HOME=${homeDir} wallfacer run -data ${dataDir} -addr :8099 -no-browser`,
    },
    null,
    2,
  ),
);
