package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"changkun.de/x/wallfacer/internal/pkg/atomicfile"
	"changkun.de/x/wallfacer/internal/pkg/set"
)

// Group represents a named set of workspace paths.
type Group struct {
	Name       string   `json:"name,omitempty"`
	Workspaces []string `json:"workspaces"`
	// MaxParallel, when non-nil, overrides WALLFACER_MAX_PARALLEL for this
	// group only. A value of 0 means "unlimited"; negative values are
	// normalized to nil (inherit the env-file default). Pointer so that an
	// absent field in on-disk JSON deserializes to nil rather than 0.
	MaxParallel *int `json:"max_parallel,omitempty"`
	// MaxTestParallel does the same for WALLFACER_MAX_TEST_PARALLEL.
	MaxTestParallel *int `json:"max_test_parallel,omitempty"`

	// Automation toggles are per-group so that switching workspaces does
	// not carry an "autopilot on" state into a group the user expected to
	// operate manually. Pointers so that absent fields in on-disk JSON
	// deserialize to nil (meaning "unset, default off"), distinguishable
	// from an explicit false the user saved. Autopush is intentionally
	// NOT per-group: push credentials / remote setup are global, so the
	// auto-push flag continues to live in the env file.
	Autopilot  *bool `json:"autopilot,omitempty"`
	Autotest   *bool `json:"autotest,omitempty"`
	Autosubmit *bool `json:"autosubmit,omitempty"`
	Autosync   *bool `json:"autosync,omitempty"`

	// CreatedBy records the principal sub of the user who first owned
	// this group in cloud mode. Empty on groups created pre-cloud or in
	// local mode. Mirrors store.Task.CreatedBy semantics.
	CreatedBy string `json:"created_by,omitempty"`
	// OrgID records the org context the group is scoped to. Empty when
	// the group is personal (CreatedBy!="") or legacy (CreatedBy==""
	// and OrgID==""). Mirrors store.Task.OrgID semantics.
	OrgID string `json:"org_id,omitempty"`
}

// groupsFilePath returns the path to the workspace-groups.json file within configDir.
func groupsFilePath(configDir string) string {
	return filepath.Join(configDir, "workspace-groups.json")
}

// LoadGroups reads workspace groups from the config directory.
func LoadGroups(configDir string) ([]Group, error) {
	raw, err := os.ReadFile(groupsFilePath(configDir))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var groups []Group
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	return NormalizeGroups(groups), nil
}

