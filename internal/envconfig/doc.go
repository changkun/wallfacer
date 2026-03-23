// Package envconfig reads and writes the wallfacer .env configuration file
// (~/.wallfacer/.env).
//
// The .env file holds authentication tokens, model selection, sandbox routing,
// container resource limits, workspace paths, and other runtime settings.
// [Parse] reads the file into a [Config] struct, and [Update] atomically modifies
// it using a callback, preserving fields that the caller does not touch. This
// ensures that concurrent reads and writes to the configuration file are safe.
//
// # Connected packages
//
// Depends on [changkun.de/x/wallfacer/internal/pkg/atomicfile] for atomic writes
// and [changkun.de/x/wallfacer/internal/sandbox] for sandbox type parsing.
// Consumed by [workspace] (env file path management), [runner] (container settings),
// [handler] (serve/update env configuration), and [cli] (build container commands).
// When adding a new .env variable, add the field to [Config], update [Parse] and
// [ToEnvMap], then update the corresponding handler and documentation.
//
// # Usage
//
//	cfg, err := envconfig.Parse("/path/to/.env")
//	err = envconfig.Update("/path/to/.env", func(c *envconfig.Config) {
//	    c.DefaultModel = "claude-sonnet-4-6-20250514"
//	})
package envconfig
