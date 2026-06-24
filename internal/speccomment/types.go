// Package speccomment defines the domain types for inline spec comments: the
// cloud-resident, coordinator-authoritative collaboration artifact of the
// coordination plane (the one scoped exception to relay-not-mirror). The types
// are shared by the coordinator (authoritative store + relay), the local
// instance client (relay to browsers), the anchoring helpers in internal/spec,
// and the handler (browser endpoints), so they live in a leaf package with no
// dependency on any of them.
//
// See specs/cloud/latere-integration/coordination-plane/spec-comments.md.
package speccomment

import "time"

// Thread status values. The UI keys on Status to decide whether a spec is
// "highlighted" (has open comments): only Active && !Resolved threads count.
const (
	StatusActive   = "active"   // anchor resolved; renders inline
	StatusResolved = "resolved" // a human addressed it; hidden until "Show resolved"
	StatusOrphaned = "orphaned" // anchor lost; leaves inline view, goes to triage
	StatusOutdated = "outdated" // a human filed it away; terminal, archived
)

// Op values carried on a comment Event. Instance -> coordinator ops are
// create/reply/resolve/reopen/replace/outdated/edit; coordinator -> instance
// adds sync (the full thread set for a repo, pushed on connect).
const (
	OpCreate   = "create"
	OpReply    = "reply"
	OpResolve  = "resolve"
	OpReopen   = "reopen"
	OpEdit     = "edit"
	OpReplace  = "replace"  // re-place an orphaned thread onto a new anchor
	OpOutdated = "outdated" // mark a thread no longer relevant (terminal)
	OpSync     = "sync"     // coordinator -> instance: full thread set for a repo
)

// Anchor pins a thread to a line or section of a spec. It is computed against
// the canonical source markdown (never the rendered DOM) so the coordinator and
// a future git-export path resolve a thread to the same line without a
// server-side authority on position. LineHash is the primary exact key;
// SectionPath plus Prefix/Suffix are the fuzzy reposition windows; LineHint and
// the git SHAs are advisory and never trusted.
type Anchor struct {
	SectionPath []string `json:"section_path"`
	LineHash    string   `json:"line_hash"`
	Prefix      string   `json:"prefix"`
	Suffix      string   `json:"suffix"`
	ExactText   string   `json:"exact_text"` // snapshot of the anchored text (triage display)
	LineHint    int      `json:"line_hint"`
	CommitSHA   string   `json:"commit_sha,omitempty"`
	BlobSHA     string   `json:"blob_sha,omitempty"`
}

// Comment is one message in a thread. The first comment (ParentID == "") is the
// thread root; replies attach by ParentID. AuthorSub is the ActorSub, stamped
// by the coordinator from the connection principal, never trusted from the wire.
type Comment struct {
	ID        string    `json:"id"`
	ThreadID  string    `json:"thread_id"`
	ParentID  string    `json:"parent_id,omitempty"`
	AuthorSub string    `json:"author_sub"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	EditedAt  time.Time `json:"edited_at"` // zero until edited
}

// Thread is the anchored unit. WorkspaceID is the normalized git remote (the
// cross-machine join key); OrgID is the tenant boundary. Both are stamped by the
// coordinator from the connection, never trusted from the wire.
type Thread struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	WorkspaceID string    `json:"workspace_id"` // normalized git remote (repo)
	SpecPath    string    `json:"spec_path"`
	Anchor      Anchor    `json:"anchor"`
	AuthorSub   string    `json:"author_sub"`
	CreatedAt   time.Time `json:"created_at"`
	Resolved    bool      `json:"resolved"`
	ResolvedBy  string    `json:"resolved_by,omitempty"`
	ResolvedAt  time.Time `json:"resolved_at"` // zero until resolved
	Status      string    `json:"status"`
	Comments    []Comment `json:"comments"`
}

// Event is the FrameSpecComment payload, bidirectional. On an instance ->
// coordinator request the server-authoritative fields (ids, AuthorSub, OrgID,
// timestamps) are absent and the coordinator stamps them; on the coordinator ->
// instance broadcast Thread is the full, authoritative thread (the client
// replaces its copy by id). Repo is the normalized remote the op scopes to.
type Event struct {
	Type    string   `json:"type"` // always coordinator.FrameSpecComment
	Op      string   `json:"op"`
	Repo    string   `json:"repo"`
	Thread  *Thread  `json:"thread,omitempty"`
	Comment *Comment `json:"comment,omitempty"`
	Threads []Thread `json:"threads,omitempty"` // op == sync
}

// Root returns the thread's root comment (ParentID == ""), or false if none.
func (t Thread) Root() (Comment, bool) {
	for _, c := range t.Comments {
		if c.ParentID == "" {
			return c, true
		}
	}
	return Comment{}, false
}
