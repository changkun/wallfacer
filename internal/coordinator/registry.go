package coordinator

import (
	"slices"
	"sync"
)

// Sender is the send side of a live instance connection. The accept handler
// implements it over the WebSocket; the registry only stores the handle so
// capability code (presence, projection, remote control, comments) can reach an
// instance without touching socket plumbing. It is nil in unit tests that
// exercise the registry alone.
type Sender interface {
	Send(v any) error
}

// Instance is one live connection in the registry. Principal is the validated
// JWT identity (never taken from the manifest body); Manifest is the latest
// registration the instance sent; Conn is the live send handle.
type Instance struct {
	Principal Principal
	Manifest  Manifest
	Conn      Sender

	generation uint64
}

// ID returns the instance's stable, persisted id (the registry key).
func (i Instance) ID() string { return i.Manifest.InstanceID }

// EventKind classifies a registry change emitted to subscribers.
type EventKind int

const (
	// EventJoin is a new instance registering for the first time.
	EventJoin EventKind = iota
	// EventLeave is an instance disconnecting (close or liveness timeout).
	EventLeave
	// EventManifest is an existing instance re-registering (reconnect or a
	// workspace-set change), replacing its prior manifest.
	EventManifest
)

// Event is a registry change broadcast to subscribers so presence and
// remote-control learn of membership changes without polling.
type Event struct {
	Kind       EventKind
	Org        string
	Principal  Principal
	InstanceID string
}

// Registration identifies the exact registry entry created by Join. It lets the
// connection that joined later leave only its own generation, not a replacement
// connection that reused the same persisted instance id.
type Registration struct {
	InstanceID string
	generation uint64
}

// Registry is the coordinator-side, in-memory, ephemeral map of live instances.
// It is rebuilt from reconnects and never persisted. It holds only registration
// metadata (principal, org, instance id, host label, version, served workspace
// remotes, capabilities), never task or content data.
//
// Scope: this is the SINGLE-REPLICA view plus this replica's local socket table.
// Its queries (Snapshot, PrincipalsInOrg, InstancesForRemote) cover only the
// instances THIS replica terminates. wallfacerd runs multiple replicas, so under
// horizontal scaling these whole-org queries must go through a Redis-backed
// Directory (Valkey index + pub/sub) instead; see
// specs/.../connection-and-presence/connection.md. This type is the
// memDirectory (single-replica) impl plus the local socket table that every
// replica keeps for delivery. Do not wire presence to these queries directly in
// multi-replica mode, or each replica shows a partial org list.
//
// The indices are derived from a single source of truth (byInstance), correct
// and simple at org scale (dozens of instances per org).
type Registry struct {
	mu         sync.RWMutex
	byInstance map[string]Instance // instance_id -> instance
	subs       map[int]chan Event
	nextSub    int
	nextGen    uint64
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		byInstance: make(map[string]Instance),
		subs:       make(map[int]chan Event),
	}
}

// Join registers an instance. A connection whose instance_id already has a
// (stale) entry replaces it rather than adding a second, so a restart that
// reconnects before the prior socket times out does not briefly show two
// instances. The replace path emits EventManifest, a fresh id emits EventJoin.
func (r *Registry) Join(inst Instance) Registration {
	r.mu.Lock()
	id := inst.ID()
	_, existed := r.byInstance[id]
	r.nextGen++
	inst.generation = r.nextGen
	r.byInstance[id] = inst
	kind := EventJoin
	if existed {
		kind = EventManifest
	}
	ev := Event{Kind: kind, Org: inst.Principal.OrgID, Principal: inst.Principal, InstanceID: id}
	reg := Registration{InstanceID: id, generation: inst.generation}
	r.mu.Unlock()
	r.broadcast(ev)
	return reg
}

