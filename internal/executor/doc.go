// Package executor defines the host process launch abstraction
// the runner drives.
//
//   - Agent types live in [harness] (harness.Claude, harness.Codex).
//   - [Backend] / [Handle] abstract process launch and supervision.
//
// [HostBackend] is the only implementation: it execs the host-installed
// claude / codex CLIs directly. [ContainerSpec] is the declarative launch
// shape (env, cwd, argv); host launch reinterprets its fields, and
// [ContainerSpec.Build] is retained for handlers that render the spec.
package executor
