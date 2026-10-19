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

// OversightAgentTimeout bounds the oversight-summary agent. Generous
// because oversight reads the full task event timeline before summarizing.
const OversightAgentTimeout = 3 * time.Minute

// CommitMessageAgentTimeout bounds the commit-message agent. Shorter than
// oversight because the input (diff stat + recent log) is small.
const CommitMessageAgentTimeout = 90 * time.Second

// ---------------------------------------------------------------------------
// Polling intervals
// ---------------------------------------------------------------------------

// DefaultWorktreeHealthInterval is the interval between worktree health checks.
const DefaultWorktreeHealthInterval = 2 * time.Minute

// DefaultWorktreeGCInterval is the interval between worktree garbage collection runs.
const DefaultWorktreeGCInterval = 24 * time.Hour

// SSEKeepaliveInterval controls how often SSE streams send keepalive comments
// to prevent proxy and OS-level TCP idle timeouts from silently closing the
// connection. Tests can lower this for faster verification.
var SSEKeepaliveInterval = 15 * time.Second

// RefinementRecentCompleteWindow is the grace window for clearing a refinement
// job that has just finished, allowing async startup/failure races to settle.
const RefinementRecentCompleteWindow = 500 * time.Millisecond

// ---------------------------------------------------------------------------
// Cache TTLs
// ---------------------------------------------------------------------------

// FileIndexTTL is the time-to-live for a cached workspace file list.
const FileIndexTTL = 30 * time.Second

// DiffCacheTTL is the time-to-live for cached diff entries for non-terminal tasks.
const DiffCacheTTL = 10 * time.Second

// ---------------------------------------------------------------------------
// Retry / concurrency limits
// ---------------------------------------------------------------------------

// MaxRebaseRetries is the maximum number of rebase attempts before giving up.
const MaxRebaseRetries = 3

// DefaultMaxConcurrentTasks is the default parallel task limit.
const DefaultMaxConcurrentTasks = 5

// ---------------------------------------------------------------------------
// Size limits
// ---------------------------------------------------------------------------

// Request body size limits.
const (
	BodyLimitDefault  int64 = 1 << 20   // 1 MiB
	BodyLimitFeedback int64 = 512 << 10 // 512 KiB
)

// ExplorerMaxFileSize is the maximum file size the explorer will read (2 MiB).
const ExplorerMaxFileSize int64 = 2 * 1024 * 1024

// DefaultMaxTurnOutputBytes is the default per-turn stdout/stderr output size
// budget. Outputs exceeding this limit are truncated server-side.
const DefaultMaxTurnOutputBytes = 8 * 1024 * 1024 // 8 MB

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

// ---------------------------------------------------------------------------
// Task schema / pruning limits
// ---------------------------------------------------------------------------

// CurrentTaskSchemaVersion is the on-disk schema version for task.json.
const CurrentTaskSchemaVersion = 3

// DefaultRetryHistoryLimit, DefaultRefineSessionsLimit, and
// DefaultPromptHistoryLimit cap the number of entries persisted for the three
// unboundedly-growing task slice fields.
const (
	DefaultRetryHistoryLimit   = 10
	DefaultRefineSessionsLimit = 5
	DefaultPromptHistoryLimit  = 20
)
