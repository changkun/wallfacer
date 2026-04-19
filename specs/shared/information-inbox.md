---
title: "Information Inbox — External Signal Aggregation & Triage"
status: drafted
depends_on:
  - specs/shared/agent-abstraction.md
  - specs/observability/telemetry-observability.md
affects:
  - internal/inbox/
  - internal/handler/
  - internal/store/
  - ui/js/
effort: xlarge
created: 2026-04-01
updated: 2026-04-01
author: changkun
dispatched_task_id: null
---

# Information Inbox — External Signal Aggregation & Triage

---

## Problem

Wallfacer is a closed loop: tasks come from the user, agents execute them, results go back to the user. But the user's decision about *what to build next* is informed by signals that live outside the system entirely:

1. **No external awareness.** A trending Hacker News discussion about a competing approach, a Reddit thread reporting a UX pain point, an email from a collaborator requesting a feature -- none of these reach the task board. The user must manually monitor dozens of sources, synthesize relevance, and translate findings into tasks.

2. **No prioritization signal.** Even when the user notices something externally, there is no structured way to capture *why* it matters (community sentiment, urgency, impact scope) alongside the raw information. Context is lost between discovery and task creation.

3. **No agent-assisted triage.** Agents can write code but cannot help the user decide what code to write. The information that drives product decisions (market signals, user feedback, technical trends, dependency advisories) never enters the agent's context.

The gap is: **external information that should influence the roadmap never reaches the system where work gets planned and executed.**

This complements the telemetry-observability spec, which closes the loop for *runtime signals from the system's own software*. This spec closes the loop for *signals from the outside world*.

---

## Goals

1. **Ingest** information from configurable external sources (RSS/Atom, email, web APIs, manual paste).
2. **Normalize** heterogeneous inputs into a uniform inbox item format with metadata (source, timestamp, content, links).
3. **Triage** items using agent-assisted scoring: relevance to current workspaces/projects, urgency, potential impact.
4. **Surface** triaged items in a dedicated inbox panel in the Wallfacer UI, sorted by priority.
5. **Act** on items: dismiss, bookmark, convert to task (with context carried over), or queue for later review.
6. **Feed** high-relevance items into agent prompts as ambient context when creating or refining tasks.

---

## Non-Goals (v1)

- Replace dedicated feed readers, email clients, or news aggregators.
- Real-time social media monitoring or sentiment analysis at scale.
- Automated task creation from inbox items without user confirmation (auto-dispatch from telemetry anomalies is a separate concern in the telemetry spec).
- Full email client functionality (send, reply, thread management).
- Crawling or scraping arbitrary websites.

---

## Design

### Information Flow

```
External Sources              Wallfacer Inbox              Task Board
┌──────────────┐
│ RSS/Atom     │──┐
│ HN/Reddit    │  │     ┌───────────────────┐
│ Email (IMAP) │──┼────►│  Ingestion Queue   │
│ GitHub       │  │     │  (fetch + normalize)│
│ Manual Paste │──┘     └────────┬──────────┘
                                 │
                                 ▼
                        ┌───────────────────┐
                        │  Triage Agent      │
                        │  (score + classify)│
                        └────────┬──────────┘
                                 │
                                 ▼
                        ┌───────────────────┐        ┌──────────────┐
                        │  Inbox Panel (UI)  │───────►│  Task Card   │
                        │  sorted by priority│  act   │  (with ctx)  │
                        └───────────────────┘        └──────────────┘
```

### Source Connectors

Each source type implements a simple connector interface:

```go
// Connector fetches items from an external source.
type Connector interface {
    // Name returns a human-readable source identifier.
    Name() string
    // Fetch retrieves new items since the given cursor.
    // Returns items and an updated cursor for pagination.
    Fetch(ctx context.Context, cursor string) ([]RawItem, string, error)
    // Configure applies source-specific settings.
    Configure(cfg json.RawMessage) error
}
```

