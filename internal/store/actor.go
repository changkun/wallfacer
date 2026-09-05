// Actor context carries the identity of the caller who triggered a
// store mutation. Handlers at the request boundary stamp the context
// via WithActorPrincipal; background writers (runner goroutines,
// scheduler callbacks) carry no principal and write empty attribution.
// Keeping the carrier in the store package avoids a circular auth →
// store → auth import chain.

package store

import "context"

// ActorType categorizes who caused a store mutation. The string
// values are what lands on TaskEvent.ActorType (see models.go).
type ActorType string

// ActorType values stamped on TaskEvent.ActorType. See the declaration
// of ActorType for the semantic contract shared across all members.
const (
	ActorUser      ActorType = "user"    // human user, JWT principal_type="user"
	ActorService   ActorType = "service" // service account JWT
	ActorAPIKey    ActorType = "apikey"  // WALLFACER_SERVER_API_KEY gated request
	ActorAnonymous ActorType = ""        // local anonymous call (legacy / default)
)

type actorCtxKey struct{}

// actorInfo is what flows through the context. Kept unexported so
// callers go through the With* helpers.
type actorInfo struct {
	Sub  string
	Type ActorType
}

// WithActorPrincipal attaches a caller principal to ctx. Handlers
// call this after resolving the caller's identity, passing the sub and the
// matching ActorType (usually ActorUser for OIDC principals,
// ActorService for service accounts, ActorAPIKey when the request
// was gated only by the static server key).
func WithActorPrincipal(ctx context.Context, sub string, t ActorType) context.Context {
	return context.WithValue(ctx, actorCtxKey{}, actorInfo{Sub: sub, Type: t})
}

// actorFromContext returns (sub, type) strings ready for stamping
// onto a TaskEvent. Zero value when no actor was attached, which the
// caller then writes as empty strings (matching legacy behavior).
func actorFromContext(ctx context.Context) (string, string) {
	if ctx == nil {
		return "", ""
	}
	a, ok := ctx.Value(actorCtxKey{}).(actorInfo)
	if !ok {
		return "", ""
	}
	return a.Sub, string(a.Type)
}
