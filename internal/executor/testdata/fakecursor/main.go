//go:build ignore

// Package main is a test double for the cursor-agent CLI used by
// HostBackend cursor-launch tests. It is not part of the normal build —
// tests compile it into t.TempDir() on demand.
//
// Unlike the shared fakeagent, it scans os.Args manually rather than using
// the flag package, so cursor's flags (--sandbox, --force, --mode, ...) do
// not trip an unknown-flag parse error. It emits cursor-style stream-json
// echoing the prompt and whether --force was present, so tests can assert
// the executor injected --force and prepended the instructions contents.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	prompt := ""
	force := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p":
			if i+1 < len(args) {
				prompt = args[i+1]
				i++
			}
		case "--force":
			force = true
		}
	}

	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(map[string]any{
		"type":       "system",
		"subtype":    "init",
		"session_id": "fake-cursor-session",
		"model":      "fake-composer",
	})
	_ = enc.Encode(map[string]any{
		"type":       "result",
		"subtype":    "success",
		"is_error":   false,
		"result":     prompt,
		"force":      force,
		"session_id": "fake-cursor-session",
		"usage": map[string]int{
			"inputTokens":      1,
			"outputTokens":     1,
			"cacheReadTokens":  0,
			"cacheWriteTokens": 0,
		},
	})

	if os.Getenv("FAKECURSOR_EXIT_1") == "1" {
		fmt.Fprintln(os.Stderr, "fakecursor: forced failure")
		os.Exit(1)
	}
}
