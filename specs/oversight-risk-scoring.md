# Oversight Risk Scoring — Real-Time Agent Action Risk Assessment

**Status:** Draft
**Date:** 2026-03-21

---

## Problem

The current oversight system generates post-hoc summaries of agent activity only after task completion (or periodically via `WALLFACER_OVERSIGHT_INTERVAL`). There is no real-time visibility into the riskiness of agent actions during execution. Users cannot tell at a glance whether an in-progress task is performing safe read/edit operations or executing destructive commands like `rm -rf` or `git push --force`.

---

## Goal

1. Collect the full sequence of agent tool calls during execution.
2. Score each action against a pre-defined risk policy.
3. Aggregate into a task-level risk score updated in real-time.
4. Display the risk score on task cards via the existing SSE delta system.
5. Provide a detailed per-action breakdown in the task detail modal.
6. Full support for Claude sandbox (via hooks for intra-turn scoring). Codex sandbox gets between-turn scoring only, with TODO placeholders for future hook integration.

---

## Design

### Data Model

Add two lightweight fields to `Task` in `internal/store/models.go` (these flow through SSE deltas):

```go
// RiskScore is the maximum risk score across all observed agent actions.
// Range [0.0, 1.0]. Updated in real-time during execution.
RiskScore float64 `json:"risk_score,omitempty"`

// RiskLevel is a human-readable label derived from RiskScore thresholds.
// Values: "low", "medium", "elevated", "high". Empty when RiskScore is 0.
RiskLevel string  `json:"risk_level,omitempty"`
```

Add per-task risk policy override field (follows `CustomPassPatterns`/`CustomFailPatterns` pattern):

```go
// RiskPolicyRules are per-task risk rule overrides. Each string is a
// JSON-encoded rule: {"id":"x","tool":"Bash","input_pattern":"rm -rf","risk":0.9,"label":"..."}
// When non-nil, these rules are prepended to (and can override) the global policy.
// nil means use the global policy as-is.
RiskPolicyRules []string `json:"risk_policy_rules,omitempty"`
```

Define `RiskAction` for the per-action log (stored separately in `risk.json`, not in `task.json`):

```go
type RiskAction struct {
    Timestamp time.Time `json:"timestamp"`
    Turn      int       `json:"turn"`
    ToolName  string    `json:"tool_name"`
    Input     string    `json:"input"`         // truncated to 200 chars
    Output    string    `json:"output,omitempty"` // truncated to 500 chars; only stored when an output rule matched
    Score     float64   `json:"score"`         // [0.0, 1.0]
    RuleID    string    `json:"rule_id"`       // which policy rule matched
    Label     string    `json:"label"`         // human-readable rule description
}
```

Full risk data structure (persisted as `<taskID>/risk.json`):

```go
type TaskRisk struct {
    Score     float64      `json:"score"`
    Level     string       `json:"level"`
    Actions   []RiskAction `json:"actions"`   // capped at 200 entries
    UpdatedAt time.Time    `json:"updated_at"`
}
```

**Why separate `risk.json`?** The per-action log can grow large (200 entries x ~200 bytes = ~40KB). Keeping it out of `task.json` avoids bloating every SSE delta payload. The card only needs `RiskScore` and `RiskLevel` (two fields on Task). The modal fetches the full breakdown on demand.

### Risk Policy Engine

New package: `internal/risk/`

**Policy format** (`internal/risk/default_policy.yaml`, embedded via `//go:embed`):

