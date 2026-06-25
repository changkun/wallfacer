package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"latere.ai/x/wallfacer/internal/harness"
	"latere.ai/x/wallfacer/internal/prompts"
	"latere.ai/x/wallfacer/internal/spec"
)

// driftDiffMaxBytes caps the diff fed to the drift agent so a huge change does
// not blow the prompt budget. The verdict is file-and-criteria level, so a
// truncated tail rarely changes the classification.
const driftDiffMaxBytes = 60000

// AssessDrift runs a one-shot agent that compares a spec against a task's
// actual changes and returns a structured drift verdict. Mirrors the
// GenerateCommitMessage one-shot pattern: a short-lived container with a
// 120-second sub-deadline, claude first with a codex fallback on a token-limit
// hit. The returned verdict's self-reported drift_level is advisory; the server
// recomputes the authoritative level via spec.ClassifyDrift.
func (r *Runner) AssessDrift(ctx context.Context, specBody string, affects, changedFiles []string, diff string) (spec.DriftVerdict, error) {
	ctx, cancel := context.WithTimeout(ctx, 120*time.Second)
	defer cancel()

	prompt := r.promptsMgr.DriftAssessment(prompts.DriftData{
		SpecBody:     specBody,
		Affects:      strings.Join(affects, "\n"),
		ChangedFiles: strings.Join(changedFiles, "\n"),
		Diff:         truncate(diff, driftDiffMaxBytes),
	})
	containerName := "wallfacer-drift-" + uuid.NewString()[:8]
	labels := map[string]string{"wallfacer.task.activity": "drift_assessment"}

	output, err := r.runCommitContainer(ctx, containerName, prompt, harness.Claude, labels)
	if err != nil {
		if isLikelyTokenLimitError(err.Error()) {
			output, err = r.runCommitContainer(ctx, containerName, prompt, harness.Codex, labels)
		}
		if err != nil {
			return spec.DriftVerdict{}, err
		}
	}
	if output == nil {
		return spec.DriftVerdict{}, fmt.Errorf("drift agent returned nil output")
	}
	if output.IsError {
		msg := strings.TrimSpace(output.Result)
		if msg == "" {
			msg = "agent returned an error result"
		}
		return spec.DriftVerdict{}, fmt.Errorf("drift agent: %s", msg)
	}
	return parseDriftVerdict(output.Result)
}

// parseDriftVerdict extracts a DriftVerdict JSON object from agent output,
// tolerating surrounding prose or a code fence the agent may emit despite the
// instruction not to.
func parseDriftVerdict(raw string) (spec.DriftVerdict, error) {
	s := strings.TrimSpace(raw)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < start {
		return spec.DriftVerdict{}, fmt.Errorf("no JSON object in drift output: %s", truncate(raw, 200))
	}
	var v spec.DriftVerdict
	if err := json.Unmarshal([]byte(s[start:end+1]), &v); err != nil {
		return spec.DriftVerdict{}, fmt.Errorf("parse drift verdict: %w", err)
	}
	return v, nil
}
