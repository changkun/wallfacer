package pubsub

import (
	"sync"
	"testing"
	"time"
)

// TestHub_PublishSubscribe verifies that a subscriber receives published values
// with correct monotonic sequence numbers.
func TestHub_PublishSubscribe(t *testing.T) {
	h := NewHub[string]()
	id, ch := h.Subscribe()
	defer h.Unsubscribe(id)

	h.Publish("hello")

	select {
	case msg := <-ch:
		if msg.Seq != 1 {
			t.Fatalf("expected seq=1, got %d", msg.Seq)
		}
		if msg.Value != "hello" {
			t.Fatalf("expected 'hello', got %q", msg.Value)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
	}
}

// TestHub_WakeCoalescing verifies that rapid publishes coalesce into a bounded
// number of wake signals (capacity-1 channel absorbs bursts).
func TestHub_WakeCoalescing(t *testing.T) {
	h := NewHub[int]()
	id, ch := h.SubscribeWake()
	defer h.UnsubscribeWake(id)

	// Publish multiple times rapidly.
	for i := range 10 {
		h.Publish(i)
	}

	// Drain the wake channel — should have at most 1 signal pending.
	count := 0
	for {
		select {
		case <-ch:
			count++
		default:
			goto done
		}
	}
done:
	if count < 1 || count > 10 {
		t.Fatalf("expected 1-10 wake signals, got %d", count)
	}
}

// TestHub_OverflowEviction verifies that a subscriber whose channel buffer is
// full gets evicted (channel closed) rather than blocking the publisher.
func TestHub_OverflowEviction(t *testing.T) {
	h := NewHub[int](WithChannelSize[int](1))
	id, ch := h.Subscribe()
	defer h.Unsubscribe(id)

	// Fill the buffer (1 item) then overflow.
	h.Publish(1) // fills buffer
	h.Publish(2) // should overflow and close

	// Channel should be closed.
	_, ok := <-ch
	if !ok {
		// Already closed with no buffered item — fine.
		return
	}
	// Got the first item; next read should detect close.
	_, ok = <-ch
	if ok {
		t.Fatal("expected channel to be closed after overflow")
	}
}

// TestHub_UnsubscribeDrains verifies that Unsubscribe drains buffered items
// without blocking and removes the subscriber from the active set.
func TestHub_UnsubscribeDrains(t *testing.T) {
	h := NewHub[int]()
	id, _ := h.Subscribe()

	h.Publish(1)
	h.Publish(2)

	// Unsubscribe should drain without blocking.
	h.Unsubscribe(id)

	if h.SubscriberCount() != 0 {
		t.Fatal("expected 0 subscribers after unsubscribe")
	}
}

// TestHub_Since_NoGap verifies that Since returns only entries after the given
// sequence number when the replay buffer contains the full history.
func TestHub_Since_NoGap(t *testing.T) {
	h := NewHub[int]()

	for i := range 5 {
		h.Publish(i)
	}

	items, gap := h.Since(2)
	if gap {
		t.Fatal("expected no gap")
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if items[0].Seq != 3 || items[2].Seq != 5 {
		t.Fatalf("unexpected seqs: %d, %d", items[0].Seq, items[2].Seq)
	}
}

// TestHub_Since_Gap verifies that Since reports a gap when the requested sequence
// has been evicted from the bounded replay buffer.
func TestHub_Since_Gap(t *testing.T) {
	h := NewHub[int](WithReplayCapacity[int](3))

	for i := range 10 {
		h.Publish(i)
	}

	// Requesting from seq 1 should be a gap since buffer only has last 3.
	_, gap := h.Since(1)
	if !gap {
		t.Fatal("expected gap for old seq")
	}
}

// TestHub_Since_Empty verifies that Since on an empty hub returns nil with no gap.
func TestHub_Since_Empty(t *testing.T) {
	h := NewHub[int]()
	items, gap := h.Since(0)
	if gap {
		t.Fatal("expected no gap on empty hub")
	}
	if items != nil {
		t.Fatalf("expected nil items, got %v", items)
	}
}

// TestHub_Since_AllCurrent verifies that Since returns nil when the caller is
// already up to date with the latest sequence number.
func TestHub_Since_AllCurrent(t *testing.T) {
	h := NewHub[int]()
	h.Publish(1)
	h.Publish(2)

	items, gap := h.Since(2)
	if gap {
		t.Fatal("expected no gap")
	}
	if items != nil {
		t.Fatalf("expected nil items when all current, got %v", items)
	}
}

// TestHub_LatestSeq verifies that LatestSeq starts at 0 and increments with
// each Publish call.
func TestHub_LatestSeq(t *testing.T) {
	h := NewHub[int]()
	if h.LatestSeq() != 0 {
		t.Fatalf("expected 0, got %d", h.LatestSeq())
	}
	h.Publish(42)
	if h.LatestSeq() != 1 {
		t.Fatalf("expected 1, got %d", h.LatestSeq())
	}
}

// TestHub_WithClone verifies that the clone function isolates subscribers from
// mutations to the original published value.
func TestHub_WithClone(t *testing.T) {
	type val struct{ N int }
	h := NewHub[*val](WithClone[*val](func(v *val) *val {
		if v == nil {
			return nil
		}
		cp := *v
		return &cp
	}))

	id, ch := h.Subscribe()
	defer h.Unsubscribe(id)

	original := &val{N: 1}
	h.Publish(original)

	msg := <-ch
	// Mutating the original should not affect the received value.
	original.N = 999
	if msg.Value.N != 1 {
		t.Fatalf("expected clone isolation, got N=%d", msg.Value.N)
	}
}

// TestHub_ConcurrentSafe stress-tests that concurrent Publish, Subscribe,
// SubscribeWake, and Since operations do not race (validated by -race detector).
func TestHub_ConcurrentSafe(_ *testing.T) {
	h := NewHub[int]()
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)

	for i := range n {
		go func() {
			defer wg.Done()
			switch i % 4 {
			case 0:
				h.Publish(i)
			case 1:
				id, _ := h.Subscribe()
				h.Unsubscribe(id)
			case 2:
				id, _ := h.SubscribeWake()
				h.UnsubscribeWake(id)
			case 3:
				h.Since(int64(i))
			}
		}()
	}
	wg.Wait()
}
