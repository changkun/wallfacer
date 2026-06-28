package executor

import (
	"context"
	"testing"
	"time"
)

// TestAcquireSlot_BudgetCaps proves the global agent budget: with MaxAgents=1 a
// second acquire blocks until the first slot is released, and an unlimited
// backend (MaxAgents=0) never blocks.
func TestAcquireSlot_BudgetCaps(t *testing.T) {
	b, err := NewHostBackend(HostBackendConfig{MaxAgents: 1})
	if err != nil {
		t.Fatalf("NewHostBackend: %v", err)
	}

	rel1, err := b.acquireSlot(context.Background())
	if err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	// Budget full: a second acquire must block. Prove it by timing out.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := b.acquireSlot(ctx); err == nil {
		t.Fatal("second acquire should block while the single slot is held")
	}

	// Releasing frees the slot for the next acquirer.
	rel1()
	rel2, err := b.acquireSlot(context.Background())
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	rel2()

	// Double release is safe (sync.Once) and does not over-free the budget.
	rel2()
	if _, err := b.acquireSlot(context.Background()); err != nil {
		t.Fatalf("acquire after double-release: %v", err)
	}
}

// TestAcquireSlot_Unlimited proves MaxAgents=0 imposes no ceiling.
func TestAcquireSlot_Unlimited(t *testing.T) {
	b, err := NewHostBackend(HostBackendConfig{MaxAgents: 0})
	if err != nil {
		t.Fatalf("NewHostBackend: %v", err)
	}
	for i := 0; i < 8; i++ {
		if _, err := b.acquireSlot(context.Background()); err != nil {
			t.Fatalf("unlimited budget blocked at %d: %v", i, err)
		}
	}
}
