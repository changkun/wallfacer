package coordinator

import (
	"testing"
)

func inst(id, sub, org string, remotes ...string) Instance {
	ws := make([]WorkspaceRef, len(remotes))
	for i, r := range remotes {
		ws[i] = WorkspaceRef{Remote: r, LocalKey: "k" + id}
	}
	return Instance{
		Principal: Principal{Sub: sub, OrgID: org},
		Manifest:  NewManifest(id, "host-"+id, "v", ws, []string{"presence"}),
	}
}

func TestRegistryJoinLeaveSnapshot(t *testing.T) {
	r := NewRegistry()
	r.Join(inst("i1", "alice", "org1", "github.com/a/b"))
	r.Join(inst("i2", "bob", "org1", "github.com/a/b"))
	r.Join(inst("i3", "carol", "org2"))

	if got := len(r.Snapshot("org1")); got != 2 {
		t.Fatalf("org1 snapshot = %d, want 2", got)
	}
	if got := len(r.Snapshot("org2")); got != 1 {
		t.Fatalf("org2 snapshot = %d, want 1", got)
	}

	r.Leave("i2")
	if got := len(r.Snapshot("org1")); got != 1 {
		t.Fatalf("after leave, org1 snapshot = %d, want 1", got)
	}
	r.Leave("nonexistent") // no-op, no panic
	if r.Len() != 2 {
		t.Fatalf("Len = %d, want 2", r.Len())
	}
}

func TestRegistryReconnectReplacesSlot(t *testing.T) {
	// A restart reuses the persisted instance_id and must re-take the same slot,
	// not create a duplicate (no presence flap).
	r := NewRegistry()
	r.Join(inst("i1", "alice", "org1", "github.com/a/b"))
	r.Join(inst("i1", "alice", "org1", "github.com/a/b")) // reconnect, same id
	if r.Len() != 1 {
		t.Fatalf("reconnect created a duplicate: Len = %d, want 1", r.Len())
	}
}

func TestRegistryPrincipalsInOrgDedup(t *testing.T) {
	// One person on two machines is one principal in the org.
	r := NewRegistry()
	r.Join(inst("laptop", "alice", "org1"))
	r.Join(inst("desktop", "alice", "org1"))
	r.Join(inst("i3", "bob", "org1"))

	ps := r.PrincipalsInOrg("org1")
	if len(ps) != 2 {
		t.Fatalf("PrincipalsInOrg = %d distinct, want 2 (alice, bob)", len(ps))
	}
}

func TestRegistryInstancesForRemote(t *testing.T) {
	r := NewRegistry()
	r.Join(inst("i1", "alice", "org1", "github.com/a/b"))
	r.Join(inst("i2", "bob", "org1", "github.com/a/b", "github.com/c/d"))
	r.Join(inst("i3", "carol", "org1", "github.com/c/d"))

	if got := len(r.InstancesForRemote("github.com/a/b")); got != 2 {
		t.Fatalf("a/b instances = %d, want 2", got)
	}
	if got := len(r.InstancesForRemote("github.com/c/d")); got != 2 {
		t.Fatalf("c/d instances = %d, want 2", got)
	}
	if got := len(r.InstancesForRemote("github.com/none")); got != 0 {
		t.Fatalf("none instances = %d, want 0", got)
	}
}

func TestRegistrySubscribe(t *testing.T) {
	r := NewRegistry()
	ch, cancel := r.Subscribe()
	defer cancel()

	r.Join(inst("i1", "alice", "org1"))
	ev := <-ch
	if ev.Kind != EventJoin || ev.InstanceID != "i1" || ev.Org != "org1" {
		t.Fatalf("join event = %+v", ev)
	}

	r.Join(inst("i1", "alice", "org1")) // reconnect -> EventManifest
	if ev := <-ch; ev.Kind != EventManifest {
		t.Fatalf("reconnect event kind = %v, want EventManifest", ev.Kind)
	}

	r.Leave("i1")
	if ev := <-ch; ev.Kind != EventLeave {
		t.Fatalf("leave event kind = %v, want EventLeave", ev.Kind)
	}
}

func TestRegistrySubscribeCancelIdempotent(t *testing.T) {
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("double cancel panicked: %v", p)
		}
	}()
	r := NewRegistry()
	_, cancel := r.Subscribe()
	cancel()
	cancel() // must not panic on double close
}

func TestRegistryUpdateManifest(t *testing.T) {
	r := NewRegistry()
	r.Join(inst("i1", "alice", "org1", "github.com/a/b"))
	// Workspace switch: now serving a different repo.
	r.UpdateManifest("i1", NewManifest("i1", "host", "v",
		[]WorkspaceRef{{Remote: "github.com/x/y", LocalKey: "k"}}, []string{"presence"}))

	if got := len(r.InstancesForRemote("github.com/a/b")); got != 0 {
		t.Fatalf("old remote still present: %d", got)
	}
	if got := len(r.InstancesForRemote("github.com/x/y")); got != 1 {
		t.Fatalf("new remote = %d, want 1", got)
	}
	r.UpdateManifest("unknown", Manifest{}) // no-op, no panic
}
