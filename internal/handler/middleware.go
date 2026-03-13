package handler

import "net/http"

const (
	BodyLimitDefault      int64 = 1 << 20   // 1 MiB
	BodyLimitInstructions int64 = 5 << 20   // 5 MiB
	BodyLimitFeedback     int64 = 512 << 10 // 512 KiB
)

// MaxBytesMiddleware limits the size of the request body for downstream handlers.
func MaxBytesMiddleware(limit int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}
