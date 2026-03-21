# Refinement and Ideation

Wallfacer includes two AI-powered features that help you work with tasks before they execute: **Prompt Refinement** sharpens a task prompt into a detailed implementation spec by analyzing your codebase, and **Ideation** proposes entirely new task ideas by exploring your workspace for improvement opportunities. Both run inside read-only sandbox containers and never modify your code.

---

## 📋 Essentials

### ✨ Prompt Refinement

Prompt refinement launches a sandbox agent that reads your codebase, understands the existing patterns and constraints, and rewrites your task prompt into a detailed implementation specification. The agent mounts your workspaces read-only and produces a structured spec with an objective, background context, implementation plan, files to change, and edge cases.

Refinement is only available for tasks in the **Backlog** column. Once a task moves to In Progress, it can no longer be refined.

#### ▶️ Starting a Refinement

1. Click a Backlog task card to open its detail panel.
2. Switch to the **Refine** tab.
3. Optionally enter **user instructions** in the text area to focus the agent (for example, "keep backward compatibility" or "prioritize performance over readability").
4. Click **Start Refinement**.

The agent begins running in a sandbox container. You will see live log output streamed in real time.

#### Live Log Streaming

While the refinement agent runs, its output streams into the Refine tab. Two display modes are available:

- **Pretty** -- formatted, syntax-highlighted output (default)
- **Raw** -- plain text with ANSI escape codes stripped

Switch between them using the tabs above the log area. The view auto-scrolls to follow new output.

#### 🔬 Reviewing the Result

When the agent finishes, the result appears in two editable fields:

- **Goal summary** -- a 1-3 sentence human-readable summary of what the task achieves. This is shown on the task card for quick scanning.
- **Spec** -- the full implementation specification, which typically includes:
  - **Backlog Outcome** -- whether the task should be kept, rewritten, or closed
  - **Objective** -- what the task should achieve and why
  - **Background** -- relevant codebase context discovered by the agent
  - **Implementation Plan** -- numbered steps
  - **Files to Change** -- specific files and required modifications
  - **Edge Cases and Considerations** -- things to watch for

You can freely edit both the goal and spec before deciding what to do with them.

#### ✅ Applying or Dismissing

After reviewing (and optionally editing) the refined prompt, choose one of two actions:

| Action | Effect |
|---|---|
| **Apply as Prompt** | Replaces the task's prompt with the refined spec and updates the goal with the refined summary (unless you have manually edited the goal). The task title is regenerated to match. The original prompt is saved to the refinement history. |
| **Dismiss** | Discards the refinement result. The task prompt and goal remain unchanged and you can start a new refinement or run the task as-is. |

#### ❌ Canceling a Running Refinement

Click the **Cancel** button that appears while the agent is running. The sandbox container is killed immediately and the refinement job is marked as failed (cancelled). You can start a new refinement at any time.

### 💡 Ideation / Brainstorm Agent

The ideation agent analyzes your workspace -- reading source files, project manifests, recent git history, churn hotspots, TODO/FIXME comments, and failed task signals -- and proposes up to three high-impact improvement ideas as new backlog cards.

#### Enabling Ideation

Ideation is **disabled by default**. To enable it:

1. Click the **Automation** menu (lightning bolt icon) in the header bar.
2. Toggle the **Brainstorm** checkbox on.

Once enabled, you can trigger runs manually or configure an automatic interval.

#### Triggering a Brainstorm

Click the **Ideate** button in the header toolbar. This immediately creates an idea-agent task card and starts the brainstorm container. The card appears in the In Progress column with a title like "Brainstorm Mar 21, 2026 14:30".

You can also trigger ideation via the API:

```
POST /api/ideate
```

#### What Ideation Produces

Each brainstorm run creates up to three backlog task cards. The agent uses a generate-then-rank pipeline:

1. **Generate** 6 candidate improvements across diverse areas (features, performance, security, code quality, architecture, and more).
2. **Self-critique** each candidate against concrete impact criteria.
3. **Output** the top 3 ranked ideas.

Each idea card includes:

