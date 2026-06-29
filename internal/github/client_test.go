package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func liveToken() *Token {
	return &Token{AccessToken: "ghu_test", Expiry: time.Now().Add(time.Hour)}
}

func TestClient_Do_AttachesAuthAndHeaders(t *testing.T) {
	var gotAuth, gotAccept, gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		gotVersion = r.Header.Get("X-GitHub-Api-Version")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	resp, err := c.Do(context.Background(), liveToken(), http.MethodGet, "/user", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotAuth != "Bearer ghu_test" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotAccept != "application/vnd.github+json" {
		t.Errorf("Accept = %q", gotAccept)
	}
	if gotVersion != apiVersion {
		t.Errorf("X-GitHub-Api-Version = %q, want %q", gotVersion, apiVersion)
	}
	if string(resp.Body) != `{"ok":true}` {
		t.Errorf("body = %q", resp.Body)
	}
}

func TestClient_Do_RejectsInvalidToken(t *testing.T) {
	c := &Client{}
	_, err := c.Do(context.Background(), &Token{}, http.MethodGet, "/user", nil)
	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("Do with invalid token = %v, want ErrNotConnected", err)
	}
}

func TestClient_Do_ParsesRateLimit(t *testing.T) {
	reset := time.Now().Add(time.Hour).Unix()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Limit", "5000")
		w.Header().Set("X-RateLimit-Remaining", "4987")
		w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(reset, 10))
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	resp, err := c.Do(context.Background(), liveToken(), http.MethodGet, "/x", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.RateLimit.Limit != 5000 || resp.RateLimit.Remaining != 4987 {
		t.Errorf("rate limit = %+v", resp.RateLimit)
	}
	if resp.RateLimit.Reset.Unix() != reset {
		t.Errorf("reset = %v, want unix %d", resp.RateLimit.Reset, reset)
	}
}

func TestClient_Do_ParsesNextPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://api.github.com/repositories?page=2>; rel="next", <https://api.github.com/repositories?page=9>; rel="last"`)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	resp, err := c.Do(context.Background(), liveToken(), http.MethodGet, "/x", nil)
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if resp.NextPage != "https://api.github.com/repositories?page=2" {
		t.Errorf("NextPage = %q", resp.NextPage)
	}
}

func TestClient_Do_NoNextPageOnLastLink(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Link", `<https://api.github.com/x?page=1>; rel="prev"`)
		_, _ = w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	resp, _ := c.Do(context.Background(), liveToken(), http.MethodGet, "/x", nil)
	if resp.NextPage != "" {
		t.Errorf("NextPage = %q, want empty", resp.NextPage)
	}
}

func TestClient_Do_StatusToTypedError(t *testing.T) {
	tests := []struct {
		name      string
		status    int
		remaining string
		sentinel  error
	}{
		{"401 unauthorized", http.StatusUnauthorized, "100", ErrUnauthorized},
		{"404 not found", http.StatusNotFound, "100", ErrNotFound},
		{"403 org boundary", http.StatusForbidden, "100", ErrForbidden},
		{"403 primary rate limit", http.StatusForbidden, "0", ErrRateLimited},
		{"429 secondary limit", http.StatusTooManyRequests, "0", ErrRateLimited},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("X-RateLimit-Remaining", tt.remaining)
				w.WriteHeader(tt.status)
				_, _ = fmt.Fprintf(w, `{"message":"boom %d"}`, tt.status)
			}))
			defer srv.Close()

			c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
			_, err := c.Do(context.Background(), liveToken(), http.MethodGet, "/x", nil)
			if !errors.Is(err, tt.sentinel) {
				t.Errorf("err = %v, want Is(%v)", err, tt.sentinel)
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.StatusCode != tt.status {
				t.Errorf("want *APIError status %d, got %v", tt.status, err)
			}
		})
	}
}

// A 403 that is a rate limit must not also match ErrForbidden, and vice versa,
// so handlers can branch cleanly on one or the other.
func TestAPIError_403IsExclusive(t *testing.T) {
	rateLimited := &APIError{StatusCode: http.StatusForbidden, RateLimit: RateLimit{Remaining: 0}}
	if !errors.Is(rateLimited, ErrRateLimited) || errors.Is(rateLimited, ErrForbidden) {
		t.Error("403 with 0 remaining should be ErrRateLimited only")
	}
	boundary := &APIError{StatusCode: http.StatusForbidden, RateLimit: RateLimit{Remaining: 42}}
	if !errors.Is(boundary, ErrForbidden) || errors.Is(boundary, ErrRateLimited) {
		t.Error("403 with remaining budget should be ErrForbidden only")
	}
}
