// Package spec provides types and parsing for spec document frontmatter.
package spec

import (
	"fmt"
	"time"

	"gopkg.in/yaml.v3"
)

// Status represents the lifecycle state of a spec document.
type Status string

// Status constants for the spec lifecycle state machine.
const (
	StatusVague     Status = "vague"     // initial idea; design incomplete
	StatusDrafted   Status = "drafted"   // enough detail for review
	StatusValidated Status = "validated" // reviewed, approved, ready to execute
	StatusComplete  Status = "complete"  // all work done
	StatusStale     Status = "stale"     // no longer matches reality
)

// Effort is a rough size estimate for a spec's implementation work.
type Effort string

// Effort constants for spec sizing.
const (
	EffortSmall  Effort = "small"  // ~50-100 lines changed
	EffortMedium Effort = "medium" // ~100-300 lines changed
	EffortLarge  Effort = "large"  // ~300+ lines changed
	EffortXLarge Effort = "xlarge" // multi-file refactor or complex feature
)

// Date wraps time.Time for YAML frontmatter dates in YYYY-MM-DD format.
type Date struct {
	time.Time
}

// UnmarshalYAML parses a YYYY-MM-DD date string from YAML.
func (d *Date) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("expected scalar for date, got %v", value.Kind)
	}
	t, err := time.Parse(time.DateOnly, value.Value)
	if err != nil {
		return fmt.Errorf("invalid date %q: expected YYYY-MM-DD format", value.Value)
	}
	d.Time = t
	return nil
}

// MarshalJSON outputs a Date as a "YYYY-MM-DD" JSON string.
func (d Date) MarshalJSON() ([]byte, error) {
	if d.IsZero() {
		return []byte(`""`), nil
	}
	return []byte(`"` + d.Format(time.DateOnly) + `"`), nil
}

// Spec represents a parsed spec document with its YAML frontmatter and body.
type Spec struct {
	Title            string   `yaml:"title" json:"title"`
	Status           Status   `yaml:"status" json:"status"`
	DependsOn        []string `yaml:"depends_on" json:"depends_on"`
	Affects          []string `yaml:"affects" json:"affects"`
	Effort           Effort   `yaml:"effort" json:"effort"`
	Created          Date     `yaml:"created" json:"created"`
	Updated          Date     `yaml:"updated" json:"updated"`
	Author           string   `yaml:"author" json:"author"`
	DispatchedTaskID *string  `yaml:"dispatched_task_id" json:"dispatched_task_id"`

	// Derived fields (not from YAML).
	Path  string `yaml:"-" json:"path"`
	Track string `yaml:"-" json:"track"`
	Body  string `yaml:"-" json:"-"` // excluded from API responses (large)
}
