package runner

import (
	"testing"
)

// TestIsContainerRuntimeError verifies that the helper correctly classifies
// container runtime errors vs. normal agent exit codes.
func TestIsContainerRuntimeError(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "nil error",
			err:  nil,
			want: false,
		},
		{
			name: "exit code 125 (container engine failure)",
			err:  &fakeExitError{code: 125},
			want: true,
		},
		{
			name: "exit code 1 (Claude agent failure)",
			err:  &fakeExitError{code: 1},
			want: false,
		},
		{
			name: "exit code 0 (success — not an error in practice)",
			err:  &fakeExitError{code: 0},
			want: false,
		},
		{
			name: "exit code 2 (normal non-zero task exit)",
			err:  &fakeExitError{code: 2},
			want: false,
		},
		{
			name: "connection refused (daemon down)",
			err:  fakeError("dial tcp: connect: connection refused"),
			want: true,
		},
		{
			name: "no such file or directory (binary missing)",
			err:  fakeError("fork/exec /opt/podman/bin/podman: no such file or directory"),
			want: true,
		},
		{
			name: "token limit error (not a runtime error)",
			err:  fakeError("exceeded token limit"),
			want: false,
		},
		{
			name: "random exec error",
			err:  fakeError("permission denied"),
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := isContainerRuntimeError(tc.err)
			if got != tc.want {
				t.Errorf("isContainerRuntimeError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// fakeExitError implements the exitCoder interface used by isContainerRuntimeError.
type fakeExitError struct{ code int }

func (e *fakeExitError) ExitCode() int { return e.code }
func (e *fakeExitError) Error() string { return "exit status " + string(rune('0'+e.code)) }

// fakeError is a plain error type for testing non-exit errors.
type fakeError string

func (e fakeError) Error() string { return string(e) }
