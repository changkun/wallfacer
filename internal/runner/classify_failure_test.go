package runner

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"changkun.de/x/wallfacer/internal/store"
)

func TestClassifyFailure(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		isError  bool
		result   string
		expected store.FailureCategory
	}{
		{
			name:     "timeout via DeadlineExceeded",
			err:      context.DeadlineExceeded,
			isError:  false,
			result:   "",
			expected: store.FailureCategoryTimeout,
		},
		{
			name:     "timeout via wrapped DeadlineExceeded",
			err:      fmt.Errorf("container exited: %w", context.DeadlineExceeded),
			isError:  false,
			result:   "",
			expected: store.FailureCategoryTimeout,
		},
		{
			name:     "budget exceeded from result text",
			err:      nil,
			isError:  false,
			result:   "cost budget exceeded: $1.0000 of $0.5000",
			expected: store.FailureCategoryBudget,
		},
		{
			name:     "agent error via isError flag",
			err:      nil,
			isError:  true,
			result:   "",
			expected: store.FailureCategoryAgentError,
		},
		{
			name:     "container crash via exit status",
			err:      errors.New("exit status 1"),
			isError:  false,
			result:   "",
			expected: store.FailureCategoryContainerCrash,
		},
		{
			name:     "container crash via empty output",
			err:      errors.New("empty output from container"),
			isError:  false,
			result:   "",
			expected: store.FailureCategoryContainerCrash,
		},
		{
			name:     "unknown for unrecognised error",
			err:      errors.New("some unexpected error"),
			isError:  false,
			result:   "",
			expected: store.FailureCategoryUnknown,
		},
		{
			name:     "unknown when no signals present",
			err:      nil,
			isError:  false,
			result:   "",
			expected: store.FailureCategoryUnknown,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyFailure(tc.err, tc.isError, tc.result)
			if got != tc.expected {
				t.Errorf("classifyFailure(err=%v, isError=%v, result=%q) = %q, want %q",
					tc.err, tc.isError, tc.result, got, tc.expected)
			}
		})
	}
}
