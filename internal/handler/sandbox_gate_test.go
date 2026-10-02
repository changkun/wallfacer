package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"latere.ai/x/wallfacer/internal/harness"
)

// TestSandboxUsable_ToposRequiresModelCredential asserts the in-process harness
// is reported unusable, with the fixed sentence, until the env file carries the
// model credential the harness reads, and usable once it does.
func TestSandboxUsable_ToposRequiresModelCredential(t *testing.T) {
	const wantReason = harness.ToposCredentialRequired

	h, envPath := newTestHandlerWithEnv(t)
	ok, reason := h.sandboxUsable(harness.Topos)
	if ok {
		t.Error("topos reported usable with no model credential")
	}
	if reason != wantReason {
		t.Errorf("reason = %q, want %q", reason, wantReason)
	}
	if err := h.validateRequestedSandboxes(harness.Topos, nil); err == nil || err.Error() != wantReason {
		t.Errorf("validateRequestedSandboxes = %v, want %q", err, wantReason)
	}

	if err := os.WriteFile(envPath, []byte("ANTHROPIC_API_KEY=sk-test\n"), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	if ok, reason := h.sandboxUsable(harness.Topos); !ok || reason != "" {
		t.Errorf("with a credential: usable = %v, reason = %q; want usable with no reason", ok, reason)
	}

	// A handler with no env file at all has no credential either.
	if ok, reason := newTestHandler(t).sandboxUsable(harness.Topos); ok || reason != wantReason {
		t.Errorf("without an env file: usable = %v, reason = %q; want unusable with %q", ok, reason, wantReason)
	}
}

// TestGetConfig_ReportsToposUnusableWithoutModelCredential asserts the config
// response the harness pickers read carries the same verdict and sentence.
func TestGetConfig_ReportsToposUnusableWithoutModelCredential(t *testing.T) {
	h, _ := newTestHandlerWithEnv(t)

	w := httptest.NewRecorder()
	h.GetConfig(w, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Usable  map[string]bool   `json:"sandbox_usable"`
		Reasons map[string]string `json:"sandbox_reasons"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if usable, present := resp.Usable["topos"]; !present || usable {
		t.Errorf("sandbox_usable.topos = %v (present %v), want false", usable, present)
	}
	if got := resp.Reasons["topos"]; got != harness.ToposCredentialRequired {
		t.Errorf("sandbox_reasons.topos = %q, want %q", got, harness.ToposCredentialRequired)
	}
}
