package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreatePull_Creates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/repos/o/repo/pulls") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"number":7,"title":"T","state":"open","html_url":"https://github.com/o/repo/pull/7","user":{"login":"me"}}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	pr, err := CreatePull(context.Background(), c, liveToken(), "o", "repo",
		CreatePullParams{Title: "T", Body: "B", Head: "feature", Base: "main"})
	if err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	if pr.Number != 7 || pr.HTMLURL != "https://github.com/o/repo/pull/7" {
		t.Errorf("pr = %+v", pr)
	}
}

// On 422 (PR already exists), CreatePull returns the existing open PR instead
// of erroring.
func TestCreatePull_ExistingReturnsOpenPR(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"A pull request already exists for o:feature."}`))
			return
		}
		// list open pulls by head
		_, _ = w.Write([]byte(`[{"number":42,"title":"Existing","state":"open","html_url":"https://github.com/o/repo/pull/42","user":{"login":"me"}}]`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	pr, err := CreatePull(context.Background(), c, liveToken(), "o", "repo",
		CreatePullParams{Title: "T", Head: "feature", Base: "main"})
	if err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	if pr.Number != 42 {
		t.Errorf("expected existing PR #42, got %+v", pr)
	}
}

// A 422 with no findable existing PR still surfaces as an error.
func TestCreatePull_422NoExistingErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"message":"Validation failed"}`))
			return
		}
		_, _ = w.Write([]byte(`[]`)) // no open PR
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := CreatePull(context.Background(), c, liveToken(), "o", "repo",
		CreatePullParams{Head: "feature", Base: "main"})
	if err == nil {
		t.Fatal("expected error on 422 with no existing PR")
	}
}

func TestCreateComment_Posts(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/issues/42/comments") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"body":"nice work","user":{"login":"me"},"html_url":"https://github.com/o/repo/pull/42#c1"}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	cm, err := CreateComment(context.Background(), c, liveToken(), "o", "repo", 42, "nice work")
	if err != nil {
		t.Fatalf("CreateComment: %v", err)
	}
	if cm.Author != "me" || cm.Body != "nice work" {
		t.Errorf("comment = %+v", cm)
	}
	if !strings.Contains(gotBody, "nice work") {
		t.Errorf("request body = %q", gotBody)
	}
}

func TestCreateComment_PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"message":"no"}`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := CreateComment(context.Background(), c, liveToken(), "o", "r", 1, "x"); err == nil {
		t.Fatal("expected error")
	}
}
