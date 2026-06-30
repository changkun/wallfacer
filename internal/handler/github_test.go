package handler

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"latere.ai/x/wallfacer/internal/github"
)

// githubHandlerWithMock returns a Handler whose GitHub provider has a seeded
// valid token and a client pointed at mockGH, so the write handlers exercise
// the full transport without ../auth.
func githubHandlerWithMock(t *testing.T, mockGH *httptest.Server) *Handler {
	t.Helper()
	store, _ := github.NewFileStore(t.TempDir())
	h := &Handler{}
	h.SetGitHub(&github.Provider{
		Store:  store,
		Client: &github.Client{BaseURL: mockGH.URL, HTTP: mockGH.Client()},
	})
	if err := store.Save(context.Background(), h.githubPrincipal(context.Background()),
		&github.Token{AccessToken: "ghu_x", Expiry: time.Now().Add(time.Hour)}); err != nil {
		t.Fatalf("seed token: %v", err)
	}
	return h
}
