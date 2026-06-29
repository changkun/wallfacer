package github

import "time"

// expiryLeeway treats a token as expired slightly before its real expiry so a
// caller that checks Expired and then makes a request does not race the
// boundary. Matches the conservative skew oauth2.Token uses internally.
const expiryLeeway = 30 * time.Second

// Token is a brokered "Latere AI" GitHub App user-to-server credential plus the
// GitHub-side metadata wallfacer needs to render auth status and scope repo
// access. It is the credential persisted by a [Store].
//
// AccessToken acts as the user for reads/writes; RefreshToken renews it when it
// expires (GitHub App user tokens are short-lived). The metadata fields
// (Login, InstallationID, Account, Permissions) describe the install the token
// came from and are surfaced on /api/config so the UI can show the connected
// state without an extra round trip.
type Token struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitzero"`

	// Login is the authenticated GitHub user login the token acts as.
	Login string `json:"login,omitempty"`
	// InstallationID is the GitHub App installation the grant belongs to.
	InstallationID int64 `json:"installation_id,omitempty"`
	// Account is the org/owner the "Latere AI" app is installed on.
	Account string `json:"account,omitempty"`
	// Permissions are the granted permission keys (e.g. "contents",
	// "pull_requests", "issues", "metadata").
	Permissions []string `json:"permissions,omitempty"`
}

// Expired reports whether the access token is at or past its expiry (minus a
// small leeway). A zero Expiry means "no known expiry" and is treated as not
// expired, leaving renewal to a 401 from the API.
func (t *Token) Expired() bool {
	if t == nil {
		return true
	}
	if t.Expiry.IsZero() {
		return false
	}
	return !t.Expiry.After(time.Now().Add(expiryLeeway))
}

// Valid reports whether the token carries a usable access token and is not
// expired. It is the cheap precondition a caller checks before an API call.
func (t *Token) Valid() bool {
	return t != nil && t.AccessToken != "" && !t.Expired()
}
