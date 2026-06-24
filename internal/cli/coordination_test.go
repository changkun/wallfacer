package cli

import (
	"context"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// fakeTokenStore is an in-memory authkit.TokenStore for exercising the token
// callback without touching disk.
type fakeTokenStore struct {
	tok    *oauth2.Token
	loaded int
}

func (s *fakeTokenStore) Load() (*oauth2.Token, error) { s.loaded++; return s.tok, nil }
func (s *fakeTokenStore) Save(t *oauth2.Token) error   { s.tok = t; return nil }
func (s *fakeTokenStore) Clear() error                 { s.tok = nil; return nil }

func TestCoordinationTokenFunc(t *testing.T) {
	ctx := context.Background()

	t.Run("signed out yields not-ok", func(t *testing.T) {
		tf := coordinationTokenFunc(ctx, &fakeTokenStore{}, nil)
		if _, ok := tf(); ok {
			t.Fatal("expected not signed in with no stored token")
		}
	})

	t.Run("valid token returns access token", func(t *testing.T) {
		store := &fakeTokenStore{tok: &oauth2.Token{
			AccessToken: "live-jwt",
			Expiry:      time.Now().Add(time.Hour),
		}}
		tf := coordinationTokenFunc(ctx, store, nil)
		got, ok := tf()
		if !ok || got != "live-jwt" {
			t.Fatalf("token = %q, ok = %v; want live-jwt, true", got, ok)
		}
	})

	t.Run("expired token without refresh yields not-ok", func(t *testing.T) {
		store := &fakeTokenStore{tok: &oauth2.Token{
			AccessToken: "stale",
			Expiry:      time.Now().Add(-time.Hour),
		}}
		// nil oidc client => no refresh path; expired token must not be sent.
		tf := coordinationTokenFunc(ctx, store, nil)
		if _, ok := tf(); ok {
			t.Fatal("expired token without refresh must report signed out")
		}
	})
}

func TestCoordinationGate(t *testing.T) {
	g := &coordinationGate{}
	if g.OptedIn() {
		t.Fatal("gate must default to closed (data boundary off by default)")
	}
	g.SetOptedIn(true)
	if !g.OptedIn() {
		t.Fatal("gate did not open after SetOptedIn(true)")
	}
	g.SetOptedIn(false)
	if g.OptedIn() {
		t.Fatal("gate did not close after SetOptedIn(false)")
	}
}

func TestEnvCoordinationOptIn(t *testing.T) {
	for _, v := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Setenv("WALLFACER_COORDINATION", v)
		if !envCoordinationOptIn() {
			t.Fatalf("WALLFACER_COORDINATION=%q should opt in", v)
		}
	}
	for _, v := range []string{"", "0", "false", "off", "garbage"} {
		t.Setenv("WALLFACER_COORDINATION", v)
		if envCoordinationOptIn() {
			t.Fatalf("WALLFACER_COORDINATION=%q should stay opted out", v)
		}
	}
}
