package github

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func staticSource(tok string) func(context.Context) (string, error) {
	return func(context.Context) (string, error) { return tok, nil }
}

func TestHTTPBroker_ReturnsMappedToken(t *testing.T) {
	exp := time.Now().Add(time.Hour).UTC().Truncate(time.Second)
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		if r.URL.Path != "/me/integrations/github/token" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"access_token":"ghu_brokered","expires_at":"` + exp.Format(time.RFC3339) + `","login":"octocat"}`))
	}))
	defer srv.Close()

	b := &HTTPBroker{AuthBaseURL: srv.URL, HTTP: srv.Client(), TokenSource: staticSource("user-oidc")}
	tok, err := b.Token(context.Background(), Principal{Sub: "u"})
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if gotAuth != "Bearer user-oidc" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if tok.AccessToken != "ghu_brokered" || tok.Login != "octocat" {
		t.Errorf("token = %+v", tok)
	}
	if !tok.Expiry.Equal(exp) {
		t.Errorf("expiry = %v, want %v", tok.Expiry, exp)
	}
	if !tok.Valid() {
		t.Error("brokered token should be valid")
	}
}

func TestHTTPBroker_NotConnectedOn404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":"not_connected"}`))
	}))
	defer srv.Close()
	b := &HTTPBroker{AuthBaseURL: srv.URL, HTTP: srv.Client(), TokenSource: staticSource("t")}
	if _, err := b.Token(context.Background(), Principal{}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

// A 401 from auth means the user session is invalid; surface as not-connected
// so the UI re-prompts rather than erroring.
func TestHTTPBroker_NotConnectedOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	b := &HTTPBroker{AuthBaseURL: srv.URL, HTTP: srv.Client(), TokenSource: staticSource("t")}
	if _, err := b.Token(context.Background(), Principal{}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

func TestHTTPBroker_ErrorsOn500(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	b := &HTTPBroker{AuthBaseURL: srv.URL, HTTP: srv.Client(), TokenSource: staticSource("t")}
	_, err := b.Token(context.Background(), Principal{})
	if err == nil || errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want a non-NotConnected error", err)
	}
}

func TestHTTPBroker_NotConnectedWhenNoSession(t *testing.T) {
	b := &HTTPBroker{AuthBaseURL: "https://auth.example", TokenSource: staticSource("")}
	if _, err := b.Token(context.Background(), Principal{}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("empty token source: err = %v, want ErrNotConnected", err)
	}
}

func TestHTTPBroker_UnconfiguredIsNotConnected(t *testing.T) {
	b := &HTTPBroker{} // no URL, no source
	if _, err := b.Token(context.Background(), Principal{}); !errors.Is(err, ErrNotConnected) {
		t.Errorf("err = %v, want ErrNotConnected", err)
	}
}

// The Provider should serve a broker token end-to-end and cache it.
func TestProvider_WithHTTPBroker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"ghu_x","expires_at":"` +
			time.Now().Add(time.Hour).UTC().Format(time.RFC3339) + `","login":"o"}`))
	}))
	defer srv.Close()
	store, _ := NewFileStore(t.TempDir())
	pr := &Provider{
		Store:  store,
		Broker: &HTTPBroker{AuthBaseURL: srv.URL, HTTP: srv.Client(), TokenSource: staticSource("t")},
	}
	tok, err := pr.Get(context.Background(), Principal{Sub: "u"})
	if err != nil || tok.AccessToken != "ghu_x" {
		t.Fatalf("Get = %+v, %v", tok, err)
	}
	// Persisted for next time.
	stored, _ := store.Load(context.Background(), Principal{Sub: "u"})
	if stored == nil || stored.AccessToken != "ghu_x" {
		t.Errorf("not persisted: %+v", stored)
	}
}
