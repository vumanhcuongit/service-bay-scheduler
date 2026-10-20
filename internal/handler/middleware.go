package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/vumanhcuongit/service-bay-scheduler/internal/model"
)

// requestIDPattern bounds a caller's X-Request-ID, which goes into every log line of the request.
var requestIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// requestID is the outermost middleware: it takes X-Request-ID, or generates one when it is
// absent or not 1–128 characters of letters, digits, '.', '_' and '-'. It echoes the id on the
// response and puts a *requestInfo carrying it in the context. It knows nothing about routes
// or bookings.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !requestIDPattern.MatchString(id) {
			id = newRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		info := &requestInfo{ID: id, Method: r.Method, Path: r.URL.Path}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestInfoKey{}, info)))
	})
}

// accessLog logs each request on the way in and on the way out, with its status and duration.
// Health checks and metric scrapes are polled constantly, so they are not logged.
func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		log.InfoContext(r.Context(), "request in")
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.InfoContext(r.Context(), "request out", "status", sw.status, "duration_ms", time.Since(start).Milliseconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

type bookingMetrics struct {
	attempts *prometheus.CounterVec
	latency  prometheus.Histogram
}

func newBookingMetrics(reg prometheus.Registerer) bookingMetrics {
	m := bookingMetrics{
		attempts: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "booking_attempts_total",
			Help: "POST /appointments requests by result.",
		}, []string{"result"}),
		latency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "booking_latency_seconds",
			Help:    "POST /appointments latency.",
			Buckets: prometheus.DefBuckets,
		}),
	}
	reg.MustRegister(m.attempts, m.latency)
	for _, r := range model.Results {
		m.attempts.WithLabelValues(string(r)) // export every result from the start, at 0
	}
	return m
}

// metrics records POST /appointments only: its result, which the handler writes to the
// *requestInfo, and its latency. A request that never set a result counts as an error.
func (m bookingMetrics) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/appointments" {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		next.ServeHTTP(w, r)
		result := model.ResultError
		if info := infoFrom(r.Context()); info != nil && info.Result != "" {
			result = info.Result
		}
		m.attempts.WithLabelValues(string(result)).Inc()
		m.latency.Observe(time.Since(start).Seconds())
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
