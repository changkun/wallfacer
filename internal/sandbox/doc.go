// Package sandbox defines sandbox runtime types and the pluggable backend
// abstraction for launching and managing containers.
//
// The package provides two layers:
//
//   - [Type] enum (Claude, Codex) — identifies which AI agent runtime a task uses.
//   - [Backend] / [Handle] interfaces — abstract the container runtime so that
//     local (podman/docker) and remote (K8s, remote Docker) backends share the
//     same lifecycle model.
//
// [LocalBackend] is the production implementation, launching containers via
// os/exec. [ContainerSpec] and [VolumeMount] describe container configuration
// declaratively; [ContainerSpec.Build] produces the CLI arg slice.
//
// # Connected packages
//
// Consumed by [runner] (container orchestration), [handler] (container
// monitoring), [envconfig] (sandbox routing), [store] (task sandbox field),
// and [cli] (sandbox image management and doctor checks).
package sandbox
