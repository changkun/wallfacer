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
// Its queries (Snapshot, InstancesForRemote) cover only the
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
	nextGen    uint64
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{byInstance: make(map[string]Instance)}
}

// Join registers an instance. A connection whose instance_id already has a
// (stale) entry replaces it rather than adding a second, so a restart that
// reconnects before the prior socket times out does not briefly show two
// instances.
func (r *Registry) Join(inst Instance) Registration {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := inst.ID()
	r.nextGen++
	inst.generation = r.nextGen
	r.byInstance[id] = inst
	return Registration{InstanceID: id, generation: inst.generation}
}

// UpdateManifest replaces the manifest for an existing instance (a reconnect or
// workspace-set change). No-op if the instance is unknown.
func (r *Registry) UpdateManifest(instanceID string, m Manifest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.byInstance[instanceID]
	if !ok {
		return
	}
	inst.Manifest = m
	r.byInstance[instanceID] = inst
}

// LeaveRegistration removes an instance (socket close or liveness timeout), but
// only if the current registry entry still matches the generation returned by
// Join. Stale sockets that were replaced by a reconnect become no-ops, as does
// an unknown instance id.
func (r *Registry) LeaveRegistration(reg Registration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	inst, ok := r.byInstance[reg.InstanceID]
	if !ok || inst.generation != reg.generation {
		return
	}
	delete(r.byInstance, reg.InstanceID)
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
