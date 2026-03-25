package runner

import (
	"context"
	"io"
	"testing"
)

func TestSandboxStateString(t *testing.T) {
	tests := []struct {
		state SandboxState
		want  string
	}{
		{SandboxCreating, "creating"},
		{SandboxRunning, "running"},
		{SandboxStreaming, "streaming"},
		{SandboxStopping, "stopping"},
		{SandboxStopped, "stopped"},
		{SandboxFailed, "failed"},
		{SandboxState(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.state.String(); got != tt.want {
			t.Errorf("SandboxState(%d).String() = %q, want %q", tt.state, got, tt.want)
		}
	}
}

// Compile-time interface compliance checks.
var (
	_ SandboxBackend = (*fakeSandboxBackend)(nil)
	_ SandboxHandle  = (*fakeSandboxHandle)(nil)
)

// Minimal stubs that satisfy the interfaces — used only for the compile-time
// checks above. They are not invoked by any test.

type fakeSandboxBackend struct{}

func (fakeSandboxBackend) Launch(_ context.Context, _ ContainerSpec) (SandboxHandle, error) {
	return nil, nil
}
func (fakeSandboxBackend) List(_ context.Context) ([]ContainerInfo, error) {
	return nil, nil
}

type fakeSandboxHandle struct{}

func (fakeSandboxHandle) State() SandboxState   { return SandboxCreating }
func (fakeSandboxHandle) Stdout() io.ReadCloser { return nil }
func (fakeSandboxHandle) Wait() (int, error)    { return 0, nil }
func (fakeSandboxHandle) Kill() error           { return nil }
func (fakeSandboxHandle) Name() string          { return "" }
