# Task 1: PKCE Utilities

**Status:** Todo
**Depends on:** None
**Phase:** Core OAuth infrastructure
**Effort:** Small

## Goal

Implement the cryptographic primitives for OAuth 2.0 PKCE (Proof Key for Code Exchange): code verifier generation, S256 challenge derivation, and random state parameter generation. These are pure functions with no I/O, forming the foundation for all OAuth flows.

## What to do

1. Create `internal/oauth/pkce.go` with:
   - `GenerateCodeVerifier() (string, error)` — 32 random bytes, base64url-encoded (43 chars). Use `crypto/rand`.
   - `S256Challenge(verifier string) string` — SHA-256 hash of verifier, base64url-encoded (no padding).
   - `GenerateState() (string, error)` — 16 random bytes, hex-encoded (32 chars).

2. All functions return deterministic output for a given input (except the random generation). Keep them stateless and side-effect-free.

## Tests

- `TestGenerateCodeVerifier` — returns a non-empty string of expected length; two calls produce different values.
- `TestS256Challenge` — verify against a known test vector (RFC 7636 Appendix B or a hand-computed example).
- `TestS256Challenge_Deterministic` — same input always produces same output.
- `TestGenerateState` — returns a 32-char hex string; two calls produce different values.

## Boundaries

- Do NOT implement the callback server, token exchange, or HTTP handlers in this task.
- Do NOT add any provider-specific logic (Claude/Codex URLs).
- Do NOT modify any existing files.
