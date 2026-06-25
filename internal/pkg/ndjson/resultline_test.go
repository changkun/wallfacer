package ndjson

import "testing"

type rline struct {
	Type       string `json:"type"`
	StopReason string `json:"stop_reason"`
	V          int    `json:"v"`
}

func terminal(r *rline) bool { return r.StopReason != "" }

func TestPreferResultLine_PrefersTerminalOverLaterNoise(t *testing.T) {
	// The result line (stop_reason set) is followed by a verbose line. Backward
	// scan must return the result, not the trailing noise.
	raw := "" +
		`{"type":"result","stop_reason":"end_turn","v":1}` + "\n" +
		`{"type":"debug","v":2}` + "\n"
	got, ok := PreferResultLine[rline](raw, true, nil, terminal)
	if !ok || got.V != 1 {
		t.Fatalf("got %+v ok=%v, want v=1", got, ok)
	}
}

func TestPreferResultLine_FallsBackToLastValidLine(t *testing.T) {
	// No line carries a stop_reason; backward scan's fallback is the LAST line.
	raw := `{"v":1}` + "\n" + `{"v":2}` + "\n"
	got, ok := PreferResultLine[rline](raw, true, nil, terminal)
	if !ok || got.V != 2 {
		t.Fatalf("got %+v ok=%v, want v=2 (last line)", got, ok)
	}
}

func TestPreferResultLine_ForwardFallbackIsFirstLine(t *testing.T) {
	raw := `{"v":1}` + "\n" + `{"v":2}` + "\n"
	got, ok := PreferResultLine[rline](raw, false, nil, terminal)
	if !ok || got.V != 1 {
		t.Fatalf("got %+v ok=%v, want v=1 (first line, forward)", got, ok)
	}
}

func TestPreferResultLine_CandidateGateExcludesFromFallback(t *testing.T) {
	// Only type=="result" lines are candidates. A trailing non-result line must
	// not become the fallback, so the result line (v=1) wins even with no
	// stop_reason anywhere.
	isResult := func(r *rline) bool { return r.Type == "result" }
	raw := `{"type":"result","v":1}` + "\n" + `{"type":"debug","v":2}` + "\n"
	got, ok := PreferResultLine[rline](raw, true, isResult, terminal)
	if !ok || got.V != 1 {
		t.Fatalf("got %+v ok=%v, want v=1 (gated fallback)", got, ok)
	}
}

func TestPreferResultLine_SkipsNonJSONAndEmpty(t *testing.T) {
	raw := "\n  \nnot json\n" + `{"stop_reason":"end_turn","v":9}` + "\n"
	got, ok := PreferResultLine[rline](raw, true, nil, terminal)
	if !ok || got.V != 9 {
		t.Fatalf("got %+v ok=%v, want v=9", got, ok)
	}
}

func TestPreferResultLine_NoCandidates(t *testing.T) {
	if _, ok := PreferResultLine[rline]("garbage\n\n", true, nil, terminal); ok {
		t.Fatal("expected ok=false when no line decodes")
	}
}
