package auth

import (
	"testing"

	"latere.ai/x/pkg/authkit"
	"latere.ai/x/pkg/authkit/conformance"
	"latere.ai/x/pkg/authkit/jwt"
	"latere.ai/x/pkg/authkit/oidc"
)

// TestConformance runs the family's rule R2 against the validator wallfacer
// installs in cloud mode: wallfacer is admitted and nothing else, and the
// issuer is called for its key set alone.
func TestConformance(t *testing.T) {
	conformance.Run(t, conformance.Service{
		Audience: "wallfacer",
		New: func(_ testing.TB, issuerURL, jwksURL string) authkit.Authenticator {
			return jwt.NewAuthenticator(BuildValidator(oidc.Config{AuthURL: issuerURL}, jwksURL, issuerURL, "wallfacer"))
		},
	})
}
