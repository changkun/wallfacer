package adversarial_test

import (
	"context"
	"testing"

	wadversarial "latere.ai/x/wallfacer/internal/adversarial"
	"latere.ai/x/agon/pkg/adversarial"
)

func TestNoopVerifier(t *testing.T) {
	var v adversarial.Verifier = wadversarial.NoopVerifier{}
	res, err := v.Verify(context.Background(), adversarial.VerifyInput{
		TaskPrompt: "add auth",
		SessionID:  "s-abc",
		DiffPatch:  "+x := 1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != nil {
		t.Fatalf("expected nil result from noop, got %+v", res)
	}
}
