package spec

// DriftVerdict is the structured output of the drift tester: which files were
// expected versus actually changed, and how the spec's acceptance criteria
// fared. The server classifies drift from these fields; the agent's own
// drift-level opinion is advisory and not trusted, so a misbehaving agent
// cannot lock in "complete" on a divergent change.
type DriftVerdict struct {
	ExpectedFiles []string      `json:"expected_files"`
	ActualFiles   []string      `json:"actual_files"`
	Unexpected    []string      `json:"unexpected"`
	Missing       []string      `json:"missing"`
	Criteria      DriftCriteria `json:"criteria"`
	Summary       string        `json:"summary"`
}

// DriftCriteria summarizes how the spec's acceptance criteria were met. Total
// is zero when the spec has no acceptance-criteria section, which triggers the
// file-level fallback in ClassifyDrift.
type DriftCriteria struct {
	Satisfied      int    `json:"satisfied"`
	Diverged       int    `json:"diverged"`
	NotImplemented int    `json:"not_implemented"`
	Superseded     int    `json:"superseded"`
	Total          int    `json:"total"`
	Notes          string `json:"notes"`
}

// DriftLevel is the classified divergence between a spec and its
// implementation.
type DriftLevel string

// Drift level constants.
const (
	DriftMinimal     DriftLevel = "minimal"
	DriftModerate    DriftLevel = "moderate"
	DriftSignificant DriftLevel = "significant"
)

// ClassifyDrift computes the drift level from the verdict fields, server-side.
//
// With acceptance criteria present (Total > 0) it uses the satisfied ratio and
// the count of unexpected files. With no criteria (Total == 0) it falls back to
// file-level drift, classifying on unexpected/missing files only. The fallback
// matters: most of the existing corpus has no acceptance-criteria section, and
// the criteria path would otherwise read 0/0 as a perfect score and always
// report minimal, giving false confidence on a divergent change.
//
// (The spec's file_ratio term is intentionally dropped — it was computed but
// never used in the classification.)
func ClassifyDrift(v DriftVerdict) DriftLevel {
	if v.Criteria.Total == 0 {
		switch {
		case len(v.Unexpected) == 0 && len(v.Missing) == 0:
			return DriftMinimal
		case len(v.Unexpected) <= 3 && len(v.Missing) == 0:
			return DriftModerate
		default:
			return DriftSignificant
		}
	}

	satisfiedRatio := float64(v.Criteria.Satisfied) / float64(v.Criteria.Total)
	switch {
	case satisfiedRatio >= 0.9 && len(v.Unexpected) <= 1:
		return DriftMinimal
	case satisfiedRatio >= 0.7 && len(v.Unexpected) <= 3:
		return DriftModerate
	default:
		return DriftSignificant
	}
}

// DriftOutcome maps a drift level to the resulting spec status and whether to
// fan out staleness to dependents. Minimal lands complete with no cascade;
// moderate lands complete but flags dependents for review; significant marks
// the spec itself stale and cascades.
func DriftOutcome(level DriftLevel) (status Status, fanOut bool) {
	switch level {
	case DriftMinimal:
		return StatusComplete, false
	case DriftModerate:
		return StatusComplete, true
	default: // significant
		return StatusStale, true
	}
}
