// Package sandbox defines the harness type enum and the launch abstraction
// the runner drives.
//
//   - [Type] (Claude, Codex) names the agent runtime a task uses.
//   - [Backend] / [Handle] abstract process launch and supervision.
//
// [HostBackend] is the only implementation: it execs the host-installed
// claude / codex CLIs directly. [ContainerSpec] is the declarative launch
// shape (env, cwd, argv); host launch reinterprets its fields, and
// [ContainerSpec.Build] is retained for handlers that render the spec.
package sandbox
