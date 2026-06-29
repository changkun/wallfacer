package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListInstallationRepos_DecodesAndProjects(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"total_count":1,"repositories":[
			{"name":"wallfacer","full_name":"latere/wallfacer","private":true,
			 "default_branch":"main","html_url":"https://github.com/latere/wallfacer",
			 "pushed_at":"2026-06-20T10:00:00Z","owner":{"login":"latere"}}
		]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	repos, err := ListInstallationRepos(context.Background(), c, liveToken())
	if err != nil {
		t.Fatalf("ListInstallationRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("got %d repos, want 1", len(repos))
	}
	r := repos[0]
	if r.Owner != "latere" || r.Name != "wallfacer" || r.FullName != "latere/wallfacer" {
		t.Errorf("projection wrong: %+v", r)
	}
	if r.DefaultBranch != "main" || !r.Private {
		t.Errorf("fields wrong: %+v", r)
	}
	if r.PushedAt.IsZero() {
		t.Error("pushed_at not parsed")
	}
}

// The endpoint must follow rel=next pagination and concatenate pages.
func TestListInstallationRepos_FollowsPagination(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"repositories":[{"name":"b","full_name":"o/b","owner":{"login":"o"}}]}`))
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/installation/repositories?page=2>; rel="next"`, srv.URL))
		_, _ = w.Write([]byte(`{"repositories":[{"name":"a","full_name":"o/a","owner":{"login":"o"}}]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	repos, err := ListInstallationRepos(context.Background(), c, liveToken())
	if err != nil {
		t.Fatalf("ListInstallationRepos: %v", err)
	}
	if len(repos) != 2 || repos[0].Name != "a" || repos[1].Name != "b" {
		t.Fatalf("pagination concat wrong: %+v", repos)
	}
}

func TestListInstallationRepos_PropagatesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"bad creds"}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := ListInstallationRepos(context.Background(), c, liveToken())
	if err == nil {
		t.Fatal("expected error on 401")
	}
}
