package workspace

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"latere.ai/x/wallfacer/internal/pkg/atomicfile"
	"latere.ai/x/wallfacer/internal/pkg/set"
)

// Workspace is an owned, stably-identified set of folder paths. Its identity
// (ID) and its storage handle (DataKey) are independent of its membership
// (Folders), so the owner may change folders without orphaning history.
//
// ID and DataKey are empty on records loaded from the legacy
// workspace-groups.json (which keyed identity off the path set). Migration
// (see migrate.go) assigns them once and writes workspaces.json; until then
// the manager derives the scoped-data key from the folder paths exactly as
// before, so legacy records keep working.
type Workspace struct {
	ID      string   `json:"id,omitempty"`       // stable UUIDv4, assigned once at creation/migration
	Name    string   `json:"name,omitempty"`     // optional human label
	Folders []string `json:"folders"`            // mutable; absolute, clean, sorted, deduped
	DataKey string   `json:"data_key,omitempty"` // stable storage handle under data/<DataKey>

	// MaxParallel, when non-nil, overrides WALLFACER_MAX_PARALLEL for this
	// workspace only. A value of 0 means "unlimited"; negative values are
	// normalized to nil (inherit the env-file default). Pointer so that an
	// absent field in on-disk JSON deserializes to nil rather than 0.
	MaxParallel *int `json:"max_parallel,omitempty"`
	// MaxTestParallel does the same for WALLFACER_MAX_TEST_PARALLEL.
	MaxTestParallel *int `json:"max_test_parallel,omitempty"`

	// Automation toggles are per-workspace so that switching workspaces does
	// not carry an "autoimplement on" state into a workspace the user expected
	// to operate manually. Pointers so that absent fields in on-disk JSON
	// deserialize to nil (meaning "unset, default off"), distinguishable
	// from an explicit false the user saved. Autopush is intentionally
	// NOT per-workspace: push credentials / remote setup are global, so the
	// auto-push flag continues to live in the env file.
	Autoimplement *bool `json:"autoimplement,omitempty"`
	Autotest      *bool `json:"autotest,omitempty"`
	Autosubmit    *bool `json:"autosubmit,omitempty"`
	Autosync      *bool `json:"autosync,omitempty"`

	// CreatedBy records the principal sub of the user who first owned
	// this workspace in cloud mode. Empty on workspaces created pre-cloud or in
	// local mode. Mirrors store.Task.CreatedBy semantics.
	CreatedBy string `json:"created_by,omitempty"`
	// OrgID records the org context the workspace is scoped to. Empty when
	// the workspace is personal (CreatedBy!="") or legacy (CreatedBy==""
	// and OrgID==""). Mirrors store.Task.OrgID semantics.
	OrgID string `json:"org_id,omitempty"`

	// Dormant marks a workspace recovered from orphaned task history during
	// migration: its DataKey addresses real history but its Folders may be
	// empty or only best-effort recovered. The owner re-points it later.
	Dormant bool `json:"dormant,omitempty"`

	// CreatedAt/UpdatedAt are RFC3339 timestamps for bookkeeping. Empty on
	// legacy records.
	CreatedAt string `json:"created_at,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"`
}

// UnmarshalJSON accepts both the current `folders` key and the legacy
// `workspaces` key (used by the pre-redesign workspace-groups.json), so the
// existing on-disk file still loads until migration rewrites it. When both are
// present `folders` wins.
func (w *Workspace) UnmarshalJSON(data []byte) error {
	type alias Workspace // avoid recursion
	var shadow struct {
		alias
		LegacyWorkspaces []string `json:"workspaces"`
	}
	if err := json.Unmarshal(data, &shadow); err != nil {
		return err
	}
	*w = Workspace(shadow.alias)
	if len(w.Folders) == 0 && len(shadow.LegacyWorkspaces) > 0 {
		w.Folders = shadow.LegacyWorkspaces
	}
	return nil
}

// workspacesFilePath returns the path to the canonical workspaces.json file.
func workspacesFilePath(configDir string) string {
	return filepath.Join(configDir, "workspaces.json")
}

