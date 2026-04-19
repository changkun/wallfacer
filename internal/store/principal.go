// Principal identifies the caller of a store operation for org-scoped
// reads. Defined here (not imported from internal/auth) so the store
// stays domain-layer and never depends on the HTTP or JWT packages.
// Handlers translate auth.Claims to *store.Principal at the request
// boundary.

package store

import (
	"context"
	"slices"
)

// Principal is the minimal identity surface the store needs to filter
// records. A zero value (empty Sub and empty OrgID) represents an
// anonymous caller; TasksForPrincipal treats it identically to nil.
type Principal struct {
	Sub   string // JWT claims.Sub — used for attribution, not filtering
	OrgID string // JWT claims.OrgID — used for tenant isolation
}

// TasksForPrincipal returns the tasks visible to the given principal.
//
// Filter rules:
//   - p == nil (local mode / anonymous call) → all tasks (today's
//     behavior).
//   - p.OrgID == ""  → only anonymous tasks (those with OrgID == "").
//     A signed-in user with no org context sees their own anonymous
//     records, not other orgs' data.
//   - p.OrgID == "X" → only tasks with OrgID == "X". Legacy anonymous
//     records (OrgID == "") are NOT returned to an org-scoped caller;
//     that would leak pre-migration data across the org boundary.
//
// Sort order matches ListTasks: position then creation time. The
// includeArchived flag behaves identically to ListTasks.
func (s *Store) TasksForPrincipal(_ context.Context, p *Principal, includeArchived bool) []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tasks := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if t == nil {
			continue
		}
		if !includeArchived && t.Archived {
			continue
		}
		if !principalSeesTask(p, t) {
			continue
		}
		tasks = append(tasks, cloneTask(t))
	}
	slices.SortFunc(tasks, cmpTaskPositionCreatedAt)
	return tasks
}

// principalSeesTask encodes the filter matrix in one place so both
// TasksForPrincipal and any future per-task visibility check (e.g.
// GetTask in cloud mode) share the same rules.
func principalSeesTask(p *Principal, t *Task) bool {
	if p == nil {
		return true
	}
	return p.OrgID == t.OrgID
}
