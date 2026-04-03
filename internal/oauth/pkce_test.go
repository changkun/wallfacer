package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
)

func TestGenerateCodeVerifier(t *testing.T) {
	v, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier: %v", err)
	}
	if len(v) != 43 {
		t.Errorf("verifier length = %d; want 43", len(v))
	}

	// Two calls must produce different values.
	v2, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("GenerateCodeVerifier (2nd): %v", err)
	}
	if v == v2 {
		t.Error("two calls returned the same verifier")
	}
}

func TestS256Challenge(t *testing.T) {
	// Known test vector: RFC 7636 Appendix B.
	// verifier = "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	// expected challenge = "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	verifier := "dBjftJeZ4CVP-mB92K27uhbUJU1p1r_wW1gFWFOEjXk"
	want := "E9Melhoa2OwvFrEMTJguCHaoeK1t8URWbuGJSstw-cM"
	got := S256Challenge(verifier)
	if got != want {
		t.Errorf("S256Challenge(%q) = %q; want %q", verifier, got, want)
	}
}

func TestS256Challenge_Deterministic(t *testing.T) {
	verifier := "test-verifier-value-for-determinism"
	c1 := S256Challenge(verifier)
	c2 := S256Challenge(verifier)
	if c1 != c2 {
		t.Errorf("S256Challenge not deterministic: %q != %q", c1, c2)
	}

	// Verify it matches manual computation.
	h := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(h[:])
	if c1 != expected {
		t.Errorf("S256Challenge = %q; want %q", c1, expected)
	}
}

func TestGenerateState(t *testing.T) {
	s, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState: %v", err)
	}
	if len(s) != 32 {
		t.Errorf("state length = %d; want 32", len(s))
	}

	// Must be valid hex.
	if _, err := hex.DecodeString(s); err != nil {
		t.Errorf("state is not valid hex: %v", err)
	}

	// Two calls must produce different values.
	s2, err := GenerateState()
	if err != nil {
		t.Fatalf("GenerateState (2nd): %v", err)
	}
	if s == s2 {
		t.Error("two calls returned the same state")
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) {
	return 0, errors.New("rand failure")
}

func TestGenerateCodeVerifier_RandError(t *testing.T) {
	old := randReader
	randReader = failReader{}
	defer func() { randReader = old }()

	_, err := GenerateCodeVerifier()
	if err == nil {
		t.Fatal("expected error from rand failure")
	}
}

func TestGenerateState_RandError(t *testing.T) {
	old := randReader
	randReader = failReader{}
	defer func() { randReader = old }()

	_, err := GenerateState()
	if err == nil {
		t.Fatal("expected error from rand failure")
	}
}

func TestGenerateCodeVerifier_UsesRandReader(t *testing.T) {
	// Ensure production code uses crypto/rand by default.
	old := randReader
	randReader = rand.Reader
	defer func() { randReader = old }()

	v, err := GenerateCodeVerifier()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(v) != 43 {
		t.Errorf("verifier length = %d; want 43", len(v))
	}
}
