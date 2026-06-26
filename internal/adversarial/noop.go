// Package adversarial wires wallfacer's harnesses into agon's adversarial
// debate protocol. It provides implementations of [adversarial.Verifier]
// — the agon-owned integration interface — backed by wallfacer's runner.
//
// The no-op path ([NoopVerifier]) is always available and is the default
// when agon is toggled off. The agon-backed path ([AgonVerifier]) is
// constructed when the agonEnabled handler flag is set and requires the
// task to have a non-nil SessionID.
package adversarial

import (
	"context"

	"latere.ai/x/agon/pkg/adversarial"
)

// NoopVerifier satisfies [adversarial.Verifier] and returns (nil, nil)
// immediately. It is the active implementation when agon is disabled.
type NoopVerifier struct{}

// Verify returns (nil, nil) — the skip path.
func (NoopVerifier) Verify(_ context.Context, _ adversarial.VerifyInput) (*adversarial.VerifyResult, error) {
	return nil, nil
}

// compile-time interface check
var _ adversarial.Verifier = NoopVerifier{}
