package runner

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/harness"
)

// RunCriticRound runs a one-shot stateless agent invocation with the given
// prompt and returns the raw markdown text output. It is the runner-side
// entry point for HarnessCritic.Round calls: no task context, no session
// resumption, no span events, no usage attribution (callers aggregate
// token counts from the CriticResult returned by the adversarial engine).
//
// The deadline parameter caps total wall time; callers should set it from
// CriticInput.Deadline (default 5 minutes in the engine).
// The sb parameter selects which harness drives the critic invocation;
// pass harness.Claude for the default path.
func (r *Runner) RunCriticRound(ctx context.Context, prompt string, sb harness.ID, deadline time.Duration) (string, error) {
	if deadline > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, deadline)
		defer cancel()
	}

	containerName := "wallfacer-agon-critic-" + uuid.NewString()[:8]
	labels := map[string]string{"wallfacer.task.activity": "agon_critic"}

	output, err := r.runCommitContainer(ctx, containerName, prompt, sb, labels)
	if err != nil {
		return "", fmt.Errorf("agon critic: %w", err)
	}
	if output == nil {
		return "", fmt.Errorf("agon critic: nil output")
	}
	if output.IsError {
		msg := strings.TrimSpace(output.Result)
		if msg == "" {
			msg = "agent returned an error result"
		}
		return "", fmt.Errorf("agon critic: %s", msg)
	}
	return output.Result, nil
}
