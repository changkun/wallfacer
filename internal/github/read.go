package github

import "time"

// Shared GitHub resource models for the write surface (spec: github-integration
// component 4). The standalone read/browse (PR/issue lists + detail) was removed
// in the task-centric redesign; only the projections the write surface returns
// remain -- a created/looked-up pull request and a posted comment.

// PullRequest is a pull request projected to the fields the task UI renders.
type PullRequest struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Author    string    `json:"author"`
	Draft     bool      `json:"draft"`
	CreatedAt time.Time `json:"created_at,omitzero"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	HTMLURL   string    `json:"html_url,omitempty"`
	Body      string    `json:"body,omitempty"`
}

// Comment is one conversation comment on a PR.
type Comment struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at,omitzero"`
	HTMLURL   string    `json:"html_url,omitempty"`
}

type prPayload struct {
	Number    int    `json:"number"`
	Title     string `json:"title"`
	State     string `json:"state"`
	Draft     bool   `json:"draft"`
	Body      string `json:"body"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

func (p prPayload) toPullRequest() PullRequest {
	return PullRequest{
		Number:    p.Number,
		Title:     p.Title,
		State:     p.State,
		Author:    p.User.Login,
		Draft:     p.Draft,
		Body:      p.Body,
		HTMLURL:   p.HTMLURL,
		CreatedAt: parseGitHubTime(p.CreatedAt),
		UpdatedAt: parseGitHubTime(p.UpdatedAt),
	}
}

type commentPayload struct {
	Body      string `json:"body"`
	HTMLURL   string `json:"html_url"`
	CreatedAt string `json:"created_at"`
	User      struct {
		Login string `json:"login"`
	} `json:"user"`
}

// parseGitHubTime parses a GitHub RFC 3339 timestamp, returning the zero time
// for empty or malformed values (timestamps are display-only metadata).
func parseGitHubTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
