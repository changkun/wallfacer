// Package store provides the in-memory task database backed by per-task directory
// persistence with event sourcing.
//
// Each task is stored in its own directory under data/<uuid>/ with JSON metadata,
// NDJSON trace files for event sourcing, and per-turn output files. The [Store]
// holds all active tasks in memory, protected by sync.RWMutex, and persists every
// mutation atomically (temp file + rename). It supports soft delete via tombstone
// files, secondary indexing for keyword search, cursor-based event pagination, and
// pub/sub change notifications for SSE streaming. The task state machine validates
// all status transitions.
//
// # Connected packages
//
// Depends on [constants] (limits and defaults), [logger], [sandbox] (sandbox type
// on tasks), [pkg/ndjson] (trace file I/O), [pkg/pubsub] (change notifications),
// and [pkg/pagination] (event cursors).
// Consumed by [handler] (all task CRUD and querying), [runner] (task mutations
// during execution), [cli] (server startup and status display), [workspace] (scoped
// store per workspace set), and [envconfig].
// The [Task] struct is the core domain model — changes to its fields require
// re-running go generate for the deep-clone function (cmd/gen-clone).
//
// # Usage
//
//	s, err := store.NewFileStore(dataDir)
//	task, err := s.CreateTask(ctx, "implement feature X", "goal", 0)
//	s.UpdateTaskStatus(ctx, task.ID, store.StatusInProgress)
//	events, _ := s.GetTaskEvents(ctx, task.ID)
package store