// UpdateManifest replaces the manifest for an existing instance (a reconnect or
// workspace-set change). No-op if the instance is unknown.
func (r *Registry) UpdateManifest(instanceID string, m Manifest) {
	r.mu.Lock()
	inst, ok := r.byInstance[instanceID]
	if !ok {
		r.mu.Unlock()
		return
	}
	inst.Manifest = m
	r.byInstance[instanceID] = inst
	ev := Event{Kind: EventManifest, Org: inst.Principal.OrgID, Principal: inst.Principal, InstanceID: instanceID}
	r.mu.Unlock()
	r.broadcast(ev)
}

// Leave removes an instance (socket close or liveness timeout). No-op if absent.
func (r *Registry) Leave(instanceID string) {
	r.mu.Lock()
	inst, ok := r.byInstance[instanceID]
	if !ok {
		r.mu.Unlock()
		return
	}
	delete(r.byInstance, instanceID)
	ev := Event{Kind: EventLeave, Org: inst.Principal.OrgID, Principal: inst.Principal, InstanceID: instanceID}
	r.mu.Unlock()
	r.broadcast(ev)
}

// LeaveRegistration removes an instance only if the current registry entry still
// matches the generation returned by Join. Stale sockets that were replaced by a
// reconnect become no-ops.
func (r *Registry) LeaveRegistration(reg Registration) {
	r.mu.Lock()
	inst, ok := r.byInstance[reg.InstanceID]
	if !ok || inst.generation != reg.generation {
		r.mu.Unlock()
		return
	}
	delete(r.byInstance, reg.InstanceID)
	ev := Event{Kind: EventLeave, Org: inst.Principal.OrgID, Principal: inst.Principal, InstanceID: reg.InstanceID}
	r.mu.Unlock()
	r.broadcast(ev)
}

// Snapshot returns every instance currently registered in the given org.
func (r *Registry) Snapshot(org string) []Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Instance
	for _, inst := range r.byInstance {
		if inst.Principal.OrgID == org {
			out = append(out, inst)
		}
	}
	return out
}

// PrincipalsInOrg returns the distinct principals present in the org (the
// org -> set<principal> index, derived). One person on many machines collapses
// to one principal.
func (r *Registry) PrincipalsInOrg(org string) []Principal {
	r.mu.RLock()
	defer r.mu.RUnlock()
	seen := make(map[string]Principal)
	for _, inst := range r.byInstance {
		if inst.Principal.OrgID == org {
			seen[inst.Principal.Sub] = inst.Principal
		}
	}
	out := make([]Principal, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	return out
}

// InstancesForRemote returns instances serving the given cross-machine workspace
// key (canonical git remote URL). Used by collaboration fan-out (comments).
func (r *Registry) InstancesForRemote(remote string) []Instance {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Instance
	for _, inst := range r.byInstance {
		if slices.Contains(inst.Manifest.Remotes(), remote) {
			out = append(out, inst)
		}
	}
	return out
}

// instance returns a single registered instance by id (for re-sync after a
// manifest update). Unexported: capability code queries by org or remote.
func (r *Registry) instance(instanceID string) (Instance, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	inst, ok := r.byInstance[instanceID]
	return inst, ok
}

// Len returns the number of registered instances.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byInstance)
}

// Subscribe returns a channel of registry events and an unsubscribe function.
// The channel is buffered; a subscriber that falls behind may miss events and
// should resync via Snapshot (presence already broadcasts full snapshots, so a
// missed delta is self-healing). The unsubscribe function is idempotent.
func (r *Registry) Subscribe() (<-chan Event, func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := r.nextSub
	r.nextSub++
	ch := make(chan Event, 64)
	r.subs[id] = ch
	var once sync.Once
	cancel := func() {
		once.Do(func() {
			r.mu.Lock()
			defer r.mu.Unlock()
			if c, ok := r.subs[id]; ok {
				delete(r.subs, id)
				close(c)
			}
		})
	}
	return ch, cancel
}

// broadcast sends an event to all subscribers without blocking: a full
// subscriber buffer drops the event (the subscriber resyncs via Snapshot).
func (r *Registry) broadcast(ev Event) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, ch := range r.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
