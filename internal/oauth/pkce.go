// Package oauth provides OAuth 2.0 PKCE utilities and token management.
package oauth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
)

// randReader is the source of randomness. Tests may override it.
var randReader io.Reader = rand.Reader

// GenerateCodeVerifier returns a cryptographically random PKCE code verifier
// (32 random bytes, base64url-encoded without padding, yielding 43 characters).
func GenerateCodeVerifier() (string, error) {
	b := make([]byte, 32)
	if _, err := io.ReadFull(randReader, b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// S256Challenge computes the S256 PKCE challenge for a given code verifier:
// SHA-256 hash of the verifier string, base64url-encoded without padding.
func S256Challenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(h[:])
}

// GenerateState returns a cryptographically random state parameter
// (16 random bytes, hex-encoded, yielding 32 characters).
func GenerateState() (string, error) {
	b := make([]byte, 16)
	if _, err := io.ReadFull(randReader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
