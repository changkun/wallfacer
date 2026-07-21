// Package envconfig reads and writes the wallfacer .env configuration file
// (~/.wallfacer/.env).
//
// The .env file holds authentication tokens, model selection, sandbox routing,
// container resource limits, workspace paths, and other runtime settings.
// [Parse] reads the file into a [Config] struct, and [Update] atomically modifies
// it by merging an [Updates] struct, preserving fields whose pointer is left nil.
// This ensures that concurrent reads and writes to the configuration file are safe.
//
// # Connected packages
//
// Depends on [latere.ai/x/wallfacer/internal/pkg/atomicfile] for atomic writes
// and [latere.ai/x/wallfacer/internal/harness] for agent-type parsing.
// Consumed by [workspace] (env file path management), [runner] (container settings),
// [handler] (serve/update env configuration), and [cli] (build container commands).
// When adding a new .env variable, add the field to [Config], update [Parse],
// add the key to knownKeys, and add the corresponding field to [Updates],
// then update the handler and documentation.
//
// # Usage
//
//	cfg, err := envconfig.Parse("/path/to/.env")
//	model := "claude-sonnet-4-6-20250514"
//	err = envconfig.Update("/path/to/.env", envconfig.Updates{DefaultModel: &model})
package envconfig
