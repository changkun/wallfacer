// Principal translation: turn validated auth.Claims in the request
// context into a store.Principal the domain layer can use. Kept as a
// one-function helper so every CreateTask / list / workspace-write
// site reads the same single line, and the store package never has
// to import internal/auth.

package handler

import (
	"net/http"

	"changkun.de/x/wallfacer/internal/auth"
	"changkun.de/x/wallfacer/internal/store"
)

// principalFromRequest returns the caller's store.Principal when the
// request was authenticated by cloud-mode middleware, or nil for
// anonymous callers. Nil matches today's local-mode behavior on the
// TasksForPrincipal filter: "unfiltered".
func principalFromRequest(r *http.Request) *store.Principal {
	c, ok := auth.PrincipalFromContext(r.Context())
	if !ok || c == nil {
		return nil
	}
	return &store.Principal{
		Sub:   c.Sub,
		OrgID: c.OrgID,
	}
}