**Built-in connectors (v1):**

| Connector | Source | Auth | Fetch Strategy |
|-----------|--------|------|----------------|
| `rss` | Any RSS/Atom feed | None | Poll at configurable interval |
| `hackernews` | HN front page + specific searches | None (public API) | Poll top/new/best stories, keyword search |
| `reddit` | Subreddit feeds, saved posts | OAuth or RSS | Poll subreddit feeds or user saved items |
| `email` | IMAP mailbox | IMAP credentials | Poll inbox folder(s) at interval |
| `github` | Notifications, issue mentions, release notes | GitHub token (reuse existing) | Poll via GitHub API |
| `manual` | User paste / URL submission | None | Direct input via UI or API |

**Connector registration** is pluggable. Third-party connectors can be added as Go plugins or compiled in. v1 ships the built-in set.

### RawItem & InboxItem

```go
// RawItem is the unprocessed output from a connector.
type RawItem struct {
    SourceName  string            `json:"source_name"`
    ExternalID  string            `json:"external_id"`  // dedup key per source
    FetchedAt   time.Time         `json:"fetched_at"`
    Title       string            `json:"title,omitempty"`
    Body        string            `json:"body,omitempty"`       // plain text or markdown
    URL         string            `json:"url,omitempty"`
    Author      string            `json:"author,omitempty"`
    PublishedAt *time.Time        `json:"published_at,omitempty"`
    Metadata    map[string]string `json:"metadata,omitempty"`   // source-specific (score, subreddit, etc.)
}

// InboxItem is a triaged, stored inbox entry.
type InboxItem struct {
    ID            uuid.UUID         `json:"id"`
    RawItem                         // embedded
    Status        InboxItemStatus   `json:"status"`          // "unread", "read", "bookmarked", "dismissed", "converted"
    Priority      float64           `json:"priority"`        // 0.0 - 1.0, higher = more urgent/relevant
    RelevanceNote string            `json:"relevance_note"`  // agent-generated explanation of why this matters
    Tags          []string          `json:"tags,omitempty"`  // auto-assigned categories
    RelatedTaskID *uuid.UUID        `json:"related_task_id,omitempty"` // if converted to a task
    TriagedAt     *time.Time        `json:"triaged_at,omitempty"`
    CreatedAt     time.Time         `json:"created_at"`
    UpdatedAt     time.Time         `json:"updated_at"`
}

type InboxItemStatus string

const (
    InboxUnread    InboxItemStatus = "unread"
    InboxRead      InboxItemStatus = "read"
    InboxBookmarked InboxItemStatus = "bookmarked"
    InboxDismissed InboxItemStatus = "dismissed"
    InboxConverted InboxItemStatus = "converted"
)
```

### Triage Agent

When new items arrive, a lightweight triage agent scores and classifies them. This reuses the agent abstraction from `specs/shared/agent-abstraction.md`.

**Input to triage agent:**
- Batch of new `RawItem`s (up to N at a time, default 20).
- Current workspace context: active workspace paths, recent task titles, project goals from AGENTS.md.
- User-defined triage criteria (optional, from inbox config).

**Output from triage agent:**
- Per-item: priority score (0.0-1.0), relevance note (1-2 sentences), tags.

**Triage strategy:**

The agent is prompted with the workspace context and asked: "For each item, assess relevance to the current project(s), urgency, and potential impact. Score 0.0 (irrelevant noise) to 1.0 (critical, act now)."

Triage runs as a background job -- it does not block the inbox from showing items. Untriaged items appear with a "pending triage" indicator and default priority 0.5.

**Cost control:**
- Triage uses the cheapest available model (Haiku-class).
- Batch processing: multiple items per prompt to amortize overhead.
- Skip triage for items matching user-defined auto-dismiss rules (e.g., "dismiss all HN items scoring below 10 points").
- Triage budget cap: configurable max spend per day on triage (default: $0.10/day).

### Connection to Telemetry

