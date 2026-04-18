//go:build ignore

// Package main is a test double for the claude / codex CLIs used by
// HostBackend tests. It is intentionally minimal and not part of the
// normal build — tests compile it into t.TempDir() on demand.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

func main() {
	// Support --help probing for the --append-system-prompt flag.
	if len(os.Args) >= 2 && os.Args[1] == "--help" {
		helpText := "fakeagent\n  -p string   prompt\n  --model string\n  --resume string\n"
		if os.Getenv("FAKEAGENT_NO_APPEND") != "1" {
			helpText += "  --append-system-prompt string   path to a file whose content is appended to the system prompt\n"
		}
		fmt.Print(helpText)
		return
	}

	fs := flag.NewFlagSet("fakeagent", flag.ContinueOnError)
	prompt := fs.String("p", "", "prompt")
	model := fs.String("model", "", "model")
	resume := fs.String("resume", "", "resume session id")
	appendSys := fs.String("append-system-prompt", "", "append system prompt file")
	verbose := fs.Bool("verbose", false, "verbose")
	outputFormat := fs.String("output-format", "", "output format")
	// Ignore unknown flags quietly so real CLI args pass through without fuss.
	fs.SetOutput(os.Stderr)
	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "fakeagent parse:", err)
		os.Exit(2)
	}
	_ = verbose
	_ = outputFormat

	// Optional sleep so Kill tests can catch a running process.
	if s := os.Getenv("FAKEAGENT_SLEEP"); s != "" {
		if secs, err := strconv.Atoi(s); err == nil && secs > 0 {
			time.Sleep(time.Duration(secs) * time.Second)
		}
	}

	cwd, _ := os.Getwd()

	// Emit two NDJSON lines: an init-style record and a final result.
	init := map[string]any{
		"type":     "system",
		"subtype":  "init",
		"cwd":      cwd,
		"agent":    os.Getenv("WALLFACER_AGENT"),
		"resume":   *resume,
		"model":    *model,
		"append":   *appendSys,
		"prompt":   *prompt,
		"env_echo": envEcho(),
	}
	enc := json.NewEncoder(os.Stdout)
	_ = enc.Encode(init)

	final := map[string]any{
		"type":           "result",
		"subtype":        "success",
		"result":         "fake",
		"session_id":     "fake-session",
		"stop_reason":    "end_turn",
		"is_error":       false,
		"total_cost_usd": 0.0,
		"usage": map[string]int{
			"input_tokens":                0,
			"output_tokens":               0,
			"cache_read_input_tokens":     0,
			"cache_creation_input_tokens": 0,
		},
	}
	_ = enc.Encode(final)

	if os.Getenv("FAKEAGENT_EXIT_1") == "1" {
		os.Exit(1)
	}
}

// envEcho returns a subset of env vars the tests care about, so they can
// assert env-file merge / spec.Env overlay without dumping the full parent env.
func envEcho() map[string]string {
	keys := []string{"FAKEAGENT_A", "FAKEAGENT_B", "FAKEAGENT_C", "WALLFACER_AGENT"}
	out := make(map[string]string, len(keys))
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			out[k] = v
		}
	}
	return out
}

// Silence unused-import complaints when stripping flags in future edits.
var _ = strings.TrimSpace
