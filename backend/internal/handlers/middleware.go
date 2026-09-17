package handlers

import (
	"log"
	"net/http"
	"time"
)

// withCORS allows the configured frontend origin to call the API. In local
// development allowedOrigin is typically http://localhost:5173 (Vite's
// default port); in production it's the deployed frontend's HTTPS URL.
// A literal "*" is supported for quick demos but should not be used
// alongside credentials.
func withCORS(next http.Handler, allowedOrigin string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if allowedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// withLogging logs one line per request. Deliberately excludes playback
// polling noise by not logging bodies or per-tick detail (assignment
// section 41: don't log every playback tick) - GET /api/state is polled
// every few seconds by every open tab, so even this one-line-per-request
// log is the right granularity, not per-item playback state.
func withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	r.status = status
	r.ResponseWriter.WriteHeader(status)
}
