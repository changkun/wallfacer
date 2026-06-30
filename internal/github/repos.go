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
// the principal, across all of the user's installations (personal + every org
// they installed on). It uses the user-to-server endpoints -- /user/installations
// then /user/installations/{id}/repositories -- which are the correct ones for
// the brokered user token (the installation-scoped /installation/repositories
// requires an installation token and is not usable here). The install grant per
// account is the org boundary; results are deduped by full name.
func ListInstallationRepos(ctx context.Context, c *Client, token *Token) ([]Repo, error) {
	installs, err := listUserInstallations(ctx, c, token)
	if err != nil {
		return nil, err
	}
	var repos []Repo
	seen := map[string]bool{}
	for _, id := range installs {
		rs, err := listUserInstallationRepos(ctx, c, token, id)
		if err != nil {
			return nil, err
		}
		for _, r := range rs {
			if r.FullName == "" || seen[r.FullName] {
				continue
			}
			seen[r.FullName] = true
			repos = append(repos, r)
		}
	}
	return repos, nil
}

// listUserInstallations returns the installation ids the user token can access
// (one per account the "Latere AI" app is installed on).
func listUserInstallations(ctx context.Context, c *Client, token *Token) ([]int64, error) {
	var ids []int64
	path := "/user/installations?per_page=100"
	for page := 0; page < maxRepoPages && path != ""; page++ {
		resp, err := c.Do(ctx, token, http.MethodGet, path, nil)
		if err != nil {
			return nil, fmt.Errorf("github: list user installations: %w", err)
		}
		var decoded struct {
			Installations []struct {
				ID int64 `json:"id"`
			} `json:"installations"`
		}
		if err := json.Unmarshal(resp.Body, &decoded); err != nil {
			return nil, fmt.Errorf("github: decode user installations: %w", err)
		}
		for _, inst := range decoded.Installations {
			ids = append(ids, inst.ID)
		}
		path = resp.NextPage
	}
	return ids, nil
}

// listUserInstallationRepos lists the repos one installation grants the user.
func listUserInstallationRepos(ctx context.Context, c *Client, token *Token, installID int64) ([]Repo, error) {
	var repos []Repo
	path := fmt.Sprintf("/user/installations/%d/repositories?per_page=100", installID)
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