The telemetry-observability spec collects **runtime signals** (errors, latency, health probes) from software Wallfacer builds. The inbox collects **external signals** (news, discussions, emails) from the outside world. Both feed into the same decision surface: the Wallfacer UI where the user plans work.

Shared patterns:
- Both produce items that surface in the UI alongside the task board.
- Both support "convert to task" with context injection.
- Both use agent-assisted triage/anomaly detection.
- Both store data in SQLite with retention policies.

The telemetry panel and inbox panel are siblings in the UI, not nested. A future "signals" umbrella view could unify them, but v1 keeps them separate because the data shapes and triage logic differ.

The multi-agent debate spec (`specs/oversight/multi-agent-debate.md`) already proposes using deliberation for telemetry signal triage. The inbox triage agent could participate in the same debate framework when multiple agents assess the relevance of an inbox item to the project.

---

## UI

### Inbox Panel

A new top-level panel accessible from the toolbar (alongside tasks, terminal, serve, telemetry).

**Layout:**
- **Item list** (left): Items sorted by priority (descending), grouped by date. Each item shows: source icon, title, priority badge, relevance note snippet, timestamp.
- **Detail pane** (right): Selected item's full content, metadata, source link, triage explanation, action buttons.
- **Filter bar** (top): Filter by source, status, priority range, tags, date range.

**Item actions:**
- **Mark read** — moves from unread to read.
- **Bookmark** — pins for later review.
- **Dismiss** — hides from default view (still searchable).
- **Create task** — opens task creation dialog pre-filled with item context (title derived from item title, prompt includes item body/URL/relevance note as context).
- **Copy to clipboard** — for manual use elsewhere.

**Indicators:**
- Unread count badge on the inbox toolbar icon.
- Priority color coding: red (>0.8), orange (>0.6), yellow (>0.4), gray (<=0.4).

### Task Creation from Inbox

When converting an inbox item to a task:
1. The task prompt is pre-populated with context from the item.
2. The item's URL, body, and relevance note are included as a structured context block.
3. The user can edit the prompt before creating the task.
4. The inbox item's status becomes "converted" and links to the created task ID.

### Notification Badge

The toolbar inbox icon shows an unread count. High-priority items (>0.8) trigger a subtle pulse animation to draw attention without being intrusive.

---

## API

### Inbox Items

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/inbox` | List inbox items (filterable by status, source, priority, date) |
| `GET` | `/api/inbox/{id}` | Get a single inbox item with full detail |
| `PATCH` | `/api/inbox/{id}` | Update item status (read, bookmark, dismiss) |
| `POST` | `/api/inbox/{id}/convert` | Create a task from an inbox item |
| `DELETE` | `/api/inbox/{id}` | Permanently delete an inbox item |
| `POST` | `/api/inbox/manual` | Submit a manual item (paste URL or text) |
| `GET` | `/api/inbox/stream` | SSE: real-time new inbox items |
| `GET` | `/api/inbox/stats` | Unread count, source breakdown, priority distribution |

### Source Configuration

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/inbox/sources` | List configured sources with status |
| `POST` | `/api/inbox/sources` | Add a new source connector |
| `PUT` | `/api/inbox/sources/{id}` | Update source configuration |
| `DELETE` | `/api/inbox/sources/{id}` | Remove a source connector |
| `POST` | `/api/inbox/sources/{id}/test` | Test connectivity for a source |

### Triage Configuration

| Method | Route | Description |
|--------|-------|-------------|
| `GET` | `/api/inbox/triage/config` | Get triage settings (model, budget, auto-dismiss rules) |
| `PUT` | `/api/inbox/triage/config` | Update triage settings |
| `POST` | `/api/inbox/triage/run` | Force immediate triage of pending items |

---

## Storage

### Local

- **SQLite**: `~/.wallfacer/inbox.db` — separate from the main store and telemetry DBs to avoid contention.
  - `inbox_items` — triaged items with full metadata.
  - `inbox_sources` — connector configurations.
  - `inbox_cursors` — per-source fetch cursors for incremental polling.
