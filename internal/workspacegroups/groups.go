package workspacegroups

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Group struct {
	Workspaces []string `json:"workspaces"`
}

func filePath(configDir string) string {
	return filepath.Join(configDir, "workspace-groups.json")
}

func Load(configDir string) ([]Group, error) {
	raw, err := os.ReadFile(filePath(configDir))
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
	return Normalize(groups), nil
}

func Save(configDir string, groups []Group) error {
	path := filePath(configDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(Normalize(groups), "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func Upsert(configDir string, workspaces []string) error {
	workspaces = normalizePaths(workspaces)
	if len(workspaces) == 0 {
		return nil
	}
	groups, err := Load(configDir)
	if err != nil {
		return err
	}
	key := groupKey(workspaces)
	for _, group := range groups {
		if groupKey(group.Workspaces) == key {
			return nil
		}
	}
	groups = append([]Group{{Workspaces: workspaces}}, groups...)
	return Save(configDir, groups)
}

func Normalize(groups []Group) []Group {
	if len(groups) == 0 {
		return nil
	}
	out := make([]Group, 0, len(groups))
	seen := map[string]struct{}{}
	for _, group := range groups {
		ws := normalizePaths(group.Workspaces)
		if len(ws) == 0 {
			continue
		}
		key := groupKey(ws)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, Group{Workspaces: ws})
	}
	return out
}

func normalizePaths(paths []string) []string {
	if len(paths) == 0 {
		return nil
	}
	out := make([]string, 0, len(paths))
	seen := map[string]struct{}{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" {
			continue
		}
		path = filepath.Clean(path)
		if _, ok := seen[path]; ok {
			continue
		}
		seen[path] = struct{}{}
		out = append(out, path)
	}
	slices.Sort(out)
	return out
}

func groupKey(paths []string) string {
	return strings.Join(paths, "\n")
}
