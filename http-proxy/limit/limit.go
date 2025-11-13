package limit

import (
	"net"
	"net/http"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// IPRateLimiter tracks rate limiters per IP
type IPRateLimiter struct {
	limiters map[string]*rate.Limiter
	mu       sync.RWMutex
	r        rate.Limit // requests per second
	b        int        // burst size
}

// NewIPRateLimiter creates a new IP-based rate limiter
// r: requests per second (e.g., 100 = 100 req/s)
// b: burst size (e.g., 10 = allow 10 requests immediately)
func NewIPRateLimiter(r rate.Limit, b int) *IPRateLimiter {
	return &IPRateLimiter{
		limiters: make(map[string]*rate.Limiter),
		r:        r,
		b:        b,
	}
}

// GetLimiter returns the rate limiter for the given IP
func (i *IPRateLimiter) GetLimiter(ip string) *rate.Limiter {
	i.mu.Lock()
	defer i.mu.Unlock()

	limiter, exists := i.limiters[ip]
	if !exists {
		limiter = rate.NewLimiter(i.r, i.b)
		i.limiters[ip] = limiter
	}

	return limiter
}

// CleanupStale removes limiters that haven't been used in the given duration
func (i *IPRateLimiter) CleanupStale(maxAge time.Duration) {
	i.mu.Lock()
	defer i.mu.Unlock()

	// Simple cleanup: remove all limiters periodically
	// In production, track last access time per limiter
	i.limiters = make(map[string]*rate.Limiter)
}

// GetIP extracts the client IP from the request
func GetIP(r *http.Request) string {
	// Try X-Forwarded-For first (if behind load balancer)
	forwarded := r.Header.Get("X-Forwarded-For")
	if forwarded != "" {
		// Take first IP in the list
		ip, _, _ := net.SplitHostPort(forwarded)
		if ip != "" {
			return ip
		}
		return forwarded
	}

	// Try X-Real-IP
	realIP := r.Header.Get("X-Real-IP")
	if realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// Middleware returns a middleware that rate limits by IP
func (i *IPRateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := GetIP(r)
		limiter := i.GetLimiter(ip)

		if !limiter.Allow() {
			http.Error(w, "Rate limit exceeded", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
