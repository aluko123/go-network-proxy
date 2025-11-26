package middleware

import (
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aluko123/go-network-proxy/pkg/blocklist"
	"github.com/aluko123/go-network-proxy/pkg/limit"
	"github.com/aluko123/go-network-proxy/pkg/metrics"
)

// Middleware type definition
type Middleware func(http.Handler) http.Handler

// Chain applies middlewares in the order they are passed
func Chain(h http.Handler, middlewares ...Middleware) http.Handler {
	for _, m := range middlewares {
		h = m(h)
	}
	return h
}

// WithRateLimit returns a middleware that enforces rate limits
func WithRateLimit(limiter limit.RateLimiter) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := limit.GetIP(r)
			if !limiter.Allow(ip) {
				http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WithBlocklist returns a middleware that blocks requests to forbidden domains
func WithBlocklist(bm *blocklist.Manager) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := r.Host
			if host == "" {
				host = r.URL.Host
			}
			// Remove port if present
			if colonIdx := strings.Index(host, ":"); colonIdx != -1 {
				host = host[:colonIdx]
			}

			if bm.IsBlocked(host) {
				metrics.BlockedRequests.Inc()
				
				if r.Method == http.MethodConnect {
					http.Error(w, "Forbidden", http.StatusForbidden)
				} else {
					w.Header().Set("Content-Type", "text/html")
					w.WriteHeader(http.StatusForbidden)
					w.Write([]byte(blocklist.GetBlockedResponse()))
				}
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// WithLogging returns a middleware that logs request details
func WithLogging(debug bool) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Metrics: Active Connections
			metrics.ActiveConnections.Inc()
			defer metrics.ActiveConnections.Dec()

			start := time.Now()

			if debug {
				log.Printf("[%s] %s %s", r.Method, r.Host, r.URL.String())
			} else {
				log.Printf("[%s] %s", r.Method, r.Host)
			}

			// Use our custom wrapper to capture status code
			recorder := &statusRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			
			next.ServeHTTP(recorder, r)

			// Metrics: Duration and Status
			duration := time.Since(start).Seconds()
			metrics.RequestDuration.WithLabelValues(r.Method).Observe(duration)
			// statusClass := fmt.Sprintf("%dxx", recorder.statusCode/100)
			// metrics.StatusCodeCounter.WithLabelValues(statusClass).Inc()
			// metrics.RequestsTotal.WithLabelValues(r.Method, http.StatusText(recorder.statusCode)).Inc()
		})
	}
}

// statusRecorder is a wrapper around http.ResponseWriter to capture the status code
type statusRecorder struct {
	http.ResponseWriter
	statusCode  int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if r.wroteHeader {
		return
	}
	r.statusCode = code
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(code)
}

// Flush implements the http.Flusher interface
func (r *statusRecorder) Flush() {
	if flusher, ok := r.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