```yaml
version: 1
default_risk: 0.1
thresholds:
  low: 0.25
  medium: 0.5
  elevated: 0.75
rules:
  - id: bash-rm-rf
    tool: Bash
    input_pattern: 'rm\s+-[a-zA-Z]*r[a-zA-Z]*f|rm\s+-[a-zA-Z]*f[a-zA-Z]*r'
    risk: 0.9
    label: Recursive force delete

  - id: bash-curl-pipe-sh
    tool: Bash
    input_pattern: 'curl.*\|\s*(ba)?sh'
    risk: 0.95
    label: Remote script execution

  - id: write-credentials
    tool: Write
    input_pattern: '\.(env|key|pem|cert|secret)$|credentials'
    risk: 0.85
    label: Write to sensitive file

  - id: bash-force-push
    tool: Bash
    input_pattern: 'git\s+push\s+.*--force'
    risk: 0.85
    label: Git force push

  - id: bash-reset-hard
    tool: Bash
    input_pattern: 'git\s+reset\s+--hard'
    risk: 0.7
    label: Git hard reset

  - id: bash-sudo
    tool: Bash
    input_pattern: '\bsudo\b'
    risk: 0.6
    label: Sudo command

  - id: write-dockerfile
    tool: Write
    input_pattern: 'Dockerfile|docker-compose'
    risk: 0.5
    label: Docker config modification

  - id: bash-npm-install
    tool: Bash
    input_pattern: 'npm\s+install\s+'
    risk: 0.3
    label: Package installation

  - id: bash-default
    tool: Bash
    input_pattern: '.*'
    risk: 0.2
    label: Shell command

  - id: edit-any
    tool: Edit
    input_pattern: '.*'
    risk: 0.1
    label: File edit

  - id: read-any
    tool: Read
    input_pattern: '.*'
    risk: 0.05
    label: File read

  # Output-based rules (second line of defense for indirect execution).
  # These catch destructive operations inside scripts, Makefiles, etc.
  # by scanning tool_output from PostToolUse.
  - id: output-rm-detected
    tool: Bash
    output_pattern: 'rm\s+-[a-zA-Z]*r[a-zA-Z]*f|removed\s+.*directory'
    risk: 0.7
    label: Destructive delete detected in output

  - id: output-force-push-detected
    tool: Bash
    output_pattern: 'forced\s+update|force-pushed'
    risk: 0.7
    label: Force push detected in output

  - id: output-permission-denied
    tool: Bash
    output_pattern: 'permission\s+denied|access\s+denied|unauthorized'
    risk: 0.5
    label: Permission error in output

  - id: output-secret-leak
    tool: Bash
    output_pattern: 'BEGIN\s+(RSA|EC|OPENSSH)\s+PRIVATE\s+KEY|AKIA[0-9A-Z]{16}'
    risk: 0.9
    label: Secret/credential detected in output
```

**Go types** (`internal/risk/policy.go`):

```go
type Rule struct {
    ID            string         `yaml:"id"`
    Tool          string         `yaml:"tool"`
    InputPattern  *regexp.Regexp // compiled from YAML "input_pattern" field
    OutputPattern *regexp.Regexp // compiled from YAML "output_pattern" field; nil = don't check output
    Risk          float64        `yaml:"risk"`
    Label         string         `yaml:"label"`
}

type Thresholds struct {
    Low      float64 `yaml:"low"`
    Medium   float64 `yaml:"medium"`
    Elevated float64 `yaml:"elevated"`
}

type Policy struct {
    DefaultRisk float64    `yaml:"default_risk"`
    Thresholds  Thresholds `yaml:"thresholds"`
    Rules       []Rule     `yaml:"rules"`
}
```

**Scoring functions** (`internal/risk/score.go`):

- `ScoreAction(policy *Policy, toolName, toolInput, toolOutput string) (score float64, ruleID, label string)` — iterates rules in order, returns first match. A rule matches when: tool name matches AND `InputPattern` matches `toolInput` (if set) AND `OutputPattern` matches `toolOutput` (if set). Rules with only `OutputPattern` (no `InputPattern`) act as output-only scanners. Unmatched → `default_risk`.
- `LevelFromScore(thresholds Thresholds, score float64) string` — returns "low"/"medium"/"elevated"/"high".
- `AggregateScore(actions []RiskAction) float64` — returns `max(all action.Score)`.

