package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"sort"
	"sync"

	"latere.ai/x/wallfacer/internal/coordinator"
	"latere.ai/x/wallfacer/internal/speccomment"
)

// ErrCoordinatorUnavailable is returned by Submit when no coordination
// connection is held (signed out, opted out, or mid-reconnect). The handler
// surfaces it as a transient 503, not a hard failure.
var ErrCoordinatorUnavailable = errors.New("coordination unavailable")

// CommentRelay is the instance side of the spec-comment relay. It holds the
// thread set the coordinator pushes down the coordination connection (the
// coordinator is authoritative; this is a read-through cache), fans coordinator
// events out to connected browsers over SSE, and forwards browser-initiated ops
// up the connection. It is never the system of record: pull the coordinator and
// the cache simply stops updating.
type CommentRelay struct {
	mu      sync.RWMutex
	threads map[string]map[string]speccomment.Thread // repo -> thread id -> thread
	subs    map[int]chan speccomment.Event
	nextSub int

	sendUp func(speccomment.Event) error // forward an op to the coordinator
	log    *slog.Logger
}

// NewCommentRelay returns an empty relay. Wire SetSendUp to the connector and
// HandleInbound to the connector's OnInbound callback.
func NewCommentRelay() *CommentRelay {
	return &CommentRelay{
		threads: make(map[string]map[string]speccomment.Thread),
		subs:    make(map[int]chan speccomment.Event),
		log:     slog.Default(),
	}
}

// SetSendUp wires the upward path (browser op -> coordinator). Until set, Submit
// reports the coordinator unavailable.
func (r *CommentRelay) SetSendUp(fn func(speccomment.Event) error) {
	r.mu.Lock()
	r.sendUp = fn
	r.mu.Unlock()
}

// HandleInbound decodes a coordinator-to-instance frame. Spec-comment frames
// update the cache and fan out to browsers; other frame types are ignored
// (presence and others are handled elsewhere). Safe to pass every inbound frame.
func (r *CommentRelay) HandleInbound(data []byte) {
	env, err := coordinator.DecodeEnvelope(data)
	if err != nil || env.Type != coordinator.FrameSpecComment {
		return
	}
	var ev speccomment.Event
	if err := json.Unmarshal(data, &ev); err != nil {
		r.log.Warn("comment relay: bad spec-comment frame", "err", err)
		return
	}
	r.apply(ev)
	r.broadcast(ev)
}

// apply folds an authoritative event into the cache. A sync replaces the whole
// thread set for a repo; create/reply/resolve/reopen upsert the one thread by id.
func (r *CommentRelay) apply(ev speccomment.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	switch ev.Op {
	case speccomment.OpSync:
		m := make(map[string]speccomment.Thread, len(ev.Threads))
		for _, t := range ev.Threads {
			m[t.ID] = t
		}
		r.threads[ev.Repo] = m
	default:
		if ev.Thread == nil {
			return
		}
		repo := ev.Thread.WorkspaceID
		if repo == "" {
			repo = ev.Repo
		}
		if r.threads[repo] == nil {
			r.threads[repo] = make(map[string]speccomment.Thread)
		}
		r.threads[repo][ev.Thread.ID] = *ev.Thread
	}
}

// ThreadsForRepo returns a snapshot of the cached threads for a repo, sorted by
// id (ULID = creation order). Includes resolved/orphaned/outdated threads so the
// caller can build the inline view and the triage list from one set.
func (r *CommentRelay) ThreadsForRepo(repo string) []speccomment.Thread {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m := r.threads[repo]
	out := make([]speccomment.Thread, 0, len(m))
	for _, t := range m {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Submit forwards a browser-initiated op to the coordinator. The coordinator is
// authoritative: it mints ids, stamps the principal, and echoes the result back
// down (apply + broadcast), so Submit does not mutate the cache itself.
func (r *CommentRelay) Submit(ev speccomment.Event) error {
	r.mu.RLock()
	send := r.sendUp
	r.mu.RUnlock()
	if send == nil {
		return ErrCoordinatorUnavailable
	}
	ev.Type = coordinator.FrameSpecComment
	return send(ev)
}

// Subscribe registers a browser SSE listener. The channel is buffered; a slow
// listener may miss an event and should reconcile via a fresh GET.
func (r *CommentRelay) Subscribe() (int, <-chan speccomment.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextSub
	r.nextSub++
	ch := make(chan speccomment.Event, 32)
	r.subs[id] = ch
	return id, ch
}

// Unsubscribe removes a listener and closes its channel.
func (r *CommentRelay) Unsubscribe(id int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ch, ok := r.subs[id]; ok {
		delete(r.subs, id)
		close(ch)
	}
}

// broadcast pushes an event to every SSE listener without blocking; a full
// buffer drops the event (the listener reconciles via GET).
func (r *CommentRelay) broadcast(ev speccomment.Event) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ch := range r.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
