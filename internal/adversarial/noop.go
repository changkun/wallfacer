// Package adversarial wires wallfacer's harnesses into review's adversarial
// debate protocol. It provides implementations of [adversarial.Verifier]
// — the review-owned integration interface — backed by wallfacer's runner.
//
// The no-op path ([NoopVerifier]) is always available and is the default
// when review is toggled off. The review-backed path ([ReviewVerifier]) is
// constructed when the reviewEnabled handler flag is set and requires the
// task to have a non-nil SessionID.
package adversarial

import (
	"context"

	"latere.ai/x/topos/adversarial"
)

// NoopVerifier satisfies [adversarial.Verifier] and returns (nil, nil)
// immediately. It is the active implementation when review is disabled.
type NoopVerifier struct{}

// Verify returns (nil, nil) — the skip path.
func (NoopVerifier) Verify(_ context.Context, _ adversarial.VerifyInput) (*adversarial.VerifyResult, error) {
	return nil, nil
}

// compile-time interface check
var _ adversarial.Verifier = NoopVerifier{}
