// Package httpjson provides helpers for decoding JSON request bodies and
// writing JSON responses in HTTP handlers.
package httpjson

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

// DecodeBody decodes the JSON request body into a new T. It rejects unknown
// fields and trailing tokens after the first JSON object, writing a 400
// response on any error. Returns (*T, true) on success or (nil, false) on error.
func DecodeBody[T any](w http.ResponseWriter, r *http.Request) (*T, bool) {
	v := new(T)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			Write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return nil, false
		}
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return nil, false
	}
	if dec.More() {
		http.Error(w, "invalid JSON: unexpected trailing content", http.StatusBadRequest)
		return nil, false
	}
	return v, true
}

// DecodeOptionalBody decodes the JSON request body into a new T when a body is
// present. An absent or empty body is silently accepted and returns a zero-value
// T pointer. When a body is present the same strict rules apply as
// DecodeBody: unknown fields and trailing tokens are rejected with a 400.
func DecodeOptionalBody[T any](w http.ResponseWriter, r *http.Request) (*T, bool) {
	if r == nil || r.Body == nil {
		return new(T), true
	}
	v := new(T)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return new(T), true // empty body — treat as no body provided
		}
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			Write(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "request body too large"})
			return nil, false
		}
		http.Error(w, "invalid JSON: "+err.Error(), http.StatusBadRequest)
		return nil, false
	}
	if dec.More() {
		http.Error(w, "invalid JSON: unexpected trailing content", http.StatusBadRequest)
		return nil, false
	}
	return v, true
}

// Write serialises v as JSON and writes it with the given HTTP status code.
func Write[T any](w http.ResponseWriter, status int, v T) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("write json", "error", err)
	}
}

// PathUUID parses a UUID from the named path segment, writing a 400
// response on failure. Returns (parsed, true) on success or
// (uuid.Nil, false) on missing/malformed input. name is the path
// parameter name (e.g. "id" for routes like "/api/tasks/{id}/...").
func PathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	raw := r.PathValue(name)
	if raw == "" {
		http.Error(w, "missing "+name, http.StatusBadRequest)
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		http.Error(w, "invalid "+name+": "+err.Error(), http.StatusBadRequest)
		return uuid.Nil, false
	}
	return id, true
}
