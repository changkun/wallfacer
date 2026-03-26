package runner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"slices"
	"sync"
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

// MockContainerExecutor is a test implementation of ContainerExecutor that
// returns pre-configured responses from a queue and records all invocations
// so tests can assert on the exact container name and args passed.
//
// When backendMock is set (by setupRunnerWithMockExecutor), RunArgsCalls and
// KillCalls delegate to the backend mock so that tests asserting on the mock
// executor automatically see calls from the backend code path.
type MockContainerExecutor struct {
	mu          sync.Mutex
	responses   []ContainerResponse
	calls       []ContainerCall
	killCalls   []string
	backendMock *MockSandboxBackend // when set, RunArgsCalls/KillCalls delegate here
}

// RunArgs pops the next response from the queue and returns it.
func (m *MockContainerExecutor) RunArgs(_ context.Context, name string, args []string) ([]byte, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, ContainerCall{Name: name, Args: args})

	if len(m.responses) == 0 {
		return nil, nil, fmt.Errorf("mock: no more responses queued")
	}

	resp := m.responses[0]
	m.responses = m.responses[1:]

	if resp.Panic {
		panic("MockContainerExecutor: simulated panic")
	}

	return resp.Stdout, resp.Stderr, resp.Err
}

// Kill records the kill invocation; it does not perform any real operation.
func (m *MockContainerExecutor) Kill(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.killCalls = append(m.killCalls, name)
}

// RunArgsCalls returns a copy of all recorded Launch/RunArgs invocations.
// When a backend mock is wired, it returns the backend mock's calls instead.
func (m *MockContainerExecutor) RunArgsCalls() []ContainerCall {
	if m.backendMock != nil {
		return m.backendMock.RunArgsCalls()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

// KillCalls returns a copy of all recorded Kill invocations.
// When a backend mock is wired, it returns the backend mock's kill calls instead.
func (m *MockContainerExecutor) KillCalls() []string {
	if m.backendMock != nil {
		return m.backendMock.KillCalls()
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.killCalls)
}

// MockSandboxBackend implements SandboxBackend for tests. It pops pre-configured
// ContainerResponse entries from a queue and records all Launch calls.
type MockSandboxBackend struct {
	mu        sync.Mutex
	responses []ContainerResponse
	calls     []ContainerCall
	killCalls []string
}

// Launch pops the next response and returns a mockSandboxHandle that yields it.
func (m *MockSandboxBackend) Launch(_ context.Context, spec ContainerSpec) (SandboxHandle, error) {
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
func (m *MockSandboxBackend) List(_ context.Context) ([]ContainerInfo, error) {
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

func (h *mockSandboxHandle) State() SandboxState   { return SandboxRunning }
func (h *mockSandboxHandle) Stdout() io.ReadCloser { return h.stdout }
func (h *mockSandboxHandle) Stderr() io.ReadCloser { return h.stderr }
func (h *mockSandboxHandle) Wait() (int, error)    { return h.exitCode, h.waitErr }
func (h *mockSandboxHandle) Kill() error {
	h.parent.mu.Lock()
	defer h.parent.mu.Unlock()
	h.parent.killCalls = append(h.parent.killCalls, h.name)
	return nil
}
func (h *mockSandboxHandle) Name() string { return h.name }
