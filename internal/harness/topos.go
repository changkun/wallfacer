package harness

import (
	"errors"
	"io"
)

func init() {
	Register(&toposHarness{})
}

// ErrInProcess is returned by an in-process harness's BuildArgv to signal that
// it has no subprocess argv: the caller must drive it through the in-process
// execution seam (the runner's agent-graph path) rather than the executor.
var ErrInProcess = errors.New("harness: in-process harness has no argv; drive it through the in-process seam")

// InProcess reports whether id names a harness that runs in-process rather than
// as a subprocess. The runner consults this to choose the in-process execution
// path over BuildArgv + the executor. Today only Topos is in-process.
func InProcess(id ID) bool { return id == Topos }

// toposHarness is the native, in-process latere.ai harness. It is a registry
// citizen so the config/UI selector, default resolution, and per-task pinning
// treat it uniformly with the CLI harnesses; its actual execution is handled
// in-process by the runner via internal/agentgraph, not by BuildArgv/executor.
type toposHarness struct{}

// ID returns the Topos identifier.
func (toposHarness) ID() ID { return Topos }

// BuildArgv returns ErrInProcess: Topos runs in-process and has no CLI argv.
// Callers must detect an in-process harness (see InProcess) and route it
// through the agent-graph seam instead of the subprocess executor.
func (toposHarness) BuildArgv(Request) ([]string, io.Reader, error) {
	return nil, nil, ErrInProcess
}

// ParseEvent is unused for an in-process harness (events are mapped directly
// from the topos observer in the runner, not parsed from NDJSON stdout). It
// returns KindUnknown so any accidental subprocess-path call records rather
// than crashes, mirroring the contract the CLI harnesses follow on schema drift.
func (toposHarness) ParseEvent(raw []byte) (Event, error) {
	return Event{Kind: KindUnknown, Raw: raw}, nil
}

// AuthEnv returns no environment variables: Topos resolves model credentials
// in-process through wallfacer's configured model gateway (Lux) inside the
// agent-graph seam, not via subprocess env injection.
func (toposHarness) AuthEnv(AuthConfig) (map[string]string, error) {
	return map[string]string{}, nil
}

// Capabilities reports what the native harness supports. Topos agents carry a
// system prompt and the runtime reports token usage; resume and MCP are not yet
// wired through the in-process seam.
func (toposHarness) Capabilities() Capabilities {
	return Capabilities{
		SupportsSystemPrompt: true,
		EmitsUsage:           true,
	}
}
