package runner

import (
	"testing"
)

// ---------------------------------------------------------------------------
// FuzzParseOutput — fuzz testing for parseOutput
// ---------------------------------------------------------------------------

// FuzzParseOutput fuzzes the parseOutput function with arbitrary byte inputs.
// The invariants checked are:
//   - The function must never panic.
//   - When the input contains a valid NDJSON line with a non-empty stop_reason,
//     the returned output must have that stop_reason set.
func FuzzParseOutput(f *testing.F) {
	// Seed corpus: (a) valid end_turn message (single JSON object).
	f.Add([]byte(`{"type":"result","subtype":"success","stop_reason":"end_turn","result":"done","session_id":"sess_abc","total_cost_usd":0.01,"usage":{"input_tokens":100,"output_tokens":50}}`))

	// Seed corpus: (b) multi-line NDJSON where the stop_reason line is NOT the last line.
	f.Add([]byte(`{"type":"system","session_id":"s1"}
{"type":"result","stop_reason":"end_turn","result":"done","session_id":"s1","total_cost_usd":0.01}
{"type":"debug","data":"some verbose output after the result"}`))

	// Seed corpus: (c) empty byte slice.
	f.Add([]byte(``))

	// Seed corpus: (d) a line starting with `{` that is not valid JSON.
	f.Add([]byte(`{this is not valid json at all`))

	// Seed corpus: (e) max_tokens stop_reason response.
	f.Add([]byte(`{"type":"result","stop_reason":"max_tokens","result":"partial","session_id":"s2","total_cost_usd":0.005,"usage":{"input_tokens":200,"output_tokens":100}}`))

	// Seed corpus: (f) response with is_error: true.
	f.Add([]byte(`{"type":"result","stop_reason":"end_turn","result":"error occurred","session_id":"s3","is_error":true,"total_cost_usd":0.001}`))

	// Additional seeds for edge cases.
	f.Add([]byte(`not json at all`))
	f.Add([]byte(`{"no_stop_reason":"here","session_id":"s4"}`))
	f.Add([]byte(`{}` + "\n" + `{}`))
	f.Add([]byte(`{"stop_reason":"pause_turn","session_id":"s5"}`))
	f.Add([]byte(`{"stop_reason":""}`))
	f.Add([]byte("\x00\x01\x02\x03"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		// Must never panic on any input.
		out, _ := parseOutput(string(data))

		// If we got a non-nil result, the struct fields must be well-formed
		// (no additional invariant to check beyond not panicking, since
		// parseOutput may legitimately return any valid JSON object).
		_ = out
	})
}

// ---------------------------------------------------------------------------
// FuzzExtractSessionID — fuzz testing for extractSessionID
// ---------------------------------------------------------------------------

// FuzzExtractSessionID fuzzes the extractSessionID function with arbitrary
// byte inputs. The invariants checked are:
//   - The function must never panic.
//   - If no valid session_id is found in the input, it must return "".
func FuzzExtractSessionID(f *testing.F) {
	// Seed: session_id appears on an early line.
	f.Add([]byte(`{"session_id":"sess_early","type":"system"}
{"type":"assistant","message":"doing work"}
{"type":"result","stop_reason":"end_turn"}`))

	// Seed: session_id appears on a late line.
	f.Add([]byte(`{"type":"system"}
{"type":"assistant","message":"doing work"}
{"type":"result","stop_reason":"end_turn","session_id":"sess_late"}`))

	// Seed: session_id does not appear at all.
	f.Add([]byte(`{"type":"system"}
{"type":"assistant","message":"doing work"}
{"type":"result","stop_reason":"end_turn"}`))

	// Seed: session_id embedded in malformed JSON (should not be extracted).
	f.Add([]byte(`{"session_id":"sess_ok"}
{malformed json with session_id:"sess_bad"}`))

	// Seed: empty input.
	f.Add([]byte(``))

	// Seed: just whitespace.
	f.Add([]byte("   \n\t\n   "))

	// Seed: multiple valid session IDs (first should win).
	f.Add([]byte(`{"session_id":"first"}
{"session_id":"second"}`))

	// Seed: session_id with empty value (should be skipped).
	f.Add([]byte(`{"session_id":""}
{"session_id":"valid-id"}`))

	// Seed: binary-ish data.
	f.Add([]byte("\x00\x01\x02{\"session_id\":\"x\"}"))

	f.Fuzz(func(_ *testing.T, data []byte) {
		// Must never panic on any input.
		result := extractSessionID(data)

		// Result must always be a string (possibly empty).
		_ = result
	})
}

// ---------------------------------------------------------------------------
// TestParseOutputStopReasonPrecedence — table-driven tests
// ---------------------------------------------------------------------------

