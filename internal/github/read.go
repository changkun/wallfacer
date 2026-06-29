package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// The read surface (spec: github-integration component 3, read-surface) lists
// pull requests and issues and opens their detail with comment threads. Lists
// are plain REST; the detail composite fetches the item plus its conversation
// comments. Review comments (line-anchored, pulls/{n}/comments) are a follow-up.

// PullRequest is a pull request projected to the list/detail fields the UI
// renders. Body is populated on detail, empty in list views.
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

// Issue is an issue projected to the list/detail fields. GitHub's issues
// endpoint also returns pull requests; those are filtered out (see ListIssues).
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Author    string    `json:"author"`
	Labels    []string  `json:"labels,omitempty"`
	CreatedAt time.Time `json:"created_at,omitzero"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	HTMLURL   string    `json:"html_url,omitempty"`
	Body      string    `json:"body,omitempty"`
}

// Comment is one conversation comment on a PR or issue.
type Comment struct {
	Author    string    `json:"author"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at,omitzero"`
	HTMLURL   string    `json:"html_url,omitempty"`
}

// PullRequestDetail is a PR plus its conversation comment thread.
type PullRequestDetail struct {
	PullRequest
	Comments []Comment `json:"comments"`
}

// IssueDetail is an issue plus its conversation comment thread.
type IssueDetail struct {
	Issue
	Comments []Comment `json:"comments"`
}

// normalizeState maps a UI state filter to a valid GitHub value, defaulting to
// "open". GitHub accepts open|closed|all.
func normalizeState(state string) string {
	switch state {
	case "closed", "all":
		return state
	default:
		return "open"
	}
}

// ListPulls lists pull requests for owner/repo filtered by state.
func ListPulls(ctx context.Context, c *Client, token *Token, owner, repo, state string) ([]PullRequest, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=%s&per_page=50",
		url.PathEscape(owner), url.PathEscape(repo), normalizeState(state))
	resp, err := c.Do(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("github: list pulls: %w", err)
	}
	var raw []prPayload
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, fmt.Errorf("github: decode pulls: %w", err)
	}
	out := make([]PullRequest, 0, len(raw))
	for _, p := range raw {
		out = append(out, p.toPullRequest())
	}
	return out, nil
}

// ListIssues lists issues for owner/repo filtered by state, excluding pull
// requests (GitHub returns PRs from the issues endpoint; items carrying a
// pull_request object are dropped).
func ListIssues(ctx context.Context, c *Client, token *Token, owner, repo, state string) ([]Issue, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues?state=%s&per_page=50",
		url.PathEscape(owner), url.PathEscape(repo), normalizeState(state))
	resp, err := c.Do(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("github: list issues: %w", err)
	}
	var raw []issuePayload
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, fmt.Errorf("github: decode issues: %w", err)
	}
	out := make([]Issue, 0, len(raw))
	for _, i := range raw {
		if i.PullRequest != nil {
			continue // a PR surfaced via the issues endpoint
		}
		out = append(out, i.toIssue())
	}
	return out, nil
}

// GetPull returns a pull request and its conversation comments.
func GetPull(ctx context.Context, c *Client, token *Token, owner, repo string, number int) (*PullRequestDetail, error) {
	path := fmt.Sprintf("/repos/%s/%s/pulls/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	resp, err := c.Do(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("github: get pull: %w", err)
	}
	var p prPayload
	if err := json.Unmarshal(resp.Body, &p); err != nil {
		return nil, fmt.Errorf("github: decode pull: %w", err)
	}
	comments, err := listComments(ctx, c, token, owner, repo, number)
	if err != nil {
		return nil, err
	}
	return &PullRequestDetail{PullRequest: p.toPullRequest(), Comments: comments}, nil
}

// GetIssue returns an issue and its conversation comments.
func GetIssue(ctx context.Context, c *Client, token *Token, owner, repo string, number int) (*IssueDetail, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d", url.PathEscape(owner), url.PathEscape(repo), number)
	resp, err := c.Do(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("github: get issue: %w", err)
	}
	var i issuePayload
	if err := json.Unmarshal(resp.Body, &i); err != nil {
		return nil, fmt.Errorf("github: decode issue: %w", err)
	}
	comments, err := listComments(ctx, c, token, owner, repo, number)
	if err != nil {
		return nil, err
	}
	return &IssueDetail{Issue: i.toIssue(), Comments: comments}, nil
}

// listComments fetches the conversation comments shared by PRs and issues (both
// live under the issues/{n}/comments endpoint).
func listComments(ctx context.Context, c *Client, token *Token, owner, repo string, number int) ([]Comment, error) {
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments?per_page=100",
		url.PathEscape(owner), url.PathEscape(repo), number)
	resp, err := c.Do(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, fmt.Errorf("github: list comments: %w", err)
	}
	var raw []commentPayload
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, fmt.Errorf("github: decode comments: %w", err)
	}
	out := make([]Comment, 0, len(raw))
	for _, cm := range raw {
		out = append(out, Comment{
			Author:    cm.User.Login,
			Body:      cm.Body,
			CreatedAt: parseGitHubTime(cm.CreatedAt),
			HTMLURL:   cm.HTMLURL,
		})
	}
	return out, nil
}

// Wire payload shapes. Kept private; the exported models above are the stable
// projection the handlers and UI consume.

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

type issuePayload struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	State       string `json:"state"`
	Body        string `json:"body"`
	HTMLURL     string `json:"html_url"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

func (i issuePayload) toIssue() Issue {
	labels := make([]string, 0, len(i.Labels))
	for _, l := range i.Labels {
		labels = append(labels, l.Name)
	}
	return Issue{
		Number:    i.Number,
		Title:     i.Title,
		State:     i.State,
		Author:    i.User.Login,
		Labels:    labels,
		Body:      i.Body,
		HTMLURL:   i.HTMLURL,
		CreatedAt: parseGitHubTime(i.CreatedAt),
		UpdatedAt: parseGitHubTime(i.UpdatedAt),
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
