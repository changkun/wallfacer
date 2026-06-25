package runner

import "testing"

func TestParseDriftVerdict(t *testing.T) {
	t.Run("plain json", func(t *testing.T) {
		raw := `{"unexpected":["a.go"],"criteria":{"satisfied":5,"total":6},"drift_level":"moderate","summary":"ok"}`
		v, err := parseDriftVerdict(raw)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(v.Unexpected) != 1 || v.Unexpected[0] != "a.go" {
			t.Errorf("unexpected = %v", v.Unexpected)
		}
		if v.Criteria.Satisfied != 5 || v.Criteria.Total != 6 {
			t.Errorf("criteria = %+v", v.Criteria)
		}
	})

	t.Run("fenced json with prose", func(t *testing.T) {
		raw := "Here is the verdict:\n```json\n{\"missing\":[\"core.go\"],\"summary\":\"diverged\"}\n```\nThanks!"
		v, err := parseDriftVerdict(raw)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if len(v.Missing) != 1 || v.Missing[0] != "core.go" {
			t.Errorf("missing = %v", v.Missing)
		}
	})

	t.Run("surrounding prose no fence", func(t *testing.T) {
		raw := `The result is {"summary":"clean"} done.`
		v, err := parseDriftVerdict(raw)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if v.Summary != "clean" {
			t.Errorf("summary = %q", v.Summary)
		}
	})

	t.Run("no json", func(t *testing.T) {
		if _, err := parseDriftVerdict("sorry, I cannot help"); err == nil {
			t.Error("expected error for output with no JSON object")
		}
	})
}
