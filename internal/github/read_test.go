package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListPulls_DecodesAndDefaultsState(t *testing.T) {
	var gotState string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotState = r.URL.Query().Get("state")
		_, _ = w.Write([]byte(`[{"number":42,"title":"Add revert","state":"open","draft":false,"user":{"login":"octocat"}}]`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	pulls, err := ListPulls(context.Background(), c, liveToken(), "latere", "wallfacer", "")
	if err != nil {
		t.Fatalf("ListPulls: %v", err)
	}
	if gotState != "open" {
		t.Errorf("default state = %q, want open", gotState)
	}
	if len(pulls) != 1 || pulls[0].Number != 42 || pulls[0].Author != "octocat" {
		t.Errorf("pulls = %+v", pulls)
	}
}

// The issues endpoint also returns PRs; ListIssues must drop items carrying a
// pull_request object so the Issues tab shows only real issues.
func TestListIssues_ExcludesPullRequests(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`[
			{"number":7,"title":"Real issue","state":"open","user":{"login":"a"},"labels":[{"name":"bug"}]},
			{"number":8,"title":"A PR","state":"open","user":{"login":"b"},"pull_request":{"url":"x"}}
		]`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	issues, err := ListIssues(context.Background(), c, liveToken(), "o", "r", "all")
	if err != nil {
		t.Fatalf("ListIssues: %v", err)
	}
	if len(issues) != 1 || issues[0].Number != 7 {
		t.Fatalf("issues = %+v, want only #7", issues)
	}
	if len(issues[0].Labels) != 1 || issues[0].Labels[0] != "bug" {
		t.Errorf("labels = %v", issues[0].Labels)
	}
}

func TestGetPull_ComposesDetailWithComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/42"):
			_, _ = w.Write([]byte(`{"number":42,"title":"T","state":"open","body":"desc","user":{"login":"octocat"}}`))
		case strings.HasSuffix(r.URL.Path, "/issues/42/comments"):
			_, _ = w.Write([]byte(`[{"body":"nice","user":{"login":"rev"},"created_at":"2026-06-20T10:00:00Z"}]`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	detail, err := GetPull(context.Background(), c, liveToken(), "o", "r", 42)
	if err != nil {
		t.Fatalf("GetPull: %v", err)
	}
	if detail.Number != 42 || detail.Body != "desc" {
		t.Errorf("pr detail = %+v", detail.PullRequest)
	}
	if len(detail.Comments) != 1 || detail.Comments[0].Author != "rev" {
		t.Errorf("comments = %+v", detail.Comments)
	}
}

func TestGetIssue_ComposesDetailWithComments(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/comments") {
			_, _ = w.Write([]byte(`[{"body":"+1","user":{"login":"x"}}]`))
			return
		}
		_, _ = w.Write([]byte(`{"number":7,"title":"I","state":"open","body":"b","user":{"login":"a"}}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	detail, err := GetIssue(context.Background(), c, liveToken(), "o", "r", 7)
	if err != nil {
		t.Fatalf("GetIssue: %v", err)
	}
	if detail.Number != 7 || len(detail.Comments) != 1 {
		t.Errorf("issue detail = %+v comments %+v", detail.Issue, detail.Comments)
	}
}

func TestNormalizeState(t *testing.T) {
	for in, want := range map[string]string{"": "open", "open": "open", "closed": "closed", "all": "all", "bogus": "open"} {
		if got := normalizeState(in); got != want {
			t.Errorf("normalizeState(%q) = %q, want %q", in, got, want)
		}
	}
}