// legacyGroupsFilePath returns the path to the pre-redesign
// workspace-groups.json file, read as a fallback until migration rewrites it.
func legacyGroupsFilePath(configDir string) string {
	return filepath.Join(configDir, "workspace-groups.json")
}

// LoadGroups reads workspaces from the config directory. It prefers the
// canonical workspaces.json and falls back to the legacy workspace-groups.json
// (whose records the Workspace UnmarshalJSON still understands) so an
// un-migrated install keeps working.
func LoadGroups(configDir string) ([]Workspace, error) {
	path := workspacesFilePath(configDir)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		raw, err = os.ReadFile(legacyGroupsFilePath(configDir))
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
	}
	if err != nil {
		return nil, err
	}
	var groups []Workspace
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	return NormalizeGroups(groups), nil
}

// SaveGroups writes workspaces to the canonical workspaces.json atomically.
func SaveGroups(configDir string, groups []Workspace) error {
	path := workspacesFilePath(configDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return atomicfile.WriteJSON(path, NormalizeGroups(groups), 0o644)
}

// NormalizeGroups deduplicates and cleans workspaces.
func NormalizeGroups(groups []Workspace) []Workspace {
	if len(groups) == 0 {
		return nil
	}
	out := make([]Workspace, 0, len(groups))
	seen := set.New[string]()
	for _, group := range groups {
		ws := normalizeGroupPaths(group.Folders)
		// A dormant workspace may legitimately have no folders (recovered
		// history awaiting re-point); it is keyed by ID/DataKey instead.
		if len(ws) == 0 && !group.Dormant {
			continue
		}
		// Deduplicate by stable identity. A workspace with an ID dedupes by ID,
		// so two distinct workspaces may share a folder set without collapsing
		// (the redesign drops the path-set uniqueness constraint). Legacy
		// records that predate IDs still dedupe by folder set, preserving the
		// old behavior until migration assigns IDs.
		dedupKey := group.ID
		if dedupKey == "" {
			dedupKey = "folders:" + GroupKey(ws)
		}
		if seen.Has(dedupKey) {
			continue
		}
		seen.Add(dedupKey)
		normalized := group
		normalized.Folders = ws
		normalized.MaxParallel = sanitizeLimit(group.MaxParallel)
		normalized.MaxTestParallel = sanitizeLimit(group.MaxTestParallel)
		out = append(out, normalized)
	}
	return out
}

// Principal is the minimal identity surface for workspace visibility.
// Matches store.Principal in shape but kept local so the workspace
// package doesn't import the store layer.
type Principal struct {
	Sub   string
	OrgID string
}

// findByID returns the index of the workspace with the given id, or -1.
func findByID(groups []Workspace, id string) int {
	if id == "" {
		return -1
	}
	for i := range groups {
		if groups[i].ID == id {
			return i
		}
	}
	return -1
}

// WorkspacesForPrincipal returns the subset of `groups` that `p` can
// see. Mirrors store.TasksForPrincipal's strict isolation:
//
//	┌─────────────────────────────┬──────────────────────────────────┐
//	│ Caller view                 │ Sees                             │
//	├─────────────────────────────┼──────────────────────────────────┤
//	│ local (nil principal)       │ every workspace                  │
//	│ personal (OrgID == "")      │ own personal + legacy no-owner   │
//	│ org X (OrgID == "X")        │ only OrgID=="X" workspaces        │
//	└─────────────────────────────┴──────────────────────────────────┘
//
// The org view does NOT include personal or legacy workspaces — those
// belong to the user's personal account, not the org. Switching
// into an org should feel like a clean slate, not a merged
// view of private + org data.
func WorkspacesForPrincipal(groups []Workspace, p *Principal) []Workspace {
	if p == nil {
		return groups
	}
	out := make([]Workspace, 0, len(groups))
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
// set of folders always produces the same key regardless of input order.
// The key uses newline as separator because it cannot appear in filesystem paths.
func GroupKey(paths []string) string {
	return strings.Join(paths, "\n")
}
