// Package sandbox defines the supported sandbox runtime types for task execution
// containers.
//
// Wallfacer supports multiple AI agent runtimes (currently Claude Code and OpenAI
// Codex). This package provides the [Type] enum, parsing, validation, and default
// resolution so that sandbox selection is consistent across configuration, container
// launching, and the UI. Claude is the default when no explicit sandbox is specified.
//
// # Connected packages
//
// No internal dependencies (stdlib only). Consumed broadly: [envconfig] (sandbox
// routing configuration), [store] (task sandbox field and activity routing),
// [runner] (container image selection and command building), [handler] (sandbox
// dropdowns and validation), and [cli] (sandbox image management). When adding a
// new sandbox type, update [All], add the new constant, and ensure container image
// build support exists in the Makefile.
//
// # Usage
//
//	sb, ok := sandbox.Parse("claude")
//	sb = sandbox.Default("")  // returns sandbox.Claude
//	for _, t := range sandbox.All() { ... }
package sandbox