- **Retention**: configurable per source, default 30 days. Items older than retention are pruned. Bookmarked items are exempt from pruning.

### Cloud

Cloud mode stores inbox data in the tenant's database (Postgres). The connector infrastructure runs per-tenant, with shared rate limits to external APIs.

---

## Configuration

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `WALLFACER_INBOX_ENABLED` | `false` | Enable the inbox system |
| `WALLFACER_INBOX_POLL_INTERVAL` | `300` | Default poll interval in seconds for source connectors |
| `WALLFACER_INBOX_RETENTION_DAYS` | `30` | Days to retain inbox items |
| `WALLFACER_INBOX_TRIAGE_MODEL` | (Haiku) | Model for triage agent |
| `WALLFACER_INBOX_TRIAGE_BUDGET` | `0.10` | Daily triage cost cap in USD |
| `WALLFACER_INBOX_MAX_ITEMS` | `1000` | Maximum stored items (oldest pruned first, excluding bookmarked) |

### Source Configuration (per-source JSON)

```json
{
  "type": "hackernews",
  "name": "HN Front Page",
  "enabled": true,
  "poll_interval_seconds": 600,
  "config": {
    "endpoint": "topstories",
    "min_score": 50,
    "keywords": ["ai agent", "code generation", "developer tools"]
  },
  "auto_dismiss_rules": [
    {"field": "metadata.score", "op": "lt", "value": "10"}
  ]
}
```

---

## Implementation Phases

### Phase 1 -- Core inbox + manual input

| File | Change |
|------|--------|
| `internal/inbox/` (new package) | `InboxItem`, `Connector` interface, `Store` (SQLite) |
| `internal/inbox/manual.go` | Manual paste connector (URL fetch + text input) |
| `internal/handler/inbox.go` | CRUD endpoints, SSE stream |
| `ui/js/inbox.js` | Inbox panel, item list, detail pane, manual submit |

**Depends on:** Nothing. Can start immediately.
**Effort:** Medium.

### Phase 2 -- RSS + Hacker News connectors

| File | Change |
|------|--------|
| `internal/inbox/rss.go` | RSS/Atom feed connector |
| `internal/inbox/hackernews.go` | HN API connector (top/new/best + keyword search) |
| `internal/inbox/poller.go` | Background polling goroutine, cursor management |

**Depends on:** Phase 1.
**Effort:** Medium.

### Phase 3 -- Triage agent

| File | Change |
|------|--------|
| `internal/inbox/triage.go` | Triage agent: batch scoring, relevance notes, tags |
| `internal/prompts/inbox-triage.tmpl` | Triage system prompt template |

**Depends on:** Phase 1, agent abstraction spec (for clean agent lifecycle).
**Effort:** Medium.

### Phase 4 -- Reddit + Email + GitHub connectors

| File | Change |
|------|--------|
| `internal/inbox/reddit.go` | Reddit connector (subreddit RSS or OAuth API) |
| `internal/inbox/email.go` | IMAP connector (fetch, parse, normalize) |
| `internal/inbox/github.go` | GitHub notifications + release notes connector |

**Depends on:** Phase 2 (shared poller infrastructure).
**Effort:** Large.

### Phase 5 -- Task conversion + context injection

| File | Change |
|------|--------|
| `internal/inbox/convert.go` | Inbox item to task conversion with context injection |
| `internal/handler/inbox.go` | `POST /api/inbox/{id}/convert` endpoint |
| `ui/js/inbox.js` | "Create task" dialog with pre-filled prompt |

**Depends on:** Phase 1.
**Effort:** Low.

### Phase 6 -- Ambient context for agents

| File | Change |
|------|--------|
| `internal/inbox/context.go` | Select high-relevance items for prompt injection |
| `internal/runner/` | Inject inbox context summary into task prompts (opt-in) |

