package workspace

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"latere.ai/x/wallfacer/internal/envconfig"
	"latere.ai/x/wallfacer/internal/prompts"
	"latere.ai/x/wallfacer/internal/store"
)

// newWorkspaceID mints a fresh stable workspace identity.
func newWorkspaceID() string { return uuid.NewString() }

// nowStamp returns the current time as an RFC3339 string for bookkeeping fields.
func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

// resolveWorkspaceForPaths finds the non-dormant workspace whose folder set
// matches `validated`, assigning a stable id and a path-seeded DataKey if the
// record predates the redesign (legacy workspace-groups.json had neither). When
// no workspace matches, it transitionally creates one with a path-seeded DataKey
// so the existing data directory (named by that same hash) is reused — this
// preserves pre-redesign behavior for plain path switches. Explicit creation via
// Create uses a RANDOM key instead; see that method.
//
// The matched/created workspace is promoted to the front (MRU) and persisted.
func (m *Manager) resolveWorkspaceForPaths(validated []string) (Workspace, error) {
	// The empty workspace set is not a persisted workspace; it is the "no
	// workspace open" state. Return a synthetic record (no id) addressing the
	// empty-set data key, without writing workspace-groups.json.
	if len(validated) == 0 {
		return Workspace{DataKey: prompts.WorkspaceDataKey(nil)}, nil
	}
	groups, err := LoadGroups(m.configDir)
	if err != nil {
		return Workspace{}, err
	}
	now := nowStamp()
	key := GroupKey(validated)
	for i := range groups {
		if groups[i].Dormant || GroupKey(groups[i].Folders) != key {
			continue
		}
		ws := groups[i]
		ws.Folders = validated
		if ws.ID == "" {
			ws.ID = newWorkspaceID()
		}
		if ws.DataKey == "" {
			ws.DataKey = prompts.WorkspaceDataKey(validated)
		}
		if ws.CreatedAt == "" {
			ws.CreatedAt = now
		}
		ws.UpdatedAt = now
		reordered := append([]Workspace{ws}, groups[:i]...)
		reordered = append(reordered, groups[i+1:]...)
		if err := SaveGroups(m.configDir, reordered); err != nil {
			return Workspace{}, err
		}
		return ws, nil
	}
	ws := Workspace{
		ID:        newWorkspaceID(),
		Folders:   validated,
		DataKey:   prompts.WorkspaceDataKey(validated),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := SaveGroups(m.configDir, append([]Workspace{ws}, groups...)); err != nil {
		return Workspace{}, err
	}
	return ws, nil
}

// Create mints a brand-new workspace with a RANDOM DataKey, owned by p (if
// signed in). Because the key is independent of the folder set, a new workspace
// pointing at the same folders as an existing one starts with empty history —
// identity-first, the core property of the redesign. The new workspace is
// prepended (MRU) and persisted but not activated; call SwitchByID to activate.
func (m *Manager) Create(name string, folders []string, p *Principal) (Workspace, error) {
	validated, err := validate(folders)
	if err != nil {
		return Workspace{}, err
	}
	if len(validated) == 0 {
		return Workspace{}, fmt.Errorf("workspace requires at least one folder")
	}
	now := nowStamp()
	ws := Workspace{
		ID:        newWorkspaceID(),
		Name:      name,
		Folders:   validated,
		DataKey:   prompts.NewDataKey(),
		CreatedAt: now,
		UpdatedAt: now,
	}
	if p != nil {
		ws.CreatedBy = p.Sub
		ws.OrgID = p.OrgID
	}
	groups, err := LoadGroups(m.configDir)
	if err != nil {
		return Workspace{}, err
	}
	if err := SaveGroups(m.configDir, append([]Workspace{ws}, groups...)); err != nil {
		return Workspace{}, err
	}
	return ws, nil
}

// UpdateFolders replaces a workspace's folder set. This is the membership edit
// the redesign makes safe: the workspace's id and DataKey are unchanged, so its
// task store, agent-session transcripts, planning state, and whiteboard stay
// attached. When the workspace is the active one, the live snapshot's paths are
// refreshed in place WITHOUT reopening the store, and the change is published.
func (m *Manager) UpdateFolders(id string, folders []string) (Workspace, error) {
	validated, err := validate(folders)
	if err != nil {
		return Workspace{}, err
	}
	if len(validated) == 0 {
		return Workspace{}, fmt.Errorf("workspace requires at least one folder; delete it instead")
	}
	groups, err := LoadGroups(m.configDir)
	if err != nil {
		return Workspace{}, err
	}
	i := findByID(groups, id)
	if i < 0 {
		return Workspace{}, fmt.Errorf("workspace not found: %s", id)
	}
	ws := groups[i]
	// Heal the DataKey from the CURRENT folders before changing them, so a
	// legacy record's existing data directory stays addressable.
	if ws.DataKey == "" {
		ws.DataKey = prompts.WorkspaceDataKey(ws.Folders)
	}
	ws.Folders = validated
	ws.Dormant = false // re-pointing folders activates a recovered workspace
	ws.UpdatedAt = nowStamp()
	groups[i] = ws
	if err := SaveGroups(m.configDir, groups); err != nil {
		return Workspace{}, err
	}

	// Refresh the live snapshot if this is the active workspace. The store,
	// DataKey, and ScopedDataDir are deliberately left untouched.
	m.mu.Lock()
	if m.current.WorkspaceID != id || id == "" {
		m.mu.Unlock()
		return ws, nil
	}
	m.current.Workspaces = cloneStrings(validated)
	if ag, ok := m.activeGroups[m.current.Key]; ok {
		ag.snapshot = m.current
	}
	snap := cloneSnapshot(m.current)
	m.mu.Unlock()

	if m.envFile != "" {
		encoded := envconfig.FormatWorkspaces(validated)
		if err := envconfig.Update(m.envFile, envconfig.Updates{Workspaces: &encoded}); err != nil {
			return Workspace{}, fmt.Errorf("persist active folders: %w", err)
		}
	}
	m.publish(snap)
	return ws, nil
}

// SwitchByID activates the workspace with the given id, opening or reusing the
// store keyed by its stable DataKey. Unlike Switch (which resolves by paths),
// this disambiguates workspaces that share a folder set. The short-circuit is
// id-aware so switching between two same-folder workspaces is never a silent
// no-op.
func (m *Manager) SwitchByID(id string) (Snapshot, error) {
	if id == "" {
		return Snapshot{}, fmt.Errorf("workspace id required")
	}
	m.mu.RLock()
	same := m.current.Generation > 0 && m.current.WorkspaceID == id
	m.mu.RUnlock()
	if same {
		return m.Snapshot(), nil
	}
	groups, err := LoadGroups(m.configDir)
	if err != nil {
		return Snapshot{}, err
	}
	i := findByID(groups, id)
	if i < 0 {
		return Snapshot{}, fmt.Errorf("workspace not found: %s", id)
	}
	ws := groups[i]
	// A non-dormant workspace must point at valid folders; a dormant one may
	// have none yet (recovered history awaiting re-point) and activates empty.
	if len(ws.Folders) > 0 {
		validated, verr := validate(ws.Folders)
		if verr != nil {
			return Snapshot{}, verr
		}
		ws.Folders = validated
	}
	if ws.DataKey == "" {
		ws.DataKey = prompts.WorkspaceDataKey(ws.Folders)
	}
	now := nowStamp()
	if ws.CreatedAt == "" {
		ws.CreatedAt = now
	}
	ws.UpdatedAt = now
	reordered := append([]Workspace{ws}, append(groups[:i:i], groups[i+1:]...)...)
	if err := SaveGroups(m.configDir, reordered); err != nil {
		return Snapshot{}, err
	}
	return m.activate(ws)
}

// Rename sets a workspace's display name.
func (m *Manager) Rename(id, name string) (Workspace, error) {
	groups, err := LoadGroups(m.configDir)
	if err != nil {
		return Workspace{}, err
	}
	i := findByID(groups, id)
	if i < 0 {
		return Workspace{}, fmt.Errorf("workspace not found: %s", id)
	}
	groups[i].Name = name
	groups[i].UpdatedAt = nowStamp()
	if err := SaveGroups(m.configDir, groups); err != nil {
		return Workspace{}, err
	}
	return groups[i], nil
}

// SetLimits sets (or clears) a workspace's per-workspace concurrency overrides.
// A nil value clears the override so the workspace inherits the global default;
// a non-negative value caps it (0 meaning unlimited, per Group semantics).
func (m *Manager) SetLimits(id string, maxParallel, maxTestParallel *int) (Workspace, error) {
	groups, err := LoadGroups(m.configDir)
	if err != nil {
		return Workspace{}, err
	}
	i := findByID(groups, id)
	if i < 0 {
		return Workspace{}, fmt.Errorf("workspace not found: %s", id)
	}
	groups[i].MaxParallel = maxParallel
	groups[i].MaxTestParallel = maxTestParallel
	groups[i].UpdatedAt = nowStamp()
	if err := SaveGroups(m.configDir, groups); err != nil {
		return Workspace{}, err
	}
	return groups[i], nil
}

// Delete removes a workspace and permanently wipes its scoped data — the task
// store, transcripts, planning state, whiteboard, and agent-session history.
// The active workspace may be deleted: the board auto-switches to the next
// usable workspace (MRU order), or to the empty "no workspace" state when none
// remain (which prompts the picker). Idempotent for an unknown id. Returns the
// resulting active snapshot so callers can reflect the switch.
func (m *Manager) Delete(id string) (Snapshot, error) {
	if id == "" {
		return m.Snapshot(), nil
	}
	groups, err := LoadGroups(m.configDir)
	if err != nil {
		return Snapshot{}, err
	}
	i := findByID(groups, id)
	if i < 0 {
		return m.Snapshot(), nil // idempotent: already gone
	}
	target := groups[i]
	m.mu.RLock()
	wasActive := m.current.WorkspaceID == id
	m.mu.RUnlock()

	// Persist the removal first so the auto-switch below resolves against the
	// remaining set.
	remaining := make([]Workspace, 0, len(groups)-1)
	remaining = append(remaining, groups[:i]...)
	remaining = append(remaining, groups[i+1:]...)
	if err := SaveGroups(m.configDir, remaining); err != nil {
		return Snapshot{}, err
	}

	// If the deleted workspace was active, move the board to the next usable
	// workspace, or to the empty state when none remain.
	if wasActive {
		if next := firstActivatable(remaining); next != "" {
			if _, err := m.SwitchByID(next); err != nil {
				if _, serr := m.Switch(nil); serr != nil {
					return Snapshot{}, serr
				}
			}
		} else if _, err := m.Switch(nil); err != nil {
			return Snapshot{}, err
		}
	}

	m.wipeWorkspaceData(target.DataKey)
	return m.Snapshot(), nil
}

// firstActivatable returns the id of the first workspace with usable folders
// (non-dormant, at least one currently-valid folder), or "" if none qualify.
func firstActivatable(groups []Workspace) string {
	for _, g := range groups {
		if g.ID == "" || g.Dormant || len(g.Folders) == 0 {
			continue
		}
		if _, err := validate(g.Folders); err != nil {
			continue
		}
		return g.ID
	}
	return ""
}

// wipeWorkspaceData closes the scoped store (if still open) and removes the
// workspace's data directory and agent-session directory. A "" key is a no-op
// so it can never escalate to removing a parent directory.
func (m *Manager) wipeWorkspaceData(dataKey string) {
	if dataKey == "" {
		return
	}
	m.mu.Lock()
	if ag, ok := m.activeGroups[dataKey]; ok {
		if ag.snapshot.Store != nil && !ag.snapshot.Store.IsClosed() {
			ag.snapshot.Store.Close()
		}
		delete(m.activeGroups, dataKey)
	}
	m.mu.Unlock()
	if m.dataDir != "" {
		_ = os.RemoveAll(filepath.Join(m.dataDir, dataKey))
	}
	if m.configDir != "" {
		_ = os.RemoveAll(store.AgentSessionUsageDir(m.configDir, dataKey))
	}
}

// ListWorkspaces returns the workspaces visible to p (nil = local, sees all).
func (m *Manager) ListWorkspaces(p *Principal) ([]Workspace, error) {
	groups, err := LoadGroups(m.configDir)
	if err != nil {
		return nil, err
	}
	return WorkspacesForPrincipal(groups, p), nil
}

// WorkspaceByID returns the workspace with the given id, if present.
func (m *Manager) WorkspaceByID(id string) (Workspace, bool, error) {
	groups, err := LoadGroups(m.configDir)
	if err != nil {
		return Workspace{}, false, err
	}
	i := findByID(groups, id)
	if i < 0 {
		return Workspace{}, false, nil
	}
	return groups[i], true, nil
}
