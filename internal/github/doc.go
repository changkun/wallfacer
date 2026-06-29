// Package github holds wallfacer's GitHub integration: the credential model
// for the brokered "Latere AI" GitHub App token, a principal-scoped token
// store, and (in later commits) the authenticated API client and read/write
// surfaces over api.github.com.
//
// The token this package stores is a distinct credential from the latere.ai
// OIDC identity token (see [latere.ai/x/wallfacer/internal/handler] device
// auth): it is a GitHub App user-to-server credential granting repo-scoped
// access (contents, pull_requests, issues, metadata). It is obtained by
// brokering through the ../auth service rather than a host `gh` login, so it
// works headless and in cloud. This package consumes that brokered token; it
// does not register the GitHub App or run the install flow (that is ../auth's
// responsibility).
//
// # Connected packages
//
// The token model and [Store] are the foundation every other GitHub feature
// builds on (repo selection, PR/issue read, PR/comment write). The HTTP
// handlers in [latere.ai/x/wallfacer/internal/handler] mount the
// /api/github/* routes over this package; [latere.ai/x/wallfacer/internal/auth]
// supplies the principal a token is scoped to.
//
// # Storage
//
// [FileStore] is the local/self-hosted backend, keeping one credential file
// per principal under a directory, mode 0600 (mirroring the authkit file token
// store conventions). A durable [Store] over the Postgres coordination plane
// is added when cloud/multi-user holds tokens for many principals.
package github
