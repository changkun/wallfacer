// Package constants consolidates tunable system parameters: timeouts,
// intervals, retry counts, size limits, concurrency caps, and pagination
// defaults used across the wallfacer backend.
package constants

import "time"

// ---------------------------------------------------------------------------
// Timeouts
// ---------------------------------------------------------------------------

// DefaultTaskTimeout is the default timeout for task execution.
const DefaultTaskTimeout = 60 * time.Minute

// IdeaAgentDefaultTimeout is the default timeout (minutes) for idea-agent task cards.
const IdeaAgentDefaultTimeout = 60

// TitleAgentTimeout bounds the background title-generation agent. Short
// because the agent is headless and only needs to emit a 2–5 word summary.
const TitleAgentTimeout = 60 * time.Second

// OversightAgentTimeout bounds the oversight-summary agent. Generous
// because oversight reads the full task event timeline before summarizing.
const OversightAgentTimeout = 3 * time.Minute

// CommitMessageAgentTimeout bounds the commit-message agent. Shorter than
// oversight because the input (diff stat + recent log) is small.
const CommitMessageAgentTimeout = 90 * time.Second

// ---------------------------------------------------------------------------
// Polling / watcher intervals
// ---------------------------------------------------------------------------

// ContainerPollInterval is the polling interval for container health monitoring.
const ContainerPollInterval = 5 * time.Second

// AutoPromoteInterval is the periodic ticker interval for the auto-promoter.
const AutoPromoteInterval = 60 * time.Second

// WaitingSyncInterval is the polling interval for syncing waiting tasks.
const WaitingSyncInterval = 30 * time.Second

// AutoTestInterval is the polling interval for the auto-test watcher.
const AutoTestInterval = 30 * time.Second

// AutoSubmitInterval is the polling interval for the auto-submit watcher.
const AutoSubmitInterval = 30 * time.Second

// FetchErrorGracePeriod is how long after a fetch error before retrying.
const FetchErrorGracePeriod = 5 * time.Minute

// DefaultWorktreeHealthInterval is the interval between worktree health checks.
const DefaultWorktreeHealthInterval = 2 * time.Minute

// DefaultWorktreeGCInterval is the interval between worktree garbage collection runs.
const DefaultWorktreeGCInterval = 24 * time.Hour

// DefaultIdeationInterval is the default interval between ideation brainstorm runs.
const DefaultIdeationInterval = 60 * time.Minute

// SSEKeepaliveInterval controls how often SSE streams send keepalive comments
// to prevent proxy and OS-level TCP idle timeouts from silently closing the
// connection. Tests can lower this for faster verification.
var SSEKeepaliveInterval = 15 * time.Second

// WatcherSettleDelay is the pause after receiving a wake signal before
// calling the action. Tests can lower this for faster verification.
var WatcherSettleDelay = 1500 * time.Millisecond

// RefinementRecentCompleteWindow is the grace window for clearing a refinement
// job that has just finished, allowing async startup/failure races to settle.
const RefinementRecentCompleteWindow = 500 * time.Millisecond

// WorkspaceIdeationCommandTTL is the timeout for workspace-scanning shell
// commands during ideation.
const WorkspaceIdeationCommandTTL = 2 * time.Second

// ---------------------------------------------------------------------------
// Cache TTLs
// ---------------------------------------------------------------------------

// FileIndexTTL is the time-to-live for a cached workspace file list.
const FileIndexTTL = 30 * time.Second

// DiffCacheTTL is the time-to-live for cached diff entries for non-terminal tasks.
const DiffCacheTTL = 10 * time.Second

// CommitsBehindCacheTTL is the time-to-live for cached CommitsBehind results.
const CommitsBehindCacheTTL = 20 * time.Second

// ---------------------------------------------------------------------------
// Retry / concurrency limits
// ---------------------------------------------------------------------------

// MaxAutoRetries is the global cap on automatic retries per task across all
// failure categories and retry paths (runner and handler).
const MaxAutoRetries = 3

// MaxRebaseRetries is the maximum number of rebase attempts before giving up.
const MaxRebaseRetries = 3

// MaxTestFailRetries is the maximum number of consecutive test failures before
// the auto-resume cycle is halted.
const MaxTestFailRetries = 3

