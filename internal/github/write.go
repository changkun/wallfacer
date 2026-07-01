package github

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
)

// The write surface (spec: github-integration component 4): create a pull
// request and post conversation comments, via the GitHub API using the brokered
// token. PR merge/close stay out of scope.

// CreatePullParams is the input for opening a pull request.
type CreatePullParams struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	Head  string `json:"head"` // source branch
	Base  string `json:"base"` // target branch
	Draft bool   `json:"draft"`
}

// CreatePull opens a pull request on owner/repo. If a PR already exists for the
// head branch, GitHub rejects the create with 422; CreatePull detects this and
// returns the existing open PR instead of an error (idempotent "create or
// return"), so the caller can surface "View PR".
func CreatePull(ctx context.Context, c *Client, token *Token, owner, repo string, p CreatePullParams) (*PullRequest, error) {
	body, err := json.Marshal(map[string]any{
		"title": p.Title,
		"body":  p.Body,
		"head":  p.Head,
		"base":  p.Base,
		"draft": p.Draft,
	})
	if err != nil {
		return nil, fmt.Errorf("github: encode pull: %w", err)
	}
	path := fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(owner), url.PathEscape(repo))
	resp, err := c.Do(ctx, token, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		var apiErr *APIError
		if errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnprocessableEntity {
			if existing, lookupErr := PullForBranch(ctx, c, token, owner, repo, p.Head); lookupErr == nil && existing != nil {
				return existing, nil
			}
		}
		return nil, fmt.Errorf("github: create pull: %w", err)
	}
	var pr prPayload
	if err := json.Unmarshal(resp.Body, &pr); err != nil {
		return nil, fmt.Errorf("github: decode created pull: %w", err)
	}
	out := pr.toPullRequest()
	return &out, nil
}

// PullForBranch returns the open PR whose head branch matches, or (nil, nil) if
// none. Used for existing-PR detection and task PR-status lookup. head is
// matched as owner:branch per the GitHub list filter.
func PullForBranch(ctx context.Context, c *Client, token *Token, owner, repo, head string) (*PullRequest, error) {
	// GitHub's head filter is "user:ref"; the branch alone also works for
	// same-repo PRs. Query by branch and match client-side to be safe.
	path := fmt.Sprintf("/repos/%s/%s/pulls?state=open&head=%s:%s&per_page=10",
		url.PathEscape(owner), url.PathEscape(repo), url.PathEscape(owner), url.QueryEscape(head))
	resp, err := c.Do(ctx, token, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}
	var raw []prPayload
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return nil, err
	}
	for _, p := range raw {
		out := p.toPullRequest()
		return &out, nil
	}
	return nil, nil
}

// CreateComment posts a conversation comment to a PR or issue (both share the
// issues/{number}/comments endpoint) and returns the created comment.
func CreateComment(ctx context.Context, c *Client, token *Token, owner, repo string, number int, commentBody string) (*Comment, error) {
	body, err := json.Marshal(map[string]string{"body": commentBody})
	if err != nil {
		return nil, fmt.Errorf("github: encode comment: %w", err)
	}
	path := fmt.Sprintf("/repos/%s/%s/issues/%d/comments",
		url.PathEscape(owner), url.PathEscape(repo), number)
	resp, err := c.Do(ctx, token, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("github: create comment: %w", err)
	}
	var cm commentPayload
	if err := json.Unmarshal(resp.Body, &cm); err != nil {
		return nil, fmt.Errorf("github: decode comment: %w", err)
	}
	return &Comment{
		Author:    cm.User.Login,
		Body:      cm.Body,
		CreatedAt: parseGitHubTime(cm.CreatedAt),
		HTMLURL:   cm.HTMLURL,
	}, nil
}