**Policy loading:** `LoadPolicy(path string) (*Policy, error)` loads from YAML. `DefaultPolicy() *Policy` returns embedded default. The runner loads the policy at startup and re-reads on file change (stat mtime check, same pattern as `oversightIntervalFromEnv()`).

Optional global user override: `~/.wallfacer/risk-policy.yaml` takes precedence over embedded default when present.

### Per-Task Policy Customization

Risk policy rules can be overridden on a per-task basis. This follows the same `CustomPassPatterns`/`CustomFailPatterns` pattern already used for test result matching.

**Resolution order** (first-match wins within the merged rule list):

1. **Per-task rules** (`task.RiskPolicyRules`) — prepended first, highest priority
2. **Global user override** (`~/.wallfacer/risk-policy.yaml`) — if present
3. **Embedded default** (`internal/risk/default_policy.yaml`)

Per-task rules are **prepended** to the global rules. Since `ScoreAction` returns the first matching rule, per-task rules naturally override globals for the same tool/pattern. A per-task rule with `risk: 0` effectively disables a global rule for that task.

**Rule format** (each element of `RiskPolicyRules` is a JSON string):

```json
{"id": "custom-allow-rm", "tool": "Bash", "input_pattern": "rm -rf /tmp/cache", "risk": 0.0, "label": "Allow cache cleanup"}
{"id": "custom-output-scan", "tool": "Bash", "output_pattern": "DROP TABLE", "risk": 0.9, "label": "Database table drop detected"}
```

Rules can specify `input_pattern`, `output_pattern`, or both. When both are set, both must match.

**Merging function** (`internal/risk/policy.go`):

```go
// MergeTaskRules prepends per-task rule overrides to a copy of the global policy.
// Each taskRule string is parsed as JSON. Invalid rules are silently skipped
// (they were validated at API ingest time).
func MergeTaskRules(global *Policy, taskRules []string) *Policy
```

Returns a new `*Policy` with the merged rule list. The global policy is not mutated.

**Validation** (`internal/handler/tasks.go`):

```go
// validateRiskPolicyRules checks each JSON-encoded rule string:
// - Valid JSON with "tool" (non-empty string) and "risk" (float64 in [0.0, 1.0])
// - "input_pattern" (if set) compiles as a valid regex
// - "output_pattern" (if set) compiles as a valid regex
// - At least one of "input_pattern" or "output_pattern" must be set
// Returns the first validation error encountered, or nil.
func validateRiskPolicyRules(rules []string) error
```

Called in both `CreateTask` and `UpdateTask` handlers, same position as `validateCustomPatterns`.

**API surface:**

- `POST /api/tasks` — accepts `risk_policy_rules` in request body.
- `PATCH /api/tasks/{id}` — accepts `risk_policy_rules` for backlog tasks.
- `TaskCreateOptions` gains `RiskPolicyRules []string`.
- `UpdateTaskBacklog` gains `riskPolicyRules *[]string` parameter.

**Runner integration:**

In `scoreTurnActions`, the effective policy is built once at the start of `Run()`:

```go
globalPolicy := r.riskPolicy // loaded at startup
effectivePolicy := globalPolicy
if len(task.RiskPolicyRules) > 0 {
    effectivePolicy = risk.MergeTaskRules(globalPolicy, task.RiskPolicyRules)
}
```

The merged policy is cached for the duration of the task execution (no re-merge per turn).

**UI:**

In the task creation/edit modal, add a collapsible "Risk Policy Rules" section:
- List of current custom rules, each showing tool, pattern, score, label.
- "Add rule" button → inline form with: tool (dropdown: Bash/Write/Edit/Read/Glob), input pattern (text), risk score (number input 0-1), label (text).
- "Remove" button per rule.
- Rules serialized as JSON strings in the PATCH request.

### Action Collection

#### A. Between-Turn Scoring (Both Sandboxes)

After `SaveTurnOutput` at `internal/runner/execute.go:400`, add:

```go
// Risk scoring: parse this turn's tool calls and update risk score.
r.scoreTurnActions(bgCtx, taskID, turns, rawStdout)
```

