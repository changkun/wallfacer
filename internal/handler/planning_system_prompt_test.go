package handler

import (
	"strings"
	"testing"

	"changkun.de/x/wallfacer/internal/prompts"
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

// TestAssemblePlanningPrompt_PrependsEmptyVariant verifies that a turn
// against an empty spec tree gets the empty-variant planning prompt
// stitched on top of the user message.
func TestAssemblePlanningPrompt_PrependsEmptyVariant(t *testing.T) {
	_, ws := newTestHandlerWithWorkspaces(t)
	base := "USER MESSAGE"

	got := assemblePlanningPrompt([]string{ws}, "", base)

	if !strings.HasSuffix(got, base) {
		t.Errorf("user message must remain as the suffix; got: %q", got)
	}
	if !strings.Contains(got, "/spec-new") {
		t.Errorf("empty-variant prompt must mention /spec-new; got: %q", got)
	}
	// No focused-archived spec → archivedSpecGuard contributes nothing,
	// so the prompt is just [planning_system_empty][\n\n][base].
	if !strings.HasPrefix(got, prompts.PlanningSystemEmpty()) {
		t.Errorf("planning_system_empty must wrap the turn from the outside; got prefix: %q", got[:min(120, len(got))])
	}
}

// TestAssemblePlanningPrompt_PrependsNonemptyVariant verifies that the
// presence of a non-archived spec flips the prompt to the non-empty
// variant — still wrapping the user base.
func TestAssemblePlanningPrompt_PrependsNonemptyVariant(t *testing.T) {
	_, ws := newTestHandlerWithWorkspaces(t)
	writeTestSpec(t, ws, "specs/local/live.md", testSpecValidated)
	base := "USER MESSAGE"

	got := assemblePlanningPrompt([]string{ws}, "", base)

	if !strings.HasSuffix(got, base) {
		t.Errorf("user message must remain as the suffix; got: %q", got)
	}
	if !strings.HasPrefix(got, prompts.PlanningSystemNonempty()) {
		t.Errorf("planning_system_nonempty must wrap the turn from the outside; got prefix: %q", got[:min(120, len(got))])
	}
	if strings.Contains(strings.ToLower(got), "clean slate") {
		t.Errorf("non-empty prompt must not include 'clean slate'; got: %q", got)
	}
}

// TestAssemblePlanningPrompt_GuardSitsBetween verifies the layered
// ordering when both layers contribute:
//
//	[planning_system_*][archivedSpecGuard][user prompt]
//
// The guard rail must be closer to the base than the system prompt is.
func TestAssemblePlanningPrompt_GuardSitsBetween(t *testing.T) {
	_, ws := newTestHandlerWithWorkspaces(t)
	// Drop one archived spec so the focused-spec guard fires AND the
	// tree counts as "effectively empty" (archived specs don't count).
	writeTestSpec(t, ws, "specs/local/dead.md", testSpecArchived)
	base := "USER MESSAGE"

	got := assemblePlanningPrompt([]string{ws}, "specs/local/dead.md", base)

	systemIdx := strings.Index(got, prompts.PlanningSystemEmpty())
	guardIdx := strings.Index(got, "This spec is archived")
	baseIdx := strings.Index(got, base)
	if systemIdx == -1 {
		t.Fatalf("planning_system_empty missing; got: %q", got)
	}
	if guardIdx == -1 {
		t.Fatalf("archivedSpecGuard missing; got: %q", got)
	}
	if baseIdx == -1 {
		t.Fatalf("base message missing; got: %q", got)
	}
	if systemIdx >= guardIdx || guardIdx >= baseIdx {
		t.Errorf("layer order must be system < guard < base; got system=%d guard=%d base=%d",
			systemIdx, guardIdx, baseIdx)
	}
}
