package github

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeBroker is an in-memory Broker for exercising Provider without ../auth.
type fakeBroker struct {
	tok   *Token
	err   error
	calls int
}

func (f *fakeBroker) Token(_ context.Context, _ Principal) (*Token, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	return f.tok, nil
}

func TestProvider_ServesValidStoredTokenWithoutBroker(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	ctx := context.Background()
	p := Principal{OrgID: "o", Sub: "u"}
	if err := store.Save(ctx, p, &Token{AccessToken: "live", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	broker := &fakeBroker{tok: &Token{AccessToken: "fresh", Expiry: time.Now().Add(time.Hour)}}
	pr := &Provider{Store: store, Broker: broker}

	got, err := pr.Get(ctx, p)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "live" {
		t.Errorf("served %q, want stored %q", got.AccessToken, "live")
	}
	if broker.calls != 0 {
		t.Errorf("broker called %d times for a valid stored token, want 0", broker.calls)
	}
}

func TestProvider_RefreshesExpiredAndPersists(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	ctx := context.Background()
	p := Principal{OrgID: "o", Sub: "u"}
	if err := store.Save(ctx, p, &Token{AccessToken: "stale", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	broker := &fakeBroker{tok: &Token{AccessToken: "fresh", Expiry: time.Now().Add(time.Hour)}}
	pr := &Provider{Store: store, Broker: broker}

	got, err := pr.Get(ctx, p)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "fresh" {
		t.Errorf("served %q, want refreshed %q", got.AccessToken, "fresh")
	}
	if broker.calls != 1 {
		t.Errorf("broker calls = %d, want 1", broker.calls)
	}
	// The refreshed token must be persisted for the next Get.
	stored, _ := store.Load(ctx, p)
	if stored == nil || stored.AccessToken != "fresh" {
		t.Errorf("refreshed token not persisted: %+v", stored)
	}
}

func TestProvider_AbsentTokenRefreshesThroughBroker(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	ctx := context.Background()
	p := Principal{OrgID: "o", Sub: "u"}
	broker := &fakeBroker{tok: &Token{AccessToken: "fresh", Expiry: time.Now().Add(time.Hour)}}
	pr := &Provider{Store: store, Broker: broker}

	got, err := pr.Get(ctx, p)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "fresh" || broker.calls != 1 {
		t.Errorf("absent token: got %q calls %d, want fresh/1", got.AccessToken, broker.calls)
	}
}

func TestProvider_NoBrokerReturnsNotConnected(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	pr := &Provider{Store: store} // no broker
	_, err := pr.Get(context.Background(), Principal{Sub: "u"})
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Get without broker = %v, want ErrNotConnected", err)
	}
}

func TestProvider_BrokerNotConnectedPropagates(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	broker := &fakeBroker{err: ErrNotConnected}
	pr := &Provider{Store: store, Broker: broker}
	_, err := pr.Get(context.Background(), Principal{Sub: "u"})
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Get = %v, want ErrNotConnected", err)
	}
}

// A broker that returns an unusable (e.g. empty) token must surface as
// not-connected rather than handing a junk credential to callers.
func TestProvider_BrokerInvalidTokenIsNotConnected(t *testing.T) {
	store, _ := NewFileStore(t.TempDir())
	broker := &fakeBroker{tok: &Token{AccessToken: ""}}
	pr := &Provider{Store: store, Broker: broker}
	_, err := pr.Get(context.Background(), Principal{Sub: "u"})
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Get with invalid broker token = %v, want ErrNotConnected", err)
	}
}

func TestProvider_NoStorePassesThroughToBroker(t *testing.T) {
	broker := &fakeBroker{tok: &Token{AccessToken: "fresh", Expiry: time.Now().Add(time.Hour)}}
	pr := &Provider{Broker: broker} // no store
	got, err := pr.Get(context.Background(), Principal{Sub: "u"})
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.AccessToken != "fresh" {
		t.Errorf("got %q, want fresh", got.AccessToken)
	}
}
