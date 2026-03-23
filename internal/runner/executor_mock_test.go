package runner

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// ContainerResponse holds the pre-configured response for a single RunArgs invocation.
type ContainerResponse struct {
	Stdout []byte
	Stderr []byte
	Err    error
	// Panic, when true, causes RunArgs to panic instead of returning a response.
	Panic bool
}

// ContainerCall records a single RunArgs invocation for later assertion.
type ContainerCall struct {
	Name string
	Args []string
}

// MockContainerExecutor is a test implementation of ContainerExecutor that
// returns pre-configured responses from a queue and records all invocations
// so tests can assert on the exact container name and args passed.
type MockContainerExecutor struct {
	mu        sync.Mutex
	responses []ContainerResponse
	calls     []ContainerCall
	killCalls []string
}

// RunArgs pops the next response from the queue and returns it.
// It panics when ContainerResponse.Panic is true, records every call, and
// returns an error if the response queue is exhausted.
func (m *MockContainerExecutor) RunArgs(_ context.Context, name string, args []string) ([]byte, []byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.calls = append(m.calls, ContainerCall{Name: name, Args: args})

	if len(m.responses) == 0 {
		// Return empty output with a non-zero exit-like error to avoid
		// infinite loops in callers that auto-continue on certain stop reasons.
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

// RunArgsCalls returns a copy of all recorded RunArgs invocations.
func (m *MockContainerExecutor) RunArgsCalls() []ContainerCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.calls)
}

// KillCalls returns a copy of all recorded Kill invocations.
func (m *MockContainerExecutor) KillCalls() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.killCalls)
}
