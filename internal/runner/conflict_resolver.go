package runner

// ConflictResolverTrigger identifies what triggered a conflict resolution attempt.
// The value is recorded in system events for observability so operators can
// distinguish sync-triggered resolutions from commit-triggered ones.
type ConflictResolverTrigger string

// Possible triggers for conflict resolution.
const (
	ConflictResolverTriggerSync   ConflictResolverTrigger = "sync"
	ConflictResolverTriggerCommit ConflictResolverTrigger = "commit"
)
