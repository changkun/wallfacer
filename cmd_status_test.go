package main

import (
	"testing"
)

func TestGroupByStatus(t *testing.T) {
	tasks := []taskSummary{
		{ID: "a", Status: "backlog"},
		{ID: "b", Status: "in_progress"},
		{ID: "c", Status: "backlog"},
		{ID: "d", Status: "done"},
		{ID: "e", Status: "failed"},
	}
	groups := groupByStatus(tasks)

	if got := len(groups["backlog"]); got != 2 {
		t.Errorf("backlog: got %d tasks, want 2", got)
	}
	if got := len(groups["in_progress"]); got != 1 {
		t.Errorf("in_progress: got %d tasks, want 1", got)
	}
	if got := len(groups["done"]); got != 1 {
		t.Errorf("done: got %d tasks, want 1", got)
	}
	if got := len(groups["failed"]); got != 1 {
		t.Errorf("failed: got %d tasks, want 1", got)
	}
	if got := len(groups["waiting"]); got != 0 {
		t.Errorf("waiting: got %d tasks, want 0", got)
	}
}

func TestGroupByStatusEmpty(t *testing.T) {
	groups := groupByStatus(nil)
	if len(groups) != 0 {
		t.Errorf("expected empty map for nil input, got %v", groups)
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		input float64
		want  string
	}{
		{0.0023, "$0.0023"},
		{0.0, "$0.0000"},
		{1.5, "$1.5000"},
		{0.00001, "$0.0000"},
		{12.3456, "$12.3456"},
		{0.00005, "$0.0001"}, // rounding: 5 rounds up at 4th decimal
	}
	for _, tc := range tests {
		got := formatCost(tc.input)
		if got != tc.want {
			t.Errorf("formatCost(%v) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		n     int
		want  string
	}{
		{"hello", 10, "hello"},
		{"hello world", 5, "hello…"},
		{"", 5, ""},
		{"exact", 5, "exact"},
		{"toolong", 4, "tool…"},
		{"αβγδε", 3, "αβγ…"},    // multi-byte rune handling
		{"αβγ", 3, "αβγ"},       // exact rune count, no ellipsis
	}
	for _, tc := range tests {
		got := truncate(tc.input, tc.n)
		if got != tc.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tc.input, tc.n, got, tc.want)
		}
	}
}

func TestMatchContainers(t *testing.T) {
	tasks := []taskSummary{
		{ID: "uuid-1"},
		{ID: "uuid-2"},
		{ID: "uuid-3"},
	}
	containers := []containerSummary{
		{Name: "wallfacer-impl-uuid-1", TaskID: "uuid-1"},
		{Name: "wallfacer-impl-uuid-3", TaskID: "uuid-3"},
		{Name: "wallfacer-unrelated", TaskID: ""},
	}
	result := matchContainers(tasks, containers)

	if got := result["uuid-1"]; got != "wallfacer-impl-uuid-1" {
		t.Errorf("uuid-1: got %q, want %q", got, "wallfacer-impl-uuid-1")
	}
	if got := result["uuid-3"]; got != "wallfacer-impl-uuid-3" {
		t.Errorf("uuid-3: got %q, want %q", got, "wallfacer-impl-uuid-3")
	}
	if _, ok := result["uuid-2"]; ok {
		t.Errorf("uuid-2 should have no container mapping")
	}
	if _, ok := result[""]; ok {
		t.Errorf("empty task ID should not produce a mapping")
	}
}

func TestMatchContainersEmpty(t *testing.T) {
	result := matchContainers(nil, nil)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

func TestMatchContainersDuplicateTaskID(t *testing.T) {
	// Last container wins when multiple containers share a task ID.
	containers := []containerSummary{
		{Name: "first", TaskID: "uuid-1"},
		{Name: "second", TaskID: "uuid-1"},
	}
	result := matchContainers(nil, containers)
	if result["uuid-1"] != "second" {
		t.Errorf("expected last-write-wins, got %q", result["uuid-1"])
	}
}
