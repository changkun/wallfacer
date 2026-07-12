package handler

import (
	"testing"

	"latere.ai/x/pkg/jwtauth"
	"latere.ai/x/wallfacer/internal/auth"
)

// dr-21 regression: auth mints delegated tokens with the flat grantor_id
// claim. delegatorSub used to read only the RFC 8693 act claim, which was
// absent on those tokens, so the fallback attributed the call to the AGENT's
// own sub instead of the delegating owner — the installation lookup then ran
// for the wrong principal.
func TestDelegatorSubGrantorIDOnlyToken(t *testing.T) {
	// Claims exactly as a grantor_id-only wire token parses: GrantorID set,
	// Act nil (the pre-v0.29 jwtauth shape this bug shipped against).
	c := &auth.Claims{
		Sub:           "agent-1",
		PrincipalType: jwtauth.PrincipalAgent,
		GrantorID:     "owner-9",
	}
	if got := delegatorSub(c); got != "owner-9" {
		t.Fatalf("delegatorSub = %q, want owner-9 (delegated call must attribute to the owner, not the agent)", got)
	}
}

func TestDelegatorSubShapes(t *testing.T) {
	cases := []struct {
		name string
		c    *auth.Claims
		want string
	}{
		{"nil claims", nil, ""},
		{"act-shaped token", &auth.Claims{Sub: "agent-1", Act: &jwtauth.ActClaims{Sub: "owner-7"}}, "owner-7"},
		{"non-delegated user: principal IS the user", &auth.Claims{Sub: "user-1"}, "user-1"},
	}
	for _, tc := range cases {
		if got := delegatorSub(tc.c); got != tc.want {
			t.Errorf("%s: delegatorSub = %q, want %q", tc.name, got, tc.want)
		}
	}
}
