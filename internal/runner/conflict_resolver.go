package runner

// ConflictResolverTrigger identifies what triggered a conflict resolution attempt.
type ConflictResolverTrigger string

// Possible triggers for conflict resolution.
const (
	ConflictResolverTriggerSync   ConflictResolverTrigger = "sync"
	ConflictResolverTriggerCommit ConflictResolverTrigger = "commit"
)
