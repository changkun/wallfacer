package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"sync"

	"changkun.de/x/wallfacer/internal/sandbox"
)

// ContainerResponse holds the pre-configured response for a single Launch invocation.
type ContainerResponse struct {
	Stdout []byte
	Stderr []byte
	Err    error
	// Panic, when true, causes Launch to panic instead of returning a response.
	Panic bool
}

// ContainerCall records a single Launch invocation for later assertion.
type ContainerCall struct {
	Name string
	Args []string
}

// MockSandboxBackend implements SandboxBackend for tests. It pops pre-configured
// ContainerResponse entries from a queue and records all Launch calls so tests
// can assert on the exact container spec and args passed.
type MockSandboxBackend struct {
	mu        sync.Mutex
	responses []ContainerResponse
	calls     []ContainerCall
	killCalls []string
}

// Launch pops the next response and returns a mockSandboxHandle that yields it.
func (m *MockSandboxBackend) Launch(_ context.Context, spec sandbox.ContainerSpec) (sandbox.Handle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, ContainerCall{Name: spec.Name, Args: spec.Build()})

	if len(m.responses) == 0 {
		return nil, fmt.Errorf("mock: no more responses queued")
	}

	resp := m.responses[0]
	m.responses = m.responses[1:]

	if resp.Panic {
		panic("MockSandboxBackend: simulated panic")
	}

	h := &mockSandboxHandle{
		name:     spec.Name,
		stdout:   io.NopCloser(bytes.NewReader(resp.Stdout)),
		stderr:   io.NopCloser(bytes.NewReader(resp.Stderr)),
		exitCode: 0,
		parent:   m,
	}
	if resp.Err != nil {
		h.exitCode = 1
		h.waitErr = resp.Err
	}
	return h, nil
}

// List returns an empty container list.
func (m *MockSandboxBackend) List(_ context.Context) ([]sandbox.ContainerInfo, error) {
	return nil, nil
}

// RunArgsCalls returns a copy of all recorded Launch invocations.
func (m *MockSandboxBackend) RunArgsCalls() []ContainerCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

// KillCalls returns a copy of all recorded Kill invocations.
func (m *MockSandboxBackend) KillCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.killCalls)
}

// mockSandboxHandle is the handle returned by MockSandboxBackend.Launch.
type mockSandboxHandle struct {
	name     string
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	exitCode int
	waitErr  error
	parent   *MockSandboxBackend
}

func (h *mockSandboxHandle) State() sandbox.BackendState { return sandbox.StateRunning }
func (h *mockSandboxHandle) Stdout() io.ReadCloser       { return h.stdout }
func (h *mockSandboxHandle) Stderr() io.ReadCloser       { return h.stderr }
func (h *mockSandboxHandle) Wait() (int, error)          { return h.exitCode, h.waitErr }
func (h *mockSandboxHandle) Kill() error {
	h.parent.mu.Lock()
	defer h.parent.mu.Unlock()
	h.parent.killCalls = append(h.parent.killCalls, h.name)
	return nil
}
func (h *mockSandboxHandle) Name() string { return h.name }
