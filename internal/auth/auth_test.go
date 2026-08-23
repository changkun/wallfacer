package auth_test

import (
	"testing"

	"latere.ai/x/pkg/oidc"
)

// TestNew_EmptyConfigReturnsNil confirms the graceful-degrade contract we
// depend on: oidc.New with an empty oidc.Config must return nil, so callers can
// distinguish "cloud mode off / missing env" from "valid client" with a
// simple nil check at every request boundary.
func TestNew_EmptyConfigReturnsNil(t *testing.T) {
	if c := oidc.New(oidc.Config{}); c != nil {
		t.Fatalf("oidc.New(empty oidc.Config) = %v, want nil", c)
	}
}