`scoreTurnActions` reuses the existing `parseTurnActivity()` from `internal/runner/oversight.go:330` which already handles both Claude NDJSON (`type: "assistant"` with `tool_use` content blocks) and Codex NDJSON (`item.started`/`item.completed` with `command_execution`). It extracts `ToolCalls` in `"ToolName(input)"` format via `canonicalizeToolName()` at `oversight.go:458`.

Flow:
1. Call `parseTurnActivity(rawStdout, turnNum)` to get `turnActivity.ToolCalls`.
2. For each tool call, parse `"ToolName(input)"` format. For output-based rules, extract tool output from the NDJSON `tool_result` / `aggregated_output` blocks that follow each `tool_use` block (already present in the turn NDJSON stream — see `ndjsonLine.AggregatedOutput` at `oversight.go:326`).
3. Call `risk.ScoreAction(policy, toolName, input, output)` — matches against both input and output patterns.
4. Append new `RiskAction` entries to the task's risk log.
5. Compute new aggregate `max(scores)`, derive level.
6. Call `store.UpdateTaskRisk()` → updates Task fields → `notify()` → SSE delta.
7. Call `store.SaveRiskScore()` → writes `risk.json`.

**Codex support:** Works out of the box because `parseTurnActivity` already handles Codex's `command_execution` items (lines 344-374 of oversight.go). Between-turn granularity only.

#### B. Intra-Turn Scoring via Hooks (Claude Sandbox Only)

For sub-turn granularity during long implementation turns, use a Claude Code `PostToolUse` hook.

**Hook setup** (`internal/runner/risk_hook.go`):

Before the turn loop in `Run()`, for `sandbox.Claude`:

1. Create a per-task temp directory: `/tmp/wallfacer-risk-<taskID[:8]>/`
2. Write `settings.json` with a PostToolUse hook:

```json
{
  "hooks": {
    "PostToolUse": [{
      "matcher": {},
      "hooks": [{
        "type": "command",
        "command": "cat /dev/stdin | jq -c '{ts: (now | todate), tool: .tool_name, input: (.tool_input | tostring | .[0:500]), output: (.tool_output | tostring | .[0:1000])}' >> /workspace/.wallfacer/actions.ndjson"
      }]
    }]
  }
}
```

3. Create `actions/` subdirectory for the NDJSON output.
4. Add bind mounts to container spec:
   - `<hookDir>/settings.json` → `/home/claude/.claude/settings.json` (ro, overlays the named volume)
   - `<hookDir>/actions/` → `/workspace/.wallfacer/` (rw)

**Action tailer goroutine:**

Start `tailActionLog(ctx, actionsPath, scoreCh)` before the turn loop. This goroutine:
1. Opens `actions.ndjson` with `os.Open` + seeks to end.
2. Polls for new lines every 500ms (or uses `fsnotify` if available).
3. Parses each line as `{ts, tool, input, output}`.
4. Calls `risk.ScoreAction(policy, tool, input, output)` and sends result to `scoreCh`.

**Risk update worker:**

A separate goroutine reads from `scoreCh`, batches actions over a 500ms window, then calls `store.UpdateTaskRisk()` + `store.SaveRiskScore()` once per batch. This prevents excessive SSE deltas during rapid tool calls.

**Cleanup:** On `Run()` return (deferred), remove the temp hook directory.

**Codex placeholder:**

```go
// TODO(codex): Codex does not support PostToolUse hooks. When Codex gains
// hook support, add intra-turn risk scoring here. Between-turn scoring via
// parseTurnActivity already covers Codex (command_execution items).
```

### Store Integration

New file: `internal/store/risk.go`

