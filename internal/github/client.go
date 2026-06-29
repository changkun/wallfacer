package github

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the GitHub REST API root. Overridable on [Client] so tests
// point at an httptest server and self-hosted GHES can target its API host.
const DefaultBaseURL = "https://api.github.com"

// apiVersion pins the GitHub REST API version. GitHub honors the
// X-GitHub-Api-Version header and warns on omission; pinning avoids silent
// behavior drift when the default version advances.
const apiVersion = "2022-11-28"

// Client is the shared authenticated transport over the GitHub API: it attaches
// the bearer token and standard headers, parses rate-limit headers, and maps
// status codes to typed errors. The repo-selection and read/write surfaces
// build their resource calls on top of it rather than re-deriving the transport.
type Client struct {
	// BaseURL is the API root; empty means [DefaultBaseURL].
	BaseURL string
	// HTTP is the underlying client; nil means a client with a sane timeout.
	HTTP *http.Client
}

func (c *Client) baseURL() string {
	if c.BaseURL == "" {
		return DefaultBaseURL
	}
	return strings.TrimRight(c.BaseURL, "/")
}

func (c *Client) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

// RateLimit is the GitHub rate-limit budget parsed from response headers. It is
// surfaced to the UI so a near-exhausted budget can disable polling/refresh
// before a hard 403. Reset is zero when the headers are absent.
type RateLimit struct {
	Limit     int
	Remaining int
	Reset     time.Time
}

// Response carries the decoded body bytes plus the cross-cutting metadata
// (status, rate limit, next-page cursor) every resource call needs.
type Response struct {
	StatusCode int
	Body       []byte
	RateLimit  RateLimit
	// NextPage is the URL of the next page from the Link header, or "" when
	// there is no further page.
	NextPage string
}

// APIError is a non-2xx GitHub response mapped to a Go error. Callers branch on
// the sentinels ([ErrUnauthorized], [ErrRateLimited], [ErrForbidden],
// [ErrNotFound]) via errors.Is.
type APIError struct {
	StatusCode int
	Message    string
	RateLimit  RateLimit
}

func (e *APIError) Error() string {
	return fmt.Sprintf("github: api status %d: %s", e.StatusCode, e.Message)
}

// Sentinels for the status conditions handlers must distinguish. APIError
// matches the relevant one through Is so callers can use errors.Is.
var (
	// ErrUnauthorized is a 401: token missing/expired -> refresh or reconnect.
	ErrUnauthorized = errors.New("github: unauthorized")
	// ErrForbidden is a 403 that is not a rate limit (e.g. org boundary).
	ErrForbidden = errors.New("github: forbidden")
	// ErrRateLimited is a primary or secondary rate-limit rejection -> back off.
	ErrRateLimited = errors.New("github: rate limited")
	// ErrNotFound is a 404.
	ErrNotFound = errors.New("github: not found")
)

// Is matches an APIError against the package status sentinels so callers can
// use errors.Is. A 403 maps to ErrRateLimited when the budget is exhausted and
// to ErrForbidden otherwise (the two are mutually exclusive).
func (e *APIError) Is(target error) bool {
	switch target {
	case ErrUnauthorized:
		return e.StatusCode == http.StatusUnauthorized
	case ErrRateLimited:
		return e.StatusCode == http.StatusTooManyRequests ||
			(e.StatusCode == http.StatusForbidden && e.RateLimit.Remaining == 0)
	case ErrForbidden:
		return e.StatusCode == http.StatusForbidden && e.RateLimit.Remaining != 0
	case ErrNotFound:
		return e.StatusCode == http.StatusNotFound
	}
	return false
}

// Do issues an authenticated request to path (absolute URL or a path relative
// to BaseURL) with the given token, returning the decoded [Response]. A non-2xx
// status yields an [*APIError] (still carrying the parsed rate limit).
func (c *Client) Do(ctx context.Context, token *Token, method, path string, body io.Reader) (*Response, error) {
	if !token.Valid() {
		return nil, ErrNotConnected
	}
	url := path
	if !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		url = c.baseURL() + path
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("github: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token.AccessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("github: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("github: read body: %w", err)
	}
	rl := parseRateLimit(resp.Header)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{
			StatusCode: resp.StatusCode,
			Message:    apiMessage(data),
			RateLimit:  rl,
		}
	}
	return &Response{
		StatusCode: resp.StatusCode,
		Body:       data,
		RateLimit:  rl,
		NextPage:   nextPageFromLink(resp.Header.Get("Link")),
	}, nil
}

// apiMessage extracts GitHub's {"message": "..."} error text from a response
// body, falling back to a truncated raw body when it is not the expected shape.
func apiMessage(data []byte) string {
	var payload struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &payload); err == nil && payload.Message != "" {
		return payload.Message
	}
	s := strings.TrimSpace(string(data))
	if len(s) > 200 {
		s = s[:200]
	}
	if s == "" {
		return "(no body)"
	}
	return s
}

// parseRateLimit reads the X-RateLimit-* headers into a RateLimit. Missing or
// malformed headers leave the corresponding field at its zero value.
func parseRateLimit(h http.Header) RateLimit {
	var rl RateLimit
	if v, err := strconv.Atoi(h.Get("X-RateLimit-Limit")); err == nil {
		rl.Limit = v
	}
	if v, err := strconv.Atoi(h.Get("X-RateLimit-Remaining")); err == nil {
		rl.Remaining = v
	}
	if v, err := strconv.ParseInt(h.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		rl.Reset = time.Unix(v, 0).UTC()
	}
	return rl
}

// nextPageFromLink extracts the rel="next" URL from a GitHub Link header, or ""
// when there is no next page. The header looks like:
//
//	<https://api.github.com/...&page=2>; rel="next", <...&page=5>; rel="last"
func nextPageFromLink(link string) string {
	if link == "" {
		return ""
	}
	for part := range strings.SplitSeq(link, ",") {
		segs := strings.Split(strings.TrimSpace(part), ";")
		if len(segs) < 2 {
			continue
		}
		isNext := false
		for _, attr := range segs[1:] {
			if strings.Contains(attr, `rel="next"`) {
				isNext = true
				break
			}
		}
		if !isNext {
			continue
		}
		url := strings.TrimSpace(segs[0])
		url = strings.TrimPrefix(url, "<")
		url = strings.TrimSuffix(url, ">")
		return url
	}
	return ""
}