- A short **display title** (the card's `Prompt` field, shown on the board)
- A detailed **execution prompt** (`ExecutionPrompt`, passed to the container when the task runs)
- **Tags**: `idea-agent` plus the idea's category, priority level, and impact score

Ideas that score below the minimum impact threshold are filtered out. Previously rejected ideas are remembered and excluded from future runs.

#### Reviewing Brainstorm Results

When Auto-Submit is **disabled** (the default), the brainstorm task card moves to Waiting after proposing ideas, allowing you to review the suggestions before they become backlog tasks. Open the card to see the summary, then:

- **Mark as Done** to approve the ideas and create backlog cards from them
- **Cancel** to discard the ideas

When Auto-Submit is **enabled**, backlog cards are created immediately without waiting for approval.

#### Canceling a Running Ideation

Cancel a brainstorm in one of two ways:

- Click the **Ideate** button again while a brainstorm is running
- Use the API: `DELETE /api/ideate`

This kills the container and marks the idea-agent task as cancelled.

---

## 🧠 Advanced Topics

### Refinement History

Each time you apply a refinement, the session is recorded. The Refine tab shows a collapsible history section listing all past refinement sessions with:

- The starting prompt before refinement
- The sandbox-generated spec (before any manual edits)
- The final prompt that was applied
- A **Show diff** button to see a line-level diff between the starting prompt and the applied prompt
- A **Revert to this version** button to load a previous prompt back into the result area for re-application

### ⚡ Auto-Refine

When **Auto-Refine** is enabled in the Automation menu, Wallfacer automatically refines any backlog task that has not yet been refined. This runs in the background as tasks are created, so they arrive at execution time with a detailed spec rather than a short prompt.

Toggle Auto-Refine from the **Automation** menu (lightning bolt icon) in the header bar.

### Refinement Sandbox Configuration

By default, the refinement agent uses the Claude sandbox. You can change which sandbox runs refinement in two ways:

- **Globally**: Set the `WALLFACER_SANDBOX_REFINEMENT` environment variable in Settings > API Configuration.
- **Per task**: Override the refinement sandbox for a specific task using the per-activity sandbox selectors in the task detail modal.

If the Claude sandbox hits a token or rate limit during refinement, Wallfacer automatically falls back to the Codex sandbox and retries.

The refinement container has a fixed 30-minute timeout.

### ⏱️ Automatic Interval

When ideation is enabled, you can set an automatic repeat interval so brainstorm runs happen periodically without manual intervention. Available intervals:

| Interval | Behavior |
|---|---|
| 0 (default) | Run immediately when the previous brainstorm completes |
| 15 min | Schedule the next run 15 minutes after the previous one finishes |
| 30 min, 1h, 2h, 4h, 8h, 24h | Correspondingly longer intervals |

Configure the interval from the Automation menu or Settings > Execution. When an interval is set and a brainstorm is not currently running, the header displays a countdown showing when the next run is scheduled (for example, "Next brainstorm in 23m").

### Ideation Sandbox Configuration

The ideation agent defaults to the Claude sandbox. Configure it globally with the `WALLFACER_SANDBOX_IDEA_AGENT` environment variable. Like refinement, if the Claude sandbox hits a token limit, the agent automatically retries with the Codex sandbox.

### 📝 System Prompt Customization

Both refinement and ideation use built-in system prompt templates that control how the agents behave:

| Template | Purpose |
|---|---|
| `refinement.tmpl` | Instructs the refinement agent to explore the codebase and produce an implementation spec |
| `ideation.tmpl` | Instructs the brainstorm agent to explore the workspace, generate candidates, self-critique, and output ranked ideas |

To customize either template:

1. Open **Settings > System Prompts**.
2. Select the template you want to modify.
3. Edit the template text and save.

Your override replaces the built-in default for all future runs. To restore the original, delete your override from the same settings page. Overrides are validated for correct template syntax before saving.

You can also manage overrides via the API:

- `GET /api/system-prompts` -- list all templates with override status
- `PUT /api/system-prompts/{name}` -- write an override
- `DELETE /api/system-prompts/{name}` -- restore the embedded default

### API Endpoints

**Refinement**

| Method | Endpoint | Description |
|---|---|---|
| `POST` | `/api/tasks/{id}/refine` | Start refinement (optional body: `{"user_instructions": "..."}`) |
| `DELETE` | `/api/tasks/{id}/refine` | Cancel a running refinement |
| `GET` | `/api/tasks/{id}/refine/logs` | Stream live refinement container logs |
| `POST` | `/api/tasks/{id}/refine/apply` | Apply refined prompt (body: `{"prompt": "..."}`) |
| `POST` | `/api/tasks/{id}/refine/dismiss` | Dismiss refinement result without applying |

**Ideation**

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/ideate` | Get ideation status (enabled, running, next scheduled run) |
| `POST` | `/api/ideate` | Trigger a brainstorm run immediately |
| `DELETE` | `/api/ideate` | Cancel a running brainstorm |

### Configuration Variables

| Variable | Description |
|---|---|
| `WALLFACER_SANDBOX_REFINEMENT` | Sandbox type for refinement agents (default: inherits from `WALLFACER_DEFAULT_SANDBOX`) |
| `WALLFACER_SANDBOX_IDEA_AGENT` | Sandbox type for ideation agents (default: inherits from `WALLFACER_DEFAULT_SANDBOX`) |

Automation toggles (set via `PUT /api/config`):

| Config Field | Description |
|---|---|
| `autorefine` | Automatically refine unrefined backlog tasks |
| `ideation` | Enable periodic brainstorm runs |
| `ideation_interval` | Minutes between brainstorm runs (0 = run immediately on completion) |

---

## See Also

- [Usage Guide](usage.md) -- full board operations, task lifecycle, and automation overview
- [Getting Started](getting-started.md) -- initial setup and configuration
- [Circuit Breakers](circuit-breakers.md) -- how automation pauses on repeated failures