```go
// SaveRiskScore writes the full risk breakdown to <taskID>/risk.json.
func (s *Store) SaveRiskScore(taskID uuid.UUID, risk TaskRisk) error

// GetRiskScore reads risk.json. Returns zero-value TaskRisk if not found.
func (s *Store) GetRiskScore(taskID uuid.UUID) (*TaskRisk, error)

// UpdateTaskRisk updates the in-memory Task's RiskScore and RiskLevel fields,
// persists task.json, and calls notify() to push an SSE delta.
func (s *Store) UpdateTaskRisk(ctx context.Context, id uuid.UUID, score float64, level string) error
```

`UpdateTaskRisk` follows the same pattern as `UpdateTaskResult` in `internal/store/tasks_update.go`: acquires `s.mu` lock, calls `mutateTask`, which handles `saveTask` + `notify`.

### API Endpoint

New route in `internal/apicontract/routes.go`:

```go
{Method: http.MethodGet, Pattern: "/api/tasks/{id}/risk", Name: "GetRiskScore",
 Description: "Risk score with per-action breakdown for a task."}
```

Handler in `internal/handler/risk.go` (follows pattern of `GetOversight` in `internal/handler/oversight.go`):

```go
func (h *Handler) GetRiskScore(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
    if _, err := h.store.GetTask(r.Context(), id); err != nil {
        http.Error(w, "task not found", http.StatusNotFound)
        return
    }
    risk, err := h.store.GetRiskScore(id)
    if err != nil {
        http.Error(w, "risk score not available", http.StatusInternalServerError)
        return
    }
    writeJSON(w, http.StatusOK, risk)
}
```

Wire in `server.go` `buildMux`.

### UI — Card Risk Badge

In `ui/js/render.js`:

Update `_cardFingerprint()` to include `t.risk_score || 0` and `t.risk_level || ''`.

Add risk badge in `updateCard()`, in the badge row alongside status and failure category:

```javascript
const riskBadge = (t.risk_score > 0)
  ? `<span class="badge badge-risk-${riskLevelClass(t.risk_level)}"
         title="Risk: ${(t.risk_score * 100).toFixed(0)}%">
      ${riskIcon(t.risk_level)} ${(t.risk_score * 100).toFixed(0)}%
    </span>`
  : '';

function riskLevelClass(level) {
  return ({'low':'low','medium':'medium','elevated':'elevated','high':'high'})[level] || 'low';
}
function riskIcon(level) {
  return ({'low':'\u2713','medium':'\u26A0','elevated':'\u26A0','high':'\u2622'})[level] || '';
}
```

CSS in `ui/css/styles.css`:

```css
.badge-risk-low      { background: var(--green-bg, #d0ebdc); color: var(--green-fg, #1a6030); }
.badge-risk-medium   { background: var(--yellow-bg, #fef3c7); color: var(--yellow-fg, #92400e); }
.badge-risk-elevated { background: var(--orange-bg, #fed7aa); color: var(--orange-fg, #9a3412); }
.badge-risk-high     { background: var(--red-bg, #f5d5d5); color: var(--red-fg, #6b1111); }
```

Dark theme variants follow existing `[data-theme="dark"]` pattern.

### UI — Modal Risk Tab

Add a fourth tab "risk" to the logs tabs in `ui/js/modal.js` (alongside "oversight", "pretty", "raw").

When selected, fetch `GET /api/tasks/{id}/risk` and render:
1. **Aggregate score header** — horizontal meter bar with color fill + percentage label.
2. **Action timeline** — scrollable list of `RiskAction` entries, each showing: timestamp, tool name (badge), score (colored), rule label, truncated input.

Poll every 3 seconds during `in_progress` status (same pattern as oversight "generating" state).

---

## Implementation Phases

### Phase 1 — Risk policy engine (standalone, no dependencies)

| File | Change |
|------|--------|
| `internal/risk/policy.go` (new) | `Rule`, `Policy`, `Thresholds` types; `LoadPolicy`, `DefaultPolicy`, `MergeTaskRules` |
| `internal/risk/score.go` (new) | `ScoreAction`, `LevelFromScore`, `AggregateScore` |
| `internal/risk/default_policy.yaml` (new) | Embedded default risk rules |
| `internal/risk/policy_test.go` (new) | Unit tests for each default rule, edge cases |
| `internal/risk/score_test.go` (new) | Unit tests for scoring and aggregation |

