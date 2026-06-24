package coordinator

import (
	"context"
	"sort"
	"sync"

	"latere.ai/x/wallfacer/internal/speccomment"
)

// CommentStore is the authoritative, durable system of record for spec comment
// threads, the one relay-not-mirror exception. Queries are always scoped by org
// (the tenant boundary) plus repo (the normalized git remote); the coordinator
// supplies both from the validated connection, never from the wire, so
// cross-tenant access is structurally impossible.
//
// The store is intentionally dumb: it upserts and queries whole threads. The
// authoritative logic (minting ids, stamping the principal, the permission gate,
// fan-out) lives in CommentService, so the store can be swapped (in-memory for
// dev and tests, Postgres for the durable release) without moving that logic.
type CommentStore interface {
	// ThreadsForRepo returns every thread for (org, repo), including resolved,
	// orphaned, and outdated ones, so the client can build the triage list
	// without a second fetch. Order is by thread id (ULID, so creation order).
	ThreadsForRepo(ctx context.Context, org, repo string) ([]speccomment.Thread, error)
	// GetThread returns one thread by id within an org, or ok=false if absent.
	GetThread(ctx context.Context, org, threadID string) (speccomment.Thread, bool, error)
	// PutThread upserts a thread (create or replace by id).
	PutThread(ctx context.Context, t speccomment.Thread) error
}

// memStore is an in-memory CommentStore for local dev and tests. Durable
// releases use the Postgres store; this one is byte-identical in behavior for a
// single process.
type memStore struct {
	mu      sync.RWMutex
	threads map[string]speccomment.Thread // thread id -> thread
}

// NewMemCommentStore returns an in-memory comment store.
func NewMemCommentStore() CommentStore {
	return &memStore{threads: make(map[string]speccomment.Thread)}
}

func (m *memStore) ThreadsForRepo(_ context.Context, org, repo string) ([]speccomment.Thread, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var out []speccomment.Thread
	for _, t := range m.threads {
		if t.OrgID == org && t.WorkspaceID == repo {
			out = append(out, t)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (m *memStore) GetThread(_ context.Context, org, threadID string) (speccomment.Thread, bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	t, ok := m.threads[threadID]
	if !ok || t.OrgID != org {
		return speccomment.Thread{}, false, nil
	}
	return t, true, nil
}

func (m *memStore) PutThread(_ context.Context, t speccomment.Thread) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.threads[t.ID] = t
	return nil
}