// SaveGroups writes workspace groups to the config directory atomically.
func SaveGroups(configDir string, groups []Group) error {
	path := groupsFilePath(configDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.WriteJSON(path, NormalizeGroups(groups), 0o644)
}

// UpsertGroup adds or promotes a workspace group to the front of the list,
// implementing most-recently-used (MRU) ordering for session restore.
// If the group already exists (matching by GroupKey), it is moved to position 0
// with its name preserved. If it does not exist, a new unnamed group is prepended.
func UpsertGroup(configDir string, workspaces []string) error {
	workspaces = normalizeGroupPaths(workspaces)
	if len(workspaces) == 0 {
		return nil
	}
	groups, err := LoadGroups(configDir)
	if err != nil {
		return err
	}
	key := GroupKey(workspaces)
	for i, group := range groups {
		if GroupKey(group.Workspaces) == key {
			if i == 0 {
				return nil
			}
			promoted := Group{
				Name:            group.Name,
				Workspaces:      workspaces,
				MaxParallel:     group.MaxParallel,
				MaxTestParallel: group.MaxTestParallel,
				Autopilot:       group.Autopilot,
				Autotest:        group.Autotest,
				Autosubmit:      group.Autosubmit,
				Autosync:        group.Autosync,
				CreatedBy:       group.CreatedBy,
				OrgID:           group.OrgID,
			}
			reordered := append([]Group{promoted}, groups[:i]...)
			reordered = append(reordered, groups[i+1:]...)
			return SaveGroups(configDir, reordered)
		}
	}
	groups = append([]Group{{Workspaces: workspaces}}, groups...)
	return SaveGroups(configDir, groups)
}

// NormalizeGroups deduplicates and cleans workspace groups.
func NormalizeGroups(groups []Group) []Group {
	if len(groups) == 0 {
		return nil
	}
	out := make([]Group, 0, len(groups))
	seen := set.New[string]()
	for _, group := range groups {
		ws := normalizeGroupPaths(group.Workspaces)
		if len(ws) == 0 {
			continue
		}
		key := GroupKey(ws)
		if seen.Has(key) {
			continue
		}
		seen.Add(key)
		out = append(out, Group{
			Name:            group.Name,
			Workspaces:      ws,
			MaxParallel:     sanitizeLimit(group.MaxParallel),
			MaxTestParallel: sanitizeLimit(group.MaxTestParallel),
			Autopilot:       group.Autopilot,
			Autotest:        group.Autotest,
			Autosubmit:      group.Autosubmit,
			Autosync:        group.Autosync,
			CreatedBy:       group.CreatedBy,
			OrgID:           group.OrgID,
		})
	}
	return out
}

// Principal is the minimal identity surface for group visibility.
// Matches store.Principal in shape but kept local so the workspace
// package doesn't import the store layer.
type Principal struct {
	Sub   string
	OrgID string
}

// ClaimGroup stamps the current principal onto the group matching
// `workspaces` if the group has no owner yet (CreatedBy=="" AND
// OrgID==""). Existing stamps are never overwritten: a group
// originally claimed by user A stays tagged to A even if user B
// later switches to the same workspace set. Idempotent.
//
// Called from the PUT /api/workspaces handler after a successful
// Switch, so groups created in cloud mode are attributed from the
// moment they're persisted. Groups created in local mode or by
// startup restore remain unclaimed (legacy) until a signed-in
// session first opens them.
func ClaimGroup(configDir string, workspaces []string, p *Principal) error {
	if p == nil || (p.Sub == "" && p.OrgID == "") {
		return nil
	}
	workspaces = normalizeGroupPaths(workspaces)
	if len(workspaces) == 0 {
		return nil
	}
	groups, err := LoadGroups(configDir)
	if err != nil {
		return err
	}
	key := GroupKey(workspaces)
	changed := false
	for i, g := range groups {
		if GroupKey(g.Workspaces) != key {
			continue
		}
		if g.CreatedBy == "" && g.OrgID == "" {
			groups[i].CreatedBy = p.Sub
			groups[i].OrgID = p.OrgID
			changed = true
		}
		break
	}
	if !changed {
		return nil
	}
	return SaveGroups(configDir, groups)
}

// GroupsForPrincipal returns the subset of `groups` that `p` can
// see. Mirrors store.TasksForPrincipal's strict isolation:
//
//	┌─────────────────────────────┬──────────────────────────────────┐
//	│ Caller view                 │ Sees                             │
//	├─────────────────────────────┼──────────────────────────────────┤
//	│ local (nil principal)       │ every group                      │
//	│ personal (OrgID == "")      │ own personal + legacy no-owner   │
//	│ org X (OrgID == "X")        │ only OrgID=="X" groups (strict)  │
//	└─────────────────────────────┴──────────────────────────────────┘
//
// The org view does NOT include personal or legacy groups — those
// belong to the user's personal workspace, not the org. Switching
// into an org should feel like a clean workspace, not a merged
// view of private + org data.
func GroupsForPrincipal(groups []Group, p *Principal) []Group {
	if p == nil {
		return groups
	}
	out := make([]Group, 0, len(groups))
	for _, g := range groups {
		if p.OrgID != "" {
			if g.OrgID == p.OrgID {
				out = append(out, g)
			}
			continue
		}
		// Personal view.
		if g.OrgID != "" {
			continue
		}
		if g.CreatedBy == "" || g.CreatedBy == p.Sub {
			out = append(out, g)
		}
	}
	return out
}

// sanitizeLimit drops negative override values so downstream callers only
// have to distinguish "nil (inherit default)" from "non-negative override".
func sanitizeLimit(v *int) *int {
	if v == nil {
		return nil
	}
	if *v < 0 {
		return nil
	}
	n := *v
	return &n
}

// normalizeGroupPaths deduplicates, trims whitespace, cleans, and sorts paths.
// Returns nil for empty input.
func normalizeGroupPaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := set.New[string]()
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if seen.Has(path) {
			continue
		}
		seen.Add(path)
		out = append(out, path)
	}
	slices.Sort(out)
	return out
}

// GroupKey returns a canonical, deterministic key for a set of workspace paths.
// Paths must be pre-sorted (as done by normalizeGroupPaths) so that the same
// set of workspaces always produces the same key regardless of input order.
// The key uses newline as separator because it cannot appear in filesystem paths.
func GroupKey(paths []string) string {
	return strings.Join(paths, "\n")
}
