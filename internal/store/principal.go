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
// Three task shapes exist on disk and get different visibility:
//
//	┌──────────────────────────────────┬────────────────────────────┐
//	│ Task shape                       │ Who sees it                │
//	├──────────────────────────────────┼────────────────────────────┤
//	│ CreatedBy=""  OrgID=""  (legacy) │ Any signed-in user + local │
//	│ CreatedBy=U   OrgID=""  (self)   │ Only user U                │
//	│ CreatedBy=*   OrgID=X   (org)    │ Anyone with claims.OrgID=X │
//	└──────────────────────────────────┴────────────────────────────┘
//
// "Legacy" = created before the cloud/org concept existed; no owner
// was ever recorded. Treated as deployment-shared so single-user
// upgrades to cloud mode don't lose their history. "Self" = personal
// space for the signed-in user, explicitly scoped to them alone.
// "Org" = the usual multi-tenant path.
//
//   - p == nil (local mode / anonymous call) → all tasks (today's
//     behavior).
//   - p signed in → the matrix above.
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
// GetTask in cloud mode) share the same rules. See TasksForPrincipal
// for the task-shape / visibility table.
func principalSeesTask(p *Principal, t *Task) bool {
	if p == nil {
		// Local mode / anonymous call: no filtering.
		return true
	}
	switch {
	case t.OrgID == "" && t.CreatedBy == "":
		// Legacy / pre-cloud task with no recorded owner. Shared
		// within the deployment so single-user upgrades don't orphan
		// history.
		return true
	case t.OrgID == "" && t.CreatedBy == p.Sub:
		// Personal space: caller's own un-org-scoped task.
		return true
	case t.OrgID != "" && t.OrgID == p.OrgID:
		// Org-scoped task matching the caller's current org claim.
		return true
	default:
		return false
	}
}