**Depends on:** Phase 3 (triage scores needed for selection).
**Effort:** Low.

---

## Key Patterns Reused

| Pattern | Source | Reused For |
|---------|--------|------------|
| SQLite persistence | `internal/store/` patterns | Inbox item storage |
| SSE streaming | `internal/handler/stream.go` | `GET /api/inbox/stream` |
| Background polling | `internal/runner/` auto-promoter | Source connector polling loop |
| Agent as sub-task | `internal/runner/` refinement agent | Triage agent execution |
| Prompt templates | `internal/prompts/` | Triage system prompt |
| Environment config | `internal/envconfig/` | Inbox feature flags and settings |

---

## Potential Challenges

1. **External API rate limits.** HN, Reddit, and GitHub APIs have rate limits. Connectors must respect them and back off gracefully. Per-source rate limit tracking is needed.

2. **Triage cost creep.** Even with Haiku-class models, triaging hundreds of items per day adds up. The daily budget cap and batch processing mitigate this, but users need visibility into triage spend.

3. **Relevance drift.** As projects evolve, the triage agent's understanding of "relevant" must update. Re-triage of bookmarked items when workspace context changes significantly could help, but adds complexity.

4. **Email security.** IMAP credentials are sensitive. Must be stored encrypted (same mechanism as API keys in `.env`). IMAP connections must use TLS. Consider OAuth2 for Gmail/Outlook instead of app passwords.

5. **Content extraction from URLs.** Manual paste of a URL requires fetching and extracting readable content. This is a best-effort operation -- some sites block bots, require JS rendering, or have paywalls. Fall back to title + URL when extraction fails.

6. **Noise management.** Without careful auto-dismiss rules and triage tuning, the inbox becomes a firehose that users ignore. The default experience should be low-noise: few sources, high thresholds, conservative scoring.

7. **UI real estate.** Adding another panel competes with the task board, terminal, serve, and telemetry for screen space. The inbox should be designed as a sidebar or overlay that doesn't displace the primary task view.

---

## Open Questions

1. **Inbox as a tab vs sidebar?** The task board is the primary view. Should the inbox be a peer panel (like terminal/telemetry) or a persistent sidebar? A sidebar keeps it visible while working on tasks, but takes horizontal space. A tab is cleaner but requires explicit switching.

2. **Shared inbox across workspaces?** Sources like HN and email are not workspace-specific. Should there be a global inbox plus workspace-specific relevance scoring, or fully separate inboxes per workspace group?

3. **Connector plugin system?** v1 compiles connectors in. Should there be a plugin mechanism (Go plugins, subprocess protocol, WASM) for third-party connectors? This adds complexity but enables community extensions (Slack channels, Discord, RSS aggregators, proprietary feeds).

4. **Bi-directional email?** The spec treats email as read-only input. Should the inbox support replying to emails from within Wallfacer (e.g., "reply with task status")? This is scope creep for v1 but could be valuable for team coordination.

5. **Integration with multi-agent debate?** High-importance inbox items (e.g., a security advisory affecting a dependency) could be fed into the multi-agent debate system for deeper analysis. Should the inbox have a "deep analyze" action that triggers a debate session?

6. **Privacy and data retention.** Inbox items may contain private emails or sensitive discussions. Retention policies, encryption at rest, and clear data deletion semantics are needed. Should items be redacted before being injected into agent prompts?

7. **Deduplication across sources.** The same news item might appear in HN, Reddit, and an RSS feed. Cross-source dedup by URL helps, but discussions about the same topic with different URLs need fuzzy matching (title similarity, entity overlap).

---

## What This Does NOT Require

- No changes to the core task execution loop (inbox is a parallel concern).
- No mandatory external accounts (the system works with zero sources configured; manual paste is always available).
- No external service dependencies beyond what the user configures (no Wallfacer-hosted aggregation service).
- No changes to existing API routes (all inbox routes are additive).
