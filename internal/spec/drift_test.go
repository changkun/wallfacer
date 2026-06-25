package spec

import "testing"

func TestClassifyDrift_WithCriteria(t *testing.T) {
	cases := []struct {
		name string
		v    DriftVerdict
		want DriftLevel
	}{
		{
			name: "all satisfied, no unexpected",
			v:    DriftVerdict{Criteria: DriftCriteria{Satisfied: 6, Total: 6}},
			want: DriftMinimal,
		},
		{
			name: "high ratio, one unexpected",
			v:    DriftVerdict{Unexpected: []string{"a.go"}, Criteria: DriftCriteria{Satisfied: 9, Total: 10}},
			want: DriftMinimal,
		},
		{
			name: "moderate ratio and unexpected",
			v:    DriftVerdict{Unexpected: []string{"a.go", "b.go"}, Criteria: DriftCriteria{Satisfied: 7, Total: 10}},
			want: DriftModerate,
		},
		{
			name: "high ratio but too many unexpected",
			v:    DriftVerdict{Unexpected: []string{"a", "b", "c"}, Criteria: DriftCriteria{Satisfied: 10, Total: 10}},
			want: DriftModerate,
		},
		{
			name: "low ratio",
			v:    DriftVerdict{Criteria: DriftCriteria{Satisfied: 3, Total: 10}},
			want: DriftSignificant,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyDrift(tc.v); got != tc.want {
				t.Errorf("ClassifyDrift = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClassifyDrift_CriteriaAbsentFallback(t *testing.T) {
	// The load-bearing case: a spec with no acceptance criteria (Total == 0)
	// and unexpected files must NOT be classified minimal.
	withUnexpected := DriftVerdict{Unexpected: []string{"surprise.go"}}
	if got := ClassifyDrift(withUnexpected); got == DriftMinimal {
		t.Errorf("criteria-less spec with unexpected files = %q, must not be minimal", got)
	}
	if got := ClassifyDrift(withUnexpected); got != DriftModerate {
		t.Errorf("one unexpected, no criteria = %q, want moderate", got)
	}

	// No criteria, no unexpected, no missing → minimal (a clean change).
	clean := DriftVerdict{ActualFiles: []string{"a.go"}, ExpectedFiles: []string{"a.go"}}
	if got := ClassifyDrift(clean); got != DriftMinimal {
		t.Errorf("clean criteria-less change = %q, want minimal", got)
	}

	// Missing expected files is a strong divergence signal → significant.
	missing := DriftVerdict{Missing: []string{"core.go"}}
	if got := ClassifyDrift(missing); got != DriftSignificant {
		t.Errorf("criteria-less spec with missing files = %q, want significant", got)
	}

	// Many unexpected files → significant even without criteria.
	many := DriftVerdict{Unexpected: []string{"a", "b", "c", "d"}}
	if got := ClassifyDrift(many); got != DriftSignificant {
		t.Errorf("four unexpected, no criteria = %q, want significant", got)
	}
}

func TestDriftOutcome(t *testing.T) {
	cases := []struct {
		level      DriftLevel
		wantStatus Status
		wantFanOut bool
	}{
		{DriftMinimal, StatusComplete, false},
		{DriftModerate, StatusComplete, true},
		{DriftSignificant, StatusStale, true},
	}
	for _, tc := range cases {
		gotStatus, gotFanOut := DriftOutcome(tc.level)
		if gotStatus != tc.wantStatus || gotFanOut != tc.wantFanOut {
			t.Errorf("DriftOutcome(%q) = (%q, %v), want (%q, %v)",
				tc.level, gotStatus, gotFanOut, tc.wantStatus, tc.wantFanOut)
		}
	}
}
