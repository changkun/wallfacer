package handler

import (
	"strings"
	"testing"
)

// testSpecArchived mirrors testSpecValidated but with status=archived so
// the planning-system-prompt selector can distinguish active from
// archived specs without pulling in the spec-transition flow.
const testSpecArchived = `---
title: Archived Spec
status: archived
depends_on: []
affects: []
effort: small
created: 2026-01-01
updated: 2026-01-01
author: test
dispatched_task_id: null
---

# Archived Spec
`

func TestSelectPlanningSystemPrompt_EmptyTree(t *testing.T) {
	// newTestHandlerWithWorkspaces returns an empty tempdir workspace —
	// no specs/ directory exists. BuildTree fails and the selector
	// falls through to the empty variant.
	_, ws := newTestHandlerWithWorkspaces(t)

	got := selectPlanningSystemPrompt([]string{ws})
	if !strings.Contains(got, "/spec-new") {
		t.Errorf("empty-tree prompt missing /spec-new hint; got: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "clean slate") {
		t.Errorf("empty-tree prompt missing 'clean slate' phrase; got: %q", got)
	}
}

func TestSelectPlanningSystemPrompt_NonEmptyTree(t *testing.T) {
	_, ws := newTestHandlerWithWorkspaces(t)
	writeTestSpec(t, ws, "specs/local/one.md", testSpecValidated)

	got := selectPlanningSystemPrompt([]string{ws})
	if !strings.Contains(got, "existing") {
		t.Errorf("non-empty prompt should mention 'existing'; got: %q", got)
	}
	// Non-empty variant must NOT use the clean-slate framing.
	if strings.Contains(strings.ToLower(got), "clean slate") {
		t.Errorf("non-empty prompt should not include 'clean slate'; got: %q", got)
	}
}

// TestSelectPlanningSystemPrompt_IgnoresArchived ensures archived specs
// don't count toward the non-empty condition. A workspace containing
// only archived specs should still activate the empty-tree prompt,
// matching the chat-first-mode spec's definition of "effectively empty".
func TestSelectPlanningSystemPrompt_IgnoresArchived(t *testing.T) {
	_, ws := newTestHandlerWithWorkspaces(t)
	writeTestSpec(t, ws, "specs/local/a.md", testSpecArchived)
	writeTestSpec(t, ws, "specs/local/b.md", testSpecArchived)

	got := selectPlanningSystemPrompt([]string{ws})
	if !strings.Contains(strings.ToLower(got), "clean slate") {
		t.Errorf("archived-only tree should select empty variant; got: %q", got)
	}
}

// TestSelectPlanningSystemPrompt_MixedWorkspaces ensures a single
// non-archived spec in any mounted workspace flips the selector from
// empty to non-empty. The check is per-call, not cached.
func TestSelectPlanningSystemPrompt_MixedWorkspaces(t *testing.T) {
	_, wsEmpty := newTestHandlerWithWorkspaces(t)
	_, wsActive := newTestHandlerWithWorkspaces(t)
	writeTestSpec(t, wsActive, "specs/local/live.md", testSpecValidated)

	got := selectPlanningSystemPrompt([]string{wsEmpty, wsActive})
	if strings.Contains(strings.ToLower(got), "clean slate") {
		t.Errorf("one active spec across any workspace should pick non-empty; got: %q", got)
	}

	// Reverse order — selection is still per-call, not workspace-order
	// dependent, so the same non-empty variant should win.
	got2 := selectPlanningSystemPrompt([]string{wsActive, wsEmpty})
	if got != got2 {
		t.Errorf("selection should be order-insensitive; got %q vs %q", got, got2)
	}
}