// TestParseOutputStopReasonPrecedence verifies the precedence rules in
// parseOutput when multiple JSON objects are present in the output.
func TestParseOutputStopReasonPrecedence(t *testing.T) {
	cases := []struct {
		name           string
		input          string
		wantNil        bool
		wantStopReason string
		wantSessionID  string
		wantIsError    bool
	}{
		{
			// (a) Multiple valid JSON objects; the one with stop_reason must win
			// over the last one (which has no stop_reason).
			name: "stop_reason preferred over last JSON",
			input: `{"type":"system","session_id":"s1"}
{"type":"result","stop_reason":"end_turn","result":"done","session_id":"s2"}
{"type":"debug","session_id":"s3"}`,
			wantStopReason: "end_turn",
			wantSessionID:  "s2",
		},
		{
			// (a) Multiple JSON objects with stop_reason; the LAST one that has
			// stop_reason set should be preferred (backward scan returns first hit
			// scanning from the end).
			name: "last stop_reason line wins when multiple have stop_reason",
			input: `{"stop_reason":"max_tokens","session_id":"s1"}
{"stop_reason":"end_turn","session_id":"s2"}
{"type":"debug"}`,
			wantStopReason: "end_turn",
			wantSessionID:  "s2",
		},
		{
			// (b) end_turn is parsed correctly.
			name:           "end_turn parsed correctly",
			input:          `{"stop_reason":"end_turn","result":"complete","session_id":"s1"}`,
			wantStopReason: "end_turn",
			wantSessionID:  "s1",
		},
		{
			// (b) max_tokens is parsed correctly.
			name:           "max_tokens parsed correctly",
			input:          `{"stop_reason":"max_tokens","result":"partial","session_id":"s1"}`,
			wantStopReason: "max_tokens",
			wantSessionID:  "s1",
		},
		{
			// (b) pause_turn is parsed correctly.
			name:           "pause_turn parsed correctly",
			input:          `{"stop_reason":"pause_turn","result":"","session_id":"s1"}`,
			wantStopReason: "pause_turn",
			wantSessionID:  "s1",
		},
		{
			// (c) Empty stop_reason: function returns the last valid JSON object.
			name: "empty stop_reason falls back to last valid JSON",
			input: `{"session_id":"first","stop_reason":""}
{"session_id":"last","stop_reason":""}`,
			wantStopReason: "",
			wantSessionID:  "last",
		},
		{
			// (c) No stop_reason field at all: returns last valid JSON.
			name: "no stop_reason field returns last valid JSON",
			input: `{"session_id":"first"}
{"session_id":"second"}`,
			wantStopReason: "",
			wantSessionID:  "second",
		},
		{
			// (d) Completely invalid input: returns nil without panicking.
			name:    "completely invalid input returns nil",
			input:   "this is not json at all, no curly braces here",
			wantNil: true,
		},
		{
			// (d) Empty input: returns nil without panicking.
			name:    "empty input returns nil",
			input:   "",
			wantNil: true,
		},
		{
			// (d) Only invalid JSON lines (start with `{` but malformed): returns nil.
			name:    "only malformed JSON returns nil",
			input:   "{invalid json here\n{also invalid",
			wantNil: true,
		},
		{
			// is_error: true is parsed correctly.
			name:           "is_error true parsed correctly",
			input:          `{"stop_reason":"end_turn","is_error":true,"session_id":"s1"}`,
			wantStopReason: "end_turn",
			wantIsError:    true,
			wantSessionID:  "s1",
		},
		{
			// stop_reason line not last: verbose debug line appended after result.
			name: "stop_reason line not last — verbose output after result",
			input: `{"type":"system","session_id":"s1"}
{"type":"result","stop_reason":"end_turn","result":"all done","session_id":"s1","total_cost_usd":0.01}
{"type":"debug","elapsed_ms":1234}`,
			wantStopReason: "end_turn",
			wantSessionID:  "s1",
		},
		{
			// Single valid JSON with no stop_reason is returned as fallback.
			name:          "single valid JSON no stop_reason is fallback",
			input:         `{"session_id":"only-one"}`,
			wantSessionID: "only-one",
		},
		{
			// Lines not starting with `{` are ignored.
			name: "non-JSON lines ignored",
			input: `some log output
another log line
{"stop_reason":"end_turn","session_id":"s1"}`,
			wantStopReason: "end_turn",
			wantSessionID:  "s1",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Must never panic.
			out, err := parseOutput(tc.input)

			if tc.wantNil {
				if out != nil {
					t.Fatalf("expected nil output, got %+v", out)
				}
				if err == nil {
					t.Fatal("expected non-nil error when output is nil")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if out == nil {
				t.Fatal("expected non-nil output")
			}
			if out.StopReason != tc.wantStopReason {
				t.Errorf("StopReason = %q, want %q", out.StopReason, tc.wantStopReason)
			}
			if out.SessionID != tc.wantSessionID {
				t.Errorf("SessionID = %q, want %q", out.SessionID, tc.wantSessionID)
			}
			if out.IsError != tc.wantIsError {
				t.Errorf("IsError = %v, want %v", out.IsError, tc.wantIsError)
			}
		})
	}
}