**Effort:** Low. Pure Go, no external dependencies.

### Phase 2 — Data model + store

| File | Change |
|------|--------|
| `internal/store/models.go` | Add `RiskScore float64`, `RiskLevel string`, `RiskPolicyRules []string` to `Task`; define `RiskAction`, `TaskRisk` |
| `internal/store/risk.go` (new) | `SaveRiskScore`, `GetRiskScore`, `UpdateTaskRisk` |
| `internal/store/tasks_create_delete.go` | Accept `RiskPolicyRules` in `TaskCreateOptions` |
| `internal/store/tasks_update.go` | Handle `RiskPolicyRules` in `UpdateTaskBacklog` |
| `internal/handler/tasks.go` | Accept + validate `risk_policy_rules` in `CreateTask`/`UpdateTask` |
| `internal/store/risk_test.go` (new) | Round-trip tests, capping, notify verification |
| `cmd/gen-clone/` | Regenerate `tasks_clone_gen.go` (`RiskPolicyRules` slice needs cloning) |

**Effort:** Low. Additive fields, `omitempty` for backward compatibility.

### Phase 3 — Runner integration (between-turn scoring)

| File | Change |
|------|--------|
| `internal/runner/risk.go` (new) | `scoreTurnActions` method on Runner; reuses `parseTurnActivity`; merges per-task policy via `risk.MergeTaskRules` |
| `internal/runner/execute.go` | Call `scoreTurnActions` after `SaveTurnOutput` (~line 402); add Codex TODO |
| `internal/runner/runner.go` | Load risk policy in constructor; store as `riskPolicy *risk.Policy` field |

**Effort:** Low-Medium. Parsing already exists; wiring is straightforward.

### Phase 4 — Hook infrastructure (Claude intra-turn scoring)

| File | Change |
|------|--------|
| `internal/runner/risk_hook.go` (new) | `writeHookSettings`, `cleanupHookDir`, `tailActionLog`, risk update worker |
| `internal/runner/container.go` | Add hook dir bind mounts for `sandbox.Claude` in `buildContainerArgsForSandbox` |
| `internal/runner/execute.go` | Before turn loop: setup hook dir + start tailer (Claude only); defer cleanup |

**Effort:** Medium. New hook plumbing, file tailing, debounced updates.

### Phase 5 — API endpoint

| File | Change |
|------|--------|
| `internal/handler/risk.go` (new) | `GetRiskScore` handler |
| `internal/apicontract/routes.go` | Register `GET /api/tasks/{id}/risk` |
| `server.go` | Wire in `buildMux` |

**Effort:** Low. Follows `GetOversight` pattern exactly.

### Phase 6 — UI

| File | Change |
|------|--------|
| `ui/js/render.js` | Risk badge in `updateCard`, fingerprint update |
| `ui/css/styles.css` | `.badge-risk-*` classes, dark theme |
| `ui/js/modal.js` | "risk" tab, `renderRiskInLogs()`, action timeline |

**Effort:** Low-Medium.

### Phase 7 — Tests, docs, contract regeneration

| File | Change |
|------|--------|
| `internal/runner/risk_test.go` (new) | Integration test for `scoreTurnActions` |
| `internal/runner/risk_hook_test.go` (new) | Test `writeHookSettings` produces valid JSON |
| `internal/apicontract/` | Regenerate contract artifacts (`make api-contract`) |

**Effort:** Low.

---

## Key Patterns Reused

