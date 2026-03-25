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
}

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

// UpsertGroup adds or promotes a workspace group to the front of the list.
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
			promoted := Group{Name: group.Name, Workspaces: workspaces}
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
		out = append(out, Group{Name: group.Name, Workspaces: ws})
	}
	return out
}

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

// GroupKey returns a canonical key for a set of workspace paths.
func GroupKey(paths []string) string {
	return strings.Join(paths, "\n")
}
