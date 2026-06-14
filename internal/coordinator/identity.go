package coordinator

import (
	"strings"
)

// NormalizeRemoteURL canonicalizes a git remote URL into the cross-machine
// workspace join key. Two teammates cloning the same repo over different
// transports (SSH vs HTTPS) or with a trailing .git must resolve to one key, so
// two instances on the same repo join the same workspace presence and
// collaboration even though their local paths (and workspace.GroupKey) differ.
//
// The canonical form is "host/owner/repo": scheme, credentials, port, and a
// trailing .git are stripped and the host is lowercased. The owner/repo path
// case is preserved (host-only lowercasing, per the connection-and-presence
// spec); within a team the clone URL is consistent, so this is a safe key.
//
// An input with no recognizable host (empty, or a bare path) returns "", which
// marks a remote-less workspace that never joins org collaboration.
func NormalizeRemoteURL(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// scp-like syntax: git@github.com:owner/repo.git
	// Detected by a colon that is not part of a scheme "://" and that comes
	// before any slash.
	if !strings.Contains(s, "://") {
		if at := strings.LastIndex(s, "@"); at >= 0 {
			s = s[at+1:] // strip user@
		}
		if host, path, ok := strings.Cut(s, ":"); ok {
			return canonForm(host, path)
		}
		// No scheme and no colon: not a URL we can key on.
		return ""
	}

	// URL syntax: <scheme>://[user[:pass]@]host[:port]/owner/repo[.git]
	_, s, _ = strings.Cut(s, "://") // "://" present in this branch
	if at := strings.LastIndex(s, "@"); at >= 0 {
		s = s[at+1:] // strip credentials
	}
	hostPort, path, ok := strings.Cut(s, "/")
	if !ok {
		return ""
	}
	host := hostPort
	if h, _, ok := strings.Cut(hostPort, ":"); ok {
		host = h // strip port
	}
	return canonForm(host, path)
}

// canonForm assembles "host/path" with the host lowercased and the path
// trimmed of leading/trailing slashes and a trailing .git.
func canonForm(host, path string) string {
	host = strings.ToLower(strings.TrimSpace(host))
	path = strings.Trim(strings.TrimSpace(path), "/")
	path = strings.TrimSuffix(path, ".git")
	path = strings.Trim(path, "/")
	if host == "" || path == "" {
		return ""
	}
	return host + "/" + path
}