| Pattern | Source | Reused For |
|---------|--------|------------|
| `parseTurnActivity` | `internal/runner/oversight.go:330` | Extracting tool calls from turn NDJSON |
| `canonicalizeToolName` | `internal/runner/oversight.go:458` | Normalizing tool names across sandbox formats |
| `UpdateTaskResult` pattern | `internal/store/tasks_update.go` | `UpdateTaskRisk` — mutateTask + notify |
| `SaveOversight` / `GetOversight` | `internal/store/oversight.go` | `SaveRiskScore` / `GetRiskScore` — same file I/O pattern |
| `GetOversight` handler | `internal/handler/oversight.go` | `GetRiskScore` handler |
| `buildBaseContainerSpec` | `internal/runner/container.go:267` | Adding hook volume mounts |
| `backgroundWg` goroutine tracking | `internal/runner/runner.go:516` | Risk update worker lifecycle |
| `oversightIntervalFromEnv` re-read | `internal/runner/oversight.go` | Policy file hot-reload via mtime check |
| `CustomPassPatterns`/`CustomFailPatterns` | `internal/store/models.go:325-329` | Per-task `RiskPolicyRules` — same `[]string` pattern on Task struct |
| `validateCustomPatterns` | `internal/handler/tasks.go` | `validateRiskPolicyRules` — same validation-at-ingest pattern |

---

## Codex Placeholders

All Claude-specific code paths include a TODO for future Codex hook support:

1. **Hook setup** (`risk_hook.go`): `// TODO(codex): Add hook config when Codex supports PostToolUse hooks.`
2. **Tailer startup** (`execute.go`): `// TODO(codex): Start action tailer when Codex supports intra-turn hooks.`
3. **Container mounts** (`container.go`): `// TODO(codex): Mount hook settings for Codex when supported.`

Between-turn scoring via `parseTurnActivity` works for Codex today — it already parses `item.started`/`command_execution` NDJSON items (oversight.go:344-374).

---

## Potential Challenges

1. **Named volume overlay**: The `claude-config` volume is mounted at `/home/claude/.claude`. A file-level bind mount for `settings.json` overlays it. Both Docker and Podman support this — the bind mount takes precedence. Verified by the existing `appendCodexAuthMount` pattern which does the same for `/home/codex/.codex`.

2. **Hook output atomicity**: Concurrent tool calls within a turn could append to `actions.ndjson` simultaneously. Using `jq -c` (compact, single-line) + `>>` (append) is safe on Linux/macOS for lines under PIPE_BUF (4096 bytes). The input (500 chars) + output (1000 chars) truncation keeps each line under ~2KB, well within the limit.

3. **SSE delta frequency**: Each `UpdateTaskRisk` triggers `notify()`. The risk update worker debounces with a 500ms window, so at most 2 deltas/second from risk scoring. This is comparable to existing turn-change deltas.

4. **`risk.json` growth**: 200 entries (cap) x ~200 bytes = ~40KB max. Comparable to existing oversight.json files.

5. **Policy hot-reload**: `os.Stat` mtime check on each scoring pass. Negligible cost for a small file. Only re-parse YAML when changed.

---

## Migration & Backward Compatibility

- `RiskScore` and `RiskLevel` are `omitempty` — old task JSON files decode cleanly.
- `risk.json` is a new optional file — missing file returns zero-value `TaskRisk`.
- No schema version bump needed (additive fields).
- Card rendering falls back gracefully: no badge shown when `risk_score == 0`.
- API endpoint returns empty risk data for tasks without scoring.

---

## Open Questions

1. **Auto-pause on high risk?** The `RiskAssessment` could include a `ShouldPause bool` for future automatic task pausing on extreme risk scores (e.g., >0.9). Deferred to v2 — display-only in v1.
2. **Risk score persistence across retries?** When a task is retried, should the risk score reset to 0 or carry forward? Recommend reset (fresh execution = fresh risk assessment).

---

## What This Does NOT Require

- No changes to the oversight agent or its prompt template — risk scoring is separate from phase summarization.
- No changes to the refinement, commit, or test-verification pipelines.
- No new sandbox activity type — risk scoring piggybacks on the implementation/testing turn output.
- No changes to the webhook notifier — risk score is part of the Task struct and flows through existing webhook payloads automatically.