// DefaultMaxConcurrentTasks is the default parallel task limit.
const DefaultMaxConcurrentTasks = 5

// DefaultMaxTestConcurrentTasks is the default parallel test-run limit.
const DefaultMaxTestConcurrentTasks = 2

// DefaultCBThreshold is the number of consecutive container launch failures
// required to open the circuit breaker.
const DefaultCBThreshold = 5

// ---------------------------------------------------------------------------
// Size limits
// ---------------------------------------------------------------------------

// Request body size limits.
const (
	BodyLimitDefault      int64 = 1 << 20   // 1 MiB
	BodyLimitInstructions int64 = 5 << 20   // 5 MiB
	BodyLimitFeedback     int64 = 512 << 10 // 512 KiB
)

// ExplorerMaxFileSize is the maximum file size the explorer will read (2 MiB).
const ExplorerMaxFileSize int64 = 2 * 1024 * 1024

// DefaultMaxTurnOutputBytes is the default per-turn stdout/stderr output size
// budget. Outputs exceeding this limit are truncated server-side.
const DefaultMaxTurnOutputBytes = 8 * 1024 * 1024 // 8 MB

// MaxDiffBytes is the maximum number of bytes to include from the git diff in
// the test prompt.
const MaxDiffBytes = 16000

// MaxOversightLogBytes caps the total log size to avoid exceeding prompt limits.
const MaxOversightLogBytes = 40000

// MaxBoardManifestBytes is the maximum size for a board manifest JSON.
const MaxBoardManifestBytes = 64 * 1024

// MaxFileListSize caps the total number of files returned to keep responses fast.
const MaxFileListSize = 8000

// MaxImmutableDiffEntries caps the number of retained terminal-task diff entries.
const MaxImmutableDiffEntries = 256

// MaxSearchResults caps the number of task search results returned.
const MaxSearchResults = 50

// SnippetPadding is the number of context characters shown on each side of a
// search match in task search results.
const SnippetPadding = 60

// MaxCommitSubjectRunes is the maximum number of runes allowed in the commit
// message subject line (after the "wallfacer: " prefix).
const MaxCommitSubjectRunes = 72 - len("wallfacer: ")

// MaxTailLines is the number of tail lines checked for test verdict detection.
const MaxTailLines = 15

// LogValueMaxLen is the maximum length for value truncation in pretty logging.
const LogValueMaxLen = 200

// ---------------------------------------------------------------------------
// Pagination / defaults
// ---------------------------------------------------------------------------

// DefaultArchivedTasksPerPage is the pagination size for archived task listings.
const DefaultArchivedTasksPerPage = 20

// DefaultTombstoneRetentionDays is the default number of days before
// pruning soft-deleted tasks.
const DefaultTombstoneRetentionDays = 7

// DefaultIdeationExploitRatio is the default exploit/explore balance for ideation.
const DefaultIdeationExploitRatio = 0.8

// ---------------------------------------------------------------------------
// Ideation parameters
// ---------------------------------------------------------------------------

// MaxIdeationIdeas is the maximum number of ideas generated per ideation run.
const MaxIdeationIdeas = 3

// DefaultIdeationImpactScore is the default impact score threshold for ideation.
const DefaultIdeationImpactScore = 60

// MaxIdeationChurnSignals is the maximum number of churn signals collected.
const MaxIdeationChurnSignals = 6

// MaxIdeationTodoSignals is the maximum number of TODO signals collected.
const MaxIdeationTodoSignals = 6

// ChurnLookbackDays only includes commits newer than this many days.
const ChurnLookbackDays = 90

// MaxChurnCommits is the hard cap so very active repos don't scan unboundedly.
const MaxChurnCommits = 200

// ---------------------------------------------------------------------------
// Task schema / pruning limits
// ---------------------------------------------------------------------------

// CurrentTaskSchemaVersion is the on-disk schema version for task.json.
const CurrentTaskSchemaVersion = 2

// DefaultRetryHistoryLimit, DefaultRefineSessionsLimit, and
// DefaultPromptHistoryLimit cap the number of entries persisted for the three
// unboundedly-growing task slice fields.
const (
	DefaultRetryHistoryLimit   = 10
	DefaultRefineSessionsLimit = 5
	DefaultPromptHistoryLimit  = 20
)
