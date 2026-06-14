//go:build ignore

// Package main is a test double for the pi CLI used by HostBackend
// pi-launch tests. It is not part of the normal build — tests compile it
// into t.TempDir() on demand.
//
// It scans os.Args manually rather than using the flag package, so pi's
// flags (-p, --mode, --provider, --model, --session, --tools, ...) do not
// trip an unknown-flag parse error. The prompt is pi's trailing positional
// argument (not the value of -p, which is a boolean). It emits pi-style
// --mode json output echoing the prompt and which --tools allowlist (if
// any) was passed, so tests can assert the executor forced Full permission
// (no --tools) and prepended the instructions contents into the prompt.
package main

import (
	"encoding/json"
	"fmt"
	"os"
)

func main() {
	args := os.Args[1:]
	prompt := ""
	tools := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-p", "--mode", "--provider", "--model", "--session":
			// Flags whose following token is a value to skip; --mode etc.
			// take an argument. -p is boolean but harmless to skip nothing.
			if args[i] != "-p" && i+1 < len(args) {
				i++
			}
		case "--tools":
			if i+1 < len(args) {
				tools = args[i+1]
				i++
			}
		default:
			// The last non-flag token is the positional prompt.
			prompt = args[i]
		}
	}

	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(map[string]any{
		"type":    "session",
		"version": 3,
		"id":      "fake-pi-session",
		"cwd":     ".",
	})
	// Echo the prompt and tools allowlist back inside the terminal agent_end
	// so the test can assert executor wiring. `result` is non-standard but
	// convenient for the test to read the prompt directly.
	_ = enc.Encode(map[string]any{
		"type":   "agent_end",
		"result": prompt,
		"tools":  tools,
		"messages": []map[string]any{
			{
				"role":    "assistant",
				"content": []map[string]any{{"type": "text", "text": prompt}},
				"usage": map[string]int{
					"input": 1, "output": 1, "cacheRead": 0, "cacheWrite": 0,
				},
				"stopReason": "stop",
			},
		},
	})

	if os.Getenv("FAKEPI_EXIT_1") == "1" {
		fmt.Fprintln(os.Stderr, "fakepi: forced failure")
		os.Exit(1)
	}
}
