// Package auth re-exports latere.ai/x/pkg/oidc so internal packages import a
// single path. Nil-safety semantics come from the platform package: oidc.New
// returns nil when the required configuration is missing, which handlers
// treat as "auth not configured" and short-circuit accordingly.
package auth

import "latere.ai/x/pkg/oidc"

// Client is an OIDC Relying Party bound to the latere.ai auth service.
type Client = oidc.Client

// Config holds the OIDC integration configuration sourced from AUTH_* env vars.
type Config = oidc.Config

// User is the subset of OIDC userinfo we surface to the UI.
type User = oidc.User

// Session holds the tokens stored in the encrypted session cookie.
type Session = oidc.Session

// LoadConfig reads the OIDC configuration from environment variables.
var LoadConfig = oidc.LoadConfig

// New constructs a Client. Returns nil when the configuration is missing
// required fields, which callers treat as "auth not configured".
var New = oidc.New

// ClearSession clears the encrypted session cookie on the response.
var ClearSession = oidc.ClearSession
