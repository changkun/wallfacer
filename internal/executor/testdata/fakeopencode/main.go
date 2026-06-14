//go:build ignore

// Package main is a test double for the opencode CLI used by HostBackend
// opencode-launch tests. It is not part of the normal build — tests compile
// it into t.TempDir() on demand.
//
// It scans os.Args manually (opencode's flags would trip the flag package),
// emits opencode-style NDJSON events (step_start, text, step_finish) echoing
// the prompt and whether --dangerously-skip-permissions was present, and —
// like the real opencode — emits NO terminal result event. The launcher's tee
// is responsible for synthesizing the final result line.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	// opencode's prompt is the last positional argument.
	prompt := ""
	if len(args) > 0 {
		prompt = args[len(args)-1]
	}
	skip := false
	for _, a := range args {
		if a == "--dangerously-skip-permissions" {
			skip = true
		}
	}

	enc := json.NewEncoder(os.Stdout)

	// Schema-drift / unrecognized-output mode: emit only events the launcher
	// does not recognise, so the synthesis must mark the result an error.
	if os.Getenv("FAKEOPENCODE_GARBAGE") == "1" {
		_ = enc.Encode(map[string]any{
			"type":      "file_edited",
			"sessionID": "fake-opencode-session",
			"properties": map[string]any{"file": "x.go"},
		})
		return
	}

	text := prompt
	if skip {
		text = "[skip-permissions] " + prompt
	}

	_ = enc.Encode(map[string]any{
		"type":      "step_start",
		"sessionID": "fake-opencode-session",
		"part":      map[string]any{"type": "step-start"},
	})
	_ = enc.Encode(map[string]any{
		"type":      "text",
		"sessionID": "fake-opencode-session",
		"part":      map[string]any{"type": "text", "text": text},
	})
	_ = enc.Encode(map[string]any{
		"type":      "step_finish",
		"sessionID": "fake-opencode-session",
		"part": map[string]any{
			"type":   "step-finish",
			"reason": "stop",
			"cost":   0.002,
			"tokens": map[string]any{
				"input":     11,
				"output":    7,
				"reasoning": 0,
				"cache":     map[string]any{"read": 3, "write": 1},
			},
		},
	})

	if os.Getenv("FAKEOPENCODE_EXIT_1") == "1" {
		fmt.Fprintln(os.Stderr, "fakeopencode: forced failure")
		os.Exit(1)
	}
}
