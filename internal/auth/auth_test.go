package auth_test

import (
	"testing"

	"changkun.de/x/wallfacer/internal/auth"
)

// TestNew_EmptyConfigReturnsNil confirms the graceful-degrade contract we
// depend on: auth.New with an empty Config must return nil, so callers can
// distinguish "cloud mode off / missing env" from "valid client" with a
// simple nil check at every request boundary.
func TestNew_EmptyConfigReturnsNil(t *testing.T) {
	if c := auth.New(auth.Config{}); c != nil {
		t.Fatalf("auth.New(empty Config) = %v, want nil", c)
	}
}
