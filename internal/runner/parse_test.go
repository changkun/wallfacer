package runner

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "regenerate golden files instead of comparing")

func TestMain(m *testing.M) {
	flag.Parse()
	os.Exit(m.Run())
}

// parseGolden wraps the result of parseOutput for golden-file comparison.
// Err is omitted from JSON when empty so successful outputs stay clean.
type parseGolden struct {
	Output *agentOutput `json:"output"`
	Err    string       `json:"err,omitempty"`
}

// sessionIDGolden wraps the result of extractSessionID for golden-file
// comparison.
type sessionIDGolden struct {
	SessionID string `json:"session_id"`
}

// TestParseOutputGolden drives golden-file regression for parseOutput.
// Every testdata/*.ndjson file is treated as a captured container stdout;
// the expected result is stored in the sibling *.golden.json file.
// Run with -update to regenerate golden files after an intentional format
// change.
func TestParseOutputGolden(t *testing.T) {
	fixtures, err := filepath.Glob("testdata/*.ndjson")
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) == 0 {
		t.Fatal("no .ndjson fixture files found in testdata/")
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(strings.TrimSuffix(filepath.Base(fixture), ".ndjson"), func(t *testing.T) {
			content, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			output, parseErr := parseOutput(string(content))
			result := parseGolden{Output: output}
			if parseErr != nil {
				result.Err = parseErr.Error()
			}

			got, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}
			got = append(got, '\n')

			golden := strings.TrimSuffix(fixture, ".ndjson") + ".golden.json"

			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("updated %s", golden)
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with -update to create)", golden, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("output mismatch:\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestExtractSessionIDGolden drives golden-file regression for
// extractSessionID using the session_id_only and no_session_id fixtures.
func TestExtractSessionIDGolden(t *testing.T) {
	cases := []string{
		"testdata/session_id_only.ndjson",
		"testdata/no_session_id.ndjson",
	}

	for _, fixture := range cases {
		fixture := fixture
		t.Run(strings.TrimSuffix(filepath.Base(fixture), ".ndjson"), func(t *testing.T) {
			content, err := os.ReadFile(fixture)
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}

			result := sessionIDGolden{SessionID: extractSessionID(content)}

			got, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				t.Fatalf("marshal result: %v", err)
			}
			got = append(got, '\n')

			golden := strings.TrimSuffix(fixture, ".ndjson") + ".sessionid.golden.json"

			if *update {
				if err := os.WriteFile(golden, got, 0o644); err != nil {
					t.Fatalf("write golden: %v", err)
				}
				t.Logf("updated %s", golden)
				return
			}

			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden %s: %v (run with -update to create)", golden, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("session_id mismatch:\ngot:\n%s\nwant:\n%s", got, want)
			}
		})
	}
}

// TestParseOutputEdgeCases covers pure-string edge cases that do not need
// fixture files.
func TestParseOutputEdgeCases(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		wantResult  string
		wantSession string
		wantStop    string
		wantErr     bool
	}{
		{
			name:    "empty input returns error",
			input:   "",
			wantErr: true,
		},
		{
			name:    "whitespace-only input returns error",
			input:   "   \n\t\n  ",
			wantErr: true,
		},
		{
			name:        "single valid JSON line with no stop_reason field uses fallback",
			input:       `{"result":"some result","session_id":"sess-xyz"}`,
			wantResult:  "some result",
			wantSession: "sess-xyz",
			wantStop:    "",
		},
		{
			// A line with stop_reason explicitly set to "" must NOT be treated
			// as the canonical result line; the line with a non-empty stop_reason
			// must win.  Regression for the correctness bug fixed in 9603f10.
			name:  "empty stop_reason string is not selected over non-empty stop_reason",
			input: `{"result":"first","session_id":"s1","stop_reason":""}` + "\n" +
				`{"result":"second","session_id":"s2","stop_reason":"end_turn"}`,
			wantResult:  "second",
			wantSession: "s2",
			wantStop:    "end_turn",
		},
		{
			name: "thread id fallback is used when result line has empty session id",
			input: `{"type":"thread.started","thread_id":"thread-123"}` + "\n" +
				`{"result":"done","session_id":"","stop_reason":"end_turn","is_error":false}`,
			wantResult:  "done",
			wantSession: "thread-123",
			wantStop:    "end_turn",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOutput(tc.input)
			if tc.wantErr {
				if err == nil {
					t.Errorf("expected error, got nil (output: %+v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Result != tc.wantResult {
				t.Errorf("Result: got %q, want %q", got.Result, tc.wantResult)
			}
			if got.SessionID != tc.wantSession {
				t.Errorf("SessionID: got %q, want %q", got.SessionID, tc.wantSession)
			}
			if got.StopReason != tc.wantStop {
				t.Errorf("StopReason: got %q, want %q", got.StopReason, tc.wantStop)
			}
		})
	}
}

func TestExtractSessionID_UsesThreadIDFallback(t *testing.T) {
	raw := []byte(`{"type":"thread.started","thread_id":"thread-xyz"}` + "\n" +
		`{"result":"done","session_id":"","stop_reason":"end_turn"}`)

	if got := extractSessionID(raw); got != "thread-xyz" {
		t.Fatalf("extractSessionID() = %q, want %q", got, "thread-xyz")
	}
}
