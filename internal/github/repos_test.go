package github

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListInstallationRepos_AcrossInstallations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/user/installations") && !strings.Contains(r.URL.Path, "/repositories"):
			// two installations: personal (11) + an org (22)
			_, _ = w.Write([]byte(`{"total_count":2,"installations":[{"id":11},{"id":22}]}`))
		case strings.Contains(r.URL.Path, "/user/installations/11/repositories"):
			_, _ = w.Write([]byte(`{"repositories":[
				{"name":"wallfacer","full_name":"changkun/wallfacer","private":true,
				 "default_branch":"main","pushed_at":"2026-06-20T10:00:00Z","owner":{"login":"changkun"}}
			]}`))
		case strings.Contains(r.URL.Path, "/user/installations/22/repositories"):
			_, _ = w.Write([]byte(`{"repositories":[
				{"name":"infra","full_name":"latere-ai/infra","owner":{"login":"latere-ai"}}
			]}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	repos, err := ListInstallationRepos(context.Background(), c, liveToken())
	if err != nil {
		t.Fatalf("ListInstallationRepos: %v", err)
	}
	if len(repos) != 2 {
		t.Fatalf("got %d repos across installs, want 2: %+v", len(repos), repos)
	}
	// personal repo projected fully
	if repos[0].FullName != "changkun/wallfacer" || repos[0].Owner != "changkun" ||
		repos[0].DefaultBranch != "main" || !repos[0].Private || repos[0].PushedAt.IsZero() {
		t.Errorf("personal repo projection wrong: %+v", repos[0])
	}
	// org repo from the second installation
	if repos[1].FullName != "latere-ai/infra" {
		t.Errorf("org repo wrong: %+v", repos[1])
	}
}

// Repos reachable through more than one installation are deduped by full name.
func TestListInstallationRepos_DedupesAcrossInstalls(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user/installations") {
			_, _ = w.Write([]byte(`{"installations":[{"id":1},{"id":2}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"repositories":[{"name":"x","full_name":"o/x","owner":{"login":"o"}}]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	repos, err := ListInstallationRepos(context.Background(), c, liveToken())
	if err != nil {
		t.Fatalf("ListInstallationRepos: %v", err)
	}
	if len(repos) != 1 {
		t.Errorf("expected dedup to 1 repo, got %d", len(repos))
	}
}

// Per-installation repo pagination is followed.
func TestListInstallationRepos_FollowsPagination(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/user/installations") {
			_, _ = w.Write([]byte(`{"installations":[{"id":1}]}`))
			return
		}
		if r.URL.Query().Get("page") == "2" {
			_, _ = w.Write([]byte(`{"repositories":[{"name":"b","full_name":"o/b","owner":{"login":"o"}}]}`))
			return
		}
		w.Header().Set("Link", fmt.Sprintf(`<%s/user/installations/1/repositories?page=2>; rel="next"`, srv.URL))
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

func TestListInstallationRepos_NoInstallationsIsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"installations":[]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	repos, err := ListInstallationRepos(context.Background(), c, liveToken())
	if err != nil {
		t.Fatalf("ListInstallationRepos: %v", err)
	}
	if len(repos) != 0 {
		t.Errorf("expected no repos, got %+v", repos)
	}
}
