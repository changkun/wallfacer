package runner

type ConflictResolverTrigger string

const (
	ConflictResolverTriggerSync   ConflictResolverTrigger = "sync"
	ConflictResolverTriggerCommit ConflictResolverTrigger = "commit"
)
