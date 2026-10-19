// Package handler implements all HTTP API route handlers for the wallfacer
// task board server.
//
// The [Handler] struct is the central dependency container, holding references
// to the task store, runner, workspace manager, prompt manager and metrics
// registry. Each concern is implemented in a separate file: tasks,
// configuration, git operations, streaming, refinement, oversight,
// and more. The package uses stdlib net/http (Go 1.22+ pattern routing) with no
// framework. Tasks move only on a request: nothing starts, tests or commits a
// task on its own.
//
// # Connected packages
//
// This is the HTTP API layer sitting between the browser and the execution engine.
// Depends on [runner] for task orchestration, [store] for persistence, [workspace]
// for workspace switching, [envconfig] for configuration, [instructions] for
// AGENTS.md, [gitutil] (indirectly via runner), [constants], [logger], [metrics],
// and several internal/pkg utilities ([cache], [atomicfile]).
// Consumed by [cli] which registers these handlers on the HTTP mux.
// Changes to [store.Task] fields, [runner.Interface] methods, or [apicontract]
// routes typically require corresponding handler updates.
//
// # Usage
//
//	h := handler.NewHandler(store, runner, configDir, metricsRegistry)
//	mux.HandleFunc("GET /api/tasks", h.ListTasks)
//	mux.HandleFunc("POST /api/tasks", h.CreateTask)
package handler
