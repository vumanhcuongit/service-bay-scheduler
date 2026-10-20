package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"time"
)

type requestIDKey struct{}

// requestID is the outermost middleware: it takes X-Request-ID or generates one, echoes it on
// the response and puts it in the context. It knows nothing about routes or bookings.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if id == "" {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// requestTimeout puts a deadline on the request context. pgx cancels a query, or a wait on a
// lock, when it passes. This replaces a session-level statement_timeout, which a
// transaction-mode pooler would reject or pin (design §8).
func requestTimeout(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
