package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// maxRepoPages bounds the installation-repo pagination so a pathological
// install (or a mock that never stops paging) cannot loop unbounded. At 100
// repos/page this covers very large installs; the UI's search narrows beyond it.
const maxRepoPages = 20

// Repo is a GitHub repository the authenticated install can access, projected
// to the fields the picker and identity resolution need. FullName is the
// canonical "owner/repo"; Owner is the org/user login.
type Repo struct {
	Owner         string    `json:"owner"`
	Name          string    `json:"name"`
	FullName      string    `json:"full_name"`
	DefaultBranch string    `json:"default_branch"`
	Private       bool      `json:"private"`
	PushedAt      time.Time `json:"pushed_at,omitzero"`
	HTMLURL       string    `json:"html_url,omitempty"`
}

// installationReposPage is the GitHub /installation/repositories response shape.
// The endpoint wraps the repo array in an object (unlike /user/repos), so the
// decode target differs from a bare list.
type installationReposPage struct {
	Repositories []struct {
		Name          string `json:"name"`
		FullName      string `json:"full_name"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
		HTMLURL       string `json:"html_url"`
		PushedAt      string `json:"pushed_at"`
		Owner         struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repositories"`
}

// ListInstallationRepos returns every repository the "Latere AI" install grants
// the principal, following pagination. The install grant is the org boundary:
// repos outside what the org admin approved are not returned, so no client-side
// org filtering is applied here.
func ListInstallationRepos(ctx context.Context, c *Client, token *Token) ([]Repo, error) {
	var repos []Repo
	path := "/installation/repositories?per_page=100"
	for page := 0; page < maxRepoPages && path != ""; page++ {
		resp, err := c.Do(ctx, token, http.MethodGet, path, nil)
		if err != nil {
			return nil, fmt.Errorf("github: list installation repos: %w", err)
		}
		var decoded installationReposPage
		if err := json.Unmarshal(resp.Body, &decoded); err != nil {
			return nil, fmt.Errorf("github: decode installation repos: %w", err)
		}
		for _, r := range decoded.Repositories {
			repos = append(repos, Repo{
				Owner:         r.Owner.Login,
				Name:          r.Name,
				FullName:      r.FullName,
				DefaultBranch: r.DefaultBranch,
				Private:       r.Private,
				HTMLURL:       r.HTMLURL,
				PushedAt:      parseGitHubTime(r.PushedAt),
			})
		}
		path = resp.NextPage
	}
	return repos, nil
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
