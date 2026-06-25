package spec

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestDate_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		input    string
		wantYear int
		wantMon  int
		wantDay  int
	}{
		{"2026-01-15", 2026, 1, 15},
		{"2025-12-31", 2025, 12, 31},
		{"2000-02-29", 2000, 2, 29},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			var d Date
			node := &yaml.Node{Kind: yaml.ScalarNode, Value: tc.input}
			if err := d.UnmarshalYAML(node); err != nil {
				t.Fatalf("UnmarshalYAML(%q): %v", tc.input, err)
			}
			if d.Year() != tc.wantYear || int(d.Month()) != tc.wantMon || d.Day() != tc.wantDay {
				t.Errorf("got %v, want %d-%02d-%02d", d.Time, tc.wantYear, tc.wantMon, tc.wantDay)
			}
		})
	}
}

func TestDate_InvalidFormat(t *testing.T) {
	invalid := []string{
		"not-a-date",
		"2026/01/15",
		"01-15-2026",
		"2026-1-5",
		"",
	}
	for _, input := range invalid {
		t.Run(input, func(t *testing.T) {
			var d Date
			node := &yaml.Node{Kind: yaml.ScalarNode, Value: input}
			if err := d.UnmarshalYAML(node); err == nil {
				t.Errorf("UnmarshalYAML(%q): expected error, got nil", input)
			}
		})
	}
}

func TestDate_NonScalarNode(t *testing.T) {
	var d Date
	node := &yaml.Node{Kind: yaml.MappingNode}
	if err := d.UnmarshalYAML(node); err == nil {
		t.Error("expected error for non-scalar node")
	}
}

func TestParse_DriftPipelineFields(t *testing.T) {
	content := `---
title: Test
status: testing
effort: small
created: 2026-01-01
updated: 2026-01-02
author: test
implementation_commit: 3a2b1c4..5f6e7d8
testing_pending: "tester failed: timeout"
---

# Body
`
	s, err := ParseBytes([]byte(content), "specs/x.md")
	if err != nil {
		t.Fatalf("ParseBytes: %v", err)
	}
	if s.ImplementationCommit == nil || *s.ImplementationCommit != "3a2b1c4..5f6e7d8" {
		t.Errorf("ImplementationCommit = %v, want 3a2b1c4..5f6e7d8", s.ImplementationCommit)
	}
	if s.TestingPending == nil || *s.TestingPending != "tester failed: timeout" {
		t.Errorf("TestingPending = %v, want the failure reason", s.TestingPending)
	}
	// Absent fields stay nil.
	s2, _ := ParseBytes([]byte("---\ntitle: T\nstatus: drafted\neffort: small\ncreated: 2026-01-01\nupdated: 2026-01-01\nauthor: t\n---\n\n# B\n"), "specs/y.md")
	if s2.ImplementationCommit != nil || s2.TestingPending != nil {
		t.Errorf("absent drift fields should be nil, got %v %v", s2.ImplementationCommit, s2.TestingPending)
	}
}
